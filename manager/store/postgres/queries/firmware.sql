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
