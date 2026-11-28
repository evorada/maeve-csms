-- The transactional outbox: domain events are written in the same transaction as the
-- change they describe and published from here. seq orders the events as they are
-- appended; published_seq orders them as they are published, and is the cursor of a
-- replay (a seq can commit after a higher one, a published_seq cannot).
CREATE TABLE IF NOT EXISTS outbox_events (
    seq           BIGSERIAL PRIMARY KEY,
    id            UUID        NOT NULL UNIQUE,
    org_id        TEXT        NOT NULL,
    type          TEXT        NOT NULL,
    subject       TEXT        NOT NULL,
    actor         TEXT        NOT NULL,
    traceparent   TEXT,
    event_time    TIMESTAMPTZ NOT NULL,
    data          JSONB       NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at  TIMESTAMPTZ,
    published_seq BIGINT UNIQUE
);
CREATE INDEX IF NOT EXISTS idx_outbox_unpublished ON outbox_events (seq) WHERE published_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_outbox_org_published ON outbox_events (org_id, published_seq) WHERE published_seq IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_outbox_published_at ON outbox_events (published_at) WHERE published_at IS NOT NULL;
CREATE SEQUENCE IF NOT EXISTS outbox_published_seq;
