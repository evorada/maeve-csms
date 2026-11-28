-- Auth
-- name: GetChargeStationAuth :one
SELECT * FROM charge_stations WHERE charge_station_id = $1;

-- SetChargeStationAuth without an org (NULL) registers a new charge station in the
-- legacy org and leaves the org of a charge station that is already there as it is.

-- name: SetChargeStationAuth :one
INSERT INTO charge_stations (
    charge_station_id, security_profile, base64_sha256_password, invalid_username_allowed, org_id,
    pending_base64_sha256_password
) VALUES (
    $1, $2, $3, $4,
    COALESCE(sqlc.narg('org_id')::uuid, '00000000-0000-0000-0000-000000000000'::uuid),
    sqlc.narg('pending_base64_sha256_password')
)
ON CONFLICT (charge_station_id) DO UPDATE
SET security_profile = EXCLUDED.security_profile,
    base64_sha256_password = EXCLUDED.base64_sha256_password,
    invalid_username_allowed = EXCLUDED.invalid_username_allowed,
    pending_base64_sha256_password = EXCLUDED.pending_base64_sha256_password,
    org_id = COALESCE(sqlc.narg('org_id')::uuid, charge_stations.org_id),
    updated_at = NOW()
RETURNING *;

-- Settings
-- name: GetChargeStationSettings :one
SELECT * FROM charge_station_settings WHERE charge_station_id = $1;

-- name: SetChargeStationSettings :one
INSERT INTO charge_station_settings (charge_station_id, settings)
VALUES ($1, $2)
ON CONFLICT (charge_station_id) DO UPDATE
SET settings = charge_station_settings.settings || EXCLUDED.settings, updated_at = NOW()
RETURNING *;

-- name: ListChargeStationSettings :many
SELECT * FROM charge_station_settings
WHERE charge_station_id > $1
ORDER BY charge_station_id ASC
LIMIT $2;

-- name: DeleteChargeStationSettings :exec
DELETE FROM charge_station_settings WHERE charge_station_id = $1;

-- Runtime
-- name: GetChargeStationRuntime :one
SELECT * FROM charge_station_runtime WHERE charge_station_id = $1;

