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

type firestoreDiagnosticsRequest struct {
	ChargeStationId string  `firestore:"chargeStationId"`
	Location        string  `firestore:"location"`
	StartTime       *string `firestore:"startTime,omitempty"`
	StopTime        *string `firestore:"stopTime,omitempty"`
	Retries         *int    `firestore:"retries,omitempty"`
	RetryInterval   *int    `firestore:"retryInterval,omitempty"`
	Status          string  `firestore:"status"`
	SendAfter       string  `firestore:"sendAfter"`
}

func (s *Store) SetDiagnosticsRequest(ctx context.Context, chargeStationId string, request *store.DiagnosticsRequest) error {
	doc := &firestoreDiagnosticsRequest{
		ChargeStationId: chargeStationId,
		Location:        request.Location,
		Retries:         request.Retries,
		RetryInterval:   request.RetryInterval,
		Status:          string(request.Status),
		SendAfter:       request.SendAfter.Format(time.RFC3339),
	}

	if request.StartTime != nil {
		startTime := request.StartTime.Format(time.RFC3339)
		doc.StartTime = &startTime
	}
	if request.StopTime != nil {
		stopTime := request.StopTime.Format(time.RFC3339)
		doc.StopTime = &stopTime
	}

	_, err := s.client.Collection("DiagnosticsRequests").Doc(chargeStationId).Set(ctx, doc)
	if err != nil {
		return fmt.Errorf("setting diagnostics request for %s: %w", chargeStationId, err)
	}
	return nil
}

func (s *Store) GetDiagnosticsRequest(ctx context.Context, chargeStationId string) (*store.DiagnosticsRequest, error) {
	snap, err := s.client.Collection("DiagnosticsRequests").Doc(chargeStationId).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("getting diagnostics request for %s: %w", chargeStationId, err)
	}

	var doc firestoreDiagnosticsRequest
	if err := snap.DataTo(&doc); err != nil {
		return nil, fmt.Errorf("decoding diagnostics request for %s: %w", chargeStationId, err)
	}

	result := &store.DiagnosticsRequest{
		ChargeStationId: doc.ChargeStationId,
		Location:        doc.Location,
		Retries:         doc.Retries,
		RetryInterval:   doc.RetryInterval,
		Status:          store.DiagnosticsRequestStatus(doc.Status),
	}

	if doc.StartTime != nil {
		if startTime, err := time.Parse(time.RFC3339, *doc.StartTime); err == nil {
			result.StartTime = &startTime
		}
	}
	if doc.StopTime != nil {
		if stopTime, err := time.Parse(time.RFC3339, *doc.StopTime); err == nil {
			result.StopTime = &stopTime
		}
	}
	if sendAfter, err := time.Parse(time.RFC3339, doc.SendAfter); err == nil {
		result.SendAfter = sendAfter
	}

	return result, nil
}

func (s *Store) DeleteDiagnosticsRequest(ctx context.Context, chargeStationId string) error {
	_, err := s.client.Collection("DiagnosticsRequests").Doc(chargeStationId).Delete(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil
		}
		return fmt.Errorf("deleting diagnostics request for %s: %w", chargeStationId, err)
	}
	return nil
}

func (s *Store) ListDiagnosticsRequests(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.DiagnosticsRequest, error) {
	query := s.client.Collection("DiagnosticsRequests").
		OrderBy(firestore.DocumentID, firestore.Asc).
		Limit(pageSize)

	if previousChargeStationId != "" {
		query = query.StartAfter(previousChargeStationId)
	}

	iter := query.Documents(ctx)
	defer iter.Stop()

	var requests []*store.DiagnosticsRequest

	for {
		snap, err := iter.Next()
		if err != nil {
			if status.Code(err) == codes.NotFound {
				break
			}
			return nil, fmt.Errorf("listing diagnostics requests: %w", err)
		}

		var doc firestoreDiagnosticsRequest
		if err := snap.DataTo(&doc); err != nil {
			return nil, fmt.Errorf("decoding diagnostics request: %w", err)
		}

		result := &store.DiagnosticsRequest{
			ChargeStationId: doc.ChargeStationId,
			Location:        doc.Location,
			Retries:         doc.Retries,
			RetryInterval:   doc.RetryInterval,
			Status:          store.DiagnosticsRequestStatus(doc.Status),
		}

		if doc.StartTime != nil {
			if startTime, err := time.Parse(time.RFC3339, *doc.StartTime); err == nil {
				result.StartTime = &startTime
			}
		}
		if doc.StopTime != nil {
			if stopTime, err := time.Parse(time.RFC3339, *doc.StopTime); err == nil {
				result.StopTime = &stopTime
			}
		}
		if sendAfter, err := time.Parse(time.RFC3339, doc.SendAfter); err == nil {
			result.SendAfter = sendAfter
		}

		requests = append(requests, result)
	}

	return requests, nil
}

