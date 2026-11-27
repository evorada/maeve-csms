// SPDX-License-Identifier: Apache-2.0

package events_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/events"
)

func TestStaticOrgResolver(t *testing.T) {
	ctx := context.Background()

	orgId, err := events.StaticOrgResolver{OrgId: testOrgId}.OrgIdForStation(ctx, "any.station")
	require.NoError(t, err)
	assert.Equal(t, testOrgId, orgId)

	_, err = events.StaticOrgResolver{}.OrgIdForStation(ctx, "any.station")
	assert.ErrorIs(t, err, events.ErrUnknownOrg)
}
