-- name: GetTransaction :one
SELECT * FROM transactions WHERE id = $1;

-- name: FindActiveTransaction :one
SELECT * FROM transactions 
WHERE charge_station_id = $1 AND stop_timestamp IS NULL
ORDER BY start_timestamp DESC
LIMIT 1;

-- name: CreateTransaction :one
INSERT INTO transactions (
    id, charge_station_id, token_uid, token_type,
    meter_start, start_timestamp, offline
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateTransaction :one
UPDATE transactions
SET meter_stop = $2,
    stop_timestamp = $3,
    stopped_reason = $4,
    updated_seq_no = $5,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: AddMeterValues :exec
INSERT INTO transaction_meter_values (transaction_id, timestamp, sampled_values)
VALUES ($1, $2, $3);

-- name: GetMeterValues :many
SELECT * FROM transaction_meter_values
WHERE transaction_id = $1
ORDER BY timestamp ASC;
