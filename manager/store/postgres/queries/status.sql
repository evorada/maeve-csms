-- name: UpsertChargeStationStatus :exec
INSERT INTO charge_station_status (
    charge_station_id,
    connected,
    last_heartbeat,
    firmware_version,
    model,
    vendor,
    serial_number,
    updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (charge_station_id) DO UPDATE SET
    connected = EXCLUDED.connected,
    last_heartbeat = EXCLUDED.last_heartbeat,
    firmware_version = COALESCE(EXCLUDED.firmware_version, charge_station_status.firmware_version),
    model = COALESCE(EXCLUDED.model, charge_station_status.model),
    vendor = COALESCE(EXCLUDED.vendor, charge_station_status.vendor),
    serial_number = COALESCE(EXCLUDED.serial_number, charge_station_status.serial_number),
    updated_at = EXCLUDED.updated_at;

-- name: GetChargeStationStatus :one
SELECT
    charge_station_id,
    connected,
    last_heartbeat,
    firmware_version,
    model,
    vendor,
    serial_number,
    updated_at,
    last_connected_at,
    last_disconnected_at,
    last_seen_at,
    connection_id
FROM charge_station_status
WHERE charge_station_id = $1;

-- name: UpdateHeartbeat :exec
INSERT INTO charge_station_status (charge_station_id, connected, last_heartbeat, updated_at)
VALUES ($1, true, $2, NOW())
ON CONFLICT (charge_station_id) DO UPDATE SET
    connected = true,
    last_heartbeat = EXCLUDED.last_heartbeat,
    updated_at = NOW();

-- name: RecordStationConnected :execrows
-- Like the other connectivity statements, it decides in its WHERE clause whether the
-- event still applies, so that the events of one station that several manager replicas
-- handle at the same time cannot interleave between a read and a write.
INSERT INTO charge_station_status (charge_station_id, connected, last_connected_at, last_seen_at, connection_id, updated_at)
VALUES (sqlc.arg(charge_station_id), true, sqlc.arg(connected_at), sqlc.arg(connected_at), sqlc.arg(connection_id), NOW())
ON CONFLICT (charge_station_id) DO UPDATE SET
    connected = true,
    last_connected_at = EXCLUDED.last_connected_at,
    last_seen_at = GREATEST(charge_station_status.last_seen_at, EXCLUDED.last_seen_at),
    connection_id = EXCLUDED.connection_id,
    updated_at = NOW()
WHERE charge_station_status.last_connected_at IS NULL
   OR charge_station_status.last_connected_at <= EXCLUDED.last_connected_at;

-- name: RecordStationDisconnected :execrows
UPDATE charge_station_status
SET connected = false, last_disconnected_at = sqlc.arg(disconnected_at), updated_at = NOW()
WHERE charge_station_id = sqlc.arg(charge_station_id)
  AND connected
  AND connection_id = sqlc.arg(connection_id)
  AND last_connected_at <= sqlc.arg(disconnected_at);

-- name: TouchStationLastSeen :exec
UPDATE charge_station_status
SET last_seen_at = sqlc.arg(seen_at), updated_at = NOW()
WHERE charge_station_id = sqlc.arg(charge_station_id)
  AND (last_seen_at IS NULL OR last_seen_at < sqlc.arg(seen_at));

-- name: DisconnectStaleStations :many
-- GREATEST ignores NULLs; a row where all three are NULL is not touched.
UPDATE charge_station_status
SET connected = false, last_disconnected_at = sqlc.arg(now), updated_at = NOW()
WHERE connected
  AND GREATEST(last_seen_at, last_connected_at, last_heartbeat) < sqlc.arg(seen_before)
RETURNING charge_station_id;

-- name: UpsertConnectorStatus :exec
INSERT INTO connector_status (
    charge_station_id,
    connector_id,
    status,
    error_code,
    info,
    timestamp,
    vendor_error_code,
    vendor_id,
    current_transaction_id,
    updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (charge_station_id, connector_id) DO UPDATE SET
    status = EXCLUDED.status,
    error_code = EXCLUDED.error_code,
    info = EXCLUDED.info,
    timestamp = EXCLUDED.timestamp,
    vendor_error_code = EXCLUDED.vendor_error_code,
    vendor_id = EXCLUDED.vendor_id,
    current_transaction_id = EXCLUDED.current_transaction_id,
    updated_at = EXCLUDED.updated_at;

-- name: GetConnectorStatus :one
SELECT
    charge_station_id,
    connector_id,
    status,
    error_code,
    info,
    timestamp,
    vendor_error_code,
    vendor_id,
    current_transaction_id,
    updated_at
FROM connector_status
WHERE charge_station_id = $1 AND connector_id = $2;

-- name: ListConnectorStatuses :many
SELECT
    charge_station_id,
    connector_id,
    status,
    error_code,
    info,
    timestamp,
    vendor_error_code,
    vendor_id,
    current_transaction_id,
    updated_at
FROM connector_status
WHERE charge_station_id = $1
ORDER BY connector_id;

-- name: CountConnectorsForOrgs :one
SELECT COUNT(*)
FROM connector_status c
JOIN charge_stations s ON s.charge_station_id = c.charge_station_id
WHERE c.connector_id > 0
  AND s.org_id = ANY(sqlc.arg(org_ids)::uuid[]);
