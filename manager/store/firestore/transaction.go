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

func (s *Store) CreateTransaction(ctx context.Context, chargeStationId, transactionId, idToken, tokenType string, meterValue []store.MeterValue, seqNo int, offline bool) error {
	transaction, err := s.FindTransaction(ctx, chargeStationId, transactionId)
	if err != nil {
		return fmt.Errorf("getting transaction: %w", err)
	}

	if transaction != nil {
		transaction.IdToken = idToken
		transaction.TokenType = tokenType
		transaction.MeterValues = append(transaction.MeterValues, meterValue...)
		transaction.StartSeqNo = seqNo
		transaction.Offline = offline
	} else {
		transaction = &store.Transaction{
			ChargeStationId:   chargeStationId,
			TransactionId:     transactionId,
			IdToken:           idToken,
			TokenType:         tokenType,
			MeterValues:       meterValue,
			StartSeqNo:        seqNo,
			EndedSeqNo:        0,
			UpdatedSeqNoCount: 0,
			Offline:           offline,
		}
	}

	return s.updateTransaction(ctx, chargeStationId, transactionId, transaction)
}

func (s *Store) FindTransaction(ctx context.Context, chargeStationId, transactionId string) (*store.Transaction, error) {
	transactionRef := s.client.Doc(getPath(chargeStationId, transactionId))
	snap, err := transactionRef.Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("lookup transaction %s/%s with code %v: %w", chargeStationId, transactionId, status.Code(err), err)
	}

	var transaction store.Transaction
	if err = snap.DataTo(&transaction); err != nil {
		return nil, fmt.Errorf("map transaction %s/%s: %w", chargeStationId, transactionId, err)
	}

	return &transaction, nil
}

func (s *Store) Transactions(ctx context.Context) ([]*store.Transaction, error) {
	transactionRefs, err := s.client.Collection("Transaction").Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("getting transactions: %w", err)
	}

	transactions := make([]*store.Transaction, 0, len(transactionRefs))
	for _, transactionRef := range transactionRefs {
		var transaction store.Transaction
		if err = transactionRef.DataTo(&transaction); err != nil {
			return nil, fmt.Errorf("map transaction %s: %w", transactionRef.Ref.ID, err)
		}
		transactions = append(transactions, &transaction)
	}

	return transactions, nil
}

func (s *Store) FindActiveTransaction(ctx context.Context, chargeStationId string) (*store.Transaction, error) {
	transactions, err := s.Transactions(ctx)
	if err != nil {
		return nil, err
	}

	for _, transaction := range transactions {
		if transaction.ChargeStationId == chargeStationId && transaction.EndedSeqNo == 0 {
			return transaction, nil
		}
	}

	return nil, nil
}

func (s *Store) UpdateTransaction(ctx context.Context, chargeStationId, transactionId string, meterValue []store.MeterValue) error {
	transaction, err := s.FindTransaction(ctx, chargeStationId, transactionId)
	if err != nil {
		return fmt.Errorf("getting transaction: %w", err)
	}

	if transaction == nil {
		transaction = &store.Transaction{
			ChargeStationId:   chargeStationId,
			TransactionId:     transactionId,
			MeterValues:       meterValue,
			UpdatedSeqNoCount: 1,
		}
	} else {
		transaction.MeterValues = append(transaction.MeterValues, meterValue...)
		transaction.UpdatedSeqNoCount++
	}

	return s.updateTransaction(ctx, chargeStationId, transactionId, transaction)
}

func (s *Store) EndTransaction(ctx context.Context, chargeStationId, transactionId, idToken, tokenType string, meterValue []store.MeterValue, seqNo int) error {
	transaction, err := s.FindTransaction(ctx, chargeStationId, transactionId)
	if err != nil {
		return fmt.Errorf("getting transaction: %w", err)
	}

	if transaction == nil {
		transaction = &store.Transaction{
			ChargeStationId: chargeStationId,
			TransactionId:   transactionId,
			IdToken:         idToken,
			TokenType:       tokenType,
			MeterValues:     meterValue,
			EndedSeqNo:      seqNo,
		}
	} else {
		transaction.MeterValues = append(transaction.MeterValues, meterValue...)
		transaction.EndedSeqNo = seqNo
	}

	return s.updateTransaction(ctx, chargeStationId, transactionId, transaction)
}

func (s *Store) UpdateTransactionCost(ctx context.Context, chargeStationId, transactionId string, totalCost float64) error {
	transaction, err := s.FindTransaction(ctx, chargeStationId, transactionId)
	if err != nil {
		return fmt.Errorf("finding transaction %s/%s: %w", chargeStationId, transactionId, err)
	}
	if transaction == nil {
		transaction = &store.Transaction{
			ChargeStationId: chargeStationId,
			TransactionId:   transactionId,
			LastCost:        &totalCost,
		}
	} else {
		cost := totalCost
		transaction.LastCost = &cost
	}
	return s.updateTransaction(ctx, chargeStationId, transactionId, transaction)
}

func (s *Store) updateTransaction(ctx context.Context, chargeStationId, transactionId string, transaction *store.Transaction) error {
	transactionRef := s.client.Doc(getPath(chargeStationId, transactionId))
	_, err := transactionRef.Set(ctx, transaction)
	if err != nil {
		return fmt.Errorf("setting transaction %s/%s: %w", chargeStationId, transactionId, err)
	}
	return nil
}

func (s *Store) ListTransactionsForChargeStation(ctx context.Context, chargeStationId, status string, startDate, endDate *time.Time, limit, offset int) ([]*store.Transaction, int64, error) {
	query := s.client.Collection("Transaction").Where("chargeStationId", "==", chargeStationId)

	docs, err := query.Documents(ctx).GetAll()
	if err != nil {
		return nil, 0, fmt.Errorf("querying transactions for %s: %w", chargeStationId, err)
	}

	var allTransactions []*store.Transaction
	for _, doc := range docs {
		var transaction store.Transaction
		if err := doc.DataTo(&transaction); err != nil {
			return nil, 0, fmt.Errorf("parsing transaction %s: %w", doc.Ref.ID, err)
		}

		isActive := transaction.EndedSeqNo == 0
		if status == "active" && !isActive {
			continue
		}
		if status == "completed" && isActive {
			continue
		}

		allTransactions = append(allTransactions, &transaction)
	}

	total := int64(len(allTransactions))

	start := offset
	if start > len(allTransactions) {
		start = len(allTransactions)
	}

	end := start + limit
	if end > len(allTransactions) {
		end = len(allTransactions)
	}

	result := allTransactions[start:end]
	return result, total, nil
}

func getPath(chargeStationId, transactionId string) string {
	return fmt.Sprintf("Transaction/%s-%s", chargeStationId, transactionId)
}
