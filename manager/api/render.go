// SPDX-License-Identifier: Apache-2.0

package api

import "net/http"

func (c ChargeStationAuth) Bind(r *http.Request) error {
	return nil
}

func (c ChargeStationAuth) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}

func (c ChargeStationSettings) Bind(r *http.Request) error {
	return nil
}

func (c ChargeStationInstallCertificates) Bind(r *http.Request) error {
	return nil
}

func (c ChargeStationTrigger) Bind(r *http.Request) error {
	return nil
}

func (t Token) Bind(r *http.Request) error {
	return nil
}

func (t Token) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}

func (t Certificate) Bind(r *http.Request) error {
	return nil
}

func (t Certificate) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}

func (r Registration) Bind(req *http.Request) error {
	return nil
}

func (r Location) Bind(req *http.Request) error {
	return nil
}

func (f FirmwareUpdateRequest) Bind(r *http.Request) error {
	return nil
}

func (f FirmwareStatus) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}

func (r ReservationRequest) Bind(req *http.Request) error {
	return nil
}

func (r ReservationList) Render(w http.ResponseWriter, req *http.Request) error {
	return nil
}

func (d DiagnosticsRequest) Bind(r *http.Request) error {
	return nil
}

func (d DiagnosticsStatus) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}

func (l LogRequest) Bind(r *http.Request) error {
	return nil
}

func (l LogStatus) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}

func (d DataTransferRequest) Bind(r *http.Request) error {
	return nil
}

func (d DataTransferResponse) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}

func (c ChangeAvailabilityRequest) Bind(r *http.Request) error {
	return nil
}

func (m MeterValuesResponse) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}

func (l LocalListVersionResponse) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}

func (l LocalAuthorizationListResponse) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}

func (u UpdateLocalListRequest) Bind(r *http.Request) error {
	return nil
}

func (s SetDisplayMessageRequest) Bind(r *http.Request) error {
	return nil
}

func (r ResetRequest) Bind(_ *http.Request) error {
	return nil
}

func (u UnlockConnectorRequest) Bind(_ *http.Request) error {
	return nil
}

func (c CertificateHashDataRequest) Bind(_ *http.Request) error {
	return nil
}

func (o OperationResponse) Render(_ http.ResponseWriter, _ *http.Request) error {
	return nil
}
