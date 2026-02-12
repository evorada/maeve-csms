-- name: GetLocalListVersion :one
SELECT version FROM local_auth_list_versions
WHERE charge_station_id = $1 LIMIT 1;

-- name: UpsertLocalListVersion :exec
INSERT INTO local_auth_list_versions (charge_station_id, version, updated_at)
VALUES ($1, $2, NOW())
ON CONFLICT (charge_station_id)
DO UPDATE SET version = $2, updated_at = NOW();

-- name: DeleteAllLocalAuthListEntries :exec
DELETE FROM local_auth_list_entries
WHERE charge_station_id = $1;

-- name: UpsertLocalAuthListEntry :exec
INSERT INTO local_auth_list_entries (charge_station_id, id_tag, status, expiry_date, parent_id_tag, updated_at)
VALUES ($1, $2, $3, $4, $5, NOW())
ON CONFLICT (charge_station_id, id_tag)
DO UPDATE SET status = $3, expiry_date = $4, parent_id_tag = $5, updated_at = NOW();

-- name: DeleteLocalAuthListEntry :exec
DELETE FROM local_auth_list_entries
WHERE charge_station_id = $1 AND id_tag = $2;

-- name: GetLocalAuthListEntries :many
SELECT * FROM local_auth_list_entries
WHERE charge_station_id = $1
ORDER BY id_tag ASC;
