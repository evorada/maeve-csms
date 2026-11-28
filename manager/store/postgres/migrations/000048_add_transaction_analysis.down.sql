-- The analysis of the ended transactions is lost. It can be worked out again from the
-- meter values, which stay.
ALTER TABLE transactions
    DROP COLUMN IF EXISTS analysed_at,
    DROP COLUMN IF EXISTS average_power_w,
    DROP COLUMN IF EXISTS charging_s,
    DROP COLUMN IF EXISTS idle_s,
    DROP COLUMN IF EXISTS idle_started_at;
