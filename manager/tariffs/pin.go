// SPDX-License-Identifier: Apache-2.0

package tariffs

import (
	"context"
	"errors"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// SelectForStart is Select for a transaction that is starting. A transaction does not
// wait for its tariff: the selection gets at most SelectTimeout and gives an error after
// that, and the caller starts the transaction without a tariff.
func (s *Selector) SelectForStart(ctx context.Context, in SelectInput) (Choice, error) {
	ctx, cancel := context.WithTimeout(ctx, SelectTimeout)
	defer cancel()
	return s.Select(ctx, in)
}

// Pin stores the choice as the tariff of the transaction: the tariff version that the
// transaction is charged with from then on, whatever happens to the tariff later. It
// reports whether the transaction has the choice now. It has not, without an error, when
// the storage backend keeps no tariff with a transaction, and when the transaction has
// its tariff already: the tariff of a transaction is chosen once.
func Pin(ctx context.Context, transactions store.TransactionStore, stationKey, transactionId string, choice Choice) (bool, error) {
	var tariffId *string
	var version *int
	if choice.TariffId != "" {
		tariffId, version = &choice.TariffId, &choice.Version
	}
	err := transactions.SetTransactionTariff(ctx, stationKey, transactionId, tariffId, version, string(choice.Source))
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, store.ErrNotImplemented), errors.Is(err, store.ErrAlreadyExists):
		return false, nil
	default:
		return false, err
	}
}
