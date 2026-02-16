// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"io/fs"
	"reflect"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/handlers"
	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/services"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"k8s.io/utils/clock"
)

func NewRouter(emitter transport.Emitter,
	clk clock.PassiveClock,
	engine store.Engine,
	tariffService services.TariffService,
	certValidationService services.CertificateValidationService,
	chargeStationCertProvider services.ChargeStationCertificateProvider,
	contractCertProvider services.ContractCertificateProvider,
	heartbeatInterval time.Duration,
	schemaFS fs.FS) transport.MessageHandler {

	return &handlers.Router{
		Emitter:     emitter,
		SchemaFS:    schemaFS,
		OcppVersion: transport.OcppVersion201,
		CallRoutes: map[string]handlers.CallRoute{
			"Authorize": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.AuthorizeRequestJson) },
				RequestSchema:  "ocpp201/AuthorizeRequest.json",
				ResponseSchema: "ocpp201/AuthorizeResponse.json",
				Handler: AuthorizeHandler{
					// PENDING: inject token auth service
					TokenAuthService: &services.OcppTokenAuthService{
						Clock:      clk,
						TokenStore: engine,
					},
					CertificateValidationService: certValidationService,
				},
			},
			"BootNotification": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.BootNotificationRequestJson) },
				RequestSchema:  "ocpp201/BootNotificationRequest.json",
				ResponseSchema: "ocpp201/BootNotificationResponse.json",
				Handler: BootNotificationHandler{
					Clock:               clk,
					HeartbeatInterval:   int(heartbeatInterval.Seconds()),
					RuntimeDetailsStore: engine,
				},
			},
			"FirmwareStatusNotification": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.FirmwareStatusNotificationRequestJson) },
				RequestSchema:  "ocpp201/FirmwareStatusNotificationRequest.json",
				ResponseSchema: "ocpp201/FirmwareStatusNotificationResponse.json",
				Handler:        FirmwareStatusNotificationHandler{Store: engine},
			},
			"PublishFirmwareStatusNotification": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.PublishFirmwareStatusNotificationRequestJson) },
				RequestSchema:  "ocpp201/PublishFirmwareStatusNotificationRequest.json",
				ResponseSchema: "ocpp201/PublishFirmwareStatusNotificationResponse.json",
				Handler:        PublishFirmwareStatusNotificationHandler{Store: engine},
			},
			"GetCertificateStatus": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.GetCertificateStatusRequestJson) },
				RequestSchema:  "ocpp201/GetCertificateStatusRequest.json",
				ResponseSchema: "ocpp201/GetCertificateStatusResponse.json",
				Handler: GetCertificateStatusHandler{
					CertificateValidationService: certValidationService,
				},
			},
			"Get15118EVCertificate": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.Get15118EVCertificateRequestJson) },
				RequestSchema:  "ocpp201/Get15118EVCertificateRequest.json",
				ResponseSchema: "ocpp201/Get15118EVCertificateResponse.json",
				Handler: Get15118EvCertificateHandler{
					ContractCertificateProvider: contractCertProvider,
				},
			},
			"Heartbeat": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.HeartbeatRequestJson) },
				RequestSchema:  "ocpp201/HeartbeatRequest.json",
				ResponseSchema: "ocpp201/HeartbeatResponse.json",
				Handler: HeartbeatHandler{
					Clock: clk,
				},
			},
			"LogStatusNotification": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.LogStatusNotificationRequestJson) },
				RequestSchema:  "ocpp201/LogStatusNotificationRequest.json",
				ResponseSchema: "ocpp201/LogStatusNotificationResponse.json",
				Handler:        LogStatusNotificationHandler{Store: engine},
			},
			"MeterValues": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.MeterValuesRequestJson) },
				RequestSchema:  "ocpp201/MeterValuesRequest.json",
				ResponseSchema: "ocpp201/MeterValuesResponse.json",
				Handler:        MeterValuesHandler{},
			},
			"NotifyReport": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.NotifyReportRequestJson) },
				RequestSchema:  "ocpp201/NotifyReportRequest.json",
				ResponseSchema: "ocpp201/NotifyReportResponse.json",
				Handler:        NotifyReportHandler{},
			},
			"StatusNotification": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.StatusNotificationRequestJson) },
				RequestSchema:  "ocpp201/StatusNotificationRequest.json",
				ResponseSchema: "ocpp201/StatusNotificationResponse.json",
				Handler:        handlers.CallHandlerFunc(StatusNotificationHandler),
			},
			"SignCertificate": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.SignCertificateRequestJson) },
				RequestSchema:  "ocpp201/SignCertificateRequest.json",
				ResponseSchema: "ocpp201/SignCertificateResponse.json",
				Handler: SignCertificateHandler{
					ChargeStationCertificateProvider: chargeStationCertProvider,
					Store:                            engine,
				},
			},
			"SecurityEventNotification": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.SecurityEventNotificationRequestJson) },
				RequestSchema:  "ocpp201/SecurityEventNotificationRequest.json",
				ResponseSchema: "ocpp201/SecurityEventNotificationResponse.json",
				Handler:        SecurityEventNotificationHandler{},
			},
			"ReservationStatusUpdate": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.ReservationStatusUpdateRequestJson) },
				RequestSchema:  "ocpp201/ReservationStatusUpdateRequest.json",
				ResponseSchema: "ocpp201/ReservationStatusUpdateResponse.json",
				Handler: ReservationStatusUpdateHandler{
					ReservationStore: engine,
				},
			},
			"TransactionEvent": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.TransactionEventRequestJson) },
				RequestSchema:  "ocpp201/TransactionEventRequest.json",
				ResponseSchema: "ocpp201/TransactionEventResponse.json",
				Handler: TransactionEventHandler{
					Store: engine,
					TokenAuthService: &services.OcppTokenAuthService{
						Clock:      clk,
						TokenStore: engine,
					},
					TariffService: tariffService,
				},
			},
		},
		CallResultRoutes: map[string]handlers.CallResultRoute{
			"CancelReservation": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.CancelReservationRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.CancelReservationResponseJson) },
				RequestSchema:  "ocpp201/CancelReservationRequest.json",
				ResponseSchema: "ocpp201/CancelReservationResponse.json",
				Handler: CancelReservationResultHandler{
					ReservationStore: engine,
				},
			},
			"CertificateSigned": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.CertificateSignedRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.CertificateSignedResponseJson) },
				RequestSchema:  "ocpp201/CertificateSignedRequest.json",
				ResponseSchema: "ocpp201/CertificateSignedResponse.json",
				Handler: CertificateSignedResultHandler{
					Store: engine,
				},
			},
			"ChangeAvailability": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.ChangeAvailabilityRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.ChangeAvailabilityResponseJson) },
				RequestSchema:  "ocpp201/ChangeAvailabilityRequest.json",
				ResponseSchema: "ocpp201/ChangeAvailabilityResponse.json",
				Handler:        ChangeAvailabilityResultHandler{},
			},
			"ClearCache": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.ClearCacheRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.ClearCacheResponseJson) },
				RequestSchema:  "ocpp201/ClearCacheRequest.json",
				ResponseSchema: "ocpp201/ClearCacheResponse.json",
				Handler:        ClearCacheResultHandler{},
			},
			"ClearVariableMonitoring": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.ClearVariableMonitoringRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.ClearVariableMonitoringResponseJson) },
				RequestSchema:  "ocpp201/ClearVariableMonitoringRequest.json",
				ResponseSchema: "ocpp201/ClearVariableMonitoringResponse.json",
				Handler:        ClearVariableMonitoringResultHandler{},
			},
			"DeleteCertificate": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.DeleteCertificateRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.DeleteCertificateResponseJson) },
				RequestSchema:  "ocpp201/DeleteCertificateRequest.json",
				ResponseSchema: "ocpp201/DeleteCertificateResponse.json",
				Handler: DeleteCertificateResultHandler{
					Store: engine,
				},
			},
			"GetBaseReport": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.GetBaseReportRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.GetBaseReportResponseJson) },
				RequestSchema:  "ocpp201/GetBaseReportRequest.json",
				ResponseSchema: "ocpp201/GetBaseReportResponse.json",
				Handler:        GetBaseReportResultHandler{},
			},
			"GetInstalledCertificateIds": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.GetInstalledCertificateIdsRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.GetInstalledCertificateIdsResponseJson) },
				RequestSchema:  "ocpp201/GetInstalledCertificateIdsRequest.json",
				ResponseSchema: "ocpp201/GetInstalledCertificateIdsResponse.json",
				Handler: GetInstalledCertificateIdsResultHandler{
					Store: engine,
				},
			},
			"GetLog": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.GetLogRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.GetLogResponseJson) },
				RequestSchema:  "ocpp201/GetLogRequest.json",
				ResponseSchema: "ocpp201/GetLogResponse.json",
				Handler: GetLogResultHandler{
					Store: engine,
				},
			},
			"GetMonitoringReport": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.GetMonitoringReportRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.GetMonitoringReportResponseJson) },
				RequestSchema:  "ocpp201/GetMonitoringReportRequest.json",
				ResponseSchema: "ocpp201/GetMonitoringReportResponse.json",
				Handler:        GetMonitoringReportResultHandler{},
			},
			"GetLocalListVersion": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.GetLocalListVersionRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.GetLocalListVersionResponseJson) },
				RequestSchema:  "ocpp201/GetLocalListVersionRequest.json",
				ResponseSchema: "ocpp201/GetLocalListVersionResponse.json",
				Handler:        GetLocalListVersionResultHandler{},
			},
			"GetReport": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.GetReportRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.GetReportResponseJson) },
				RequestSchema:  "ocpp201/GetReportRequest.json",
				ResponseSchema: "ocpp201/GetReportResponse.json",
				Handler:        GetReportResultHandler{},
			},
			"GetTransactionStatus": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.GetTransactionStatusRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.GetTransactionStatusResponseJson) },
				RequestSchema:  "ocpp201/GetTransactionStatusRequest.json",
				ResponseSchema: "ocpp201/GetTransactionStatusResponse.json",
				Handler:        GetTransactionStatusResultHandler{},
			},
			"GetVariables": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.GetVariablesRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.GetVariablesResponseJson) },
				RequestSchema:  "ocpp201/GetVariablesRequest.json",
				ResponseSchema: "ocpp201/GetVariablesResponse.json",
				Handler:        GetVariablesResultHandler{},
			},
			"InstallCertificate": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.InstallCertificateRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.InstallCertificateResponseJson) },
				RequestSchema:  "ocpp201/InstallCertificateRequest.json",
				ResponseSchema: "ocpp201/InstallCertificateResponse.json",
				Handler: InstallCertificateResultHandler{
					Store: engine,
				},
			},
			"RequestStartTransaction": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.RequestStartTransactionRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.RequestStartTransactionResponseJson) },
				RequestSchema:  "ocpp201/RequestStartTransactionRequest.json",
				ResponseSchema: "ocpp201/RequestStartTransactionResponse.json",
				Handler:        RequestStartTransactionResultHandler{},
			},
			"RequestStopTransaction": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.RequestStopTransactionRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.RequestStopTransactionResponseJson) },
				RequestSchema:  "ocpp201/RequestStopTransactionRequest.json",
				ResponseSchema: "ocpp201/RequestStopTransactionResponse.json",
				Handler:        RequestStopTransactionResultHandler{},
			},
			"ReserveNow": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.ReserveNowRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.ReserveNowResponseJson) },
				RequestSchema:  "ocpp201/ReserveNowRequest.json",
				ResponseSchema: "ocpp201/ReserveNowResponse.json",
				Handler: ReserveNowResultHandler{
					ReservationStore: engine,
				},
			},
			"Reset": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.ResetRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.ResetResponseJson) },
				RequestSchema:  "ocpp201/ResetRequest.json",
				ResponseSchema: "ocpp201/ResetResponse.json",
				Handler:        ResetResultHandler{},
			},
			"SendLocalList": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.SendLocalListRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.SendLocalListResponseJson) },
				RequestSchema:  "ocpp201/SendLocalListRequest.json",
				ResponseSchema: "ocpp201/SendLocalListResponse.json",
				Handler:        SendLocalListResultHandler{},
			},
			"SetNetworkProfile": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.SetNetworkProfileRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.SetNetworkProfileResponseJson) },
				RequestSchema:  "ocpp201/SetNetworkProfileRequest.json",
				ResponseSchema: "ocpp201/SetNetworkProfileResponse.json",
				Handler:        SetNetworkProfileResultHandler{},
			},
			"SetMonitoringBase": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.SetMonitoringBaseRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.SetMonitoringBaseResponseJson) },
				RequestSchema:  "ocpp201/SetMonitoringBaseRequest.json",
				ResponseSchema: "ocpp201/SetMonitoringBaseResponse.json",
				Handler:        SetMonitoringBaseResultHandler{},
			},
			"SetMonitoringLevel": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.SetMonitoringLevelRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.SetMonitoringLevelResponseJson) },
				RequestSchema:  "ocpp201/SetMonitoringLevelRequest.json",
				ResponseSchema: "ocpp201/SetMonitoringLevelResponse.json",
				Handler:        SetMonitoringLevelResultHandler{},
			},
			"SetVariableMonitoring": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.SetVariableMonitoringRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.SetVariableMonitoringResponseJson) },
				RequestSchema:  "ocpp201/SetVariableMonitoringRequest.json",
				ResponseSchema: "ocpp201/SetVariableMonitoringResponse.json",
				Handler:        SetVariableMonitoringResultHandler{},
			},
			"SetVariables": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.SetVariablesRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.SetVariablesResponseJson) },
				RequestSchema:  "ocpp201/SetVariablesRequest.json",
				ResponseSchema: "ocpp201/SetVariablesResponse.json",
				Handler: SetVariablesResultHandler{
					Store: engine,
				},
			},
			"TriggerMessage": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.TriggerMessageRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.TriggerMessageResponseJson) },
				RequestSchema:  "ocpp201/TriggerMessageRequest.json",
				ResponseSchema: "ocpp201/TriggerMessageResponse.json",
				Handler: TriggerMessageResultHandler{
					Store: engine,
				},
			},
			"UnlockConnector": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.UnlockConnectorRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.UnlockConnectorResponseJson) },
				RequestSchema:  "ocpp201/UnlockConnectorRequest.json",
				ResponseSchema: "ocpp201/UnclockConnectorResponse.json",
				Handler:        UnlockConnectorResultHandler{},
			},
			"PublishFirmware": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.PublishFirmwareRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.PublishFirmwareResponseJson) },
				RequestSchema:  "ocpp201/PublishFirmwareRequest.json",
				ResponseSchema: "ocpp201/PublishFirmwareResponse.json",
				Handler: PublishFirmwareResultHandler{
					Store: engine,
				},
			},
			"UpdateFirmware": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.UpdateFirmwareRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.UpdateFirmwareResponseJson) },
				RequestSchema:  "ocpp201/UpdateFirmwareRequest.json",
				ResponseSchema: "ocpp201/UpdateFirmwareResponse.json",
				Handler: UpdateFirmwareResultHandler{
					Store: engine,
				},
			},
			"UnpublishFirmware": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.UnpublishFirmwareRequestJson) },
				NewResponse:    func() ocpp.Response { return new(ocpp201.UnpublishFirmwareResponseJson) },
				RequestSchema:  "ocpp201/UnpublishFirmwareRequest.json",
				ResponseSchema: "ocpp201/UnpublishFirmwareResponse.json",
				Handler: UnpublishFirmwareResultHandler{
					Store: engine,
				},
			},
		},
	}
}

