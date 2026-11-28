-- One row per CSMS→station command: what was asked, by whom, and what came of it.
CREATE TABLE IF NOT EXISTS commands (
    id                UUID PRIMARY KEY,
    org_id            UUID NOT NULL,
    station_key       VARCHAR(80) NOT NULL,
    ocpp_version      TEXT NOT NULL CHECK (ocpp_version IN ('ocpp1.6', 'ocpp2.0.1')),
    action            TEXT NOT NULL,
    payload           JSONB NOT NULL,
    status            TEXT NOT NULL CHECK (status IN ('queued', 'sent', 'accepted', 'rejected', 'failed', 'timeout', 'cancelled')),
    result            JSONB,
    error_code        TEXT,
    error_description TEXT,
    progress          JSONB,
    progress_at       TIMESTAMPTZ,
    actor             TEXT,
    jti               TEXT,
    created_at        TIMESTAMPTZ NOT NULL,
    sent_at           TIMESTAMPTZ,
    completed_at      TIMESTAMPTZ,
    expires_at        TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS commands_org_station_created_idx ON commands (org_id, station_key, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS commands_station_action_created_idx ON commands (station_key, action, created_at DESC);
CREATE INDEX IF NOT EXISTS commands_queued_idx ON commands (station_key, created_at) WHERE status = 'queued';
CREATE INDEX IF NOT EXISTS commands_queued_expiry_idx ON commands (expires_at) WHERE status = 'queued';
CREATE INDEX IF NOT EXISTS commands_sent_idx ON commands (sent_at) WHERE status = 'sent';
CREATE INDEX IF NOT EXISTS commands_completed_idx ON commands (completed_at) WHERE completed_at IS NOT NULL;
-- At most one command in flight per station, across all manager replicas.
CREATE UNIQUE INDEX IF NOT EXISTS commands_one_sent_per_station_idx ON commands (station_key) WHERE status = 'sent';
