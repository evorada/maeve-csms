// SPDX-License-Identifier: Apache-2.0

package tenancy_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
)

func TestStationKeyRoundTrip(t *testing.T) {
	key := tenancy.StationKey("k7f2", "CP001")
	assert.Equal(t, "k7f2.CP001", key)

	orgKey, stationId, ok := tenancy.SplitStationKey(key)
	assert.True(t, ok)
	assert.Equal(t, "k7f2", orgKey)
	assert.Equal(t, "CP001", stationId)
}

func TestStationKeyWithDotInStationId(t *testing.T) {
	key := tenancy.StationKey("k7f2", "CP.001")
	assert.Equal(t, "k7f2.CP.001", key)

	// only the first "." separates: the org key never contains one
	orgKey, stationId, ok := tenancy.SplitStationKey(key)
	assert.True(t, ok)
	assert.Equal(t, "k7f2", orgKey)
	assert.Equal(t, "CP.001", stationId)
}

func TestSplitStationKeyWithoutDot(t *testing.T) {
	orgKey, stationId, ok := tenancy.SplitStationKey("CP001")
	assert.False(t, ok)
	assert.Equal(t, "", orgKey)
	assert.Equal(t, "CP001", stationId)
}

func TestStationKeyWithoutOrgKey(t *testing.T) {
	assert.Equal(t, "CP001", tenancy.StationKey("", "CP001"))
}
