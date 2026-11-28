-- What the manager works out about a transaction after it has ended, from the readings
-- of its energy register: when it stopped charging (idle_started_at), how long it stayed
-- plugged in after that (idle_s) and how long it charged (charging_s), both in seconds,
-- and its average power while it charged (average_power_w, in W). analysed_at is when
-- that was worked out: NULL for a transaction that has no analysis.
ALTER TABLE transactions
    ADD COLUMN IF NOT EXISTS idle_started_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS idle_s          BIGINT,
    ADD COLUMN IF NOT EXISTS charging_s      BIGINT,
    ADD COLUMN IF NOT EXISTS average_power_w NUMERIC(10,1),
    ADD COLUMN IF NOT EXISTS analysed_at     TIMESTAMPTZ;
