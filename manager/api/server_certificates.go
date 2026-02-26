// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"
	"net/http"

	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"

	"github.com/go-chi/render"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func (s *Server) GetInstalledCertificates(w http.ResponseWriter, r *http.Request, csId string, params GetInstalledCertificatesParams) {
	var certType *string
	if params.CertificateType != nil {
		ct := string(*params.CertificateType)
		certType = &ct
	}

	err := s.store.SetChargeStationCertificateQuery(r.Context(), csId, &store.ChargeStationCertificateQuery{
		ChargeStationId: csId,
		CertificateType: certType,
		QueryStatus:     store.CertificateQueryStatusPending,
		SendAfter:       s.clock.Now(),
	})
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	status := OperationResponseStatusPending
	resp := &OperationResponse{
		OperationId: &csId,
		Status:      &status,
	}
	w.WriteHeader(http.StatusAccepted)
	_ = render.Render(w, r, resp)
}

func (s *Server) InstallChargeStationCertificates(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(ChargeStationInstallCertificates)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	var certs []*store.ChargeStationInstallCertificate
	for _, cert := range req.Certificates {
		certId, err := handlers.GetCertificateId(cert.Certificate)
		if err != nil {
			_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("invalid certificate: %w", err)))
			return
		}

		status := store.CertificateInstallationPending
		if cert.Status != nil {
			status = store.CertificateInstallationStatus(*cert.Status)
		}

		certs = append(certs, &store.ChargeStationInstallCertificate{
			CertificateType:               store.CertificateType(cert.Type),
			CertificateId:                 certId,
			CertificateData:               cert.Certificate,
			CertificateInstallationStatus: status,
		})
	}

	err := s.store.UpdateChargeStationInstallCertificates(r.Context(), csId, &store.ChargeStationInstallCertificates{
		ChargeStationId: csId,
		Certificates:    certs,
	})
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}
}

func (s *Server) DeleteChargeStationCertificate(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(CertificateHashDataRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	err := s.store.SetChargeStationCertificateDeletion(r.Context(), csId, &store.ChargeStationCertificateDeletion{
		ChargeStationId: csId,
		HashAlgorithm:   string(req.CertificateHashData.HashAlgorithm),
		IssuerNameHash:  req.CertificateHashData.IssuerNameHash,
		IssuerKeyHash:   req.CertificateHashData.IssuerKeyHash,
		SerialNumber:    req.CertificateHashData.SerialNumber,
		DeletionStatus:  store.CertificateDeletionStatusPending,
		SendAfter:       s.clock.Now(),
	})
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	status := OperationResponseStatusPending
	resp := &OperationResponse{
		OperationId: &csId,
		Status:      &status,
	}
	w.WriteHeader(http.StatusAccepted)
	_ = render.Render(w, r, resp)
}

func (s *Server) UploadCertificate(w http.ResponseWriter, r *http.Request) {
	req := new(Certificate)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	err := s.store.SetCertificate(r.Context(), req.Certificate)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (s *Server) DeleteCertificate(w http.ResponseWriter, r *http.Request, certificateHash string) {
	err := s.store.DeleteCertificate(r.Context(), certificateHash)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) LookupCertificate(w http.ResponseWriter, r *http.Request, certificateHash string) {
	cert, err := s.store.LookupCertificate(r.Context(), certificateHash)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}
	if cert == "" {
		_ = render.Render(w, r, ErrNotFound)
		return
	}

	resp := &Certificate{
		Certificate: cert,
	}
	_ = render.Render(w, r, resp)
}

// Render implementations

func (c ChargeStationInstallCertificates) Bind(r *http.Request) error {
	return nil
}

// Render implementations

func (c CertificateHashDataRequest) Bind(_ *http.Request) error {
	return nil
}

// Render implementations

func (t Certificate) Bind(r *http.Request) error {
	return nil
}

func (t Certificate) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}

// Render implementations

func (o OperationResponse) Render(_ http.ResponseWriter, _ *http.Request) error {
	return nil
}
