// SPDX-License-Identifier: Apache-2.0

package storetest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// TestCountConnectorsForOrgs checks how a storage backend that stores orgs counts the
// connectors of orgs.
func TestCountConnectorsForOrgs(t *testing.T, newStore func(t *testing.T) store.Engine) {
	ctx := context.Background()
	s := newStore(t)

	report := func(csId string, connectorIds ...int) {
		t.Helper()
		for _, connectorId := range connectorIds {
			require.NoError(t, s.SetConnectorStatus(ctx, csId, connectorId, &store.ConnectorStatus{
				ChargeStationId: csId,
				ConnectorId:     connectorId,
				Status:          store.ConnectorStatusAvailable,
				ErrorCode:       store.ConnectorErrorCodeNoError,
			}))
		}
	}
	count := func(orgIds ...string) int {
		t.Helper()
		got, err := s.CountConnectorsForOrgs(ctx, orgIds)
		require.NoError(t, err)
		return got
	}

	// connector 0 is the charge station as a whole
	for csId, orgId := range map[string]string{"aaaa.CP001": orgA, "aaaa.CP002": orgA, "bbbb.CP001": orgB} {
		require.NoError(t, s.SetChargeStationAuth(ctx, csId, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: orgId}))
	}
	report("aaaa.CP001", 0, 1)
	report("aaaa.CP002", 0, 2)
	report("bbbb.CP001", 1)
	// a charge station that reports without being registered belongs to no org
	report("zzzz.CP009", 1, 2)

	assert.Equal(t, 2, count(orgA))
	assert.Equal(t, 1, count(orgB))
	assert.Equal(t, 3, count(orgA, orgB))
	// no charge station is registered for org C
	assert.Equal(t, 3, count(orgB, orgC, orgA))
	assert.Equal(t, 0, count(orgC))
	assert.Equal(t, 0, count())
	assert.Equal(t, 2, count(strings.ToUpper(orgA)), "an org id is a UUID in any case")

	t.Run("a connector that reports again is counted once", func(t *testing.T) {
		report("aaaa.CP001", 1)
		assert.Equal(t, 2, count(orgA))
	})

	t.Run("a new connector is counted", func(t *testing.T) {
		report("bbbb.CP001", 2)
		assert.Equal(t, 2, count(orgB))
		assert.Equal(t, 4, count(orgA, orgB))
	})

	t.Run("a charge station that is registered without connectors counts nothing", func(t *testing.T) {
		require.NoError(t, s.SetChargeStationAuth(ctx, "aaaa.CP003", &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: orgA}))
		assert.Equal(t, 2, count(orgA))
	})

	t.Run("the connectors of a charge station that is not active are not counted", func(t *testing.T) {
		// a station that connected without being registered reports whatever it likes
		_, err := s.RecordOnboardingConnection(ctx, "aaaa.NEW01", orgA, store.OnboardingTransportWs, store.OnboardingCredentialOpen, 50, time.Now())
		require.NoError(t, err)
		report("aaaa.NEW01", 1, 2, 3, 4, 5, 6, 7, 8)
		require.NoError(t, s.SetChargeStationAuth(ctx, "aaaa.CP004", &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: orgA}))
		report("aaaa.CP004", 1, 2)
		require.NoError(t, s.SetChargeStationState(ctx, "aaaa.CP004", store.StationStateDecommissioned, nil))
		assert.Equal(t, 2, count(orgA))

		// once it is active its connectors count
		require.NoError(t, s.SetChargeStationState(ctx, "aaaa.NEW01", store.StationStateActive, nil))
		assert.Equal(t, 10, count(orgA))
	})
}
