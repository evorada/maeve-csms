// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	"k8s.io/utils/clock"
)

func TestGetLocalListVersionResultHandler(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.GetLocalListVersionResultHandler{Store: engine}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, `test`)
		defer span.End()

		req := &types.GetLocalListVersionRequestJson{}
		resp := &types.GetLocalListVersionResponseJson{
			VersionNumber: 42,
		}

		err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
		require.NoError(t, err)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"get_local_list_version.version_number": 42,
	})

	version, err := engine.GetLocalListVersion(ctx, "cs001")
	require.NoError(t, err)
	assert.Equal(t, 42, version)
}
