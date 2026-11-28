-- The sweep of the command effects reads, every few seconds and for all stations, the
-- accepted commands created in the last hour or so whose progress waits for the
-- station. Without this index it would read the whole table each time.
CREATE INDEX IF NOT EXISTS commands_accepted_progress_created_idx ON commands (created_at)
    WHERE status = 'accepted' AND progress IS NOT NULL;
