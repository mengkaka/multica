-- name: ListProjects :many
SELECT * FROM project
WHERE workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('priority')::text IS NULL OR priority = sqlc.narg('priority'))
  AND (
    sqlc.arg('archived_mode')::text = 'all'
    OR (sqlc.arg('archived_mode')::text = 'only' AND archived_at IS NOT NULL)
    OR (sqlc.arg('archived_mode')::text = 'active' AND archived_at IS NULL)
  )
ORDER BY created_at DESC;

-- name: GetProject :one
SELECT * FROM project
WHERE id = $1;

-- name: GetProjectInWorkspace :one
SELECT * FROM project
WHERE id = $1 AND workspace_id = $2;

-- name: LockProjectInWorkspaceForUpdate :one
SELECT id FROM project
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: LockProjectForChatSessionCreate :one
-- Conflicts with project deletion so a chat session cannot commit a soft
-- project reference after the delete transaction has swept existing sessions.
SELECT id FROM project
WHERE id = $1 AND workspace_id = $2
FOR KEY SHARE;

-- name: LockProjectForDelete :one
-- Serializes project deletion with chat-session creation. The handler locks,
-- clears every soft chat reference, and deletes the project in one transaction.
SELECT id FROM project
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: CreateProject :one
INSERT INTO project (
    workspace_id, title, description, icon, status,
    lead_type, lead_id, priority, start_date, due_date
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
) RETURNING *;

-- name: UpdateProject :one
UPDATE project SET
    title = COALESCE(sqlc.narg('title'), title),
    description = sqlc.narg('description'),
    icon = sqlc.narg('icon'),
    status = COALESCE(sqlc.narg('status'), status),
    priority = COALESCE(sqlc.narg('priority'), priority),
    lead_type = sqlc.narg('lead_type'),
    lead_id = sqlc.narg('lead_id'),
    start_date = sqlc.narg('start_date'),
    due_date = sqlc.narg('due_date'),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteProject :exec
-- Defense-in-depth: workspace_id is a SQL-layer tenant guard. See DeleteIssue.
DELETE FROM project WHERE id = $1 AND workspace_id = $2;

-- name: ArchiveProject :one
UPDATE project
SET archived_at = COALESCE(archived_at, now()),
    archived_by = CASE WHEN archived_at IS NULL THEN @archived_by ELSE archived_by END,
    updated_at = CASE WHEN archived_at IS NULL THEN now() ELSE updated_at END
WHERE id = @id AND workspace_id = @workspace_id
RETURNING *;

-- name: RestoreProject :one
UPDATE project
SET archived_at = NULL,
    archived_by = NULL,
    updated_at = CASE WHEN archived_at IS NULL THEN updated_at ELSE now() END
WHERE id = @id AND workspace_id = @workspace_id
RETURNING *;

-- name: TryAutoArchivePMOProject :one
UPDATE project AS p
SET archived_at = COALESCE(p.archived_at, now()),
    archived_by = CASE WHEN p.archived_at IS NULL THEN NULL ELSE p.archived_by END,
    updated_at = CASE WHEN p.archived_at IS NULL THEN now() ELSE p.updated_at END
WHERE p.id = @id
  AND p.workspace_id = @workspace_id
  AND p.status IN ('completed', 'cancelled')
  AND EXISTS (
      SELECT 1
      FROM pmo_sync_link AS l
      WHERE l.workspace_id = p.workspace_id
        AND l.local_type = 'project'
        AND l.local_id = p.id
        AND l.external_type = 'requirement'
        AND l.parent_external_key IS NULL
        AND l.externally_removed_at IS NULL
        AND l.baseline_external->>'status' IN ('completed', 'cancelled')
  )
  AND EXISTS (
      SELECT 1
      FROM issue AS i
      WHERE i.workspace_id = p.workspace_id
        AND i.project_id = p.id
  )
  AND NOT EXISTS (
      SELECT 1
      FROM issue AS i
      WHERE i.workspace_id = p.workspace_id
        AND i.project_id = p.id
        AND i.status NOT IN ('done', 'cancelled')
  )
RETURNING p.*;

-- name: CountIssuesByProject :one
SELECT count(*) FROM issue
WHERE project_id = $1;

-- name: GetProjectIssueStats :many
SELECT project_id,
       count(*)::bigint AS total_count,
       count(*) FILTER (WHERE status IN ('done', 'cancelled'))::bigint AS done_count
FROM issue
WHERE project_id = ANY(sqlc.arg('project_ids')::uuid[])
GROUP BY project_id;
