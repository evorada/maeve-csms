-- name: InsertCommand :exec
INSERT INTO commands (
    id, org_id, station_key, ocpp_version, action, payload, status, result,
    error_code, error_description, progress, progress_at, actor, jti,
    created_at, sent_at, completed_at, expires_at, redact_paths
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19);

-- name: GetCommand :one
SELECT * FROM commands WHERE id = $1;

-- name: ListCommands :many
SELECT * FROM commands
WHERE (sqlc.narg('org_id')::uuid IS NULL OR org_id = sqlc.narg('org_id')::uuid)
  AND (sqlc.narg('station_key')::text IS NULL OR station_key = sqlc.narg('station_key')::text)
  AND (cardinality(sqlc.arg('actions')::text[]) = 0 OR action = ANY(sqlc.arg('actions')::text[]))
  AND (cardinality(sqlc.arg('statuses')::text[]) = 0 OR status = ANY(sqlc.arg('statuses')::text[]))
  AND (sqlc.narg('payload_contains')::jsonb IS NULL OR payload @> sqlc.narg('payload_contains')::jsonb)
  AND (sqlc.narg('progress_contains')::jsonb IS NULL OR progress @> sqlc.narg('progress_contains')::jsonb)
  AND (sqlc.narg('created_after')::timestamptz IS NULL OR created_at > sqlc.narg('created_after')::timestamptz)
  AND (sqlc.narg('created_before')::timestamptz IS NULL OR created_at < sqlc.narg('created_before')::timestamptz)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- The same conditions as ListCommands.
-- name: CountCommands :one
SELECT count(*) FROM commands
WHERE (sqlc.narg('org_id')::uuid IS NULL OR org_id = sqlc.narg('org_id')::uuid)
  AND (sqlc.narg('station_key')::text IS NULL OR station_key = sqlc.narg('station_key')::text)
  AND (cardinality(sqlc.arg('actions')::text[]) = 0 OR action = ANY(sqlc.arg('actions')::text[]))
  AND (cardinality(sqlc.arg('statuses')::text[]) = 0 OR status = ANY(sqlc.arg('statuses')::text[]))
  AND (sqlc.narg('payload_contains')::jsonb IS NULL OR payload @> sqlc.narg('payload_contains')::jsonb)
  AND (sqlc.narg('progress_contains')::jsonb IS NULL OR progress @> sqlc.narg('progress_contains')::jsonb)
  AND (sqlc.narg('created_after')::timestamptz IS NULL OR created_at > sqlc.narg('created_after')::timestamptz)
  AND (sqlc.narg('created_before')::timestamptz IS NULL OR created_at < sqlc.narg('created_before')::timestamptz);

-- Serialises the claims of one station until the transaction ends. The two-key form of
-- the advisory lock has a key space of its own, apart from the one-key locks (such as
-- the outbox relay lock); lock_class is a constant of the store that names this lock.
-- Two stations whose keys hash alike only wait for each other's claims.
-- name: LockStationCommands :exec
SELECT pg_advisory_xact_lock(sqlc.arg('lock_class')::int, hashtext(sqlc.arg('station_key')::text));

-- name: HasSentCommand :one
SELECT EXISTS (SELECT 1 FROM commands WHERE station_key = $1 AND status = 'sent');

-- name: NextQueuedCommandForUpdate :one
SELECT * FROM commands
WHERE station_key = sqlc.arg('station_key') AND status = 'queued' AND expires_at > sqlc.arg('now')
ORDER BY created_at, id
LIMIT 1
FOR UPDATE;

-- name: MarkCommandSent :one
UPDATE commands SET status = 'sent', sent_at = sqlc.arg('sent_at')
WHERE id = sqlc.arg('id') AND status = 'queued'
RETURNING *;

-- name: TransitionCommand :one
UPDATE commands
SET status            = sqlc.arg('status'),
    result            = COALESCE(sqlc.narg('result')::jsonb, result),
    error_code        = sqlc.narg('error_code'),
    error_description = sqlc.narg('error_description'),
    sent_at           = sqlc.narg('sent_at'),
    completed_at      = sqlc.narg('completed_at')
WHERE id = sqlc.arg('id') AND status = ANY(sqlc.arg('from_statuses')::text[])
RETURNING *;

-- name: SetCommandProgress :one
UPDATE commands SET progress = sqlc.arg('progress'), progress_at = sqlc.arg('progress_at')
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: RedactCommandPayload :exec
UPDATE commands SET payload = sqlc.arg('payload')
WHERE id = sqlc.arg('id');

-- name: ListQueuedStationKeys :many
SELECT DISTINCT station_key FROM commands
WHERE status = 'queued' AND expires_at > sqlc.arg('now') AND station_key > sqlc.arg('after_station_key')
ORDER BY station_key
LIMIT sqlc.arg('row_limit');

-- name: ListExpiredQueuedCommands :many
SELECT * FROM commands
WHERE status = 'queued' AND expires_at <= sqlc.arg('now')
ORDER BY expires_at, id
LIMIT sqlc.arg('row_limit');

-- name: ListOverdueSentCommands :many
SELECT * FROM commands
WHERE status = 'sent' AND sent_at <= sqlc.arg('sent_before')
ORDER BY sent_at, id
LIMIT sqlc.arg('row_limit');

-- Only terminal commands: a queued or sent command is never deleted, whatever its
-- completed_at. The subquery has an alias only for sqlc, which would find the columns
-- ambiguous without it.
-- name: DeleteCompletedCommandsBefore :execrows
DELETE FROM commands WHERE id IN (
    SELECT c.id FROM commands c
    WHERE c.completed_at < sqlc.arg('before') AND c.status NOT IN ('queued', 'sent')
    ORDER BY c.completed_at
    LIMIT sqlc.arg('row_limit'));
