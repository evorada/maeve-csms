-- What a transaction costs with its tariff, as the rating engine worked it out: the total
-- without and with VAT (cost_excl_vat, cost_incl_vat) in the currency of the tariff
-- (cost_currency). cost_final is false for the cost so far of a transaction that runs and
-- true for the cost of a transaction that has ended; cost_rated_at is when the cost was
-- worked out: NULL for a transaction that was not rated. last_cost (migration 000011)
-- keeps the amount with VAT, as before.
ALTER TABLE transactions
    ADD COLUMN IF NOT EXISTS cost_currency CHAR(3),
    ADD COLUMN IF NOT EXISTS cost_excl_vat NUMERIC(12,4),
    ADD COLUMN IF NOT EXISTS cost_incl_vat NUMERIC(12,4),
    ADD COLUMN IF NOT EXISTS cost_final    BOOLEAN,
    ADD COLUMN IF NOT EXISTS cost_rated_at TIMESTAMPTZ;
