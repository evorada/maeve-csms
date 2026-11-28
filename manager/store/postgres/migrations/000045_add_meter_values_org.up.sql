-- The org that owns a standalone meter value (what a charge station sends with
-- MeterValues): the org of its transaction when the transaction is known, else the org
-- that owned the charge station when the meter value arrived. It does not change when
-- the charge station moves to another org, so the history of a session stays with the
-- org that the session belongs to. The column has no default: a new row must say which
-- org it belongs to.
ALTER TABLE meter_values ADD COLUMN org_id UUID;

-- The meter values that are already there get the org of their transaction when the
-- transaction is known, else the org that owns the charge station now (the owner at the
-- time they arrived was not recorded), else the legacy org (the nil UUID: a charge
-- station that is not registered).
UPDATE meter_values mv
SET org_id = COALESCE(
    (SELECT t.org_id FROM transactions t
      WHERE t.charge_station_id = mv.charge_station_id AND t.id = mv.transaction_id),
    (SELECT cs.org_id FROM charge_stations cs
      WHERE cs.charge_station_id = mv.charge_station_id),
    '00000000-0000-0000-0000-000000000000'::uuid);

ALTER TABLE meter_values ALTER COLUMN org_id SET NOT NULL;