type firestoreLogRequest struct {
	ChargeStationId string  `firestore:"chargeStationId"`
	LogType         string  `firestore:"logType"`
	RequestId       int     `firestore:"requestId"`
	RemoteLocation  string  `firestore:"remoteLocation"`
	OldestTimestamp *string `firestore:"oldestTimestamp,omitempty"`
	LatestTimestamp *string `firestore:"latestTimestamp,omitempty"`
	Retries         *int    `firestore:"retries,omitempty"`
	RetryInterval   *int    `firestore:"retryInterval,omitempty"`
	Status          string  `firestore:"status"`
	SendAfter       string  `firestore:"sendAfter"`
}

func (s *Store) SetLogRequest(ctx context.Context, chargeStationId string, request *store.LogRequest) error {
	doc := &firestoreLogRequest{
		ChargeStationId: chargeStationId,
		LogType:         request.LogType,
		RequestId:       request.RequestId,
		RemoteLocation:  request.RemoteLocation,
		Retries:         request.Retries,
		RetryInterval:   request.RetryInterval,
		Status:          string(request.Status),
		SendAfter:       request.SendAfter.Format(time.RFC3339),
	}

	if request.OldestTimestamp != nil {
		oldestTimestamp := request.OldestTimestamp.Format(time.RFC3339)
		doc.OldestTimestamp = &oldestTimestamp
	}
	if request.LatestTimestamp != nil {
		latestTimestamp := request.LatestTimestamp.Format(time.RFC3339)
		doc.LatestTimestamp = &latestTimestamp
	}

	_, err := s.client.Collection("LogRequests").Doc(chargeStationId).Set(ctx, doc)
	if err != nil {
		return fmt.Errorf("setting log request for %s: %w", chargeStationId, err)
	}
	return nil
}

func (s *Store) GetLogRequest(ctx context.Context, chargeStationId string) (*store.LogRequest, error) {
	snap, err := s.client.Collection("LogRequests").Doc(chargeStationId).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("getting log request for %s: %w", chargeStationId, err)
	}

	var doc firestoreLogRequest
	if err := snap.DataTo(&doc); err != nil {
		return nil, fmt.Errorf("decoding log request for %s: %w", chargeStationId, err)
	}

	result := &store.LogRequest{
		ChargeStationId: doc.ChargeStationId,
		LogType:         doc.LogType,
		RequestId:       doc.RequestId,
		RemoteLocation:  doc.RemoteLocation,
		Retries:         doc.Retries,
		RetryInterval:   doc.RetryInterval,
		Status:          store.LogRequestStatus(doc.Status),
	}

	if doc.OldestTimestamp != nil {
		if oldestTimestamp, err := time.Parse(time.RFC3339, *doc.OldestTimestamp); err == nil {
			result.OldestTimestamp = &oldestTimestamp
		}
	}
	if doc.LatestTimestamp != nil {
		if latestTimestamp, err := time.Parse(time.RFC3339, *doc.LatestTimestamp); err == nil {
			result.LatestTimestamp = &latestTimestamp
		}
	}
	if sendAfter, err := time.Parse(time.RFC3339, doc.SendAfter); err == nil {
		result.SendAfter = sendAfter
	}

	return result, nil
}

func (s *Store) DeleteLogRequest(ctx context.Context, chargeStationId string) error {
	_, err := s.client.Collection("LogRequests").Doc(chargeStationId).Delete(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil
		}
		return fmt.Errorf("deleting log request for %s: %w", chargeStationId, err)
	}
	return nil
}

func (s *Store) ListLogRequests(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.LogRequest, error) {
	query := s.client.Collection("LogRequests").
		OrderBy(firestore.DocumentID, firestore.Asc).
		Limit(pageSize)

	if previousChargeStationId != "" {
		query = query.StartAfter(previousChargeStationId)
	}

	iter := query.Documents(ctx)
	defer iter.Stop()

	var requests []*store.LogRequest

	for {
		snap, err := iter.Next()
		if err != nil {
			if status.Code(err) == codes.NotFound {
				break
			}
			return nil, fmt.Errorf("listing log requests: %w", err)
		}

		var doc firestoreLogRequest
		if err := snap.DataTo(&doc); err != nil {
			return nil, fmt.Errorf("decoding log request: %w", err)
		}

		result := &store.LogRequest{
			ChargeStationId: doc.ChargeStationId,
			LogType:         doc.LogType,
			RequestId:       doc.RequestId,
			RemoteLocation:  doc.RemoteLocation,
			Retries:         doc.Retries,
			RetryInterval:   doc.RetryInterval,
			Status:          store.LogRequestStatus(doc.Status),
		}

		if doc.OldestTimestamp != nil {
			if oldestTimestamp, err := time.Parse(time.RFC3339, *doc.OldestTimestamp); err == nil {
				result.OldestTimestamp = &oldestTimestamp
			}
		}
		if doc.LatestTimestamp != nil {
			if latestTimestamp, err := time.Parse(time.RFC3339, *doc.LatestTimestamp); err == nil {
				result.LatestTimestamp = &latestTimestamp
			}
		}
		if sendAfter, err := time.Parse(time.RFC3339, doc.SendAfter); err == nil {
			result.SendAfter = sendAfter
		}

		requests = append(requests, result)
	}

	return requests, nil
}
