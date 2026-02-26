-- name: UpsertVariableMonitoring :one
INSERT INTO variable_monitoring (
    charge_station_id,
    component_name,
    component_instance,
    variable_name,
    variable_instance,
    monitor_type,
    value,
    severity,
    transaction,
    created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
ON CONFLICT (id) DO UPDATE SET
    component_name = EXCLUDED.component_name,
    component_instance = EXCLUDED.component_instance,
    variable_name = EXCLUDED.variable_name,
    variable_instance = EXCLUDED.variable_instance,
    monitor_type = EXCLUDED.monitor_type,
    value = EXCLUDED.value,
    severity = EXCLUDED.severity,
    transaction = EXCLUDED.transaction
RETURNING id;

-- name: UpsertVariableMonitoringWithId :exec
INSERT INTO variable_monitoring (
    id,
    charge_station_id,
    component_name,
    component_instance,
    variable_name,
    variable_instance,
    monitor_type,
    value,
    severity,
    transaction,
    created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW())
ON CONFLICT (id) DO UPDATE SET
    component_name = EXCLUDED.component_name,
    component_instance = EXCLUDED.component_instance,
    variable_name = EXCLUDED.variable_name,
    variable_instance = EXCLUDED.variable_instance,
    monitor_type = EXCLUDED.monitor_type,
    value = EXCLUDED.value,
    severity = EXCLUDED.severity,
    transaction = EXCLUDED.transaction;

-- name: GetVariableMonitoring :one
SELECT id, charge_station_id, component_name, component_instance, variable_name, variable_instance,
       monitor_type, value, severity, transaction, created_at
FROM variable_monitoring
WHERE charge_station_id = $1 AND id = $2;

-- name: DeleteVariableMonitoring :exec
DELETE FROM variable_monitoring
WHERE charge_station_id = $1 AND id = $2;

-- name: ListVariableMonitoring :many
SELECT id, charge_station_id, component_name, component_instance, variable_name, variable_instance,
       monitor_type, value, severity, transaction, created_at
FROM variable_monitoring
WHERE charge_station_id = $1
ORDER BY id
LIMIT $2 OFFSET $3;

-- name: InsertChargeStationEvent :one
INSERT INTO charge_station_event (
    charge_station_id,
    timestamp,
    event_type,
    tech_code,
    tech_info,
    event_data,
    component_id,
    variable_id,
    cleared,
    created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
RETURNING id;

-- name: ListChargeStationEvents :many
SELECT id, charge_station_id, timestamp, event_type, tech_code, tech_info,
       event_data, component_id, variable_id, cleared, created_at
FROM charge_station_event
WHERE charge_station_id = $1
ORDER BY timestamp DESC
LIMIT $2 OFFSET $3;

-- name: CountChargeStationEvents :one
SELECT COUNT(*) FROM charge_station_event
WHERE charge_station_id = $1;

-- name: InsertDeviceReport :one
INSERT INTO device_report (
    charge_station_id,
    request_id,
    generated_at,
    report_type,
    report_data,
    created_at
)
VALUES ($1, $2, $3, $4, $5, NOW())
RETURNING id;

-- name: ListDeviceReports :many
SELECT id, charge_station_id, request_id, generated_at, report_type, report_data, created_at
FROM device_report
WHERE charge_station_id = $1
ORDER BY generated_at DESC
LIMIT $2 OFFSET $3;

-- name: CountDeviceReports :one
SELECT COUNT(*) FROM device_report
WHERE charge_station_id = $1;
