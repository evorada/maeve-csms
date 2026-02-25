CREATE TABLE IF NOT EXISTS display_messages (
    charge_station_id TEXT NOT NULL,
    message_id INTEGER NOT NULL,
    priority TEXT NOT NULL,
    state TEXT,
    start_date_time TIMESTAMPTZ,
    end_date_time TIMESTAMPTZ,
    transaction_id TEXT,
    content TEXT NOT NULL,
    language TEXT,
    format TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (charge_station_id, message_id)
);

CREATE INDEX idx_display_messages_charge_station ON display_messages (charge_station_id);
CREATE INDEX idx_display_messages_state ON display_messages (state) WHERE state IS NOT NULL;
CREATE INDEX idx_display_messages_priority ON display_messages (priority);
