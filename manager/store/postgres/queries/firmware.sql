-- name: UpsertFirmwareUpdateStatus :exec
INSERT INTO firmware_update_status (charge_station_id, status, location, retrieve_date, retry_count, updated_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (charge_station_id) DO UPDATE SET
    status = EXCLUDED.status,
    location = EXCLUDED.location,
    retrieve_date = EXCLUDED.retrieve_date,
    retry_count = EXCLUDED.retry_count,
    updated_at = EXCLUDED.updated_at;

-- name: GetFirmwareUpdateStatus :one
SELECT charge_station_id, status, location, retrieve_date, retry_count, updated_at
FROM firmware_update_status
WHERE charge_station_id = $1;

-- name: UpsertDiagnosticsStatus :exec
INSERT INTO diagnostics_status (charge_station_id, status, location, updated_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (charge_station_id) DO UPDATE SET
    status = EXCLUDED.status,
    location = EXCLUDED.location,
    updated_at = EXCLUDED.updated_at;

-- name: GetDiagnosticsStatus :one
SELECT charge_station_id, status, location, updated_at
FROM diagnostics_status
WHERE charge_station_id = $1;

-- name: UpsertPublishFirmwareStatus :exec
INSERT INTO publish_firmware_status (charge_station_id, status, location, checksum, request_id, updated_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (charge_station_id) DO UPDATE SET
    status = EXCLUDED.status,
    location = EXCLUDED.location,
    checksum = EXCLUDED.checksum,
    request_id = EXCLUDED.request_id,
    updated_at = EXCLUDED.updated_at;

-- name: GetPublishFirmwareStatus :one
SELECT charge_station_id, status, location, checksum, request_id, updated_at
FROM publish_firmware_status
WHERE charge_station_id = $1;

-- name: UpsertFirmwareUpdateRequest :exec
INSERT INTO firmware_update_request (
    charge_station_id,
    location,
    retrieve_date,
    retries,
    retry_interval,
    signature,
    signing_certificate,
    status,
    send_after
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (charge_station_id) DO UPDATE SET
    location = EXCLUDED.location,
    retrieve_date = EXCLUDED.retrieve_date,
    retries = EXCLUDED.retries,
    retry_interval = EXCLUDED.retry_interval,
    signature = EXCLUDED.signature,
    signing_certificate = EXCLUDED.signing_certificate,
    status = EXCLUDED.status,
    send_after = EXCLUDED.send_after;

-- name: GetFirmwareUpdateRequest :one
SELECT charge_station_id, location, retrieve_date, retries, retry_interval, signature, signing_certificate, status, send_after
FROM firmware_update_request
WHERE charge_station_id = $1;

-- name: DeleteFirmwareUpdateRequest :exec
DELETE FROM firmware_update_request
WHERE charge_station_id = $1;

-- name: ListFirmwareUpdateRequests :many
SELECT charge_station_id, location, retrieve_date, retries, retry_interval, signature, signing_certificate, status, send_after
FROM firmware_update_request
WHERE charge_station_id > $1
ORDER BY charge_station_id
LIMIT $2;
