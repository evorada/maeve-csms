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
