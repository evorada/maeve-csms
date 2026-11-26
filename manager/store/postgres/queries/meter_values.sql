-- name: StoreMeterValue :exec
INSERT INTO meter_values (charge_station_id, evse_id, transaction_id, timestamp, sampled_values)
VALUES ($1, $2, $3, $4, $5);

-- name: GetMeterValuesByStationAndEvse :many
SELECT * FROM meter_values
WHERE charge_station_id = $1 AND evse_id = $2
ORDER BY timestamp DESC
LIMIT $3;

-- name: GetAllMeterValuesByStation :many
SELECT * FROM meter_values
WHERE charge_station_id = $1 AND evse_id = $2
ORDER BY timestamp DESC;

-- name: QueryMeterValues :many
-- The optional filters are nullable parameters: NULL means "no filter".
SELECT * FROM meter_values
WHERE charge_station_id = $1
  AND (sqlc.narg('connector_id')::int IS NULL OR evse_id = sqlc.narg('connector_id')::int)
  AND (sqlc.narg('transaction_id')::text IS NULL OR transaction_id = sqlc.narg('transaction_id')::text)
  AND (sqlc.narg('start_time')::timestamp IS NULL OR timestamp >= sqlc.narg('start_time')::timestamp)
  AND (sqlc.narg('end_time')::timestamp IS NULL OR timestamp <= sqlc.narg('end_time')::timestamp)
ORDER BY timestamp DESC
LIMIT $2 OFFSET $3;

-- name: CountMeterValues :one
SELECT COUNT(*) FROM meter_values
WHERE charge_station_id = $1
  AND (sqlc.narg('connector_id')::int IS NULL OR evse_id = sqlc.narg('connector_id')::int)
  AND (sqlc.narg('transaction_id')::text IS NULL OR transaction_id = sqlc.narg('transaction_id')::text)
  AND (sqlc.narg('start_time')::timestamp IS NULL OR timestamp >= sqlc.narg('start_time')::timestamp)
  AND (sqlc.narg('end_time')::timestamp IS NULL OR timestamp <= sqlc.narg('end_time')::timestamp);
