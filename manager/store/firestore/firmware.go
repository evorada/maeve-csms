// SPDX-License-Identifier: Apache-2.0

package firestore

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type firestoreFirmwareUpdateStatus struct {
	ChargeStationId string `firestore:"chargeStationId"`
	Status          string `firestore:"status"`
	Location        string `firestore:"location"`
	RetrieveDate    string `firestore:"retrieveDate"`
	RetryCount      int    `firestore:"retryCount"`
	UpdatedAt       string `firestore:"updatedAt"`
}

type firestoreDiagnosticsStatus struct {
	ChargeStationId string `firestore:"chargeStationId"`
	Status          string `firestore:"status"`
	Location        string `firestore:"location"`
	UpdatedAt       string `firestore:"updatedAt"`
}

func (s *Store) SetFirmwareUpdateStatus(ctx context.Context, chargeStationId string, fwStatus *store.FirmwareUpdateStatus) error {
	doc := &firestoreFirmwareUpdateStatus{
		ChargeStationId: chargeStationId,
		Status:          string(fwStatus.Status),
		Location:        fwStatus.Location,
		RetrieveDate:    fwStatus.RetrieveDate.Format(time.RFC3339),
		RetryCount:      fwStatus.RetryCount,
		UpdatedAt:       fwStatus.UpdatedAt.Format(time.RFC3339),
	}

	_, err := s.client.Collection("FirmwareUpdateStatus").Doc(chargeStationId).Set(ctx, doc)
	if err != nil {
		return fmt.Errorf("setting firmware update status for %s: %w", chargeStationId, err)
	}
	return nil
}

func (s *Store) GetFirmwareUpdateStatus(ctx context.Context, chargeStationId string) (*store.FirmwareUpdateStatus, error) {
	snap, err := s.client.Collection("FirmwareUpdateStatus").Doc(chargeStationId).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("getting firmware update status for %s: %w", chargeStationId, err)
	}

	var doc firestoreFirmwareUpdateStatus
	if err := snap.DataTo(&doc); err != nil {
		return nil, fmt.Errorf("decoding firmware update status for %s: %w", chargeStationId, err)
	}

	retrieveDate, _ := time.Parse(time.RFC3339, doc.RetrieveDate)
	updatedAt, _ := time.Parse(time.RFC3339, doc.UpdatedAt)

	return &store.FirmwareUpdateStatus{
		ChargeStationId: doc.ChargeStationId,
		Status:          store.FirmwareUpdateStatusType(doc.Status),
		Location:        doc.Location,
		RetrieveDate:    retrieveDate,
		RetryCount:      doc.RetryCount,
		UpdatedAt:       updatedAt,
	}, nil
}

func (s *Store) SetDiagnosticsStatus(ctx context.Context, chargeStationId string, diagStatus *store.DiagnosticsStatus) error {
	doc := &firestoreDiagnosticsStatus{
		ChargeStationId: chargeStationId,
		Status:          string(diagStatus.Status),
		Location:        diagStatus.Location,
		UpdatedAt:       diagStatus.UpdatedAt.Format(time.RFC3339),
	}

	_, err := s.client.Collection("DiagnosticsStatus").Doc(chargeStationId).Set(ctx, doc)
	if err != nil {
		return fmt.Errorf("setting diagnostics status for %s: %w", chargeStationId, err)
	}
	return nil
}

func (s *Store) GetDiagnosticsStatus(ctx context.Context, chargeStationId string) (*store.DiagnosticsStatus, error) {
	snap, err := s.client.Collection("DiagnosticsStatus").Doc(chargeStationId).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("getting diagnostics status for %s: %w", chargeStationId, err)
	}

	var doc firestoreDiagnosticsStatus
	if err := snap.DataTo(&doc); err != nil {
		return nil, fmt.Errorf("decoding diagnostics status for %s: %w", chargeStationId, err)
	}

	updatedAt, _ := time.Parse(time.RFC3339, doc.UpdatedAt)

	return &store.DiagnosticsStatus{
		ChargeStationId: doc.ChargeStationId,
		Status:          store.DiagnosticsStatusType(doc.Status),
		Location:        doc.Location,
		UpdatedAt:       updatedAt,
	}, nil
}

type firestorePublishFirmwareStatus struct {
	ChargeStationId string `firestore:"chargeStationId"`
	Status          string `firestore:"status"`
	Location        string `firestore:"location"`
	Checksum        string `firestore:"checksum"`
	RequestId       int    `firestore:"requestId"`
	UpdatedAt       string `firestore:"updatedAt"`
}

func (s *Store) SetPublishFirmwareStatus(ctx context.Context, chargeStationId string, pubStatus *store.PublishFirmwareStatus) error {
	doc := &firestorePublishFirmwareStatus{
		ChargeStationId: chargeStationId,
		Status:          string(pubStatus.Status),
		Location:        pubStatus.Location,
		Checksum:        pubStatus.Checksum,
		RequestId:       pubStatus.RequestId,
		UpdatedAt:       pubStatus.UpdatedAt.Format(time.RFC3339),
	}

	_, err := s.client.Collection("PublishFirmwareStatus").Doc(chargeStationId).Set(ctx, doc)
	if err != nil {
		return fmt.Errorf("setting publish firmware status for %s: %w", chargeStationId, err)
	}
	return nil
}

