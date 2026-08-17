-- name: ListDevices :many
SELECT * FROM devices WHERE transport = 'usb' ORDER BY last_seen_at DESC, name ASC;

-- name: GetDevice :one
SELECT * FROM devices WHERE id = ? AND transport = 'usb';

-- name: UpsertDevice :one
INSERT INTO devices (
    transport, native_id, name, address, write_uuid, notify_uuid, last_seen_at, updated_at
) VALUES ('usb', ?, ?, ?, '', NULL, ?, ?)
ON CONFLICT(native_id) DO UPDATE SET
    transport = 'usb',
    name = excluded.name,
    address = excluded.address,
    write_uuid = '',
    notify_uuid = NULL,
    last_seen_at = excluded.last_seen_at,
    updated_at = excluded.updated_at
RETURNING *;

-- name: DeleteDevice :execrows
DELETE FROM devices WHERE id = ? AND transport = 'usb';
