ALTER TABLE charge_stations
    DROP COLUMN IF EXISTS shared_credentials,
    DROP COLUMN IF EXISTS pending_base64_sha256_password,
    DROP COLUMN IF EXISTS onboarding;
