-- Auth
-- name: GetChargeStationAuth :one
SELECT * FROM charge_stations WHERE charge_station_id = $1;

-- name: SetChargeStationAuth :one
INSERT INTO charge_stations (
    charge_station_id, security_profile, base64_sha256_password, invalid_username_allowed
) VALUES ($1, $2, $3, $4)
ON CONFLICT (charge_station_id) DO UPDATE
SET security_profile = EXCLUDED.security_profile,
    base64_sha256_password = EXCLUDED.base64_sha256_password,
    invalid_username_allowed = EXCLUDED.invalid_username_allowed,
    updated_at = NOW()
RETURNING *;

-- Settings
-- name: GetChargeStationSettings :one
SELECT * FROM charge_station_settings WHERE charge_station_id = $1;

-- name: SetChargeStationSettings :one
INSERT INTO charge_station_settings (charge_station_id, settings)
VALUES ($1, $2)
ON CONFLICT (charge_station_id) DO UPDATE
SET settings = EXCLUDED.settings, updated_at = NOW()
RETURNING *;

-- Runtime
-- name: GetChargeStationRuntime :one
SELECT * FROM charge_station_runtime WHERE charge_station_id = $1;

-- name: SetChargeStationRuntime :one
INSERT INTO charge_station_runtime (charge_station_id, ocpp_version, vendor, model, serial_number, firmware_version)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (charge_station_id) DO UPDATE
SET ocpp_version = EXCLUDED.ocpp_version,
    vendor = EXCLUDED.vendor,
    model = EXCLUDED.model,
    serial_number = EXCLUDED.serial_number,
    firmware_version = EXCLUDED.firmware_version,
    updated_at = NOW()
RETURNING *;

-- Certificates
-- name: GetChargeStationCertificates :many
SELECT * FROM charge_station_certificates 
WHERE charge_station_id = $1
ORDER BY created_at DESC;

-- name: AddChargeStationCertificate :one
INSERT INTO charge_station_certificates (charge_station_id, certificate_type, certificate)
VALUES ($1, $2, $3)
RETURNING *;

-- name: DeleteChargeStationCertificates :exec
DELETE FROM charge_station_certificates WHERE charge_station_id = $1;

-- Triggers
-- name: GetChargeStationTriggers :many
SELECT * FROM charge_station_triggers
WHERE charge_station_id = $1
ORDER BY created_at ASC;

-- name: AddChargeStationTrigger :one
INSERT INTO charge_station_triggers (charge_station_id, message_type)
VALUES ($1, $2)
RETURNING *;

-- name: DeleteChargeStationTriggers :exec
DELETE FROM charge_station_triggers WHERE charge_station_id = $1;
