// SPDX-License-Identifier: Apache-2.0

package controlplane_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	clocktesting "k8s.io/utils/clock/testing"
)

// inventoryTime is when the tests of the inventory read it.
var inventoryTime = time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)

// inventoryFixture is a control plane that serves the inventories of the tenants of
// addTenants, at inventoryTime:
//   - orgTop: the active station topp.CP001
//   - orgA: the active station aaaa.CP001 at its location plaza, with EVSE 1 (one
//     connector) and EVSE 2 (two connectors); the decommissioned station aaaa.CP002; the
//     valid token ES*AAA*TAG1 and the token ES*AAA*TAG2 that is not valid
//   - orgB: the active station bbbb.CP001
type inventoryFixture struct {
	*fixture
	plaza *store.AssetLocation
	evses []*store.AssetEvse
}

func newInventoryFixture(t *testing.T) *inventoryFixture {
	t.Helper()
	ctx := context.Background()
	f := &inventoryFixture{fixture: newFixture(t)}
	f.addTenants(t)
	f.server.Inventory = f.engine
	f.server.Clock = clocktesting.NewFakePassiveClock(inventoryTime)

	for stationKey, orgId := range map[string]string{"topp.CP001": orgTop, "aaaa.CP001": orgA, "aaaa.CP002": orgA, "bbbb.CP001": orgB} {
		require.NoError(t, f.engine.SetChargeStationAuth(ctx, stationKey, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: orgId}))
	}
	require.NoError(t, f.engine.SetChargeStationState(ctx, "aaaa.CP002", store.StationStateDecommissioned, ptr("rejected")))

	f.plaza = &store.AssetLocation{
		OrgId: orgA, PartyRef: "cpo-a", OcpiId: "PLAZA", Address: "Calle Mayor 1", City: "Madrid", Country: "ESP",
		Latitude: "40.416775", Longitude: "-3.703790", TimeZone: "Europe/Madrid", Publish: true,
	}
	require.NoError(t, f.engine.CreateAssetLocation(ctx, f.plaza))
	for ocppEvseId, connectors := range [][]int{{1}, {1, 2}} {
		evse := &store.AssetEvse{OrgId: orgA, LocationId: f.plaza.Id, StationKey: "aaaa.CP001", OcppEvseId: ocppEvseId + 1}
		for _, id := range connectors {
			evse.Connectors = append(evse.Connectors, &store.AssetConnector{
				OcppConnectorId: id, Standard: "IEC_62196_T2", Format: "SOCKET", PowerType: "AC_3_PHASE", MaxVoltage: 400, MaxAmperage: 32,
			})
		}
		require.NoError(t, f.engine.CreateAssetEvse(ctx, evse))
		f.evses = append(f.evses, evse)
	}
	require.NoError(t, f.engine.MoveChargeStationToLocation(ctx, "aaaa.CP001", f.plaza.Id))

	for uid, valid := range map[string]bool{"TAG1": true, "TAG2": false} {
		require.NoError(t, f.engine.SetToken(ctx, &store.Token{
			OrgId: orgA, CountryCode: "ES", PartyId: "AAA", Type: "RFID", Uid: uid, ContractId: "ESAAA" + uid,
			Issuer: "AAA", Valid: valid, CacheMode: store.CacheModeAlways,
		}))
	}
	return f
}

// inventoryReader returns the token of a platform service that may read the inventories
// that its org and org scope reach.
func (f *fixture) inventoryReader(orgId string, scope tenancy.OrgScope) string {
	claims := f.platform.Claims(time.Now())
	claims["sub"] = "svc:inventory"
	claims["org"] = orgId
	claims["org_scope"] = string(scope)
	claims["perms"] = []string{tenancy.PermTenantManage, tenancy.PermStationRead}
	return f.platform.Sign(claims)
}

func (f *fixture) getInventory(token, orgId string, query url.Values) *httptest.ResponseRecorder {
	target := "/tenants/" + orgId + "/inventory"
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	return f.do(http.MethodGet, target, token, "")
}

type inventoryItem struct {
	Id    string            `json:"id"`
	Urn   string            `json:"urn"`
	Attrs map[string]string `json:"attrs"`
}

