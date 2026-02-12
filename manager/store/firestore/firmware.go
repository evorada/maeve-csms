// SPDX-License-Identifier: Apache-2.0

package firestore

import (
	"context"
	"fmt"
	"time"

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

// Ensure firestore.Store still satisfies the interface at compile time
var _ store.FirmwareStore = (*Store)(nil)
