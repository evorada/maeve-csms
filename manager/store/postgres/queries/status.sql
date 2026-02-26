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
    updated_at
FROM charge_station_status
WHERE charge_station_id = $1;

-- name: UpdateHeartbeat :exec
INSERT INTO charge_station_status (charge_station_id, connected, last_heartbeat, updated_at)
VALUES ($1, true, $2, NOW())
ON CONFLICT (charge_station_id) DO UPDATE SET
    connected = true,
    last_heartbeat = EXCLUDED.last_heartbeat,
    updated_at = NOW();

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
