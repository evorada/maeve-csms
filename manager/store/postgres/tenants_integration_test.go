// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"testing"

	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/storetest"
)

func TestTenantStore(t *testing.T) {
	storetest.TestTenantStore(t, func(t *testing.T) store.TenantStore {
		truncateAll(t)
		t.Cleanup(func() { truncateAll(t) })
		return testStore
	})
}
