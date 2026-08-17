-- name: ListProfiles :many
SELECT * FROM label_profiles ORDER BY active DESC, name ASC;

-- name: GetActiveProfile :one
SELECT * FROM label_profiles WHERE active = 1 LIMIT 1;

-- name: GetProfile :one
SELECT * FROM label_profiles WHERE id = ?;

-- name: CreateProfile :one
INSERT INTO label_profiles (
    name, width_um, height_um, gap_um, paper_type, darkness, speed, active, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?)
RETURNING *;

-- name: UpdateProfile :one
UPDATE label_profiles
SET name = ?, width_um = ?, height_um = ?, gap_um = ?, paper_type = ?, darkness = ?, speed = ?, updated_at = ?
WHERE id = ?
RETURNING *;

-- name: ClearActiveProfile :exec
UPDATE label_profiles SET active = 0 WHERE active = 1;

-- name: ActivateProfile :execrows
UPDATE label_profiles SET active = 1, updated_at = ? WHERE id = ?;

-- name: DeleteProfile :execrows
DELETE FROM label_profiles WHERE id = ? AND active = 0;
