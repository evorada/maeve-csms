// SPDX-License-Identifier: Apache-2.0

package firestore

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type connectorStatus struct {
	ChargeStationId      string     `firestore:"chargeStationId"`
	ConnectorId          int        `firestore:"connectorId"`
	Status               string     `firestore:"status"`
	ErrorCode            string     `firestore:"errorCode"`
	Info                 *string    `firestore:"info,omitempty"`
	Timestamp            *time.Time `firestore:"timestamp,omitempty"`
	VendorErrorCode      *string    `firestore:"vendorErrorCode,omitempty"`
	VendorId             *string    `firestore:"vendorId,omitempty"`
	CurrentTransactionId *string    `firestore:"currentTransactionId,omitempty"`
	UpdatedAt            time.Time  `firestore:"updatedAt"`
}

type chargeStationStatus struct {
	ChargeStationId string     `firestore:"chargeStationId"`
	Connected       bool       `firestore:"connected"`
	LastHeartbeat   *time.Time `firestore:"lastHeartbeat,omitempty"`
	FirmwareVersion *string    `firestore:"firmwareVersion,omitempty"`
	Model           *string    `firestore:"model,omitempty"`
	Vendor          *string    `firestore:"vendor,omitempty"`
	SerialNumber    *string    `firestore:"serialNumber,omitempty"`
	UpdatedAt       time.Time  `firestore:"updatedAt"`
}

func (s *Store) SetConnectorStatus(ctx context.Context, chargeStationId string, connectorId int, status *store.ConnectorStatus) error {
	docRef := s.client.Doc(fmt.Sprintf("ConnectorStatus/%s_%d", chargeStationId, connectorId))

	data := &connectorStatus{
		ChargeStationId:      chargeStationId,
		ConnectorId:          connectorId,
		Status:               string(status.Status),
		ErrorCode:            string(status.ErrorCode),
		Info:                 status.Info,
		Timestamp:            status.Timestamp,
		VendorErrorCode:      status.VendorErrorCode,
		VendorId:             status.VendorId,
		CurrentTransactionId: status.CurrentTransactionId,
		UpdatedAt:            s.clock.Now(),
	}

	_, err := docRef.Set(ctx, data)
	if err != nil {
		return fmt.Errorf("setting connector status for %s connector %d: %w", chargeStationId, connectorId, err)
	}
	return nil
}

func (s *Store) GetConnectorStatus(ctx context.Context, chargeStationId string, connectorId int) (*store.ConnectorStatus, error) {
	docRef := s.client.Doc(fmt.Sprintf("ConnectorStatus/%s_%d", chargeStationId, connectorId))
	snap, err := docRef.Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, fmt.Errorf("connector %d on charge station %s not found", connectorId, chargeStationId)
		}
		return nil, fmt.Errorf("getting connector status for %s connector %d: %w", chargeStationId, connectorId, err)
	}

	var data connectorStatus
	if err := snap.DataTo(&data); err != nil {
		return nil, fmt.Errorf("unmarshaling connector status: %w", err)
	}

	return &store.ConnectorStatus{
		ChargeStationId:      data.ChargeStationId,
		ConnectorId:          data.ConnectorId,
		Status:               store.ConnectorStatusType(data.Status),
		ErrorCode:            store.ConnectorErrorCode(data.ErrorCode),
		Info:                 data.Info,
		Timestamp:            data.Timestamp,
		VendorErrorCode:      data.VendorErrorCode,
		VendorId:             data.VendorId,
		CurrentTransactionId: data.CurrentTransactionId,
		UpdatedAt:            data.UpdatedAt,
	}, nil
}

