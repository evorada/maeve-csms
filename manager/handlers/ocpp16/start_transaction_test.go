// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	clockTest "k8s.io/utils/clock/testing"
)

const (
	startTransactionChargeStationId = "cs001"
	startTransactionIdTag           = "MYRFIDTAG"
	startTransactionGroupId         = "MYGROUP"

	// the time of the handler's clock: no stored time may be equal to it,
	// unless the request has no usable timestamp
	startTransactionClockTime = "2023-06-15T15:05:00+01:00"
	// the time that the station reports in the request
	startTransactionStationTime = "2023-06-15T14:58:30Z"
)

// newStartTransactionHandler returns a handler that stores in a new in-memory store.
// The store has one token, startTransactionIdTag, in the group startTransactionGroupId.
func newStartTransactionHandler(t *testing.T) (handlers.StartTransactionHandler, *inmemory.Store) {
	t.Helper()

	now, err := time.Parse(time.RFC3339, startTransactionClockTime)
	require.NoError(t, err)
	clk := clockTest.NewFakePassiveClock(now)
	engine := inmemory.NewStore(clk)

	groupId := startTransactionGroupId
	err = engine.SetToken(context.Background(), &store.Token{
		CountryCode: "GB",
		PartyId:     "TWK",
		Type:        "RFID",
		Uid:         startTransactionIdTag,
		ContractId:  "GBTWK012345678V",
		Issuer:      "Thoughtworks",
		GroupId:     &groupId,
		Valid:       true,
		CacheMode:   "NEVER",
	})
	require.NoError(t, err)

	return handlers.StartTransactionHandler{
		Clock:            clk,
		TokenStore:       engine,
		TransactionStore: engine,
		ReservationStore: engine,
	}, engine
}

func startTransactionRequest(idTag string) *types.StartTransactionJson {
	return &types.StartTransactionJson{
		ConnectorId: 2,
		IdTag:       idTag,
		MeterStart:  100,
		Timestamp:   startTransactionStationTime,
	}
}

// startTransaction calls the handler and returns its response.
func startTransaction(t *testing.T, handler handlers.StartTransactionHandler, chargeStationId string, req *types.StartTransactionJson) *types.StartTransactionResponseJson {
	t.Helper()

	resp, err := handler.HandleCall(context.Background(), chargeStationId, req)
	require.NoError(t, err)
	got, ok := resp.(*types.StartTransactionResponseJson)
	require.True(t, ok, "unexpected response type %T", resp)
	return got
}

// beginMeterValue is the meter value that the handler stores for the meter start.
func beginMeterValue(timestamp string, wh float64) store.MeterValue {
	sampleContext := "Transaction.Begin"
	location := "Outlet"
	measurand := "Energy.Active.Import.Register"
	return store.MeterValue{
		Timestamp: timestamp,
		SampledValues: []store.SampledValue{
			{
				Context:       &sampleContext,
				Location:      &location,
				Measurand:     &measurand,
				UnitOfMeasure: &store.UnitOfMeasure{Unit: "Wh", Multipler: 0},
				Value:         wh,
			},
		},
	}
}

// faultyTransactionStore wraps a transaction store and fails the calls it is told to fail.
type faultyTransactionStore struct {
	store.TransactionStore
	// createErrs are returned by the successive calls of CreateTransaction;
	// a nil error, or a call after the last one, reaches the wrapped store
	createErrs []error
	// createdIds are the transaction ids of all calls of CreateTransaction
	createdIds       []string
	nextIdErr        error
	updateDetailsErr error
}

func (s *faultyTransactionStore) CreateTransaction(ctx context.Context, chargeStationId, transactionId, idToken, tokenType string, meterValues []store.MeterValue, seqNo int, offline bool) error {
	call := len(s.createdIds)
	s.createdIds = append(s.createdIds, transactionId)
	if call < len(s.createErrs) && s.createErrs[call] != nil {
		return s.createErrs[call]
	}
	return s.TransactionStore.CreateTransaction(ctx, chargeStationId, transactionId, idToken, tokenType, meterValues, seqNo, offline)
}

func (s *faultyTransactionStore) NextOcpp16TransactionId(ctx context.Context) (int, error) {
	if s.nextIdErr != nil {
		return 0, s.nextIdErr
	}
	return s.TransactionStore.NextOcpp16TransactionId(ctx)
}

