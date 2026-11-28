-- StoreMeterValue stores a meter value for the org that owns it: the org of its
-- transaction when the transaction is known, else the org of its charge station, else
-- the legacy org (a charge station that is not registered).

-- name: StoreMeterValue :exec
INSERT INTO meter_values (charge_station_id, evse_id, transaction_id, timestamp, sampled_values, org_id)
VALUES (
    $1, $2, $3, $4, $5,
    COALESCE(
        (SELECT t.org_id FROM transactions t WHERE t.charge_station_id = $1::varchar AND t.id = $3::varchar),
        (SELECT cs.org_id FROM charge_stations cs WHERE cs.charge_station_id = $1::varchar),
        '00000000-0000-0000-0000-000000000000'::uuid)
);

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
-- The optional filters are nullable parameters: NULL means "no filter". No org is every
-- org.
SELECT * FROM meter_values
WHERE charge_station_id = $1
  AND (cardinality(sqlc.arg('org_ids')::uuid[]) = 0 OR org_id = ANY(sqlc.arg('org_ids')::uuid[]))
  AND (sqlc.narg('connector_id')::int IS NULL OR evse_id = sqlc.narg('connector_id')::int)
  AND (sqlc.narg('transaction_id')::text IS NULL OR transaction_id = sqlc.narg('transaction_id')::text)
  AND (sqlc.narg('start_time')::timestamp IS NULL OR timestamp >= sqlc.narg('start_time')::timestamp)
  AND (sqlc.narg('end_time')::timestamp IS NULL OR timestamp <= sqlc.narg('end_time')::timestamp)
ORDER BY timestamp DESC
LIMIT $2 OFFSET $3;

-- name: CountMeterValues :one
SELECT COUNT(*) FROM meter_values
WHERE charge_station_id = $1
  AND (cardinality(sqlc.arg('org_ids')::uuid[]) = 0 OR org_id = ANY(sqlc.arg('org_ids')::uuid[]))
  AND (sqlc.narg('connector_id')::int IS NULL OR evse_id = sqlc.narg('connector_id')::int)
  AND (sqlc.narg('transaction_id')::text IS NULL OR transaction_id = sqlc.narg('transaction_id')::text)
  AND (sqlc.narg('start_time')::timestamp IS NULL OR timestamp >= sqlc.narg('start_time')::timestamp)
  AND (sqlc.narg('end_time')::timestamp IS NULL OR timestamp <= sqlc.narg('end_time')::timestamp);
