// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"testing"

	"github.com/thoughtworks/maeve-csms/manager/store/storetest"
)

func TestInventoryStore(t *testing.T) {
	storetest.TestInventoryStore(t, emptyTestStore)
}
