// SPDX-License-Identifier: Apache-2.0

package inmemory_test

import (
	"testing"

	"k8s.io/utils/clock"

	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/store/storetest"
)

func TestTokenGroupStore(t *testing.T) {
	storetest.TestTokenGroupStore(t, func(t *testing.T) store.Engine {
		return inmemory.NewStore(clock.RealClock{})
	})
}

func TestTransactionTariff(t *testing.T) {
	storetest.TestTransactionTariff(t, func(t *testing.T) store.Engine {
		return inmemory.NewStore(clock.RealClock{})
	})
}