-- name: SetChargeStationRuntime :one
INSERT INTO charge_station_runtime (
    charge_station_id, ocpp_version, vendor, model, serial_number, firmware_version,
    iccid, imsi, meter_serial_number, meter_type, last_boot_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (charge_station_id) DO UPDATE
SET ocpp_version = EXCLUDED.ocpp_version,
    vendor = EXCLUDED.vendor,
    model = EXCLUDED.model,
    serial_number = EXCLUDED.serial_number,
    firmware_version = EXCLUDED.firmware_version,
    iccid = EXCLUDED.iccid,
    imsi = EXCLUDED.imsi,
    meter_serial_number = EXCLUDED.meter_serial_number,
    meter_type = EXCLUDED.meter_type,
    last_boot_at = EXCLUDED.last_boot_at,
    updated_at = NOW()
RETURNING *;

-- Certificates
-- name: GetChargeStationCertificates :many
SELECT * FROM charge_station_certificates 
WHERE charge_station_id = $1
ORDER BY created_at DESC;

-- name: AddChargeStationCertificate :one
INSERT INTO charge_station_certificates (charge_station_id, certificate_id, certificate_type, certificate, certificate_installation_status, send_after)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateChargeStationCertificate :one
UPDATE charge_station_certificates
SET certificate = $3,
    certificate_installation_status = $4,
    send_after = $5
WHERE charge_station_id = $1 AND certificate_id = $2
RETURNING *;

-- name: DeleteChargeStationCertificates :exec
DELETE FROM charge_station_certificates WHERE charge_station_id = $1;

-- name: ListChargeStationCertificates :many
SELECT DISTINCT ON (charge_station_id) charge_station_id, certificate_id, certificate_type, certificate, certificate_installation_status, send_after, created_at
FROM charge_station_certificates
WHERE charge_station_id > $1
ORDER BY charge_station_id ASC, created_at DESC
LIMIT $2;

-- Triggers
-- name: GetChargeStationTrigger :one
SELECT * FROM charge_station_triggers
WHERE charge_station_id = $1;

-- name: SetChargeStationTrigger :one
INSERT INTO charge_station_triggers (charge_station_id, message_type, connector_id, trigger_status, send_after)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (charge_station_id) DO UPDATE
SET message_type = EXCLUDED.message_type,
    connector_id = EXCLUDED.connector_id,
    trigger_status = EXCLUDED.trigger_status,
    send_after = EXCLUDED.send_after
RETURNING *;

-- name: DeleteChargeStationTrigger :exec
DELETE FROM charge_station_triggers WHERE charge_station_id = $1;

-- name: ListChargeStationTriggers :many
SELECT * FROM charge_station_triggers
WHERE charge_station_id > $1
ORDER BY charge_station_id ASC
LIMIT $2;

-- Data Transfer
-- name: GetChargeStationDataTransfer :one
SELECT * FROM charge_station_data_transfer
WHERE charge_station_id = $1;

-- name: SetChargeStationDataTransfer :one
INSERT INTO charge_station_data_transfer (charge_station_id, vendor_id, message_id, data, status, send_after)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (charge_station_id) DO UPDATE
SET vendor_id = EXCLUDED.vendor_id,
    message_id = EXCLUDED.message_id,
    data = EXCLUDED.data,
    status = EXCLUDED.status,
    send_after = EXCLUDED.send_after,
    updated_at = NOW()
RETURNING *;

-- name: DeleteChargeStationDataTransfer :exec
DELETE FROM charge_station_data_transfer WHERE charge_station_id = $1;

-- name: ListChargeStationDataTransfers :many
SELECT * FROM charge_station_data_transfer
WHERE charge_station_id > $1
ORDER BY charge_station_id ASC
LIMIT $2;

-- Clear Cache
-- name: GetChargeStationClearCache :one
SELECT * FROM charge_station_clear_cache
WHERE charge_station_id = $1;

-- name: SetChargeStationClearCache :one
INSERT INTO charge_station_clear_cache (charge_station_id, status, send_after)
VALUES ($1, $2, $3)
ON CONFLICT (charge_station_id) DO UPDATE
SET status = EXCLUDED.status,
    send_after = EXCLUDED.send_after,
    updated_at = NOW()
RETURNING *;

-- name: DeleteChargeStationClearCache :exec
DELETE FROM charge_station_clear_cache WHERE charge_station_id = $1;

-- name: ListChargeStationClearCaches :many
SELECT * FROM charge_station_clear_cache
WHERE charge_station_id > $1
ORDER BY charge_station_id ASC
LIMIT $2;

-- Change Availability
-- name: GetChargeStationChangeAvailability :one
SELECT * FROM charge_station_change_availability
WHERE charge_station_id = $1;

-- name: SetChargeStationChangeAvailability :one
INSERT INTO charge_station_change_availability (charge_station_id, connector_id, evse_id, availability_type, status, send_after)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (charge_station_id) DO UPDATE
SET connector_id = EXCLUDED.connector_id,
    evse_id = EXCLUDED.evse_id,
    availability_type = EXCLUDED.availability_type,
    status = EXCLUDED.status,
    send_after = EXCLUDED.send_after,
    updated_at = NOW()
RETURNING *;

-- name: DeleteChargeStationChangeAvailability :exec
DELETE FROM charge_station_change_availability WHERE charge_station_id = $1;

-- name: ListChargeStationChangeAvailabilities :many
SELECT * FROM charge_station_change_availability
WHERE charge_station_id > $1
ORDER BY charge_station_id ASC
LIMIT $2;

-- name: SetChargeStationCertificateQuery :one
INSERT INTO charge_station_certificate_queries (charge_station_id, certificate_type, query_status, send_after)
VALUES ($1, $2, $3, $4)
ON CONFLICT (charge_station_id) DO UPDATE SET
    certificate_type = EXCLUDED.certificate_type,
    query_status = EXCLUDED.query_status,
    send_after = EXCLUDED.send_after
RETURNING *;

-- name: DeleteChargeStationCertificateQuery :exec
DELETE FROM charge_station_certificate_queries WHERE charge_station_id = $1;

-- name: LookupChargeStationCertificateQuery :one
SELECT * FROM charge_station_certificate_queries WHERE charge_station_id = $1;

-- name: ListChargeStationCertificateQueries :many
SELECT * FROM charge_station_certificate_queries
WHERE charge_station_id > $1
ORDER BY charge_station_id ASC
LIMIT $2;

-- name: SetChargeStationCertificateDeletion :one
INSERT INTO charge_station_certificate_deletions (charge_station_id, hash_algorithm, issuer_name_hash, issuer_key_hash, serial_number, deletion_status, send_after)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (charge_station_id) DO UPDATE SET
    hash_algorithm = EXCLUDED.hash_algorithm,
    issuer_name_hash = EXCLUDED.issuer_name_hash,
    issuer_key_hash = EXCLUDED.issuer_key_hash,
    serial_number = EXCLUDED.serial_number,
    deletion_status = EXCLUDED.deletion_status,
    send_after = EXCLUDED.send_after
RETURNING *;

-- name: DeleteChargeStationCertificateDeletion :exec
DELETE FROM charge_station_certificate_deletions WHERE charge_station_id = $1;

-- name: LookupChargeStationCertificateDeletion :one
SELECT * FROM charge_station_certificate_deletions WHERE charge_station_id = $1;

-- name: ListChargeStationCertificateDeletions :many
SELECT * FROM charge_station_certificate_deletions
WHERE charge_station_id > $1
ORDER BY charge_station_id ASC
LIMIT $2;

-- Registry: the record of a charge station with what it reported at its last boot.
-- created_at and updated_at are TIMESTAMP columns written with NOW() in the time zone
-- of the session; the cast reads them back as the same instant.

-- name: GetChargeStationRecord :one
SELECT
    cs.charge_station_id,
    cs.org_id,
    cs.location_id,
    cs.display_name,
    cs.max_capacity_kw,
    cs.allow_any_token,
    cs.state,
    cs.state_changed_at,
    cs.state_reason,
    cs.onboarding,
    cs.shared_credentials,
    cs.created_at::timestamptz AS created_at,
    cs.updated_at::timestamptz AS updated_at,
    r.ocpp_version,
    r.vendor,
    r.model,
    r.serial_number,
    r.firmware_version,
    r.iccid,
    r.imsi,
    r.meter_serial_number,
    r.meter_type,
    r.last_boot_at
FROM charge_stations cs
LEFT JOIN charge_station_runtime r ON r.charge_station_id = cs.charge_station_id
WHERE cs.charge_station_id = $1;

-- name: UpdateChargeStationRecord :one
UPDATE charge_stations
SET location_id = sqlc.narg('location_id')::uuid,
    display_name = sqlc.narg('display_name')::text,
    max_capacity_kw = sqlc.narg('max_capacity_kw')::float8,
    allow_any_token = sqlc.arg('allow_any_token')::boolean,
    updated_at = NOW()
WHERE charge_station_id = sqlc.arg('charge_station_id')
RETURNING updated_at::timestamptz;

-- name: SetChargeStationState :execrows
UPDATE charge_stations
SET state = sqlc.arg('state')::text,
    state_reason = sqlc.narg('state_reason')::text,
    state_changed_at = NOW(),
    updated_at = NOW()
WHERE charge_station_id = sqlc.arg('charge_station_id');

-- name: SetStationLocation :execrows
UPDATE charge_stations
SET location_id = sqlc.arg('location_id')::uuid,
    updated_at = NOW()
WHERE charge_station_id = sqlc.arg('charge_station_id');

-- The state and status lists are never NULL: an empty list selects every state or
-- status. A station without a status row has never connected.

-- name: ListChargeStationRecords :many
SELECT
    cs.charge_station_id,
    cs.org_id,
    cs.location_id,
    cs.display_name,
    cs.max_capacity_kw,
    cs.allow_any_token,
    cs.state,
    cs.state_changed_at,
    cs.state_reason,
    cs.onboarding,
    cs.shared_credentials,
    cs.created_at::timestamptz AS created_at,
    cs.updated_at::timestamptz AS updated_at,
    r.ocpp_version,
    r.vendor,
    r.model,
    r.serial_number,
    r.firmware_version,
    r.iccid,
    r.imsi,
    r.meter_serial_number,
    r.meter_type,
    r.last_boot_at
FROM charge_stations cs
LEFT JOIN charge_station_runtime r ON r.charge_station_id = cs.charge_station_id
LEFT JOIN charge_station_status st ON st.charge_station_id = cs.charge_station_id
WHERE cs.org_id = ANY(sqlc.arg('org_ids')::uuid[])
  AND (sqlc.narg('location_id')::uuid IS NULL OR cs.location_id = sqlc.narg('location_id')::uuid)
  AND (cardinality(sqlc.arg('states')::text[]) = 0 OR cs.state = ANY(sqlc.arg('states')::text[]))
  AND (sqlc.narg('vendor')::text IS NULL OR lower(r.vendor) = lower(sqlc.narg('vendor')::text))
  AND (sqlc.narg('model')::text IS NULL OR lower(r.model) = lower(sqlc.narg('model')::text))
  AND (sqlc.narg('connected')::boolean IS NULL OR COALESCE(st.connected, false) = sqlc.narg('connected')::boolean)
  AND (cardinality(sqlc.arg('statuses')::text[]) = 0 OR EXISTS (
        SELECT 1 FROM connector_status s
        WHERE s.charge_station_id = cs.charge_station_id
          AND s.status = ANY(sqlc.arg('statuses')::text[])))
  AND (sqlc.narg('query')::text IS NULL
       OR cs.charge_station_id ILIKE '%' || sqlc.narg('query')::text || '%'
       OR cs.display_name ILIKE '%' || sqlc.narg('query')::text || '%'
       OR r.serial_number ILIKE '%' || sqlc.narg('query')::text || '%'
       OR r.iccid ILIKE '%' || sqlc.narg('query')::text || '%')
  AND cs.charge_station_id > sqlc.arg('after_key')::text
ORDER BY cs.charge_station_id
LIMIT sqlc.arg('row_limit');

-- name: CountChargeStationRecords :one
SELECT count(*) FROM charge_stations
WHERE org_id = sqlc.arg('org_id')::uuid
  AND (cardinality(sqlc.arg('states')::text[]) = 0 OR state = ANY(sqlc.arg('states')::text[]));

-- Serialises the onboarding connections of one org until the transaction ends, so that
-- its pending stations are counted and created one connection at a time. lock_class is
-- a constant of the store that names this lock (see LockStationCommands).
-- name: LockOrgOnboarding :exec
SELECT pg_advisory_xact_lock(sqlc.arg('lock_class')::int, hashtext(sqlc.arg('org_id')::text));

-- name: GetChargeStationOnboardingForUpdate :one
SELECT state, onboarding FROM charge_stations
WHERE charge_station_id = $1
FOR UPDATE;

-- A station that was registered at the same moment is left as it is (no row).
-- name: CreatePendingChargeStation :execrows
INSERT INTO charge_stations (
    charge_station_id, security_profile, invalid_username_allowed, org_id, state, onboarding
) VALUES (
    sqlc.arg('charge_station_id'), sqlc.arg('security_profile'), false, sqlc.arg('org_id')::uuid,
    'pending', sqlc.arg('onboarding')::jsonb
)
ON CONFLICT (charge_station_id) DO NOTHING;

-- name: SetChargeStationOnboarding :exec
UPDATE charge_stations
SET onboarding = sqlc.arg('onboarding')::jsonb,
    updated_at = NOW()
WHERE charge_station_id = sqlc.arg('charge_station_id');
