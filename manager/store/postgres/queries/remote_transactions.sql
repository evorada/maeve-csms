-- name: SetRemoteStartTransactionRequest :one
INSERT INTO remote_start_transaction_requests (charge_station_id, id_tag, connector_id, charging_profile, status, send_after, request_type)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (charge_station_id) DO UPDATE SET
    id_tag = EXCLUDED.id_tag,
    connector_id = EXCLUDED.connector_id,
    charging_profile = EXCLUDED.charging_profile,
    status = EXCLUDED.status,
    send_after = EXCLUDED.send_after,
    request_type = EXCLUDED.request_type
RETURNING *;

-- name: GetRemoteStartTransactionRequest :one
SELECT * FROM remote_start_transaction_requests WHERE charge_station_id = $1;

-- name: DeleteRemoteStartTransactionRequest :exec
DELETE FROM remote_start_transaction_requests WHERE charge_station_id = $1;

-- name: ListRemoteStartTransactionRequests :many
SELECT * FROM remote_start_transaction_requests
WHERE charge_station_id > $1
ORDER BY charge_station_id ASC
LIMIT $2;

-- name: SetRemoteStopTransactionRequest :one
INSERT INTO remote_stop_transaction_requests (charge_station_id, transaction_id, status, send_after, request_type)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (charge_station_id) DO UPDATE SET
    transaction_id = EXCLUDED.transaction_id,
    status = EXCLUDED.status,
    send_after = EXCLUDED.send_after,
    request_type = EXCLUDED.request_type
RETURNING *;

-- name: GetRemoteStopTransactionRequest :one
SELECT * FROM remote_stop_transaction_requests WHERE charge_station_id = $1;

-- name: DeleteRemoteStopTransactionRequest :exec
DELETE FROM remote_stop_transaction_requests WHERE charge_station_id = $1;

-- name: ListRemoteStopTransactionRequests :many
SELECT * FROM remote_stop_transaction_requests
WHERE charge_station_id > $1
ORDER BY charge_station_id ASC
LIMIT $2;
