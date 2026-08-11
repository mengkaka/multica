-- name: ListPMOSyncConfigs :many
SELECT * FROM pmo_sync_config
WHERE workspace_id = $1
ORDER BY updated_at DESC, created_at DESC;

-- name: GetPMOSyncConfig :one
SELECT * FROM pmo_sync_config
WHERE id = $1 AND workspace_id = $2;

-- name: GetPMOSyncConfigForUpdate :one
SELECT * FROM pmo_sync_config
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: CreatePMOSyncConfig :one
INSERT INTO pmo_sync_config (
    workspace_id, name, agent_id, root_external_key, orchestration_squad_id, created_by
) VALUES (
    $1, $2, $3, $4, sqlc.narg('orchestration_squad_id'), $5
)
RETURNING *;

-- name: UpdatePMOSyncConfig :one
UPDATE pmo_sync_config
SET name = $3,
    agent_id = $4,
    root_external_key = $5,
    schedule_enabled = $6,
    orchestration_squad_id = sqlc.narg('orchestration_squad_id'),
    next_run_at = CASE
        WHEN $6::boolean THEN COALESCE(next_run_at, now() + interval '30 minutes')
        ELSE NULL
    END,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: SetPMOSyncConfigOrchestrationIssue :one
UPDATE pmo_sync_config
SET orchestration_issue_id = @orchestration_issue_id,
    updated_at = now()
WHERE id = @id
  AND workspace_id = @workspace_id
  AND orchestration_issue_id IS NULL
RETURNING *;

-- name: SetPMOSyncConfigWorkloadProperty :one
UPDATE pmo_sync_config
SET workload_property_id = $3,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: MarkPMOSyncConfigApplied :one
UPDATE pmo_sync_config
SET last_applied_at = now(),
    next_run_at = CASE
        WHEN schedule_enabled THEN COALESCE(next_run_at, now() + interval '30 minutes')
        ELSE NULL
    END,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: MarkPMOSyncConfigRunStarted :one
UPDATE pmo_sync_config
SET last_run_at = now(),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: ClaimDuePMOSyncConfig :one
WITH candidate AS (
    SELECT c.id
    FROM pmo_sync_config AS c
    WHERE c.schedule_enabled = true
      AND c.last_applied_at IS NOT NULL
      AND c.next_run_at IS NOT NULL
      AND c.next_run_at <= now()
      AND NOT EXISTS (
          SELECT 1
          FROM pmo_sync_run AS r
          WHERE r.workspace_id = c.workspace_id
            AND r.config_id = c.id
            AND r.status IN ('queued', 'running')
      )
    ORDER BY c.next_run_at, c.created_at
    FOR UPDATE OF c SKIP LOCKED
    LIMIT 1
)
UPDATE pmo_sync_config AS c
SET next_run_at = now() + interval '30 minutes',
    last_run_at = now(),
    updated_at = now()
FROM candidate
WHERE c.id = candidate.id
RETURNING c.*;

-- name: DeletePMOSyncConfig :execrows
DELETE FROM pmo_sync_config AS c
WHERE c.id = $1
  AND c.workspace_id = $2
  AND NOT EXISTS (
      SELECT 1
      FROM pmo_sync_run AS r
      WHERE r.workspace_id = c.workspace_id
        AND r.config_id = c.id
        AND r.status IN ('queued', 'running')
  );

-- name: DeletePMOSyncConfigsByWorkspace :exec
DELETE FROM pmo_sync_config
WHERE workspace_id = $1;

-- name: CreatePMOSyncRun :one
INSERT INTO pmo_sync_run (
    workspace_id, config_id, trigger, status, requested_by
) VALUES (
    $1, $2, $3, 'queued', sqlc.narg('requested_by')
)
RETURNING *;

-- name: GetPMOSyncRun :one
SELECT * FROM pmo_sync_run
WHERE id = $1 AND workspace_id = $2;

-- name: GetPMOSyncRunForUpdate :one
SELECT * FROM pmo_sync_run
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: GetPMOSyncRunByAgentTask :one
SELECT * FROM pmo_sync_run
WHERE agent_task_id = $1 AND workspace_id = $2;

-- name: GetActivePMOSyncRun :one
SELECT * FROM pmo_sync_run
WHERE workspace_id = $1
  AND config_id = $2
  AND status IN ('queued', 'running')
ORDER BY created_at DESC
LIMIT 1;

-- name: ListPMOSyncRuns :many
SELECT * FROM pmo_sync_run
WHERE workspace_id = $1
  AND (sqlc.narg('config_id')::uuid IS NULL OR config_id = sqlc.narg('config_id'))
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: SetPMOSyncRunAgentTask :one
UPDATE pmo_sync_run
SET agent_task_id = $3
WHERE id = $1
  AND workspace_id = $2
  AND status = 'queued'
RETURNING *;

-- name: MarkPMOSyncRunRunning :one
UPDATE pmo_sync_run
SET status = 'running',
    started_at = COALESCE(started_at, now())
WHERE id = $1
  AND workspace_id = $2
  AND status = 'queued'
RETURNING *;

-- name: StorePMOSyncRunPreview :one
UPDATE pmo_sync_run
SET status = 'preview_ready',
    source_snapshot = $3,
    diff = $4,
    summary = $5,
    error_code = NULL,
    error_message = NULL,
    completed_at = now()
