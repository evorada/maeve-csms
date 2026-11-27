// SPDX-License-Identifier: Apache-2.0

package inmemory_test

import (
	"testing"

	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/store/storetest"
	"k8s.io/utils/clock"
)

func TestOrgOwnership(t *testing.T) {
	storetest.TestOrgOwnership(t, func(t *testing.T) store.Engine {
		return inmemory.NewStore(clock.RealClock{})
	})
}

func TestStationKeys(t *testing.T) {
	storetest.TestStationKeys(t, func(t *testing.T) store.Engine {
		return inmemory.NewStore(clock.RealClock{})
	})
}