func (s *faultyTransactionStore) UpdateTransactionDetails(ctx context.Context, chargeStationId, transactionId string, details *store.TransactionDetails) error {
	if s.updateDetailsErr != nil {
		return s.updateDetailsErr
	}
	return s.TransactionStore.UpdateTransactionDetails(ctx, chargeStationId, transactionId, details)
}

// alreadyExists is the error of a store for a transaction id that is in use.
func alreadyExists(transactionId int) error {
	return fmt.Errorf("transaction cs001/%s: %w", handlers.ConvertToUUID(transactionId), store.ErrAlreadyExists)
}

func TestStartTransaction(t *testing.T) {
	handler, engine := newStartTransactionHandler(t)

	got := startTransaction(t, handler, startTransactionChargeStationId, startTransactionRequest(startTransactionIdTag))

	// the token must not be cached: it expires at the time of the handler's clock
	expiryDate := "2023-06-15T14:05:00Z"
	parentIdTag := startTransactionGroupId
	// the first id of the sequence of a new store
	want := &types.StartTransactionResponseJson{
		IdTagInfo: types.StartTransactionResponseJsonIdTagInfo{
			Status:      types.StartTransactionResponseJsonIdTagInfoStatusAccepted,
			ExpiryDate:  &expiryDate,
			ParentIdTag: &parentIdTag,
		},
		TransactionId: 1,
	}
	assert.Equal(t, want, got)

	transactionId := handlers.ConvertToUUID(1)
	found, err := engine.FindTransaction(context.Background(), startTransactionChargeStationId, transactionId)
	require.NoError(t, err)
	require.NotNil(t, found)

	// the time of the station, not of the handler's clock
	startedAt := time.Date(2023, 6, 15, 14, 58, 30, 0, time.UTC)
	connectorId := 2
	meterStart := int64(100)
	groupId := startTransactionGroupId
	expected := &store.Transaction{
		ChargeStationId: startTransactionChargeStationId,
		TransactionId:   transactionId,
		IdToken:         startTransactionIdTag,
		TokenType:       "ISO14443",
		MeterValues: []store.MeterValue{
			beginMeterValue(startTransactionStationTime, 100),
		},
		ConnectorId:  &connectorId,
		EvseId:       &connectorId,
		StartedAt:    &startedAt,
		MeterStartWh: &meterStart,
		IdTokenGroup: &groupId,
	}
	assert.Equal(t, expected, found)
}

func TestStartTransactionKeepsTheOffsetAndFractionOfTheStationTime(t *testing.T) {
	handler, engine := newStartTransactionHandler(t)

	req := startTransactionRequest(startTransactionIdTag)
	req.Timestamp = "2023-06-15T16:58:30.25+02:00"
	got := startTransaction(t, handler, startTransactionChargeStationId, req)

	found, err := engine.FindTransaction(context.Background(), startTransactionChargeStationId, handlers.ConvertToUUID(got.TransactionId))
	require.NoError(t, err)
	require.NotNil(t, found)

	require.NotNil(t, found.StartedAt)
	want := time.Date(2023, 6, 15, 14, 58, 30, 250_000_000, time.UTC)
	assert.True(t, want.Equal(*found.StartedAt), "started at %s, want %s", found.StartedAt, want)

	// the begin sample has the same instant
	require.Len(t, found.MeterValues, 1)
	sampleTime, err := time.Parse(time.RFC3339, found.MeterValues[0].Timestamp)
	require.NoError(t, err)
	assert.True(t, want.Equal(sampleTime), "begin sample at %s, want %s", sampleTime, want)
}

func TestStartTransactionWithInvalidTimestampUsesTheClock(t *testing.T) {
	handler, engine := newStartTransactionHandler(t)

	req := startTransactionRequest(startTransactionIdTag)
	req.Timestamp = "yesterday"
	got := startTransaction(t, handler, startTransactionChargeStationId, req)
	assert.Equal(t, types.StartTransactionResponseJsonIdTagInfoStatusAccepted, got.IdTagInfo.Status)

	found, err := engine.FindTransaction(context.Background(), startTransactionChargeStationId, handlers.ConvertToUUID(got.TransactionId))
	require.NoError(t, err)
	require.NotNil(t, found)

	clockTime, err := time.Parse(time.RFC3339, startTransactionClockTime)
	require.NoError(t, err)
	require.NotNil(t, found.StartedAt)
	assert.True(t, clockTime.Equal(*found.StartedAt), "started at %s, want %s", found.StartedAt, clockTime)
	assert.Equal(t, []store.MeterValue{beginMeterValue(startTransactionClockTime, 100)}, found.MeterValues)
}

