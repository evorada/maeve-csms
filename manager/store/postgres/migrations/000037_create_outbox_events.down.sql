DROP SEQUENCE IF EXISTS outbox_published_seq;
DROP INDEX IF EXISTS idx_outbox_published_at;
DROP INDEX IF EXISTS idx_outbox_org_published;
DROP INDEX IF EXISTS idx_outbox_unpublished;
DROP TABLE IF EXISTS outbox_events;
