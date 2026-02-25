-- name: UpsertDiagnosticsRequest :exec
INSERT INTO diagnostics_request (
    charge_station_id,
    location,
    start_time,
    stop_time,
    retries,
    retry_interval,
    status,
    send_after
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (charge_station_id) DO UPDATE SET
    location = EXCLUDED.location,
    start_time = EXCLUDED.start_time,
    stop_time = EXCLUDED.stop_time,
    retries = EXCLUDED.retries,
    retry_interval = EXCLUDED.retry_interval,
    status = EXCLUDED.status,
    send_after = EXCLUDED.send_after;

-- name: GetDiagnosticsRequest :one
SELECT charge_station_id, location, start_time, stop_time, retries, retry_interval, status, send_after
FROM diagnostics_request
WHERE charge_station_id = $1;

-- name: DeleteDiagnosticsRequest :exec
DELETE FROM diagnostics_request
WHERE charge_station_id = $1;

-- name: ListDiagnosticsRequests :many
SELECT charge_station_id, location, start_time, stop_time, retries, retry_interval, status, send_after
FROM diagnostics_request
WHERE charge_station_id > $1
ORDER BY charge_station_id
LIMIT $2;

-- name: UpsertLogRequest :exec
INSERT INTO log_request (
    charge_station_id,
    log_type,
    request_id,
    remote_location,
    oldest_timestamp,
    latest_timestamp,
    retries,
    retry_interval,
    status,
    send_after
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (charge_station_id) DO UPDATE SET
    log_type = EXCLUDED.log_type,
    request_id = EXCLUDED.request_id,
    remote_location = EXCLUDED.remote_location,
    oldest_timestamp = EXCLUDED.oldest_timestamp,
    latest_timestamp = EXCLUDED.latest_timestamp,
    retries = EXCLUDED.retries,
    retry_interval = EXCLUDED.retry_interval,
    status = EXCLUDED.status,
    send_after = EXCLUDED.send_after;

-- name: GetLogRequest :one
SELECT charge_station_id, log_type, request_id, remote_location, oldest_timestamp, latest_timestamp, retries, retry_interval, status, send_after
FROM log_request
WHERE charge_station_id = $1;

-- name: DeleteLogRequest :exec
DELETE FROM log_request
WHERE charge_station_id = $1;

-- name: ListLogRequests :many
SELECT charge_station_id, log_type, request_id, remote_location, oldest_timestamp, latest_timestamp, retries, retry_interval, status, send_after
FROM log_request
WHERE charge_station_id > $1
ORDER BY charge_station_id
LIMIT $2;
