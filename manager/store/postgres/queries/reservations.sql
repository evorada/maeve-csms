-- The reservation belongs to the org of its charge station, or to the legacy org when
-- the charge station is not registered.

-- name: CreateReservation :exec
INSERT INTO reservations (
    reservation_id, charge_station_id, connector_id, id_tag, parent_id_tag, expiry_date, status, created_at, org_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8,
    COALESCE((SELECT cs.org_id FROM charge_stations cs WHERE cs.charge_station_id = $2), '00000000-0000-0000-0000-000000000000'::uuid)
);

-- name: GetReservation :one
SELECT * FROM reservations WHERE reservation_id = $1;

-- name: CancelReservation :exec
UPDATE reservations SET status = 'Cancelled' WHERE reservation_id = $1;

-- name: UpdateReservationStatus :exec
UPDATE reservations SET status = $2 WHERE reservation_id = $1;

-- name: GetActiveReservations :many
SELECT * FROM reservations
WHERE charge_station_id = $1 AND status = 'Accepted'
ORDER BY created_at ASC;

-- name: GetReservationByConnector :one
SELECT * FROM reservations
WHERE charge_station_id = $1 AND connector_id = $2 AND status = 'Accepted'
LIMIT 1;

-- name: ExpireReservations :execrows
UPDATE reservations SET status = 'Expired'
WHERE status = 'Accepted' AND expiry_date < NOW();