func NewCallMaker(e transport.Emitter) *handlers.OcppCallMaker {
	return &handlers.OcppCallMaker{
		Emitter:     e,
		OcppVersion: transport.OcppVersion201,
		Actions: map[reflect.Type]string{
			reflect.TypeOf(&ocpp201.CancelReservationRequestJson{}):          "CancelReservation",
			reflect.TypeOf(&ocpp201.CertificateSignedRequestJson{}):          "CertificateSigned",
			reflect.TypeOf(&ocpp201.ChangeAvailabilityRequestJson{}):         "ChangeAvailability",
			reflect.TypeOf(&ocpp201.ClearCacheRequestJson{}):                 "ClearCache",
			reflect.TypeOf(&ocpp201.ClearVariableMonitoringRequestJson{}):    "ClearVariableMonitoring",
			reflect.TypeOf(&ocpp201.DeleteCertificateRequestJson{}):          "DeleteCertificate",
			reflect.TypeOf(&ocpp201.GetBaseReportRequestJson{}):              "GetBaseReport",
			reflect.TypeOf(&ocpp201.GetInstalledCertificateIdsRequestJson{}): "GetInstalledCertificateIds",
			reflect.TypeOf(&ocpp201.GetLogRequestJson{}):                     "GetLog",
			reflect.TypeOf(&ocpp201.GetMonitoringReportRequestJson{}):        "GetMonitoringReport",
			reflect.TypeOf(&ocpp201.GetLocalListVersionRequestJson{}):        "GetLocalListVersion",
			reflect.TypeOf(&ocpp201.GetReportRequestJson{}):                  "GetReport",
			reflect.TypeOf(&ocpp201.GetTransactionStatusRequestJson{}):       "GetTransactionStatus",
			reflect.TypeOf(&ocpp201.GetVariablesRequestJson{}):               "GetVariables",
			reflect.TypeOf(&ocpp201.InstallCertificateRequestJson{}):         "InstallCertificate",
			reflect.TypeOf(&ocpp201.RequestStartTransactionRequestJson{}):    "RequestStartTransaction",
			reflect.TypeOf(&ocpp201.RequestStopTransactionRequestJson{}):     "RequestStopTransaction",
			reflect.TypeOf(&ocpp201.ReserveNowRequestJson{}):                 "ReserveNow",
			reflect.TypeOf(&ocpp201.ResetRequestJson{}):                      "Reset",
			reflect.TypeOf(&ocpp201.SendLocalListRequestJson{}):              "SendLocalList",
			reflect.TypeOf(&ocpp201.SetNetworkProfileRequestJson{}):          "SetNetworkProfile",
			reflect.TypeOf(&ocpp201.SetMonitoringBaseRequestJson{}):          "SetMonitoringBase",
			reflect.TypeOf(&ocpp201.SetMonitoringLevelRequestJson{}):         "SetMonitoringLevel",
			reflect.TypeOf(&ocpp201.SetVariableMonitoringRequestJson{}):      "SetVariableMonitoring",
			reflect.TypeOf(&ocpp201.SetVariablesRequestJson{}):               "SetVariables",
			reflect.TypeOf(&ocpp201.TriggerMessageRequestJson{}):             "TriggerMessage",
			reflect.TypeOf(&ocpp201.UnlockConnectorRequestJson{}):            "UnlockConnector",
			reflect.TypeOf(&ocpp201.PublishFirmwareRequestJson{}):            "PublishFirmware",
			reflect.TypeOf(&ocpp201.UpdateFirmwareRequestJson{}):             "UpdateFirmware",
			reflect.TypeOf(&ocpp201.UnpublishFirmwareRequestJson{}):          "UnpublishFirmware",
		},
	}
}
