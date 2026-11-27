// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/render"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func (s *Server) ListTransactions(w http.ResponseWriter, r *http.Request, csId string, params ListTransactionsParams) {
	status := "all"
	if params.Status != nil {
		status = string(*params.Status)
	}

	limit := 50
	if params.Limit != nil {
		limit = *params.Limit
	}

	offset := 0
	if params.Offset != nil {
		offset = *params.Offset
	}

	var startDate, endDate *time.Time
	if params.StartDate != nil {
		parsed := time.Time(*params.StartDate)
		startDate = &parsed
	}
	if params.EndDate != nil {
		parsed := time.Time(*params.EndDate)
		endDate = &parsed
	}

	transactions, total, err := s.store.ListTransactionsForChargeStation(
		r.Context(), csId, status, startDate, endDate, limit, offset,
	)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	response := &TransactionList{
		Transactions: make([]TransactionSummary, 0, len(transactions)),
		Total:        total,
		Limit:        limit,
		Offset:       offset,
	}

	for _, txn := range transactions {
		startTime, stopTime, meterStart, meterStop := transactionView(txn)

		txnStatus := TransactionSummaryStatusActive
		if txn.EndedSeqNo > 0 {
			txnStatus = TransactionSummaryStatusCompleted
		}

		summary := TransactionSummary{
			TransactionId: txn.TransactionId,
			IdTag:         txn.IdToken,
			StartTime:     startTime,
			StopTime:      stopTime,
			MeterStart:    meterStart,
			MeterStop:     meterStop,
			Status:        txnStatus,
			ConnectorId:   txn.ConnectorId,
			EvseId:        txn.EvseId,
			StopReason:    txn.StopReason,
			EnergyWh:      txn.EnergyWh,
		}
		response.Transactions = append(response.Transactions, summary)
	}

	_ = render.Render(w, r, response)
}

func (s *Server) GetTransactionDetails(w http.ResponseWriter, r *http.Request, csId string, transactionId string) {
	txn, err := s.store.FindTransaction(r.Context(), csId, transactionId)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	if txn == nil {
		w.WriteHeader(http.StatusNotFound)
		errMsg := "Transaction not found"
		_ = render.Render(w, r, &Status{
			Status: "error",
			Error:  &errMsg,
		})
		return
	}

	startTime, stopTime, meterStart, meterStop := transactionView(txn)

	txnStatus := TransactionDetailStatusActive
	if txn.EndedSeqNo > 0 {
		txnStatus = TransactionDetailStatusCompleted
	}

	apiMeterValues := make([]MeterValue, 0, len(txn.MeterValues))
	for _, mv := range txn.MeterValues {
		timestamp, _ := time.Parse(time.RFC3339, mv.Timestamp)

		sampledValues := make([]MeterValuesSampledValue, 0, len(mv.SampledValues))
		for _, sv := range mv.SampledValues {
			var unit *string
			if sv.UnitOfMeasure != nil {
				unit = &sv.UnitOfMeasure.Unit
			}
			apiSV := MeterValuesSampledValue{
				Value:     fmt.Sprintf("%g", sv.Value),
				Context:   sv.Context,
				Measurand: sv.Measurand,
				Phase:     sv.Phase,
				Location:  sv.Location,
				Unit:      unit,
			}
			sampledValues = append(sampledValues, apiSV)
		}

		apiMeterValues = append(apiMeterValues, MeterValue{
			Timestamp:    timestamp,
			SampledValue: sampledValues,
		})
	}

	response := &TransactionDetail{
		TransactionId: txn.TransactionId,
		IdTag:         txn.IdToken,
		TokenType:     &txn.TokenType,
		StartTime:     startTime,
		StopTime:      stopTime,
		MeterStart:    meterStart,
		MeterStop:     meterStop,
		Status:        txnStatus,
		MeterValues:   apiMeterValues,
		ConnectorId:   txn.ConnectorId,
		EvseId:        txn.EvseId,
		StopReason:    txn.StopReason,
		EnergyWh:      txn.EnergyWh,
	}

	_ = render.Render(w, r, response)
}

// energyRegisterMeasurand is the measurand of the energy meter reading that the meter
// start and stop values are read from when the store does not have them.
const energyRegisterMeasurand = "Energy.Active.Import.Register"

