// SPDX-License-Identifier: Apache-2.0

package controlplane_test

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/controlplane"
)

func TestParseURN(t *testing.T) {
	t.Run("a charge station", func(t *testing.T) {
		urn, err := controlplane.ParseURN("urn:cpms:" + orgA + ":station/k7f2.CP001")
		require.NoError(t, err)
		assert.Equal(t, controlplane.URN{Product: "cpms", OrgId: orgA, Type: "station", Id: "k7f2.CP001"}, urn)
		assert.Equal(t, "urn:cpms:"+orgA+":station/k7f2.CP001", urn.String())
	})

	t.Run("the org in upper case", func(t *testing.T) {
		urn, err := controlplane.ParseURN("urn:cpms:0193B2C4-0000-7000-8000-0000000000A1:station/k7f2.CP001")
		require.NoError(t, err)
		assert.Equal(t, orgA, urn.OrgId)
	})

	t.Run("an id with a colon or a slash", func(t *testing.T) {
		urn, err := controlplane.ParseURN("urn:cpms:" + orgA + ":station/k7f2.CP:01/x")
		require.NoError(t, err)
		assert.Equal(t, "station", urn.Type)
		assert.Equal(t, "k7f2.CP:01/x", urn.Id)
	})

	t.Run("another type", func(t *testing.T) {
		urn, err := controlplane.ParseURN("urn:cpms:" + orgA + ":location/0193b2c5-0000-7000-8000-000000000001")
		require.NoError(t, err)
		assert.Equal(t, "location", urn.Type)
	})

	t.Run("the escaped form, once unescaped", func(t *testing.T) {
		escaped := "urn%3Acpms%3A" + orgA + "%3Astation%2Fk7f2.CP001"
		_, err := controlplane.ParseURN(escaped)
		assert.Error(t, err, "the URN of a path is unescaped first")
		unescaped, err := url.PathUnescape(escaped)
		require.NoError(t, err)
		urn, err := controlplane.ParseURN(unescaped)
		require.NoError(t, err)
		assert.Equal(t, "k7f2.CP001", urn.Id)
	})

	for name, text := range map[string]string{
		"empty":                  "",
		"not a URN":              "cpms:" + orgA + ":station/k7f2.CP001",
		"another product":        "urn:ocpi:" + orgA + ":station/k7f2.CP001",
		"another scheme":         "uri:cpms:" + orgA + ":station/k7f2.CP001",
		"no org":                 "urn:cpms:station/k7f2.CP001",
		"an org that is no UUID": "urn:cpms:acme:station/k7f2.CP001",
		"a UUID without hyphens": "urn:cpms:0193b2c400007000800000000000000a1:station/k7f2.CP001",
		"no type":                "urn:cpms:" + orgA + ":k7f2.CP001",
		"an empty type":          "urn:cpms:" + orgA + ":/k7f2.CP001",
		"no id":                  "urn:cpms:" + orgA + ":station/",
		"no id and no slash":     "urn:cpms:" + orgA + ":station",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := controlplane.ParseURN(text)
			assert.EqualError(t, err, "must be a URN of the form urn:cpms:<org_id>:<type>/<id>")
		})
	}
}
