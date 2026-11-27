// SPDX-License-Identifier: Apache-2.0

package inmemory_test

import (
	"testing"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/store/storetest"
	clockTest "k8s.io/utils/clock/testing"
)

func TestConnectivityStore(t *testing.T) {
	storetest.TestConnectivityStore(t, func(t *testing.T) store.Engine {
		return inmemory.NewStore(clockTest.NewFakePassiveClock(time.Now()))
	})
}
