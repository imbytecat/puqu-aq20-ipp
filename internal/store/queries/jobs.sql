-- name: CreateJob :one
INSERT INTO print_jobs (
    name, user_name, state, document_format, copies, bytes, error, created_at
) VALUES (?, ?, 'pending', ?, ?, ?, NULL, ?)
RETURNING *;

-- name: GetJob :one
SELECT * FROM print_jobs WHERE id = ?;

-- name: ListJobs :many
SELECT * FROM print_jobs ORDER BY id DESC LIMIT ?;

-- name: ListJobsByState :many
SELECT * FROM print_jobs WHERE state = ? ORDER BY id ASC;

-- name: StartJob :execrows
UPDATE print_jobs SET state = 'processing', started_at = ?, error = NULL WHERE id = ? AND state = 'pending';

-- name: SetJobBytes :execrows
UPDATE print_jobs SET bytes = ? WHERE id = ? AND state = 'pending';

-- name: CompleteJob :execrows
UPDATE print_jobs SET state = 'completed', completed_at = ?, error = NULL WHERE id = ? AND state = 'processing';

-- name: CancelJob :execrows
UPDATE print_jobs SET state = 'canceled', completed_at = ? WHERE id = ? AND state IN ('pending', 'processing');

-- name: AbortJob :execrows
UPDATE print_jobs SET state = 'aborted', completed_at = ?, error = ? WHERE id = ? AND state IN ('pending', 'processing');

-- name: AbortInterruptedJobs :exec
UPDATE print_jobs
SET state = 'aborted', completed_at = ?, error = 'service restarted while job state was uncertain'
WHERE state IN ('pending', 'processing');

-- name: PruneJobs :exec
DELETE FROM print_jobs
WHERE id NOT IN (SELECT id FROM print_jobs ORDER BY id DESC LIMIT ?);