func TestStartTransactionWithUnknownIdTag(t *testing.T) {
	handler, engine := newStartTransactionHandler(t)
	ctx := context.Background()

	first := startTransaction(t, handler, startTransactionChargeStationId, startTransactionRequest("BADRFIDTAG"))
	second := startTransaction(t, handler, startTransactionChargeStationId, startTransactionRequest("BADRFIDTAG"))

	// OCPP 1.6 requires a transaction id in every StartTransaction.conf
	assert.Equal(t, types.StartTransactionResponseJsonIdTagInfoStatusInvalid, first.IdTagInfo.Status)
	assert.Equal(t, types.StartTransactionResponseJsonIdTagInfoStatusInvalid, second.IdTagInfo.Status)
	assert.Equal(t, 1, first.TransactionId)
	assert.Equal(t, 2, second.TransactionId)

	for _, id := range []int{first.TransactionId, second.TransactionId} {
		found, err := engine.FindTransaction(ctx, startTransactionChargeStationId, handlers.ConvertToUUID(id))
		require.NoError(t, err)
		require.NotNilf(t, found, "transaction %d is not recorded", id)

		assert.Equal(t, "BADRFIDTAG", found.IdToken)
		assert.Equal(t, []store.MeterValue{beginMeterValue(startTransactionStationTime, 100)}, found.MeterValues)
		require.NotNil(t, found.ConnectorId)
		assert.Equal(t, 2, *found.ConnectorId)
		require.NotNil(t, found.MeterStartWh)
		assert.Equal(t, int64(100), *found.MeterStartWh)
		assert.Nil(t, found.IdTokenGroup, "an unknown idTag has no group")
	}

	transactions, err := engine.Transactions(ctx)
	require.NoError(t, err)
	assert.Len(t, transactions, 2)
}

func TestStartTransactionWithTokenThatIsNotAccepted(t *testing.T) {
	// one second before the time of the handler's clock
	expiredAt := time.Date(2023, 6, 15, 14, 4, 59, 0, time.UTC)
	parentIdTag := startTransactionGroupId

	tests := []struct {
		name string
		// change makes the token of the store unacceptable
		change     func(tok *store.Token)
		wantStatus types.StartTransactionResponseJsonIdTagInfoStatus
	}{
		{
			name:       "blocked token",
			change:     func(tok *store.Token) { tok.Valid = false },
			wantStatus: types.StartTransactionResponseJsonIdTagInfoStatusBlocked,
		},
		{
			name:       "expired token",
			change:     func(tok *store.Token) { tok.ExpiresAt = &expiredAt },
			wantStatus: types.StartTransactionResponseJsonIdTagInfoStatusExpired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, engine := newStartTransactionHandler(t)
			ctx := context.Background()

			tok, err := engine.LookupToken(ctx, startTransactionIdTag)
			require.NoError(t, err)
			require.NotNil(t, tok)
			tt.change(tok)
			require.NoError(t, engine.SetToken(ctx, tok))

			got := startTransaction(t, handler, startTransactionChargeStationId, startTransactionRequest(startTransactionIdTag))

			// no expiry date: the token is not accepted
			want := &types.StartTransactionResponseJson{
				IdTagInfo: types.StartTransactionResponseJsonIdTagInfo{
					Status:      tt.wantStatus,
					ParentIdTag: &parentIdTag,
				},
				TransactionId: 1,
			}
			assert.Equal(t, want, got)

			// the start is recorded all the same, with the group of the token
			found, err := engine.FindTransaction(ctx, startTransactionChargeStationId, handlers.ConvertToUUID(got.TransactionId))
			require.NoError(t, err)
			require.NotNil(t, found, "the transaction is not recorded")
			assert.Equal(t, startTransactionIdTag, found.IdToken)
			assert.Equal(t, []store.MeterValue{beginMeterValue(startTransactionStationTime, 100)}, found.MeterValues)
			require.NotNil(t, found.MeterStartWh)
			assert.Equal(t, int64(100), *found.MeterStartWh)
			require.NotNil(t, found.IdTokenGroup)
			assert.Equal(t, startTransactionGroupId, *found.IdTokenGroup)
		})
	}
}

