-- name: GetTransaction :one
SELECT * FROM transactions WHERE id = $1;

-- name: ListTransactions :many
SELECT * FROM transactions
ORDER BY start_timestamp DESC;

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

-- name: ListTransactionsFiltered :many
SELECT * FROM transactions
WHERE charge_station_id = $1
    AND (sqlc.narg('status')::text IS NULL 
        OR (sqlc.narg('status')::text = 'active' AND stop_timestamp IS NULL)
        OR (sqlc.narg('status')::text = 'completed' AND stop_timestamp IS NOT NULL)
        OR sqlc.narg('status')::text = 'all')
    AND (sqlc.narg('start_date')::timestamp IS NULL OR start_timestamp >= sqlc.narg('start_date')::timestamp)
    AND (sqlc.narg('end_date')::timestamp IS NULL OR start_timestamp <= sqlc.narg('end_date')::timestamp)
ORDER BY start_timestamp DESC
LIMIT $2 OFFSET $3;

-- name: CountTransactionsFiltered :one
SELECT COUNT(*) FROM transactions
WHERE charge_station_id = $1
    AND (sqlc.narg('status')::text IS NULL 
        OR (sqlc.narg('status')::text = 'active' AND stop_timestamp IS NULL)
        OR (sqlc.narg('status')::text = 'completed' AND stop_timestamp IS NOT NULL)
        OR sqlc.narg('status')::text = 'all')
    AND (sqlc.narg('start_date')::timestamp IS NULL OR start_timestamp >= sqlc.narg('start_date')::timestamp)
    AND (sqlc.narg('end_date')::timestamp IS NULL OR start_timestamp <= sqlc.narg('end_date')::timestamp);
