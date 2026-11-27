// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchPathResolvesAParameterFollowedByAnAction(t *testing.T) {
	swagger, err := GetSwagger()
	require.NoError(t, err)
	const commandId = "0193b2c4-0000-7000-8000-000000000001"

	tests := map[string]struct {
		method, target string
		wantPath       string // "" when no path of the spec matches
		wantParams     map[string]string
	}{
		"the action":                {http.MethodPost, "/commands/" + commandId + ":cancel", "/commands/{commandId}:cancel", map[string]string{"commandId": commandId}},
		"a plain id":                {http.MethodGet, "/commands/" + commandId, "/commands/{commandId}", map[string]string{"commandId": commandId}},
		"another action":            {http.MethodPost, "/commands/" + commandId + ":archive", "/commands/{commandId}", map[string]string{"commandId": commandId + ":archive"}},
		"the action without an id":  {http.MethodPost, "/commands/:cancel", "/commands/{commandId}", map[string]string{"commandId": ":cancel"}},
		"a charge station":          {http.MethodPost, "/cs/aaaa.CP001/commands", "/cs/{csId}/commands", map[string]string{"csId": "aaaa.CP001"}},
		"a path that is not served": {http.MethodPost, "/commands/" + commandId + ":cancel/now", "", nil},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			path, params := matchPath(httptest.NewRequest(test.method, test.target, nil), swagger)
			if test.wantPath == "" {
				assert.Nil(t, path)
				return
			}
			require.NotNil(t, path)
			assert.Equal(t, test.wantPath, *path)
			assert.Equal(t, test.wantParams, params)
		})
	}
}

func TestMatchParamSegment(t *testing.T) {
	tests := []struct {
		spec, request string
		wantName      string
		wantValue     string
		wantOk        bool
	}{
		{"{id}", "abc", "id", "abc", true},
		{"{id}", "abc:cancel", "id", "abc:cancel", true},
		{"{id}:cancel", "abc:cancel", "id", "abc", true},
		{"{id}:cancel", "abc", "", "", false},
		{"{id}:cancel", ":cancel", "", "", false},
		{"{id}:cancel", "abc:cancelled", "", "", false},
		{"{id}.json", "abc.json", "", "", false},
		{"id", "id", "", "", false},
		{"{}", "abc", "", "", false},
		{"{id", "abc", "", "", false},
	}
	for _, test := range tests {
		name, value, ok := matchParamSegment(test.spec, test.request)
		assert.Equal(t, test.wantOk, ok, "%s %s", test.spec, test.request)
		assert.Equal(t, test.wantName, name, "%s %s", test.spec, test.request)
		assert.Equal(t, test.wantValue, value, "%s %s", test.spec, test.request)
	}
}