func (s *Store) GetPublishFirmwareStatus(ctx context.Context, chargeStationId string) (*store.PublishFirmwareStatus, error) {
	snap, err := s.client.Collection("PublishFirmwareStatus").Doc(chargeStationId).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("getting publish firmware status for %s: %w", chargeStationId, err)
	}

	var doc firestorePublishFirmwareStatus
	if err := snap.DataTo(&doc); err != nil {
		return nil, fmt.Errorf("decoding publish firmware status for %s: %w", chargeStationId, err)
	}

	updatedAt, _ := time.Parse(time.RFC3339, doc.UpdatedAt)

	return &store.PublishFirmwareStatus{
		ChargeStationId: doc.ChargeStationId,
		Status:          store.PublishFirmwareStatusType(doc.Status),
		Location:        doc.Location,
		Checksum:        doc.Checksum,
		RequestId:       doc.RequestId,
		UpdatedAt:       updatedAt,
	}, nil
}

// Ensure firestore.Store still satisfies the interface at compile time
var _ store.FirmwareStore = (*Store)(nil)

type firestoreFirmwareUpdateRequest struct {
	ChargeStationId    string  `firestore:"chargeStationId"`
	Location           string  `firestore:"location"`
	RetrieveDate       *string `firestore:"retrieveDate,omitempty"`
	Retries            *int    `firestore:"retries,omitempty"`
	RetryInterval      *int    `firestore:"retryInterval,omitempty"`
	Signature          *string `firestore:"signature,omitempty"`
	SigningCertificate *string `firestore:"signingCertificate,omitempty"`
	Status             string  `firestore:"status"`
	SendAfter          string  `firestore:"sendAfter"`
}

// FirmwareUpdateRequest store methods
func (s *Store) SetFirmwareUpdateRequest(ctx context.Context, chargeStationId string, request *store.FirmwareUpdateRequest) error {
	doc := &firestoreFirmwareUpdateRequest{
		ChargeStationId:    chargeStationId,
		Location:           request.Location,
		Retries:            request.Retries,
		RetryInterval:      request.RetryInterval,
		Signature:          request.Signature,
		SigningCertificate: request.SigningCertificate,
		Status:             string(request.Status),
		SendAfter:          request.SendAfter.Format(time.RFC3339),
	}

	if request.RetrieveDate != nil {
		retrieveDate := request.RetrieveDate.Format(time.RFC3339)
		doc.RetrieveDate = &retrieveDate
	}

	_, err := s.client.Collection("FirmwareUpdateRequests").Doc(chargeStationId).Set(ctx, doc)
	if err != nil {
		return fmt.Errorf("setting firmware update request for %s: %w", chargeStationId, err)
	}
	return nil
}

func (s *Store) GetFirmwareUpdateRequest(ctx context.Context, chargeStationId string) (*store.FirmwareUpdateRequest, error) {
	snap, err := s.client.Collection("FirmwareUpdateRequests").Doc(chargeStationId).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("getting firmware update request for %s: %w", chargeStationId, err)
	}

	var doc firestoreFirmwareUpdateRequest
	if err := snap.DataTo(&doc); err != nil {
		return nil, fmt.Errorf("decoding firmware update request for %s: %w", chargeStationId, err)
	}

	result := &store.FirmwareUpdateRequest{
		ChargeStationId:    doc.ChargeStationId,
		Location:           doc.Location,
		Retries:            doc.Retries,
		RetryInterval:      doc.RetryInterval,
		Signature:          doc.Signature,
		SigningCertificate: doc.SigningCertificate,
		Status:             store.FirmwareUpdateRequestStatus(doc.Status),
	}

	if doc.RetrieveDate != nil {
		if retrieveDate, err := time.Parse(time.RFC3339, *doc.RetrieveDate); err == nil {
			result.RetrieveDate = &retrieveDate
		}
	}

	if sendAfter, err := time.Parse(time.RFC3339, doc.SendAfter); err == nil {
		result.SendAfter = sendAfter
	}

	return result, nil
}

func (s *Store) DeleteFirmwareUpdateRequest(ctx context.Context, chargeStationId string) error {
	_, err := s.client.Collection("FirmwareUpdateRequests").Doc(chargeStationId).Delete(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil
		}
		return fmt.Errorf("deleting firmware update request for %s: %w", chargeStationId, err)
	}
	return nil
}

func (s *Store) ListFirmwareUpdateRequests(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.FirmwareUpdateRequest, error) {
	query := s.client.Collection("FirmwareUpdateRequests").
		OrderBy(firestore.DocumentID, firestore.Asc).
		Limit(pageSize)

	if previousChargeStationId != "" {
		query = query.StartAfter(previousChargeStationId)
	}

	iter := query.Documents(ctx)
	defer iter.Stop()

	var requests []*store.FirmwareUpdateRequest

	for {
		snap, err := iter.Next()
		if err != nil {
			if status.Code(err) == codes.NotFound {
				break
			}
			return nil, fmt.Errorf("listing firmware update requests: %w", err)
		}

		var doc firestoreFirmwareUpdateRequest
		if err := snap.DataTo(&doc); err != nil {
			return nil, fmt.Errorf("decoding firmware update request: %w", err)
		}

		result := &store.FirmwareUpdateRequest{
			ChargeStationId:    doc.ChargeStationId,
			Location:           doc.Location,
			Retries:            doc.Retries,
			RetryInterval:      doc.RetryInterval,
			Signature:          doc.Signature,
			SigningCertificate: doc.SigningCertificate,
			Status:             store.FirmwareUpdateRequestStatus(doc.Status),
		}

		if doc.RetrieveDate != nil {
			if retrieveDate, err := time.Parse(time.RFC3339, *doc.RetrieveDate); err == nil {
				result.RetrieveDate = &retrieveDate
			}
		}

		if sendAfter, err := time.Parse(time.RFC3339, doc.SendAfter); err == nil {
			result.SendAfter = sendAfter
		}

		requests = append(requests, result)
	}

	return requests, nil
}
