-- name: CreateReservation :exec
INSERT INTO reservations (
    reservation_id, charge_station_id, connector_id, id_tag, parent_id_tag, expiry_date, status, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

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
