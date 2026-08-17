-- name: ListDevices :many
SELECT * FROM ble_devices ORDER BY last_seen_at DESC, name ASC;

-- name: GetDevice :one
SELECT * FROM ble_devices WHERE id = ?;

-- name: UpsertDevice :one
INSERT INTO ble_devices (
    native_id, name, address, write_uuid, notify_uuid, last_seen_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(native_id) DO UPDATE SET
    name = excluded.name,
    address = excluded.address,
    write_uuid = excluded.write_uuid,
    notify_uuid = excluded.notify_uuid,
    last_seen_at = excluded.last_seen_at,
    updated_at = excluded.updated_at
RETURNING *;

-- name: DeleteDevice :execrows
DELETE FROM ble_devices WHERE id = ?;