func TestStartTransactionConsumesReservation(t *testing.T) {
	const reservationId = 77
	otherGroup := "OTHERGROUP"
	group := startTransactionGroupId

	tests := []struct {
		name string
		// the reservation that the store has
		reservation store.Reservation
		// the idTag that starts the transaction (default: startTransactionIdTag)
		idTag      string
		wantStatus store.ReservationStatus
	}{
		{
			name: "reservation for the idTag on the connector is used",
			reservation: store.Reservation{
				ChargeStationId: startTransactionChargeStationId,
				ConnectorId:     2,
				IdTag:           startTransactionIdTag,
				Status:          store.ReservationStatusAccepted,
			},
			wantStatus: store.ReservationStatusUsed,
		},
		{
			name: "reservation for any connector of the charge station is used",
			reservation: store.Reservation{
				ChargeStationId: startTransactionChargeStationId,
				ConnectorId:     0,
				IdTag:           startTransactionIdTag,
				Status:          store.ReservationStatusAccepted,
			},
			wantStatus: store.ReservationStatusUsed,
		},
		{
			name: "reservation for the group of the idTag is used",
			reservation: store.Reservation{
				ChargeStationId: startTransactionChargeStationId,
				ConnectorId:     2,
				IdTag:           "ANOTHERTAG",
				ParentIdTag:     &group,
				Status:          store.ReservationStatusAccepted,
			},
			wantStatus: store.ReservationStatusUsed,
		},
		{
			name: "reservation for an idTag that is not a known token is used",
			reservation: store.Reservation{
				ChargeStationId: startTransactionChargeStationId,
				ConnectorId:     2,
				IdTag:           "BADRFIDTAG",
				Status:          store.ReservationStatusAccepted,
			},
			idTag:      "BADRFIDTAG",
			wantStatus: store.ReservationStatusUsed,
		},
		{
			name: "reservation for another idTag is unchanged",
			reservation: store.Reservation{
				ChargeStationId: startTransactionChargeStationId,
				ConnectorId:     2,
				IdTag:           "ANOTHERTAG",
				Status:          store.ReservationStatusAccepted,
			},
			wantStatus: store.ReservationStatusAccepted,
		},
		{
			name: "reservation for another group is unchanged",
			reservation: store.Reservation{
				ChargeStationId: startTransactionChargeStationId,
				ConnectorId:     2,
				IdTag:           "ANOTHERTAG",
				ParentIdTag:     &otherGroup,
				Status:          store.ReservationStatusAccepted,
			},
			wantStatus: store.ReservationStatusAccepted,
		},
		{
			name: "reservation for a group is unchanged for an unknown idTag",
			reservation: store.Reservation{
				ChargeStationId: startTransactionChargeStationId,
				ConnectorId:     2,
				IdTag:           "ANOTHERTAG",
				ParentIdTag:     &group,
				Status:          store.ReservationStatusAccepted,
			},
			idTag:      "BADRFIDTAG",
			wantStatus: store.ReservationStatusAccepted,
		},
		{
			name: "reservation of another charge station is unchanged",
			reservation: store.Reservation{
				ChargeStationId: "cs002",
				ConnectorId:     2,
				IdTag:           startTransactionIdTag,
				Status:          store.ReservationStatusAccepted,
			},
			wantStatus: store.ReservationStatusAccepted,
		},
		{
			name: "reservation for another connector is unchanged",
			reservation: store.Reservation{
				ChargeStationId: startTransactionChargeStationId,
				ConnectorId:     1,
				IdTag:           startTransactionIdTag,
				Status:          store.ReservationStatusAccepted,
			},
			wantStatus: store.ReservationStatusAccepted,
		},
		{
			name: "cancelled reservation is unchanged",
			reservation: store.Reservation{
				ChargeStationId: startTransactionChargeStationId,
				ConnectorId:     2,
				IdTag:           startTransactionIdTag,
				Status:          store.ReservationStatusCancelled,
			},
			wantStatus: store.ReservationStatusCancelled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, engine := newStartTransactionHandler(t)
			ctx := context.Background()

			reservation := tt.reservation
			reservation.ReservationId = reservationId
			reservation.ExpiryDate = time.Date(2023, 6, 15, 16, 0, 0, 0, time.UTC)
			reservation.CreatedAt = time.Date(2023, 6, 15, 14, 0, 0, 0, time.UTC)
			require.NoError(t, engine.CreateReservation(ctx, &reservation))

			idTag := tt.idTag
			if idTag == "" {
				idTag = startTransactionIdTag
			}
			req := startTransactionRequest(idTag)
			req.ReservationId = intPtr(reservationId)

			// the reservation never changes the outcome of the start
			got := startTransaction(t, handler, startTransactionChargeStationId, req)
			assert.Equal(t, 1, got.TransactionId)

			stored, err := engine.GetReservation(ctx, reservationId)
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.Equal(t, tt.wantStatus, stored.Status)

			// the transaction records the reservation that the station reported
			found, err := engine.FindTransaction(ctx, startTransactionChargeStationId, handlers.ConvertToUUID(got.TransactionId))
			require.NoError(t, err)
			require.NotNil(t, found)
			require.NotNil(t, found.ReservationId)
			assert.Equal(t, reservationId, *found.ReservationId)
		})
	}
}

