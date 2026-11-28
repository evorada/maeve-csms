-- The org of every standalone meter value is lost: a new up migration derives it again
-- from the transaction or, without one, from the org that owns the charge station then.
ALTER TABLE meter_values DROP COLUMN IF EXISTS org_id;
