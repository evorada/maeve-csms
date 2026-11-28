// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/render"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// sessionOrgIds returns the orgs whose sessions (transactions and meter values) the
// caller may read in the history of a charge station: the orgs that it may reach, as
// for the list of the charge stations (Server.stationOrgIds). A session stays with the
// org that owned the charge station when it started, so a charge station that was
// transferred has sessions of an org that its new owner may not reach.
//
// AuthzMiddleware has checked that the caller may reach the charge station as it is
// owned now. Without a caller, or when its scope cannot be resolved, the request is
// answered and false is reported.
func (s *Server) sessionOrgIds(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	p, ok := s.caller(w, r)
	if !ok {
		return nil, false
	}
	orgIds, err := s.stationOrgIds(r.Context(), p)
	if err != nil {
		renderScopeError(w, r, err)
		return nil, false
	}
	return orgIds, true
}

// ownedByOneOf reports whether what the org orgId owns belongs to one of the orgs. A
// store that does not keep the org of a transaction (Firestore) gives it no org: it has
// one org, and every caller that reaches the charge station may read its history.
func ownedByOneOf(orgId string, orgIds []string) bool {
	if len(orgIds) == 0 {
		return false
	}
	return orgId == "" || slices.Contains(orgIds, strings.ToLower(orgId))
}

// ListTransactions answers a page of the transactions of the charge station that belong
// to the orgs that the caller may reach. The total counts those only.
func (s *Server) ListTransactions(w http.ResponseWriter, r *http.Request, csId string, params ListTransactionsParams) {
	orgIds, ok := s.sessionOrgIds(w, r)
	if !ok {
		return
	}

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

	var transactions []*store.Transaction
	var total int64
	// no org is every org for the store: a caller that reaches no org gets nothing
	if len(orgIds) > 0 {
		var err error
		transactions, total, err = s.store.ListTransactionsForChargeStation(
			r.Context(), csId, status, startDate, endDate, limit, offset, orgIds,
		)
		if err != nil {
			_ = render.Render(w, r, ErrInternalError(err))
			return
		}
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
		summary.IdleStartedAt, summary.IdleS, summary.ChargingS, summary.AveragePowerW = analysisView(txn)
		summary.TariffId, summary.TariffVersion, summary.Cost = txn.TariffId, txn.TariffVersion, costView(txn)
		response.Transactions = append(response.Transactions, summary)
	}

	_ = render.Render(w, r, response)
}

// GetTransactionDetails answers with a transaction of the charge station. A transaction
// of an org that the caller may not reach (of a previous owner of the charge station) is
// answered exactly like a transaction that does not exist.
func (s *Server) GetTransactionDetails(w http.ResponseWriter, r *http.Request, csId string, transactionId string) {
	orgIds, ok := s.sessionOrgIds(w, r)
	if !ok {
		return
	}

	txn, err := s.store.FindTransaction(r.Context(), csId, transactionId)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	if txn == nil || !ownedByOneOf(txn.OrgId, orgIds) {
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
	response.IdleStartedAt, response.IdleS, response.ChargingS, response.AveragePowerW = analysisView(txn)
	response.TariffId, response.TariffVersion, response.Cost = txn.TariffId, txn.TariffVersion, costView(txn)

	_ = render.Render(w, r, response)
}

// costView returns the API form of the cost of a transaction: nil for a transaction that
// was not rated (one without a price, or of a store that keeps no cost).
func costView(txn *store.Transaction) *TransactionCost {
	cost := txn.Cost
	if cost == nil {
		return nil
	}
	exclVat, _ := cost.Total.ExclVat.Rat().Float64()
	inclVat, _ := cost.Total.InclVat.Rat().Float64()
	return &TransactionCost{Currency: cost.Currency, ExclVat: exclVat, InclVat: inclVat, Final: cost.Final}
}

// analysisView returns the API fields of the analysis of a transaction: all nil for a
// transaction that was not analysed (one that runs, or of a store that keeps no
// analysis).
func analysisView(txn *store.Transaction) (idleStartedAt *time.Time, idleS, chargingS *int64, averagePowerW *float64) {
	analysis := txn.Analysis
	if analysis == nil {
		return nil, nil, nil, nil
	}
	return analysis.IdleStartedAt, &analysis.IdleS, &analysis.ChargingS, analysis.AveragePowerW
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

// remoteStartPayload is the payload of the RemoteStartTransaction command of an OCPP 1.6
// charge station. The charging profile of the API has the fields of the OCPP 1.6
// charging profile, so it is sent as the API has it.
type remoteStartPayload struct {
	ConnectorId     *int            `json:"connectorId,omitempty"`
	IdTag           string          `json:"idTag"`
	ChargingProfile json.RawMessage `json:"chargingProfile,omitempty"`
}

// RemoteStartTransaction asks the charge station to start a transaction. For an OCPP 1.6
// charge station it queues a RemoteStartTransaction command and answers it: a charge
// station that is not connected gets it when it connects, while the command has not
// expired. Other charge stations get a remote start request in the store.
func (s *Server) RemoteStartTransaction(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(RemoteStartTransactionRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	var chargingProfileJSON json.RawMessage
	if req.ChargingProfile != nil {
		profileBytes, err := json.Marshal(req.ChargingProfile)
		if err != nil {
			_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("invalid charging profile: %w", err)))
			return
		}
		chargingProfileJSON = profileBytes
	}

	if s.answerThroughQueue(w, r, csId, http.StatusAccepted, "RemoteStartTransaction", &remoteStartPayload{
		ConnectorId:     req.ConnectorId,
		IdTag:           req.IdTag,
		ChargingProfile: chargingProfileJSON,
	}) {
		return
	}
	if s.refusedUnlessChargingAllowed(w, r, csId) {
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

	var storedProfile *string
	if chargingProfileJSON != nil {
		profile := string(chargingProfileJSON)
		storedProfile = &profile
	}

	transactionReq := &store.RemoteStartTransactionRequest{
		ChargeStationId: csId,
		IdTag:           req.IdTag,
		ConnectorId:     req.ConnectorId,
		ChargingProfile: storedProfile,
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

// RemoteStopTransaction asks the charge station to stop a transaction. For an OCPP 1.6
// charge station it queues a RemoteStopTransaction command and answers it; the
// transaction is named by its OCPP transaction id or by its id in the manager (see
// ocpp16TransactionId). Other charge stations get a remote stop request in the store.
func (s *Server) RemoteStopTransaction(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(RemoteStopTransactionRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	queue, ok := s.useQueue(w, r, csId)
	if !ok {
		return
	}
	if queue {
		transactionId, ok := ocpp16TransactionId(req.TransactionId)
		if !ok {
			_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf(
				"transactionId %q is neither an OCPP 1.6 transaction id nor the id of one in the manager", req.TransactionId)))
			return
		}
		if cmd := s.enqueueFor(w, r, csId, "RemoteStopTransaction",
			&ocpp16.RemoteStopTransactionJson{TransactionId: transactionId}); cmd != nil {
			s.renderQueued(w, r, http.StatusAccepted, cmd)
		}
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

// ocpp16TransactionId returns the OCPP 1.6 transaction id of a transaction named by the
// integer that the charge station gave it or by the id under which the manager stores
// it (ocpp16.ConvertToUUID of that integer, the id that the transaction list shows).
func ocpp16TransactionId(id string) (int, bool) {
	if n, err := strconv.Atoi(id); err == nil {
		return n, true
	}
	return ocpp16.ConvertFromUUID(id)
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
