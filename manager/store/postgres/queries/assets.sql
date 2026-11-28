-- name: CreateAssetLocation :one
INSERT INTO asset_locations (
    id, org_id, party_ref, ocpi_id, name, address, city, postal_code, state, country,
    latitude, longitude, time_zone, parking_type, publish, opening_times, facilities,
    charging_when_closed, external_refs
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19
)
RETURNING *;

-- An update that names a version that is not stored returns no row.
-- name: UpdateAssetLocation :one
UPDATE asset_locations SET
    party_ref = @party_ref,
    ocpi_id = @ocpi_id,
    name = @name,
    address = @address,
    city = @city,
    postal_code = @postal_code,
    state = @state,
    country = @country,
    latitude = @latitude,
    longitude = @longitude,
    time_zone = @time_zone,
    parking_type = @parking_type,
    publish = @publish,
    opening_times = @opening_times,
    facilities = @facilities,
    charging_when_closed = @charging_when_closed,
    external_refs = @external_refs,
    version = version + 1,
    updated_at = NOW()
WHERE id = @id AND version = @version
RETURNING *;

-- name: GetAssetLocation :one
SELECT * FROM asset_locations WHERE id = $1;

-- name: GetAssetLocationByOcpiId :one
SELECT * FROM asset_locations WHERE party_ref = $1 AND ocpi_id = $2;

-- The query is a LIKE pattern: the store escapes %, _ and \ in it.
-- name: ListAssetLocations :many
SELECT * FROM asset_locations
WHERE org_id = ANY(sqlc.arg('org_ids')::uuid[])
  AND (sqlc.narg('party_ref')::text IS NULL OR party_ref = sqlc.narg('party_ref')::text)
  AND (sqlc.narg('publish')::boolean IS NULL OR publish = sqlc.narg('publish')::boolean)
  AND (sqlc.narg('query')::text IS NULL
       OR name ILIKE '%' || sqlc.narg('query')::text || '%'
       OR address ILIKE '%' || sqlc.narg('query')::text || '%'
       OR city ILIKE '%' || sqlc.narg('query')::text || '%'
       OR ocpi_id ILIKE '%' || sqlc.narg('query')::text || '%')
  AND id > sqlc.arg('after_id')::uuid
ORDER BY id
LIMIT sqlc.arg('row_limit');

-- name: CountAssetEvsesForLocation :one
SELECT count(*) FROM asset_evses WHERE location_id = $1;

-- name: DeleteAssetLocation :exec
DELETE FROM asset_locations WHERE id = $1;

