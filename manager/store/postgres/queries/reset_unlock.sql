-- Reset Request
-- name: SetResetRequest :one
INSERT INTO reset_request (charge_station_id, type, status, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (charge_station_id) DO UPDATE
SET type = EXCLUDED.type,
    status = EXCLUDED.status,
    updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: GetResetRequest :one
SELECT * FROM reset_request WHERE charge_station_id = $1;

-- name: DeleteResetRequest :exec
DELETE FROM reset_request WHERE charge_station_id = $1;

-- Unlock Connector Request
-- name: SetUnlockConnectorRequest :one
INSERT INTO unlock_connector_request (charge_station_id, connector_id, status, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (charge_station_id) DO UPDATE
SET connector_id = EXCLUDED.connector_id,
    status = EXCLUDED.status,
    updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: GetUnlockConnectorRequest :one
SELECT * FROM unlock_connector_request WHERE charge_station_id = $1;

-- name: DeleteUnlockConnectorRequest :exec
DELETE FROM unlock_connector_request WHERE charge_station_id = $1;