// transactionView derives the API fields, preferring the enriched store fields and
// falling back to the first/last sample for transactions that do not have them
// (older records, or a store that does not keep the details).
func transactionView(txn *store.Transaction) (start time.Time, stop *time.Time, meterStart int, meterStop *int) {
	completed := txn.EndedSeqNo > 0

	// a timestamp that cannot be parsed is the zero time, as for a transaction without samples
	sampleTime := func(mv store.MeterValue) time.Time {
		t, _ := time.Parse(time.RFC3339, mv.Timestamp)
		return t
	}

	switch {
	case txn.StartedAt != nil:
		start = *txn.StartedAt
	case len(txn.MeterValues) > 0:
		start = sampleTime(txn.MeterValues[0])
	}

	switch {
	case txn.StoppedAt != nil:
		stoppedAt := *txn.StoppedAt
		stop = &stoppedAt
	case completed && len(txn.MeterValues) > 0:
		stoppedAt := sampleTime(txn.MeterValues[len(txn.MeterValues)-1])
		stop = &stoppedAt
	}

	if txn.MeterStartWh != nil {
		meterStart = int(*txn.MeterStartWh)
	} else if reading, ok := firstEnergyRegisterReading(txn.MeterValues); ok {
		meterStart = reading
	}

	if txn.MeterStopWh != nil {
		reading := int(*txn.MeterStopWh)
		meterStop = &reading
	} else if completed {
		if reading, ok := lastEnergyRegisterReading(txn.MeterValues); ok {
			meterStop = &reading
		}
	}

	return start, stop, meterStart, meterStop
}

// firstEnergyRegisterReading returns the first energy register sample of the meter values.
func firstEnergyRegisterReading(meterValues []store.MeterValue) (int, bool) {
	for _, mv := range meterValues {
		for _, sv := range mv.SampledValues {
			if sv.Measurand != nil && *sv.Measurand == energyRegisterMeasurand {
				return int(sv.Value), true
			}
		}
	}
	return 0, false
}

// lastEnergyRegisterReading returns the last energy register sample of the meter values.
func lastEnergyRegisterReading(meterValues []store.MeterValue) (int, bool) {
	for i := len(meterValues) - 1; i >= 0; i-- {
		sampledValues := meterValues[i].SampledValues
		for j := len(sampledValues) - 1; j >= 0; j-- {
			sv := sampledValues[j]
			if sv.Measurand != nil && *sv.Measurand == energyRegisterMeasurand {
				return int(sv.Value), true
			}
		}
	}
	return 0, false
}

func (s *Server) RemoteStartTransaction(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(RemoteStartTransactionRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	runtime, err := s.store.LookupChargeStationRuntimeDetails(r.Context(), csId)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}
	if runtime == nil {
		_ = render.Render(w, r, ErrNotFound)
		return
	}

	var chargingProfileJSON *string
	if req.ChargingProfile != nil {
		profileBytes, err := json.Marshal(req.ChargingProfile)
		if err != nil {
			_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("invalid charging profile: %w", err)))
			return
		}
		profileStr := string(profileBytes)
		chargingProfileJSON = &profileStr
	}

	transactionReq := &store.RemoteStartTransactionRequest{
		ChargeStationId: csId,
		IdTag:           req.IdTag,
		ConnectorId:     req.ConnectorId,
		ChargingProfile: chargingProfileJSON,
		Status:          store.RemoteTransactionRequestStatusPending,
		SendAfter:       s.clock.Now(),
		RequestType:     store.RemoteTransactionRequestTypeStart,
	}

	err = s.store.SetRemoteStartTransactionRequest(r.Context(), csId, transactionReq)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) RemoteStopTransaction(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(RemoteStopTransactionRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	runtime, err := s.store.LookupChargeStationRuntimeDetails(r.Context(), csId)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}
	if runtime == nil {
		_ = render.Render(w, r, ErrNotFound)
		return
	}

	transactionReq := &store.RemoteStopTransactionRequest{
		ChargeStationId: csId,
		TransactionId:   req.TransactionId,
		Status:          store.RemoteTransactionRequestStatusPending,
		SendAfter:       s.clock.Now(),
		RequestType:     store.RemoteTransactionRequestTypeStop,
	}

	err = s.store.SetRemoteStopTransactionRequest(r.Context(), csId, transactionReq)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func toRendererList(response []ConnectorStatusResponse) []render.Renderer {
	list := make([]render.Renderer, len(response))
	for i := range response {
		list[i] = response[i]
	}
	return list
}

// Render implementations

func (t TransactionList) Render(_ http.ResponseWriter, _ *http.Request) error {
	return nil
}

// Render implementations

func (t TransactionDetail) Render(_ http.ResponseWriter, _ *http.Request) error {
	return nil
}

// Render implementations

func (r RemoteStartTransactionRequest) Bind(_ *http.Request) error {
	return nil
}

// Render implementations

func (r RemoteStopTransactionRequest) Bind(_ *http.Request) error {
	return nil
}
