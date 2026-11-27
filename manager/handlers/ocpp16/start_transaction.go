// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"
)

// transactionIdAttempts is the number of transaction ids that are tried for a
// StartTransaction.req before giving up, when the charge station already has a
// transaction with the allocated id.
const transactionIdAttempts = 3

// StartTransactionHandler records the transaction that a charge station has started.
//
// The transaction is recorded whatever the authorisation status of the idTag: OCPP 1.6
// requires a transaction id in every StartTransaction.conf, and the station sends a
// StopTransaction.req for it (or keeps charging when StopTransactionOnInvalidId is false).
type StartTransactionHandler struct {
	Clock            clock.PassiveClock
	TokenStore       store.TokenStore
	OrgResolver      tenancy.StationOrgResolver
	TransactionStore store.TransactionStore
	ReservationStore store.ReservationStore
}

func (t StartTransactionHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
	req := request.(*types.StartTransactionJson)

	slog.Info("starting transaction", slog.Any("request", req))

	auth, err := authorizeIdTag(ctx, t.TokenStore, t.OrgResolver, t.Clock, chargeStationId, req.IdTag)
	if err != nil {
		return nil, err
	}

	startedAt, err := time.Parse(time.RFC3339, req.Timestamp)
	if err != nil {
		startedAt = t.Clock.Now()
		slog.Warn("invalid start transaction timestamp, using the current time",
			slog.String("chargeStationId", chargeStationId), slog.String("timestamp", req.Timestamp), "err", err)
	}

	transactionId, transactionUuid, err := t.createTransaction(ctx, chargeStationId, req, startedAt)
	if err != nil {
		return nil, err
	}

	connectorId := req.ConnectorId
	meterStart := int64(req.MeterStart)
	details := &store.TransactionDetails{
		// OCPP 1.6 has no EVSEs: the connector id is used for both
		ConnectorId:   &connectorId,
		EvseId:        &connectorId,
		StartedAt:     &startedAt,
		MeterStartWh:  &meterStart,
		ReservationId: req.ReservationId,
	}
	if auth.Token != nil {
		details.IdTokenGroup = auth.Token.GroupId
	}
	err = t.TransactionStore.UpdateTransactionDetails(ctx, chargeStationId, transactionUuid, details)
	if errors.Is(err, store.ErrNotImplemented) {
		slog.Debug("transaction details are not stored by this store",
			slog.String("chargeStationId", chargeStationId), slog.String("transactionId", transactionUuid))
	} else if err != nil {
		return nil, err
	}

	if req.ReservationId != nil {
		t.consumeReservation(ctx, chargeStationId, req, auth.Token)
	}

	return &types.StartTransactionResponseJson{
		IdTagInfo: types.StartTransactionResponseJsonIdTagInfo{
			Status:      types.StartTransactionResponseJsonIdTagInfoStatus(auth.Status),
			ExpiryDate:  auth.ExpiryDate,
			ParentIdTag: auth.ParentIdTag,
		},
		TransactionId: transactionId,
	}, nil
}

// createTransaction allocates a transaction id and creates the transaction with the
// meter reading at its start. It returns the OCPP 1.6 transaction id and the id that the
// transaction is stored with.
func (t StartTransactionHandler) createTransaction(ctx context.Context, chargeStationId string, req *types.StartTransactionJson, startedAt time.Time) (int, string, error) {
	beginSample := energyRegisterMeterValue(startedAt, sampledValueContextTransactionBegin, req.MeterStart)

	var err error
	for attempt := 1; attempt <= transactionIdAttempts; attempt++ {
		var transactionId int
		transactionId, err = t.TransactionStore.NextOcpp16TransactionId(ctx)
		if errors.Is(err, store.ErrNotImplemented) {
			//#nosec G404 - transaction id does not require secure random number generator
			transactionId = int(rand.Int31())
		} else if err != nil {
			return 0, "", err
		}

		transactionUuid := ConvertToUUID(transactionId)
		err = t.TransactionStore.CreateTransaction(ctx, chargeStationId, transactionUuid, req.IdTag, "ISO14443",
			[]store.MeterValue{beginSample}, 0, false)
		if err == nil {
			return transactionId, transactionUuid, nil
		}
		if !errors.Is(err, store.ErrAlreadyExists) {
			return 0, "", err
		}
		slog.Warn("transaction id is already in use, allocating another one",
			slog.String("chargeStationId", chargeStationId), slog.String("transactionId", transactionUuid),
			slog.Int("attempt", attempt))
	}

	return 0, "", fmt.Errorf("allocating a transaction id for %s: %w", chargeStationId, err)
}

// consumeReservation marks the reservation that the station says the transaction
// terminates as used. The station enforces reservations, the CSMS only records them:
// nothing here fails the start of the transaction.
func (t StartTransactionHandler) consumeReservation(ctx context.Context, chargeStationId string, req *types.StartTransactionJson, tok *store.Token) {
	span := trace.SpanFromContext(ctx)
	reservationId := *req.ReservationId
	logAttrs := []any{
		slog.String("chargeStationId", chargeStationId),
		slog.Int("reservationId", reservationId),
		slog.Int("connectorId", req.ConnectorId),
		slog.String("idTag", req.IdTag),
	}

	reservation, err := t.ReservationStore.GetReservation(ctx, reservationId)
	if err != nil {
		slog.Error("failed to look up the reservation of a started transaction", append(logAttrs, "err", err)...)
		span.RecordError(err)
		return
	}
	if reservation == nil || reservation.ChargeStationId != chargeStationId {
		slog.Warn("transaction started for an unknown reservation", logAttrs...)
		return
	}
	if reservation.Status != store.ReservationStatusAccepted {
		slog.Warn("transaction started for a reservation that is not active",
			append(logAttrs, slog.String("status", string(reservation.Status)))...)
		return
	}

	// the reservation is for the idTag itself or for its group (parentIdTag)
	idTagMatches := reservation.IdTag == req.IdTag ||
		(reservation.ParentIdTag != nil && tok != nil && tok.GroupId != nil && *reservation.ParentIdTag == *tok.GroupId)
	// connector 0 reserves any connector of the charge station
	connectorMatches := reservation.ConnectorId == 0 || reservation.ConnectorId == req.ConnectorId
	if !idTagMatches || !connectorMatches {
		slog.Warn("transaction started for a reservation of another idTag or connector", logAttrs...)
		return
	}

	err = t.ReservationStore.UpdateReservationStatus(ctx, reservation.ReservationId, store.ReservationStatusUsed)
	if err != nil {
		slog.Error("failed to mark the reservation of a started transaction as used", append(logAttrs, "err", err)...)
		span.RecordError(err)
	}
}

func ConvertToUUID(transactionId int) string {
	uuidBytes := []byte{
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		(byte)(transactionId >> 24),
		(byte)(transactionId >> 16),
		(byte)(transactionId >> 8),
		(byte)(transactionId),
	}
	return uuid.Must(uuid.FromBytes(uuidBytes)).String()
}
