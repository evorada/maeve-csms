-- The cost of the transactions is lost, but for its amount with VAT in last_cost. It can
-- be worked out again from the meter values and the tariff versions, which stay.
ALTER TABLE transactions
    DROP COLUMN IF EXISTS cost_rated_at,
    DROP COLUMN IF EXISTS cost_final,
    DROP COLUMN IF EXISTS cost_incl_vat,
    DROP COLUMN IF EXISTS cost_excl_vat,
    DROP COLUMN IF EXISTS cost_currency;
