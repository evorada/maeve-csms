CREATE TABLE IF NOT EXISTS local_auth_list_versions (
    charge_station_id TEXT PRIMARY KEY,
    version INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS local_auth_list_entries (
    charge_station_id TEXT NOT NULL,
    id_tag TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'Accepted',
    expiry_date TEXT,
    parent_id_tag TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (charge_station_id, id_tag)
);

CREATE INDEX IF NOT EXISTS idx_local_auth_list_entries_station ON local_auth_list_entries (charge_station_id);
