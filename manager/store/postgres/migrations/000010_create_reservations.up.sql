CREATE TABLE IF NOT EXISTS reservations (
    reservation_id INTEGER PRIMARY KEY,
    charge_station_id TEXT NOT NULL,
    connector_id INTEGER NOT NULL,
    id_tag TEXT NOT NULL,
    parent_id_tag TEXT,
    expiry_date TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL DEFAULT 'Accepted',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_reservations_charge_station ON reservations (charge_station_id);
CREATE INDEX idx_reservations_status ON reservations (status);
CREATE INDEX idx_reservations_expiry ON reservations (expiry_date) WHERE status = 'Accepted';