func TestStartTransactionWithoutReservationLeavesReservationsAlone(t *testing.T) {
	handler, engine := newStartTransactionHandler(t)
	ctx := context.Background()

	require.NoError(t, engine.CreateReservation(ctx, &store.Reservation{
		ReservationId:   77,
		ChargeStationId: startTransactionChargeStationId,
		ConnectorId:     2,
		IdTag:           startTransactionIdTag,
		Status:          store.ReservationStatusAccepted,
	}))

	got := startTransaction(t, handler, startTransactionChargeStationId, startTransactionRequest(startTransactionIdTag))

	stored, err := engine.GetReservation(ctx, 77)
	require.NoError(t, err)
	assert.Equal(t, store.ReservationStatusAccepted, stored.Status)

	found, err := engine.FindTransaction(ctx, startTransactionChargeStationId, handlers.ConvertToUUID(got.TransactionId))
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Nil(t, found.ReservationId)
}

// failingReservationStore fails every call that the handler makes.
type failingReservationStore struct {
	store.ReservationStore
	getErr    error
	updateErr error
	// reservation is returned by GetReservation when getErr is nil
	reservation *store.Reservation
}

func (s failingReservationStore) GetReservation(context.Context, int) (*store.Reservation, error) {
	return s.reservation, s.getErr
}

func (s failingReservationStore) UpdateReservationStatus(context.Context, int, store.ReservationStatus) error {
	return s.updateErr
}

func TestStartTransactionSucceedsWhenTheReservationStoreFails(t *testing.T) {
	tests := []struct {
		name  string
		store failingReservationStore
	}{
		{
			name:  "reservation lookup fails",
			store: failingReservationStore{getErr: errors.New("lookup failed")},
		},
		{
			name: "reservation update fails",
			store: failingReservationStore{
				updateErr: errors.New("update failed"),
				reservation: &store.Reservation{
					ReservationId:   77,
					ChargeStationId: startTransactionChargeStationId,
					ConnectorId:     2,
					IdTag:           startTransactionIdTag,
					Status:          store.ReservationStatusAccepted,
				},
			},
		},
		{
			name:  "reservation is not known",
			store: failingReservationStore{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, engine := newStartTransactionHandler(t)
			handler.ReservationStore = tt.store

			req := startTransactionRequest(startTransactionIdTag)
			req.ReservationId = intPtr(77)
			got := startTransaction(t, handler, startTransactionChargeStationId, req)

			assert.Equal(t, types.StartTransactionResponseJsonIdTagInfoStatusAccepted, got.IdTagInfo.Status)
			found, err := engine.FindTransaction(context.Background(), startTransactionChargeStationId, handlers.ConvertToUUID(got.TransactionId))
			require.NoError(t, err)
			assert.NotNil(t, found)
		})
	}
}

func TestStartTransactionRetriesWhenTheTransactionIdIsInUse(t *testing.T) {
	handler, engine := newStartTransactionHandler(t)
	transactionStore := &faultyTransactionStore{
		TransactionStore: engine,
		createErrs:       []error{alreadyExists(1)},
	}
	handler.TransactionStore = transactionStore
	ctx := context.Background()

	got := startTransaction(t, handler, startTransactionChargeStationId, startTransactionRequest(startTransactionIdTag))

	// the response has the id of the transaction that was created
	assert.Equal(t, 2, got.TransactionId)
	assert.Equal(t, []string{handlers.ConvertToUUID(1), handlers.ConvertToUUID(2)}, transactionStore.createdIds)

	found, err := engine.FindTransaction(ctx, startTransactionChargeStationId, handlers.ConvertToUUID(2))
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, []store.MeterValue{beginMeterValue(startTransactionStationTime, 100)}, found.MeterValues)
	require.NotNil(t, found.ConnectorId, "the details are stored with the transaction that was created")
	assert.Equal(t, 2, *found.ConnectorId)

	notCreated, err := engine.FindTransaction(ctx, startTransactionChargeStationId, handlers.ConvertToUUID(1))
	require.NoError(t, err)
	assert.Nil(t, notCreated)
}

