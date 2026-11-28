// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tariffs"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"
)

// transactionIdAttempts is the number of transaction ids that are tried for a
// StartTransaction.req before giving up, when the charge station already has a
// transaction with the allocated id.
const transactionIdAttempts = 3

// idTagTokenType is the token type that the transactions of OCPP 1.6 idTags are stored
// with.
const idTagTokenType = "ISO14443"

// refusedTransactionId is the transaction id of a StartTransaction.conf for which no
// transaction was recorded.
const refusedTransactionId = -1

// StartTransactionHandler records the transaction that a charge station has started.
//
// The transaction is recorded whatever the authorisation status of the idTag: OCPP 1.6
// requires a transaction id in every StartTransaction.conf, and the station sends a
// StopTransaction.req for it (or keeps charging when StopTransactionOnInvalidId is false).
// The exception is a charge station that may not charge (pending or decommissioned):
// nothing is recorded, and it gets Invalid with the transaction id -1.
//
// The transaction, its details, the reservation it uses, its tariff and the
// session.started event are stored in one transaction. The event belongs to the org of
// the stored transaction, like the later events of the session. Once it is stored, a
// transaction whose idTag is accepted is given to the CommandTracker (when there is one),
// which links it to the remote start that asked for it; nothing the tracker does changes
// the answer.
//
// The tariff of the transaction is chosen once, here, whatever the status of the idTag (a
// station may charge on with an idTag that was refused): the version that the
// TariffSelector selects is stored with the transaction and stays, also when the tariff
// gets a new version during the session. It is selected for the org of the transaction.
// Charging never waits for a tariff and is never refused for want of one: when the
// selection fails or takes too long, that is logged and the transaction starts without a
// tariff.
type StartTransactionHandler struct {
	Clock      clock.PassiveClock
	TokenStore store.TokenStore
	// RegistryStore holds the state and allow_any_token of the charge station: an unknown
	// idTag is accepted when allow_any_token is set. When RegistryStore is nil, every
	// charge station may charge and no unknown idTag is accepted.
	RegistryStore    store.ChargeStationRegistryStore
	OrgResolver      tenancy.StationOrgResolver
	TransactionStore store.TransactionStore
	ReservationStore store.ReservationStore
	Tx               store.Transactor
	Events           events.Emitter
	CommandTracker   CommandTracker // nil: commands are not followed
	// TariffSelector selects the tariff of the transaction. Nil: transactions get no tariff.
	TariffSelector *tariffs.Selector
}

