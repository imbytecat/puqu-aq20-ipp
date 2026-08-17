-- name: ListProfiles :many
SELECT * FROM label_profiles ORDER BY name ASC, id ASC;

-- name: GetProfile :one
SELECT * FROM label_profiles WHERE id = ?;

-- name: CreateProfile :one
INSERT INTO label_profiles (
    name, width_um, height_um, gap_um, paper_type, darkness, speed, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateProfile :one
UPDATE label_profiles
SET name = ?, width_um = ?, height_um = ?, gap_um = ?, paper_type = ?, darkness = ?, speed = ?, updated_at = ?
WHERE id = ?
RETURNING *;

-- name: DeleteProfile :execrows
DELETE FROM label_profiles WHERE id = ?;