type inventoryAnswer struct {
	OrgId      string          `json:"org_id"`
	Type       string          `json:"type"`
	AsOf       time.Time       `json:"as_of"`
	Count      int             `json:"count"`
	Items      []inventoryItem `json:"items"`
	NextCursor *string         `json:"next_cursor"`
}

// inventory asks for the inventory, checks that it is answered with 200, and returns it.
func (f *fixture) inventory(t *testing.T, token, orgId string, query url.Values) inventoryAnswer {
	t.Helper()
	rr := f.getInventory(token, orgId, query)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	var answer inventoryAnswer
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &answer))
	require.NotNil(t, answer.Items, "a list, not null: %s", rr.Body.String())
	return answer
}

func itemIds(answer inventoryAnswer) []string {
	ids := make([]string, len(answer.Items))
	for i, item := range answer.Items {
		ids[i] = item.Id
	}
	return ids
}

func TestInventoryOfEachType(t *testing.T) {
	f := newInventoryFixture(t)
	token := f.inventoryReader(orgA, tenancy.OrgScopeSelf)

	rr := f.getInventory(token, orgA, url.Values{"type": {"connector"}})
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	urn := "urn:cpms:" + orgA + ":connector/"
	assert.JSONEq(t, `{
		"org_id": "`+orgA+`",
		"type": "connector",
		"as_of": "2026-11-26T10:00:00Z",
		"count": 3,
		"items": [
			{"id": "aaaa.CP001#1", "urn": "`+urn+`aaaa.CP001#1",
			 "attrs": {"station_key": "aaaa.CP001", "evse_uid": "`+f.evses[0].Id+`", "connector_uid": "`+f.evses[0].Connectors[0].Id+`"}},
			{"id": "aaaa.CP001#2.1", "urn": "`+urn+`aaaa.CP001#2.1",
			 "attrs": {"station_key": "aaaa.CP001", "evse_uid": "`+f.evses[1].Id+`", "connector_uid": "`+f.evses[1].Connectors[0].Id+`"}},
			{"id": "aaaa.CP001#2.2", "urn": "`+urn+`aaaa.CP001#2.2",
			 "attrs": {"station_key": "aaaa.CP001", "evse_uid": "`+f.evses[1].Id+`", "connector_uid": "`+f.evses[1].Connectors[1].Id+`"}}
		],
		"next_cursor": null
	}`, rr.Body.String())

	stations := f.inventory(t, token, orgA, url.Values{"type": {"station"}})
	assert.Equal(t, 1, stations.Count)
	assert.Equal(t, []inventoryItem{{
		Id: "aaaa.CP001", Urn: "urn:cpms:" + orgA + ":station/aaaa.CP001",
		Attrs: map[string]string{"state": "active", "location_id": f.plaza.Id},
	}}, stations.Items)

	locations := f.inventory(t, token, orgA, url.Values{"type": {"location"}})
	assert.Equal(t, 1, locations.Count)
	assert.Equal(t, []inventoryItem{{
		Id: f.plaza.Id, Urn: "urn:cpms:" + orgA + ":location/" + f.plaza.Id,
		Attrs: map[string]string{"ocpi_id": "PLAZA", "party_ref": "cpo-a", "publish": "true"},
	}}, locations.Items)

	tokens := f.inventory(t, token, orgA, url.Values{"type": {"token"}})
	assert.Equal(t, 1, tokens.Count)
	assert.Equal(t, []inventoryItem{{
		Id: "ES*AAA*TAG1", Urn: "urn:cpms:" + orgA + ":token/ES*AAA*TAG1",
		Attrs: map[string]string{"type": "RFID", "valid": "true"},
	}}, tokens.Items)

	// every URN reads back as the resource of the org
	for _, answer := range []inventoryAnswer{stations, locations, tokens} {
		for _, item := range answer.Items {
			assert.Equal(t, orgA, answer.OrgId)
			assert.Equal(t, inventoryTime, answer.AsOf)
			assert.Nil(t, answer.NextCursor)
			assert.Equal(t, "urn:cpms:"+orgA+":"+answer.Type+"/"+item.Id, item.Urn)
		}
	}
}