func (s *Store) ListConnectorStatuses(ctx context.Context, chargeStationId string) ([]*store.ConnectorStatus, error) {
	query := s.client.Collection("ConnectorStatus").
		Where("chargeStationId", "==", chargeStationId).
		OrderBy("connectorId", firestore.Asc)

	iter := query.Documents(ctx)
	defer iter.Stop()

	var statuses []*store.ConnectorStatus
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("iterating connector statuses for %s: %w", chargeStationId, err)
		}

		var data connectorStatus
		if err := doc.DataTo(&data); err != nil {
			return nil, fmt.Errorf("unmarshaling connector status: %w", err)
		}

		statuses = append(statuses, &store.ConnectorStatus{
			ChargeStationId:      data.ChargeStationId,
			ConnectorId:          data.ConnectorId,
			Status:               store.ConnectorStatusType(data.Status),
			ErrorCode:            store.ConnectorErrorCode(data.ErrorCode),
			Info:                 data.Info,
			Timestamp:            data.Timestamp,
			VendorErrorCode:      data.VendorErrorCode,
			VendorId:             data.VendorId,
			CurrentTransactionId: data.CurrentTransactionId,
			UpdatedAt:            data.UpdatedAt,
		})
	}

	return statuses, nil
}

func (s *Store) SetChargeStationStatus(ctx context.Context, chargeStationId string, status *store.ChargeStationStatus) error {
	docRef := s.client.Doc(fmt.Sprintf("ChargeStationStatus/%s", chargeStationId))

	data := &chargeStationStatus{
		ChargeStationId: chargeStationId,
		Connected:       status.Connected,
		LastHeartbeat:   status.LastHeartbeat,
		FirmwareVersion: status.FirmwareVersion,
		Model:           status.Model,
		Vendor:          status.Vendor,
		SerialNumber:    status.SerialNumber,
		UpdatedAt:       s.clock.Now(),
	}

	_, err := docRef.Set(ctx, data)
	if err != nil {
		return fmt.Errorf("setting charge station status for %s: %w", chargeStationId, err)
	}
	return nil
}

func (s *Store) GetChargeStationStatus(ctx context.Context, chargeStationId string) (*store.ChargeStationStatus, error) {
	docRef := s.client.Doc(fmt.Sprintf("ChargeStationStatus/%s", chargeStationId))
	snap, err := docRef.Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, fmt.Errorf("charge station %s not found", chargeStationId)
		}
		return nil, fmt.Errorf("getting charge station status for %s: %w", chargeStationId, err)
	}

	var data chargeStationStatus
	if err := snap.DataTo(&data); err != nil {
		return nil, fmt.Errorf("unmarshaling charge station status: %w", err)
	}

	return &store.ChargeStationStatus{
		ChargeStationId: data.ChargeStationId,
		Connected:       data.Connected,
		LastHeartbeat:   data.LastHeartbeat,
		FirmwareVersion: data.FirmwareVersion,
		Model:           data.Model,
		Vendor:          data.Vendor,
		SerialNumber:    data.SerialNumber,
		UpdatedAt:       data.UpdatedAt,
	}, nil
}

func (s *Store) UpdateHeartbeat(ctx context.Context, chargeStationId string, timestamp time.Time) error {
	docRef := s.client.Doc(fmt.Sprintf("ChargeStationStatus/%s", chargeStationId))

	updates := []firestore.Update{
		{Path: "lastHeartbeat", Value: timestamp},
		{Path: "connected", Value: true},
		{Path: "updatedAt", Value: s.clock.Now()},
	}

	_, err := docRef.Update(ctx, updates)
	if err != nil {
		// If document doesn't exist, create it
		if status.Code(err) == codes.NotFound {
			data := &chargeStationStatus{
				ChargeStationId: chargeStationId,
				Connected:       true,
				LastHeartbeat:   &timestamp,
				UpdatedAt:       s.clock.Now(),
			}
			_, err = docRef.Set(ctx, data)
			if err != nil {
				return fmt.Errorf("creating charge station status for heartbeat update: %w", err)
			}
			return nil
		}
		return fmt.Errorf("updating heartbeat for %s: %w", chargeStationId, err)
	}
	return nil
}
