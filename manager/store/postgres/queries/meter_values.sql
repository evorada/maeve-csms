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
SELECT * FROM meter_values
WHERE charge_station_id = $1
  AND ($2::int IS NULL OR evse_id = $2)
  AND ($3::text IS NULL OR transaction_id = $3)
  AND ($4::timestamp IS NULL OR timestamp >= $4)
  AND ($5::timestamp IS NULL OR timestamp <= $5)
ORDER BY timestamp DESC
LIMIT $6 OFFSET $7;

-- name: CountMeterValues :one
SELECT COUNT(*) FROM meter_values
WHERE charge_station_id = $1
  AND ($2::int IS NULL OR evse_id = $2)
  AND ($3::text IS NULL OR transaction_id = $3)
  AND ($4::timestamp IS NULL OR timestamp >= $4)
  AND ($5::timestamp IS NULL OR timestamp <= $5);
