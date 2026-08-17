-- name: ListPrinters :many
SELECT * FROM printers ORDER BY name ASC, id ASC;

-- name: GetPrinter :one
SELECT * FROM printers WHERE id = ?;

-- name: GetPrinterBySlug :one
SELECT * FROM printers WHERE slug = ?;

-- name: CreatePrinter :one
INSERT INTO printers (
    name, slug, uuid, driver, device_id, profile_id, enabled, advertise, airprint, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdatePrinter :one
UPDATE printers
SET name = ?, driver = ?, device_id = ?, profile_id = ?, enabled = ?, advertise = ?, airprint = ?, updated_at = ?
WHERE id = ?
RETURNING *;

-- name: DeletePrinter :execrows
DELETE FROM printers WHERE id = ?;