WHERE id = $1
  AND workspace_id = $2
  AND status IN ('queued', 'running')
RETURNING *;

-- name: FailPMOSyncRun :one
UPDATE pmo_sync_run
SET status = 'failed',
    error_code = $3,
    error_message = $4,
    completed_at = now()
WHERE id = $1
  AND workspace_id = $2
  AND status IN ('queued', 'running')
RETURNING *;

-- name: SetPMOSyncRunPreviewError :one
UPDATE pmo_sync_run
SET error_code = $3,
    error_message = $4
WHERE id = $1
  AND workspace_id = $2
  AND status = 'preview_ready'
RETURNING *;

-- name: MarkPMOSyncRunApplied :one
UPDATE pmo_sync_run
SET status = $3,
    diff = $4,
    summary = $5,
    error_code = NULL,
    error_message = NULL,
    applied_at = now()
WHERE id = $1
  AND workspace_id = $2
  AND status = 'preview_ready'
RETURNING *;

-- name: DeletePMOSyncRunsByConfig :exec
DELETE FROM pmo_sync_run
WHERE workspace_id = $1 AND config_id = $2;

-- name: DeletePMOSyncRunsByWorkspace :exec
DELETE FROM pmo_sync_run
WHERE workspace_id = $1;

-- name: ListPMOSyncLinks :many
SELECT * FROM pmo_sync_link
WHERE workspace_id = $1 AND config_id = $2
ORDER BY external_type, external_key;

-- name: GetPMOSyncLink :one
SELECT * FROM pmo_sync_link
WHERE workspace_id = $1
  AND config_id = $2
  AND external_type = $3
  AND external_key = $4;

-- name: ListUnresolvedPMOAssignees :many
SELECT * FROM pmo_sync_link
WHERE workspace_id = $1
  AND config_id = $2
  AND external_type = 'assignee'
  AND local_id IS NULL
  AND externally_removed_at IS NULL
ORDER BY external_key;

-- name: UpsertPMOSyncLink :one
INSERT INTO pmo_sync_link (
    workspace_id, config_id, external_type, external_key,
    external_display_number, external_numeric_id, external_task_id,
    parent_external_key, local_type, local_id, baseline_external,
    baseline_local, external_metadata, externally_removed_at
) VALUES (
    $1, $2, $3, $4,
    sqlc.narg('external_display_number'), sqlc.narg('external_numeric_id'), sqlc.narg('external_task_id'),
    sqlc.narg('parent_external_key'), sqlc.narg('local_type'), sqlc.narg('local_id'), $5,
    $6, $7, sqlc.narg('externally_removed_at')
)
ON CONFLICT (workspace_id, config_id, external_type, external_key) DO UPDATE
SET external_display_number = EXCLUDED.external_display_number,
    external_numeric_id = EXCLUDED.external_numeric_id,
    external_task_id = EXCLUDED.external_task_id,
    parent_external_key = EXCLUDED.parent_external_key,
    local_type = EXCLUDED.local_type,
    local_id = EXCLUDED.local_id,
    baseline_external = EXCLUDED.baseline_external,
    baseline_local = EXCLUDED.baseline_local,
    external_metadata = EXCLUDED.external_metadata,
    externally_removed_at = EXCLUDED.externally_removed_at,
    updated_at = now()
RETURNING *;

-- name: SetPMOAssigneeMapping :one
UPDATE pmo_sync_link
SET local_type = 'member',
    local_id = $4,
    externally_removed_at = NULL,
    updated_at = now()
WHERE workspace_id = $1
  AND config_id = $2
  AND external_type = 'assignee'
  AND external_key = $3
RETURNING *;

-- name: MarkPMOSyncLinksExternallyRemoved :execrows
UPDATE pmo_sync_link
SET externally_removed_at = now(),
    updated_at = now()
WHERE workspace_id = $1
  AND config_id = $2
  AND external_type = $3
  AND NOT (external_key = ANY($4::text[]))
  AND externally_removed_at IS NULL;

-- name: MarkPMOSyncLinkExternallyRemoved :one
UPDATE pmo_sync_link
SET externally_removed_at = now(),
    updated_at = now()
WHERE id = $1
  AND workspace_id = $2
  AND externally_removed_at IS NULL
RETURNING *;

-- name: ClearPMOSyncLinkExternallyRemoved :one
UPDATE pmo_sync_link
SET externally_removed_at = NULL,
    updated_at = now()
WHERE id = $1
  AND workspace_id = $2
  AND externally_removed_at IS NOT NULL
RETURNING *;

-- name: GetIssuePropertyByWorkspaceAndName :one
-- Reuse path for the PMO-owned numeric workload property: one definition per
-- workspace, shared by every PMO configuration in it.
SELECT * FROM issue_property
WHERE workspace_id = $1 AND name = $2;

-- name: DeletePMOSyncLinksByConfig :exec
DELETE FROM pmo_sync_link
WHERE workspace_id = $1 AND config_id = $2;

-- name: DeletePMOSyncLinksByWorkspace :exec
DELETE FROM pmo_sync_link
WHERE workspace_id = $1;
