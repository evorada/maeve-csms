-- Transactions are identified by (charge_station_id, id): ids are only unique per station.

-- name: GetTransaction :one
SELECT * FROM transactions WHERE charge_station_id = $1 AND id = $2;

-- name: ListTransactions :many
SELECT * FROM transactions
ORDER BY start_timestamp DESC;

-- name: FindActiveTransaction :one
SELECT * FROM transactions 
WHERE charge_station_id = $1 AND stop_timestamp IS NULL
ORDER BY start_timestamp DESC
LIMIT 1;

-- CreateTransaction returns no row when the transaction already exists, instead of
-- raising a unique violation that would abort an enclosing database transaction.
-- The transaction belongs to the org of its charge station, or to the legacy org when
-- the charge station is not registered.

-- name: CreateTransaction :one
INSERT INTO transactions (
    id, charge_station_id, token_uid, token_type,
    meter_start, start_timestamp, offline, org_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    COALESCE((SELECT cs.org_id FROM charge_stations cs WHERE cs.charge_station_id = $2::varchar), '00000000-0000-0000-0000-000000000000'::uuid)
)
ON CONFLICT (charge_station_id, id) DO NOTHING
RETURNING *;

-- name: UpdateTransaction :one
UPDATE transactions
SET meter_stop = $3,
    stop_timestamp = $4,
    stopped_reason = $5,
    updated_seq_no = $6,
    updated_at = NOW()
WHERE charge_station_id = $1 AND id = $2
RETURNING *;

-- name: AddMeterValues :exec
INSERT INTO transaction_meter_values (charge_station_id, transaction_id, timestamp, sampled_values)
VALUES ($1, $2, $3, $4);

-- name: GetMeterValues :many
SELECT * FROM transaction_meter_values
WHERE charge_station_id = $1 AND transaction_id = $2
ORDER BY timestamp ASC;

-- name: UpdateTransactionDetails :execrows
UPDATE transactions SET
    evse_id         = COALESCE(sqlc.narg('evse_id'), evse_id),
    connector_id    = COALESCE(sqlc.narg('connector_id'), connector_id),
    start_timestamp = COALESCE(sqlc.narg('started_at'), start_timestamp),
    stop_timestamp  = COALESCE(sqlc.narg('stopped_at'), stop_timestamp),
    meter_start     = COALESCE(sqlc.narg('meter_start_wh'), meter_start),
    meter_stop      = COALESCE(sqlc.narg('meter_stop_wh'), meter_stop),
    energy_wh       = COALESCE(sqlc.narg('energy_wh'), energy_wh),
    stopped_reason  = COALESCE(sqlc.narg('stop_reason'), stopped_reason),
    id_token_group  = COALESCE(sqlc.narg('id_token_group'), id_token_group),
    reservation_id  = COALESCE(sqlc.narg('reservation_id'), reservation_id),
    updated_at      = NOW()
WHERE charge_station_id = sqlc.arg('charge_station_id') AND id = sqlc.arg('id');

-- The tariff of a transaction is set once: no row is changed when the transaction has a
-- tariff source already.
-- name: SetTransactionTariff :execrows
UPDATE transactions SET
    tariff_id      = sqlc.narg('tariff_id')::uuid,
    tariff_version = sqlc.narg('tariff_version')::int,
    tariff_source  = sqlc.arg('tariff_source')::text,
    updated_at     = NOW()
WHERE charge_station_id = sqlc.arg('charge_station_id') AND id = sqlc.arg('id') AND tariff_source IS NULL;

-- The analysis of an ended transaction. An earlier analysis is replaced. The average
-- power is rounded to the one decimal that the column keeps.
-- name: SetTransactionAnalysis :execrows
UPDATE transactions SET
    idle_started_at = sqlc.narg('idle_started_at')::timestamptz,
    idle_s          = sqlc.arg('idle_s')::bigint,
    charging_s      = sqlc.arg('charging_s')::bigint,
    average_power_w = sqlc.narg('average_power_w')::float8,
    analysed_at     = sqlc.arg('analysed_at')::timestamptz,
    updated_at      = NOW()
WHERE charge_station_id = sqlc.arg('charge_station_id') AND id = sqlc.arg('id');

-- name: NextOcpp16TransactionId :one
SELECT nextval('ocpp16_transaction_id_seq')::integer AS id;

-- The transactions of a charge station, of the given orgs: no org is every org.

-- name: ListTransactionsFiltered :many
SELECT * FROM transactions
WHERE charge_station_id = $1
    AND (cardinality(sqlc.arg('org_ids')::uuid[]) = 0 OR org_id = ANY(sqlc.arg('org_ids')::uuid[]))
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
    AND (cardinality(sqlc.arg('org_ids')::uuid[]) = 0 OR org_id = ANY(sqlc.arg('org_ids')::uuid[]))
    AND (sqlc.narg('status')::text IS NULL 
        OR (sqlc.narg('status')::text = 'active' AND stop_timestamp IS NULL)
        OR (sqlc.narg('status')::text = 'completed' AND stop_timestamp IS NOT NULL)
        OR sqlc.narg('status')::text = 'all')
    AND (sqlc.narg('start_date')::timestamp IS NULL OR start_timestamp >= sqlc.narg('start_date')::timestamp)
    AND (sqlc.narg('end_date')::timestamp IS NULL OR start_timestamp <= sqlc.narg('end_date')::timestamp);