func TestInventoryWithTheInactiveResources(t *testing.T) {
	f := newInventoryFixture(t)
	token := f.inventoryReader(orgA, tenancy.OrgScopeSelf)

	for _, value := range []string{"", "false"} {
		query := url.Values{"type": {"station"}}
		if value != "" {
			query.Set("include_inactive", value)
		}
		assert.Equal(t, []string{"aaaa.CP001"}, itemIds(f.inventory(t, token, orgA, query)), value)
	}
	stations := f.inventory(t, token, orgA, url.Values{"type": {"station"}, "include_inactive": {"true"}})
	assert.Equal(t, 2, stations.Count)
	assert.Equal(t, inventoryItem{
		Id: "aaaa.CP002", Urn: "urn:cpms:" + orgA + ":station/aaaa.CP002", Attrs: map[string]string{"state": "decommissioned"},
	}, stations.Items[1])

	tokens := f.inventory(t, token, orgA, url.Values{"type": {"token"}, "include_inactive": {"true"}})
	assert.Equal(t, []string{"ES*AAA*TAG1", "ES*AAA*TAG2"}, itemIds(tokens))

	// a decommissioned station takes its connectors out of the inventory
	require.NoError(t, f.engine.DecommissionChargeStation(context.Background(), "aaaa.CP001", nil))
	assert.Empty(t, itemIds(f.inventory(t, token, orgA, url.Values{"type": {"connector"}})))
	assert.Equal(t, 3, f.inventory(t, token, orgA, url.Values{"type": {"connector"}, "include_inactive": {"true"}}).Count)
}

func TestInventoryCountsTheWholeInventoryAndPagesIt(t *testing.T) {
	f := newInventoryFixture(t)
	token := f.inventoryReader(orgA, tenancy.OrgScopeSelf)
	for _, stationKey := range []string{"aaaa.CP003", "aaaa.CP004", "aaaa.CP005", "aaaa.CP006"} {
		require.NoError(t, f.engine.SetChargeStationAuth(context.Background(), stationKey, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: orgA}))
	}

	var ids []string
	query := url.Values{"type": {"station"}, "limit": {"2"}}
	for _, size := range []int{2, 2, 1} {
		page := f.inventory(t, token, orgA, query)
		assert.Equal(t, 5, page.Count, "the count of the whole inventory, not of the page")
		require.Len(t, page.Items, size)
		ids = append(ids, itemIds(page)...)
		if size == 1 {
			assert.Nil(t, page.NextCursor, "null after the last page")
			break
		}
		require.NotNil(t, page.NextCursor)
		query.Set("cursor", *page.NextCursor)
	}
	assert.Equal(t, []string{"aaaa.CP001", "aaaa.CP003", "aaaa.CP004", "aaaa.CP005", "aaaa.CP006"}, ids)

	// without a limit, a page holds up to 1000
	assert.Len(t, f.inventory(t, token, orgA, url.Values{"type": {"station"}}).Items, 5)
	assert.Len(t, f.inventory(t, token, orgA, url.Values{"type": {"station"}, "limit": {"5000"}}).Items, 5)
}