func (t StartTransactionHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
	req := request.(*types.StartTransactionJson)

	slog.Info("starting transaction", slog.Any("request", req))

	allowed, err := chargingAllowed(ctx, t.RegistryStore, chargeStationId)
	if err != nil {
		return nil, err
	}
	if !allowed {
		slog.Warn("refusing a transaction of a charge station that may not charge",
			slog.String("chargeStationId", chargeStationId), slog.Int("connectorId", req.ConnectorId))
		return &types.StartTransactionResponseJson{
			IdTagInfo:     types.StartTransactionResponseJsonIdTagInfo{Status: types.StartTransactionResponseJsonIdTagInfoStatusInvalid},
			TransactionId: refusedTransactionId,
		}, nil
	}

	auth, err := authorizeIdTag(ctx, t.TokenStore, t.RegistryStore, t.OrgResolver, t.Clock, chargeStationId, req.IdTag)
	if err != nil {
		return nil, err
	}

	startedAt, err := time.Parse(time.RFC3339, req.Timestamp)
	if err != nil {
		startedAt = t.Clock.Now()
		slog.Warn("invalid start transaction timestamp, using the current time",
			slog.String("chargeStationId", chargeStationId), slog.String("timestamp", req.Timestamp), "err", err)
	}

	// the tariff is selected before the transaction is stored: the selection reads from
	// several stores, and none of that holds up or breaks the storing
	selection := t.selectTariff(ctx, chargeStationId, req, auth.OrgId)

	var transactionId int
	var transactionUuid string
	err = t.Tx.RunInTx(ctx, func(ctx context.Context) error {
		var err error
		transactionId, transactionUuid, err = t.createTransaction(ctx, chargeStationId, req, startedAt)
		if err != nil {
			return err
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
			return err
		}

		if req.ReservationId != nil {
			t.consumeReservation(ctx, chargeStationId, req, auth.Token)
		}

		// the event belongs to the org that the store gave the transaction: read here,
		// in the transaction that created it, so that the two cannot differ when the
		// charge station moves to another org at the same time
		stored, err := t.TransactionStore.FindTransaction(ctx, chargeStationId, transactionUuid)
		if err != nil {
			return err
		}

		tariff := t.pinTariff(ctx, selection, stored, chargeStationId, transactionUuid)

		return t.Events.Emit(ctx, sessionStartedEvent(chargeStationId, req, transactionId, transactionUuid, details, auth.Status, sessionOrgId(stored), tariff))
	})
	if err != nil {
		return nil, err
	}
	t.pinTariffOfOtherOrg(ctx, selection, chargeStationId, transactionUuid, req)

	if auth.Status == string(types.StartTransactionResponseJsonIdTagInfoStatusAccepted) {
		trackCommand(ctx, t.CommandTracker, chargeStationId, "StartTransaction", func(tracker CommandTracker) {
			tracker.OnTransactionStarted(ctx, chargeStationId, req.ConnectorId, req.IdTag, transactionUuid, transactionId)
		})
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
		err = t.TransactionStore.CreateTransaction(ctx, chargeStationId, transactionUuid, req.IdTag, idTagTokenType,
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

// tariffSelection is the tariff that was selected for a transaction before the
// transaction was stored.
type tariffSelection struct {
	// orgId is the org that the tariff was selected for: the org that owned the charge
	// station when the idTag was authorised
	orgId  string
	choice tariffs.Choice
	// err says why there is no choice
	err error
	// otherOrgId is set when the stored transaction turned out to belong to another org
	// than orgId (the charge station moved in between): its tariff is selected again
	otherOrgId string
}

// selectTariff selects the tariff of the transaction that the station has started, for
// the org that owns the charge station. It returns nil when there is no selector.
func (t StartTransactionHandler) selectTariff(ctx context.Context, chargeStationId string, req *types.StartTransactionJson, orgId string) *tariffSelection {
	if t.TariffSelector == nil {
		return nil
	}
	selection := &tariffSelection{orgId: orgId}
	selection.choice, selection.err = t.TariffSelector.SelectForStart(ctx, t.tariffInput(chargeStationId, req, orgId))
	return selection
}

func (t StartTransactionHandler) tariffInput(chargeStationId string, req *types.StartTransactionJson, orgId string) tariffs.SelectInput {
	return tariffs.SelectInput{
		OrgId:       orgId,
		StationKey:  chargeStationId,
		OcppVersion: string(transport.OcppVersion16),
		// OCPP 1.6 has no EVSEs: the connector id is used for both
		EvseId:      req.ConnectorId,
		ConnectorId: req.ConnectorId,
		TokenUid:    req.IdTag,
		At:          t.Clock.Now(),
	}
}

// pinTariff stores the selected tariff with the transaction that was just stored, in the
// database transaction that stored it. It returns the tariff that the transaction has
// now, nil when it has none. Nothing here fails the start of the transaction.
//
// The tariff was selected for the org that owned the charge station a moment ago. The
// stored transaction has the org that owns it now: when the charge station moved to
// another org in between, the selected tariff is not a tariff of the transaction, and
// another one is selected after the transaction is stored (pinTariffOfOtherOrg).
func (t StartTransactionHandler) pinTariff(ctx context.Context, selection *tariffSelection, stored *store.Transaction, chargeStationId, transactionUuid string) *tariffs.Choice {
	if selection == nil {
		return nil
	}
	logAttrs := []any{slog.String("chargeStationId", chargeStationId), slog.String("transactionId", transactionUuid)}
	// a transaction of the legacy org, or without an org, belongs to the org of its
	// charge station
	if orgId := sessionOrgId(stored); orgId != "" && !strings.EqualFold(orgId, selection.orgId) {
		selection.otherOrgId = orgId
		return nil
	}
	if selection.err != nil {
		slog.Error("failed to select the tariff of a transaction, it starts without one", append(logAttrs, "err", selection.err)...)
		trace.SpanFromContext(ctx).RecordError(selection.err)
		return nil
	}
	pinned, err := tariffs.Pin(ctx, t.TransactionStore, chargeStationId, transactionUuid, selection.choice)
	if err != nil {
		slog.Error("failed to store the tariff of a transaction, it starts without one", append(logAttrs, "err", err)...)
		trace.SpanFromContext(ctx).RecordError(err)
		return nil
	}
	if !pinned {
		return nil
	}
	return &selection.choice
}

// pinTariffOfOtherOrg selects and stores the tariff of a transaction that belongs to
// another org than the one that its tariff was selected for first, after the transaction
// is stored. Nothing here fails the start of the transaction.
func (t StartTransactionHandler) pinTariffOfOtherOrg(ctx context.Context, selection *tariffSelection, chargeStationId, transactionUuid string, req *types.StartTransactionJson) {
	if selection == nil || selection.otherOrgId == "" {
		return
	}
	choice, err := t.TariffSelector.SelectForStart(ctx, t.tariffInput(chargeStationId, req, selection.otherOrgId))
	if err == nil {
		_, err = tariffs.Pin(ctx, t.TransactionStore, chargeStationId, transactionUuid, choice)
	}
	if err != nil {
		slog.Error("failed to select the tariff of a transaction, it starts without one",
			slog.String("chargeStationId", chargeStationId), slog.String("transactionId", transactionUuid), "err", err)
		trace.SpanFromContext(ctx).RecordError(err)
	}
}

// sessionStartedEvent is the session.started event of a transaction with the details
// that were stored for it, and the authorisation status that the station gets. orgId is
// the org of the stored transaction (sessionOrgId): "" leaves it to the emitter, which
// takes the org of the charge station. tariff is the tariff that was stored with the
// transaction, nil when it has none.
func sessionStartedEvent(chargeStationId string, req *types.StartTransactionJson, transactionId int, transactionUuid string,
	details *store.TransactionDetails, authStatus, orgId string, tariff *tariffs.Choice) events.Event {
	var tariffId, tariffSource *string
	var tariffVersion *int
	if tariff != nil {
		source := string(tariff.Source)
		tariffSource = &source
		if tariff.TariffId != "" {
			id, version := tariff.TariffId, tariff.Version
			tariffId, tariffVersion = &id, &version
		}
	}
	return events.Event{
		Type:         events.TypeSessionStarted,
		OrgId:        orgId,
		StationKey:   chargeStationId,
		ResourceType: events.ResourceSession,
		ResourceId:   transactionUuid,
		Actor:        events.StationActor(chargeStationId),
		Data: events.SessionStartedData{
			StationKey:        chargeStationId,
			StationId:         events.StationId(chargeStationId),
			TransactionId:     transactionUuid,
			OcppTransactionId: strconv.Itoa(transactionId),
			OcppVersion:       string(transport.OcppVersion16),
			EvseId:            details.EvseId,
			ConnectorId:       details.ConnectorId,
			IdToken:           req.IdTag,
			TokenType:         idTagTokenType,
			IdTokenGroup:      details.IdTokenGroup,
			AuthStatus:        authStatus,
			StartedAt:         details.StartedAt.UTC(),
			MeterStartWh:      details.MeterStartWh,
			ReservationId:     details.ReservationId,
			TariffId:          tariffId,
			TariffVersion:     tariffVersion,
			TariffSource:      tariffSource,
		},
	}
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

// ConvertToUUID returns the stored id of the transaction that an OCPP 1.6 charge station
// numbered transactionId (see ConvertToUUID of the OCPP 1.6 types).
func ConvertToUUID(transactionId int) string {
	return types.ConvertToUUID(transactionId)
}