func TestStartTransactionGivesUpWhenEveryTransactionIdIsInUse(t *testing.T) {
	handler, engine := newStartTransactionHandler(t)
	transactionStore := &faultyTransactionStore{
		TransactionStore: engine,
		createErrs:       []error{alreadyExists(1), alreadyExists(2), alreadyExists(3)},
	}
	handler.TransactionStore = transactionStore

	resp, err := handler.HandleCall(context.Background(), startTransactionChargeStationId, startTransactionRequest(startTransactionIdTag))

	require.ErrorIs(t, err, store.ErrAlreadyExists)
	assert.Nil(t, resp)
	assert.Len(t, transactionStore.createdIds, 3, "a StartTransaction.req tries three transaction ids")
}

func TestStartTransactionReturnsStoreErrors(t *testing.T) {
	storeErr := errors.New("store failed")

	tests := []struct {
		name  string
		fault func(s *faultyTransactionStore)
	}{
		{
			name:  "transaction id allocation fails",
			fault: func(s *faultyTransactionStore) { s.nextIdErr = storeErr },
		},
		{
			name:  "transaction creation fails",
			fault: func(s *faultyTransactionStore) { s.createErrs = []error{storeErr} },
		},
		{
			name:  "transaction details update fails",
			fault: func(s *faultyTransactionStore) { s.updateDetailsErr = storeErr },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, engine := newStartTransactionHandler(t)
			transactionStore := &faultyTransactionStore{TransactionStore: engine}
			tt.fault(transactionStore)
			handler.TransactionStore = transactionStore

			resp, err := handler.HandleCall(context.Background(), startTransactionChargeStationId, startTransactionRequest(startTransactionIdTag))

			require.ErrorIs(t, err, storeErr)
			assert.Nil(t, resp)
			assert.LessOrEqual(t, len(transactionStore.createdIds), 1, "only an id that is in use is retried")
		})
	}
}

// TestStartTransactionWithStoreWithoutIdSequenceAndDetails covers the Firestore
// behaviour: the transaction id is random and the details are not stored.
func TestStartTransactionWithStoreWithoutIdSequenceAndDetails(t *testing.T) {
	handler, engine := newStartTransactionHandler(t)
	handler.TransactionStore = &faultyTransactionStore{
		TransactionStore: engine,
		nextIdErr:        fmt.Errorf("NextOcpp16TransactionId: %w", store.ErrNotImplemented),
		updateDetailsErr: fmt.Errorf("UpdateTransactionDetails: %w", store.ErrNotImplemented),
	}
	ctx := context.Background()

	require.NoError(t, engine.CreateReservation(ctx, &store.Reservation{
		ReservationId:   77,
		ChargeStationId: startTransactionChargeStationId,
		ConnectorId:     2,
		IdTag:           startTransactionIdTag,
		Status:          store.ReservationStatusAccepted,
	}))
	req := startTransactionRequest(startTransactionIdTag)
	req.ReservationId = intPtr(77)

	got := startTransaction(t, handler, startTransactionChargeStationId, req)

	assert.Equal(t, types.StartTransactionResponseJsonIdTagInfoStatusAccepted, got.IdTagInfo.Status)
	// a non-negative 31 bit random number
	assert.GreaterOrEqual(t, got.TransactionId, 0)
	assert.Equal(t, 0, got.TransactionId>>31)

	transactionId := handlers.ConvertToUUID(got.TransactionId)
	found, err := engine.FindTransaction(ctx, startTransactionChargeStationId, transactionId)
	require.NoError(t, err)
	expected := &store.Transaction{
		ChargeStationId: startTransactionChargeStationId,
		TransactionId:   transactionId,
		IdToken:         startTransactionIdTag,
		TokenType:       "ISO14443",
		MeterValues: []store.MeterValue{
			beginMeterValue(startTransactionStationTime, 100),
		},
	}
	assert.Equal(t, expected, found)

	// the reservation is consumed all the same
	stored, err := engine.GetReservation(ctx, 77)
	require.NoError(t, err)
	assert.Equal(t, store.ReservationStatusUsed, stored.Status)
}