-- name: CreateAssetEvse :one
INSERT INTO asset_evses (
    id, org_id, location_id, station_key, ocpp_evse_id, evse_id, physical_reference, status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
RETURNING *;

-- An update that names a version that is not stored returns no row.
-- name: UpdateAssetEvse :one
UPDATE asset_evses SET
    location_id = @location_id,
    station_key = @station_key,
    ocpp_evse_id = @ocpp_evse_id,
    evse_id = @evse_id,
    physical_reference = @physical_reference,
    status = @status,
    version = version + 1,
    updated_at = NOW()
WHERE id = @id AND version = @version
RETURNING *;

-- name: GetAssetEvse :one
SELECT * FROM asset_evses WHERE id = $1;

-- name: GetAssetEvseByOcpp :one
SELECT * FROM asset_evses WHERE station_key = $1 AND ocpp_evse_id = $2;

-- name: ListAssetEvsesByLocation :many
SELECT * FROM asset_evses WHERE location_id = $1 ORDER BY ocpp_evse_id, station_key;

-- name: ListAssetEvsesByStation :many
SELECT * FROM asset_evses WHERE station_key = $1 ORDER BY ocpp_evse_id;

-- name: DeleteAssetEvse :exec
DELETE FROM asset_evses WHERE id = $1;

-- The EVSEs of a charge station that is moved or decommissioned. An EVSE that is already
-- where the change would put it keeps its version.

-- name: MoveStationEvses :exec
UPDATE asset_evses
SET location_id = @location_id, version = version + 1, updated_at = NOW()
WHERE station_key = @station_key AND location_id <> @location_id;

-- name: RemoveStationEvses :exec
UPDATE asset_evses
SET status = 'REMOVED', version = version + 1, updated_at = NOW()
WHERE station_key = @station_key AND status <> 'REMOVED';

-- A charge station that moves to another org, with its EVSEs, their connectors, its
-- charging profiles and maybe its location. The station row is locked first, so that
-- transfers of one station happen one after the other.

-- name: LockChargeStation :one
SELECT org_id, location_id, state FROM charge_stations
WHERE charge_station_id = $1
FOR UPDATE;

-- A location only moves to another org when no other station and no EVSE of another
-- station is at it: no row is changed otherwise.
-- name: TransferLocation :execrows
UPDATE asset_locations l SET
    org_id = sqlc.arg('to_org_id')::uuid,
    party_ref = sqlc.arg('party_ref')::text,
    version = l.version + 1,
    updated_at = NOW()
WHERE l.id = sqlc.arg('location_id')::uuid
  AND NOT EXISTS (
        SELECT 1 FROM asset_evses e
        WHERE e.location_id = sqlc.arg('location_id')::uuid AND e.station_key <> sqlc.arg('station_key')::text)
  AND NOT EXISTS (
        SELECT 1 FROM charge_stations cs
        WHERE cs.location_id = sqlc.arg('location_id')::uuid AND cs.charge_station_id <> sqlc.arg('station_key')::text);

-- name: TransferChargingProfiles :exec
UPDATE charging_profiles SET org_id = @to_org_id WHERE charge_station_id = @charge_station_id;

-- The connectors of the EVSEs of the station with the tariffs that they refer to, locked
-- until the transaction ends.
-- name: LockStationConnectors :many
SELECT c.id, c.tariff_ref, c.adhoc_tariff_ref FROM asset_connectors c
JOIN asset_evses e ON e.id = c.evse_uid
WHERE e.station_key = $1
ORDER BY c.id
FOR UPDATE OF c;

-- Tariffs belong to an org: the connectors of a station that moves to another org lose
-- theirs.
-- name: TransferStationConnectors :execrows
UPDATE asset_connectors SET
    org_id = @to_org_id,
    tariff_ref = NULL,
    adhoc_tariff_ref = NULL,
    version = version + 1,
    updated_at = NOW()
WHERE id = ANY(sqlc.arg('ids')::uuid[]);

-- name: TransferStationEvses :execrows
UPDATE asset_evses SET
    org_id = @to_org_id,
    location_id = @location_id,
    version = version + 1,
    updated_at = NOW()
WHERE station_key = @station_key;

-- name: TransferStationRow :exec
UPDATE charge_stations SET
    org_id = @to_org_id,
    location_id = sqlc.narg('location_id')::uuid,
    updated_at = NOW()
WHERE charge_station_id = @charge_station_id;

-- A connector belongs to the org of its EVSE. Returns no row when there is no such EVSE.
-- name: CreateAssetConnector :one
INSERT INTO asset_connectors (
    id, org_id, evse_uid, ocpp_connector_id, ocpi_id, standard, format, power_type,
    max_voltage, max_amperage, max_electric_power, tariff_ref, adhoc_tariff_ref
)
SELECT
    sqlc.arg('id')::uuid, e.org_id, e.id, sqlc.arg('ocpp_connector_id')::int,
    sqlc.arg('ocpi_id')::text, sqlc.arg('standard')::text, sqlc.arg('format')::text,
    sqlc.arg('power_type')::text, sqlc.arg('max_voltage')::int, sqlc.arg('max_amperage')::int,
    sqlc.narg('max_electric_power')::int, sqlc.narg('tariff_ref')::uuid,
    sqlc.narg('adhoc_tariff_ref')::uuid
FROM asset_evses e
WHERE e.id = sqlc.arg('evse_uid')::uuid
RETURNING *;

-- An update that names a version that is not stored returns no row.
-- name: UpdateAssetConnector :one
UPDATE asset_connectors SET
    ocpp_connector_id = @ocpp_connector_id,
    ocpi_id = @ocpi_id,
    standard = @standard,
    format = @format,
    power_type = @power_type,
    max_voltage = @max_voltage,
    max_amperage = @max_amperage,
    max_electric_power = @max_electric_power,
    tariff_ref = @tariff_ref,
    adhoc_tariff_ref = @adhoc_tariff_ref,
    version = version + 1,
    updated_at = NOW()
WHERE id = @id AND version = @version
RETURNING *;

-- name: GetAssetConnector :one
SELECT * FROM asset_connectors WHERE id = $1;

-- name: GetAssetConnectorByOcpp :one
SELECT c.* FROM asset_connectors c
JOIN asset_evses e ON e.id = c.evse_uid
WHERE e.station_key = $1 AND e.ocpp_evse_id = $2 AND c.ocpp_connector_id = $3;

-- name: ListAssetConnectorsByEvses :many
SELECT * FROM asset_connectors
WHERE evse_uid = ANY(sqlc.arg('evse_uids')::uuid[])
ORDER BY evse_uid, ocpp_connector_id;

-- name: DeleteAssetConnector :exec
DELETE FROM asset_connectors WHERE id = $1;
