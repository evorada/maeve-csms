-- Onboarding of charge stations that connect without being registered: what the CSMS
-- knows about a pending station (JSON, NULL for every other station), the password hash
-- of an approval that is in flight, and whether the station uses the onboarding password
-- that it shares with the other stations of its tenant. The pending stations of an org
-- are counted with idx_charge_stations_org_state.
ALTER TABLE charge_stations
    ADD COLUMN IF NOT EXISTS onboarding                     JSONB,
    ADD COLUMN IF NOT EXISTS pending_base64_sha256_password VARCHAR(255),
    ADD COLUMN IF NOT EXISTS shared_credentials             BOOLEAN NOT NULL DEFAULT false;
