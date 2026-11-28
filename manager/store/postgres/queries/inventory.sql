-- The inventory of an org: what it owns itself (org_id), never what the orgs below it
-- own. A page holds the items whose id sorts after after_id, byte by byte (COLLATE "C",
-- as the other backends compare), and row_limit items at most. Without include_inactive
-- the decommissioned stations, the connectors of a REMOVED EVSE or of a station that is
-- not active, and the tokens that are not valid are left out.

-- name: ListInventoryStations :many
SELECT charge_station_id, state, location_id
FROM charge_stations
WHERE org_id = sqlc.arg('org_id')::uuid
  AND (state IN ('pending', 'active') OR sqlc.arg('include_inactive')::boolean)
  AND charge_station_id COLLATE "C" > sqlc.arg('after_id')::text
ORDER BY charge_station_id COLLATE "C"
LIMIT sqlc.arg('row_limit');

-- name: CountInventoryStations :one
SELECT count(*) FROM charge_stations
WHERE org_id = sqlc.arg('org_id')::uuid
  AND (state IN ('pending', 'active') OR sqlc.arg('include_inactive')::boolean);

-- The id of a connector is its subject key (store.ConnectorSubjectKey): the station key
-- and the OCPP EVSE id, followed by the OCPP connector id when the EVSE has more than
-- one connector.
-- name: ListInventoryConnectors :many
SELECT id, evse_uid, connector_uid, station_key
FROM (
    SELECT
        (CASE
            WHEN (SELECT count(*) FROM asset_connectors o WHERE o.evse_uid = c.evse_uid) > 1
            THEN e.station_key || '#' || e.ocpp_evse_id::text || '.' || c.ocpp_connector_id::text
            ELSE e.station_key || '#' || e.ocpp_evse_id::text
        END)::text AS id,
        c.evse_uid,
        c.id AS connector_uid,
        e.station_key
    FROM asset_connectors c
    JOIN asset_evses e ON e.id = c.evse_uid
    JOIN charge_stations s ON s.charge_station_id = e.station_key
    WHERE c.org_id = sqlc.arg('org_id')::uuid
      AND ((e.status <> 'REMOVED' AND s.state = 'active') OR sqlc.arg('include_inactive')::boolean)
) items
WHERE id COLLATE "C" > sqlc.arg('after_id')::text
ORDER BY id COLLATE "C"
LIMIT sqlc.arg('row_limit');

-- name: CountInventoryConnectors :one
SELECT count(*)
FROM asset_connectors c
JOIN asset_evses e ON e.id = c.evse_uid
JOIN charge_stations s ON s.charge_station_id = e.station_key
WHERE c.org_id = sqlc.arg('org_id')::uuid
  AND ((e.status <> 'REMOVED' AND s.state = 'active') OR sqlc.arg('include_inactive')::boolean);

-- Locations have no inactive state.
-- name: ListInventoryLocations :many
SELECT id, ocpi_id, party_ref, publish
FROM asset_locations
WHERE org_id = sqlc.arg('org_id')::uuid
  AND id::text COLLATE "C" > sqlc.arg('after_id')::text
ORDER BY id::text COLLATE "C"
LIMIT sqlc.arg('row_limit');

-- name: CountInventoryLocations :one
SELECT count(*) FROM asset_locations
WHERE org_id = sqlc.arg('org_id')::uuid;

-- The id of a token is {country_code}*{party_id}*{uid} (store.TokenInventoryId).
-- name: ListInventoryTokens :many
SELECT id, type, valid
FROM (
    SELECT (country_code || '*' || party_id || '*' || uid)::text AS id, type, valid
    FROM tokens
    WHERE org_id = sqlc.arg('org_id')::uuid
      AND (valid OR sqlc.arg('include_inactive')::boolean)
) items
WHERE id COLLATE "C" > sqlc.arg('after_id')::text
ORDER BY id COLLATE "C"
LIMIT sqlc.arg('row_limit');

-- name: CountInventoryTokens :one
SELECT count(*) FROM tokens
WHERE org_id = sqlc.arg('org_id')::uuid
  AND (valid OR sqlc.arg('include_inactive')::boolean);
