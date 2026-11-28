-- name: InsertOutboxEvent :one
INSERT INTO outbox_events (id, org_id, type, subject, actor, traceparent, event_time, data)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING seq, created_at;

-- The key of the lock is a constant of the store (outboxRelayLockKey): a transaction
-- that holds it is the only relay batch of every manager that shares the database.
-- name: TryLockOutboxRelay :one
SELECT pg_try_advisory_xact_lock(sqlc.arg('lock_key')::bigint) AS locked;

-- name: ClaimUnpublishedOutboxEvents :many
SELECT * FROM outbox_events WHERE published_at IS NULL ORDER BY seq LIMIT $1 FOR UPDATE;

-- name: MarkOutboxEventPublished :exec
UPDATE outbox_events SET published_at = $2, published_seq = nextval('outbox_published_seq') WHERE seq = $1;

-- name: ListPublishedOutboxEvents :many
SELECT * FROM outbox_events
WHERE published_seq > sqlc.arg('after_published_seq')::bigint
  AND (cardinality(sqlc.arg('org_ids')::text[]) = 0 OR org_id = ANY(sqlc.arg('org_ids')::text[]))
  AND (cardinality(sqlc.arg('types')::text[]) = 0 OR type = ANY(sqlc.arg('types')::text[]))
ORDER BY published_seq
LIMIT sqlc.arg('row_limit');

-- Deletes at most 10000 of the events published before $1, so that one statement does
-- not lock the whole table. The subquery has an alias only for sqlc, which would find
-- published_at ambiguous without it.
-- name: PurgePublishedOutboxEventsBatch :execrows
DELETE FROM outbox_events WHERE seq IN (
    SELECT o.seq FROM outbox_events o WHERE o.published_at < $1 ORDER BY o.seq LIMIT 10000);
