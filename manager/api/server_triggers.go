// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/render"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// pncVendorId is the vendor of the DataTransfer messages that carry the OCPP 2.0.1
// messages of ISO 15118 Plug & Charge to an OCPP 1.6 charge station.
const pncVendorId = "org.openchargealliance.iso15118pnc"

// TriggerChargeStation asks the charge station to send a message. For an OCPP 1.6
// charge station it queues a command and answers it: a TriggerMessage for the messages
// of OCPP 1.6, otherwise the OCPP 2.0.1 TriggerMessage in a Plug & Charge DataTransfer,
// as the trigger sync sends them. Other charge stations get a trigger message in the
// store.
func (s *Server) TriggerChargeStation(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(ChargeStationTrigger)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	queue, ok := s.useQueue(w, r, csId)
	if !ok {
		return
	}
	if queue {
		action, payload, err := triggerCommand(req)
		if err != nil {
			renderInternalError(w, r, "building the payload of a trigger", err)
			return
		}
		if cmd := s.enqueueFor(w, r, csId, action, payload); cmd != nil {
			s.renderQueued(w, r, http.StatusCreated, cmd)
		}
		return
	}

	err := s.store.SetChargeStationTriggerMessage(r.Context(), csId, &store.ChargeStationTriggerMessage{
		TriggerMessage: store.TriggerMessage(req.Trigger),
		ConnectorId:    req.ConnectorId,
		TriggerStatus:  store.TriggerStatusPending,
	})
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// triggerCommand returns the OCPP 1.6 action and payload that ask a charge station for
// the message of the trigger.
func triggerCommand(req *ChargeStationTrigger) (string, any, error) {
	switch req.Trigger {
	case BootNotification, DiagnosticsStatusNotification, FirmwareStatusNotification,
		Heartbeat, MeterValues, StatusNotification:
		return "TriggerMessage", &ocpp16.TriggerMessageJson{
			RequestedMessage: ocpp16.TriggerMessageJsonRequestedMessage(req.Trigger),
			ConnectorId:      req.ConnectorId,
		}, nil
	}

	trigger := &ocpp201.TriggerMessageRequestJson{
		RequestedMessage: ocpp201.MessageTriggerEnumType(req.Trigger),
	}
	if req.ConnectorId != nil {
		trigger.Evse = &ocpp201.EVSEType{Id: *req.ConnectorId}
	}
	data, err := json.Marshal(trigger)
	if err != nil {
		return "", nil, err
	}
	messageId, dataString := "TriggerMessage", string(data)
	return "DataTransfer", &ocpp16.DataTransferJson{
		VendorId:  pncVendorId,
		MessageId: &messageId,
		Data:      &dataString,
	}, nil
}

// Render implementations

func (c ChargeStationTrigger) Bind(r *http.Request) error {
	return nil
}