func TestInventoryReachesTheOrgsOfTheToken(t *testing.T) {
	f := newInventoryFixture(t)
	station := url.Values{"type": {"station"}}

	// an org and its tenants below it, each with what it owns itself
	top := f.inventoryReader(orgTop, tenancy.OrgScopeSubtree)
	assert.Equal(t, []string{"topp.CP001"}, itemIds(f.inventory(t, top, orgTop, station)), "not the stations of the tenants below")
	assert.Equal(t, []string{"aaaa.CP001"}, itemIds(f.inventory(t, top, orgA, station)))
	assert.Equal(t, []string{"bbbb.CP001"}, itemIds(f.inventory(t, top, orgB, station)))
	assert.Equal(t, []string{"aaaa.CP001"}, itemIds(f.inventory(t, f.inventoryReader(orgA, tenancy.OrgScopeSubtree), orgA, station)))

	// an org that the token does not reach is answered like a tenant that does not exist
	notFound := `{"status":"Not Found","error":"tenant_not_found"}`
	for name, refused := range map[string]struct {
		token string
		orgId string
	}{
		"another org with the scope self":               {f.inventoryReader(orgA, tenancy.OrgScopeSelf), orgB},
		"the org above with the scope self":             {f.inventoryReader(orgA, tenancy.OrgScopeSelf), orgTop},
		"the org above with the scope subtree":          {f.inventoryReader(orgA, tenancy.OrgScopeSubtree), orgTop},
		"another tree":                                  {f.inventoryReader(orgOther, tenancy.OrgScopeSubtree), orgA},
		"the tenant below with the scope self":          {f.inventoryReader(orgTop, tenancy.OrgScopeSelf), orgA},
		"an org that is not a tenant, with its subtree": {f.inventoryReader(testutil.PlatformOrgId, tenancy.OrgScopeSubtree), orgA},
	} {
		rr := f.getInventory(refused.token, refused.orgId, station)
		assert.Equal(t, http.StatusNotFound, rr.Code, name)
		assert.JSONEq(t, notFound, rr.Body.String(), name)
	}

	// a token of an org that is not a tenant reaches nothing, not even its own org
	rr := f.getInventory(f.inventoryReader(testutil.PlatformOrgId, tenancy.OrgScopeSelf), testutil.PlatformOrgId, station)
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.JSONEq(t, notFound, rr.Body.String())

	// the org id in any case
	assert.Equal(t, orgA, f.inventory(t, top, "0193B2C4-0000-7000-8000-0000000000A1", station).OrgId)

	// a suspended tenant keeps its inventory
	_, err := f.engine.SetTenantStatus(context.Background(), orgA, store.TenantStatusSuspended)
	require.NoError(t, err)
	assert.Equal(t, []string{"aaaa.CP001"}, itemIds(f.inventory(t, top, orgA, station)))
}

func TestInventoryOfAnUnknownTenant(t *testing.T) {
	f := newInventoryFixture(t)
	const unknown = "0193b2c4-0000-7000-8000-0000000000d4"
	// the scope self reaches the org of the token, whether it is a tenant or not
	rr := f.getInventory(f.inventoryReader(unknown, tenancy.OrgScopeSelf), unknown, url.Values{"type": {"station"}})
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.JSONEq(t, `{"status":"Not Found","error":"tenant_not_found"}`, rr.Body.String())
}

func TestInventoryOfATransferredStation(t *testing.T) {
	f := newTransferFixture(t)
	f.server.Inventory = f.engine
	rr := f.transfer(stationURN(orgA, transferStation), `{"to_org_id":"`+orgB+`"}`)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	readerA, readerB := f.inventoryReader(orgA, tenancy.OrgScopeSelf), f.inventoryReader(orgB, tenancy.OrgScopeSelf)
	for _, resourceType := range []string{"station", "connector", "location"} {
		answer := f.inventory(t, readerA, orgA, url.Values{"type": {resourceType}, "include_inactive": {"true"}})
		assert.Zero(t, answer.Count, resourceType)
		assert.Empty(t, answer.Items, resourceType)
	}
	// the station key keeps the org key of the old org; the URN has the new org
	stations := f.inventory(t, readerB, orgB, url.Values{"type": {"station"}})
	assert.Equal(t, []inventoryItem{{
		Id: transferStation, Urn: stationURN(orgB, transferStation),
		Attrs: map[string]string{"state": "active", "location_id": f.plaza.Id},
	}}, stations.Items)
	assert.Equal(t, []string{"aaaa.CP001#1", "aaaa.CP001#2.1", "aaaa.CP001#2.2"},
		itemIds(f.inventory(t, readerB, orgB, url.Values{"type": {"connector"}})))
	assert.Equal(t, []string{f.plaza.Id}, itemIds(f.inventory(t, readerB, orgB, url.Values{"type": {"location"}})))
}

