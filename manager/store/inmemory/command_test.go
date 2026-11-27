// SPDX-License-Identifier: Apache-2.0

package inmemory_test

import (
	"testing"

	"k8s.io/utils/clock"

	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/store/storetest"
)

func TestCommandStore(t *testing.T) {
	storetest.TestCommandStore(t, func(t *testing.T) store.Engine {
		return inmemory.NewStore(clock.RealClock{})
	})
}
