-- name: CreateOrUpdateDisplayMessage :exec
INSERT INTO display_messages (
    charge_station_id, message_id, priority, state, start_date_time, end_date_time,
    transaction_id, content, language, format, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (charge_station_id, message_id)
DO UPDATE SET
    priority = EXCLUDED.priority,
    state = EXCLUDED.state,
    start_date_time = EXCLUDED.start_date_time,
    end_date_time = EXCLUDED.end_date_time,
    transaction_id = EXCLUDED.transaction_id,
    content = EXCLUDED.content,
    language = EXCLUDED.language,
    format = EXCLUDED.format,
    updated_at = EXCLUDED.updated_at;

-- name: GetDisplayMessage :one
SELECT * FROM display_messages
WHERE charge_station_id = $1 AND message_id = $2;

-- name: ListDisplayMessages :many
SELECT * FROM display_messages
WHERE charge_station_id = $1
ORDER BY message_id ASC;

-- name: ListDisplayMessagesByState :many
SELECT * FROM display_messages
WHERE charge_station_id = $1 AND state = $2
ORDER BY message_id ASC;

-- name: ListDisplayMessagesByPriority :many
SELECT * FROM display_messages
WHERE charge_station_id = $1 AND priority = $2
ORDER BY message_id ASC;

-- name: ListDisplayMessagesByStateAndPriority :many
SELECT * FROM display_messages
WHERE charge_station_id = $1 AND state = $2 AND priority = $3
ORDER BY message_id ASC;

-- name: DeleteDisplayMessage :exec
DELETE FROM display_messages
WHERE charge_station_id = $1 AND message_id = $2;

-- name: DeleteAllDisplayMessages :exec
DELETE FROM display_messages
WHERE charge_station_id = $1;