func TestInventoryRefusesParametersThatAreNotValid(t *testing.T) {
	f := newInventoryFixture(t)
	token := f.inventoryReader(orgA, tenancy.OrgScopeSelf)
	invalid := func(field, message string) string {
		return `{"status":"Bad Request","error":"invalid_field","field":"` + field + `","message":"` + message + `"}`
	}
	notAType := invalid("type", "must be one of station, connector, location, token")
	for name, test := range map[string]struct {
		orgId string
		query url.Values
		body  string
	}{
		"no type":                         {orgA, nil, invalid("type", "cannot be read")},
		"an empty type":                   {orgA, url.Values{"type": {""}}, invalid("type", "cannot be read")},
		"a type that is not listed":       {orgA, url.Values{"type": {"session"}}, notAType},
		"a type in upper case":            {orgA, url.Values{"type": {"Station"}}, notAType},
		"a type with the product":         {orgA, url.Values{"type": {"cpms.station"}}, notAType},
		"a limit of 0":                    {orgA, url.Values{"type": {"station"}, "limit": {"0"}}, invalid("limit", "must be between 1 and 5000")},
		"a limit above 5000":              {orgA, url.Values{"type": {"station"}, "limit": {"5001"}}, invalid("limit", "must be between 1 and 5000")},
		"a limit that is not a number":    {orgA, url.Values{"type": {"station"}, "limit": {"ten"}}, invalid("limit", "cannot be read")},
		"a cursor that was not returned":  {orgA, url.Values{"type": {"station"}, "cursor": {"%%%"}}, invalid("cursor", "must be a cursor that a page of the inventory returned")},
		"include_inactive not a boolean":  {orgA, url.Values{"type": {"station"}, "include_inactive": {"maybe"}}, invalid("include_inactive", "cannot be read")},
		"an org id that is not a UUID":    {"acme", url.Values{"type": {"station"}}, invalid("org_id", "must be a UUID")},
		"an org id without hyphens":       {"0193b2c400007000800000000000000a1", url.Values{"type": {"station"}}, invalid("org_id", "must be a UUID")},
		"a type that is not valid, first": {orgB, url.Values{"type": {"tariff"}}, notAType},
	} {
		rr := f.getInventory(token, test.orgId, test.query)
		assert.Equal(t, http.StatusBadRequest, rr.Code, name)
		assert.JSONEq(t, test.body, rr.Body.String(), name)
	}
}

func TestInventoryNeedsThePermissionToReadChargeStations(t *testing.T) {
	f := newInventoryFixture(t)
	claims := f.platform.Claims(time.Now())
	claims["sub"] = "svc:inventory"
	claims["org"] = orgA
	claims["org_scope"] = string(tenancy.OrgScopeSelf)
	claims["perms"] = []string{tenancy.PermTenantManage, tenancy.PermStationWrite, tenancy.PermTokenRead}
	rr := f.getInventory(f.platform.Sign(claims), orgA, url.Values{"type": {"station"}})
	assert.Equal(t, http.StatusForbidden, rr.Code)
	assert.JSONEq(t, `{"status":"Forbidden","error":"forbidden"}`, rr.Body.String())

	// the permission alone is not enough: the control plane is for the services that may manage tenants
	claims["perms"] = []string{tenancy.PermStationRead}
	rr = f.getInventory(f.platform.Sign(claims), orgA, url.Values{"type": {"station"}})
	assert.Equal(t, http.StatusForbidden, rr.Code)
}

// failingInventory is an inventory store that fails with err.
type failingInventory struct {
	err error
}

func (s failingInventory) ListInventory(context.Context, string, string, bool, store.PageRequest) ([]store.InventoryItem, string, error) {
	return nil, "", s.err
}

func (s failingInventory) CountInventory(context.Context, string, string, bool) (int, error) {
	return 0, s.err
}

func TestInventoryWhenTheStoreCannotServeIt(t *testing.T) {
	f := newInventoryFixture(t)
	token := f.inventoryReader(orgA, tenancy.OrgScopeSelf)
	query := url.Values{"type": {"station"}}

	f.server.Inventory = nil
	rr := f.getInventory(token, orgA, query)
	assert.Equal(t, http.StatusNotImplemented, rr.Code)
	assert.JSONEq(t, `{"status":"Not Implemented","error":"not_implemented"}`, rr.Body.String())

	f.server.Inventory = failingInventory{err: store.ErrNotImplemented}
	rr = f.getInventory(token, orgA, query)
	assert.Equal(t, http.StatusNotImplemented, rr.Code)

	f.server.Inventory = failingInventory{err: errors.New("connection refused by db.internal:5432")}
	rr = f.getInventory(token, orgA, query)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.JSONEq(t, `{"status":"Internal Server Error","error":"internal_error"}`, rr.Body.String(), "nothing of what the store said")

	f.server.Inventory = f.engine
	f.server.Scoper = nil
	rr = f.getInventory(token, orgA, query)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}
