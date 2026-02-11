-- name: UpsertChargingProfile :exec
INSERT INTO charging_profiles (
    charge_station_id, connector_id, charging_profile_id, transaction_id,
    stack_level, charging_profile_purpose, charging_profile_kind, recurrency_kind,
    valid_from, valid_to, charging_rate_unit, duration, start_schedule,
    min_charging_rate, charging_schedule_periods, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, NOW())
ON CONFLICT (charge_station_id, charging_profile_id) DO UPDATE SET
    connector_id = EXCLUDED.connector_id,
    transaction_id = EXCLUDED.transaction_id,
    stack_level = EXCLUDED.stack_level,
    charging_profile_purpose = EXCLUDED.charging_profile_purpose,
    charging_profile_kind = EXCLUDED.charging_profile_kind,
    recurrency_kind = EXCLUDED.recurrency_kind,
    valid_from = EXCLUDED.valid_from,
    valid_to = EXCLUDED.valid_to,
    charging_rate_unit = EXCLUDED.charging_rate_unit,
    duration = EXCLUDED.duration,
    start_schedule = EXCLUDED.start_schedule,
    min_charging_rate = EXCLUDED.min_charging_rate,
    charging_schedule_periods = EXCLUDED.charging_schedule_periods,
    updated_at = NOW();

-- name: GetChargingProfilesByStation :many
SELECT * FROM charging_profiles
WHERE charge_station_id = $1
ORDER BY stack_level ASC, charging_profile_id ASC;

-- name: GetChargingProfilesByStationAndConnector :many
SELECT * FROM charging_profiles
WHERE charge_station_id = $1 AND connector_id = $2
ORDER BY stack_level ASC, charging_profile_id ASC;

-- name: DeleteChargingProfileById :execrows
DELETE FROM charging_profiles
WHERE charge_station_id = $1 AND charging_profile_id = $2;

-- name: DeleteChargingProfilesByStation :execrows
DELETE FROM charging_profiles
WHERE charge_station_id = $1;

-- name: DeleteChargingProfilesByStationAndConnector :execrows
DELETE FROM charging_profiles
WHERE charge_station_id = $1 AND connector_id = $2;

-- name: DeleteChargingProfilesByStationAndPurpose :execrows
DELETE FROM charging_profiles
WHERE charge_station_id = $1 AND charging_profile_purpose = $2;

-- name: DeleteChargingProfilesByStationConnectorPurposeStack :execrows
DELETE FROM charging_profiles
WHERE charge_station_id = $1
  AND ($2::integer IS NULL OR connector_id = $2)
  AND ($3::text IS NULL OR charging_profile_purpose = $3)
  AND ($4::integer IS NULL OR stack_level = $4);
