// SPDX-License-Identifier: Apache-2.0

package firestore

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"google.golang.org/api/iterator"
)

const meterValuesCollection = "MeterValues"

type firestoreMeterValue struct {
	ChargeStationId string               `firestore:"chargeStationId"`
	EvseId          int                  `firestore:"evseId"`
	TransactionId   string               `firestore:"transactionId,omitempty"`
	Timestamp       string               `firestore:"timestamp"`
	SampledValues   []store.SampledValue `firestore:"sampledValues"`
	ReceivedAt      time.Time            `firestore:"receivedAt"`
}

// StoreMeterValues stores meter values received from a charge station.
func (s *Store) StoreMeterValues(ctx context.Context, chargeStationId string, evseId int, transactionId string, meterValues []store.MeterValue) error {
	for _, mv := range meterValues {
		doc := firestoreMeterValue{
			ChargeStationId: chargeStationId,
			EvseId:          evseId,
			TransactionId:   transactionId,
			Timestamp:       mv.Timestamp,
			SampledValues:   mv.SampledValues,
			ReceivedAt:      s.clock.Now(),
		}

		// Use charge station ID, EVSE ID, and timestamp as document ID for uniqueness
		docId := fmt.Sprintf("%s_%d_%s", chargeStationId, evseId, mv.Timestamp)
		_, err := s.client.Collection(meterValuesCollection).Doc(docId).Set(ctx, doc)
		if err != nil {
			return fmt.Errorf("storing meter value: %w", err)
		}
	}

	return nil
}

// GetMeterValues retrieves meter values for a specific charge station and EVSE.
func (s *Store) GetMeterValues(ctx context.Context, chargeStationId string, evseId int, limit int) ([]store.StoredMeterValue, error) {
	query := s.client.Collection(meterValuesCollection).
		Where("chargeStationId", "==", chargeStationId).
		Where("evseId", "==", evseId).
		OrderBy("timestamp", firestore.Desc)

	if limit > 0 {
		query = query.Limit(limit)
	}

	iter := query.Documents(ctx)
	defer iter.Stop()

	var result []store.StoredMeterValue
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("iterating meter values: %w", err)
		}

		var fmv firestoreMeterValue
		if err := doc.DataTo(&fmv); err != nil {
			return nil, fmt.Errorf("unmarshaling meter value: %w", err)
		}

		result = append(result, store.StoredMeterValue{
			ChargeStationId: fmv.ChargeStationId,
			EvseId:          fmv.EvseId,
			TransactionId:   fmv.TransactionId,
			MeterValue: store.MeterValue{
				Timestamp:     fmv.Timestamp,
				SampledValues: fmv.SampledValues,
			},
		})
	}

	return result, nil
}
