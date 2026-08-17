-- name: GetSettings :one
SELECT * FROM app_settings WHERE id = 1;

-- name: UpdateSettings :one
UPDATE app_settings
SET ipp_name = ?, ipp_listen = ?, admin_listen = ?, advertise = ?, airprint = ?, updated_at = ?
WHERE id = 1
RETURNING *;

-- name: SetPrinterUUID :exec
UPDATE app_settings SET printer_uuid = ?, updated_at = ? WHERE id = 1;

-- name: ListDevices :many
SELECT * FROM ble_devices ORDER BY selected DESC, last_seen_at DESC, name ASC;

-- name: GetSelectedDevice :one
SELECT * FROM ble_devices WHERE selected = 1 LIMIT 1;

-- name: UpsertDevice :one
INSERT INTO ble_devices (
    native_id, name, address, write_uuid, notify_uuid, selected, last_seen_at, updated_at
) VALUES (?, ?, ?, ?, ?, 0, ?, ?)
ON CONFLICT(native_id) DO UPDATE SET
    name = excluded.name,
    address = excluded.address,
    write_uuid = excluded.write_uuid,
    notify_uuid = excluded.notify_uuid,
    last_seen_at = excluded.last_seen_at,
    updated_at = excluded.updated_at
RETURNING *;

-- name: ClearSelectedDevice :exec
UPDATE ble_devices SET selected = 0 WHERE selected = 1;

-- name: SelectDevice :execrows
UPDATE ble_devices SET selected = 1, updated_at = ? WHERE id = ?;

-- name: DeleteDevice :execrows
DELETE FROM ble_devices WHERE id = ?;
