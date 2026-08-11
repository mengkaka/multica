# PMO Project Intake And Archive Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Do not dispatch subagents for `SY-12`; the user selected one direct development Agent.

**Goal:** Extend the existing PMO requirement sync so imported Projects use requirement-number titles, optionally start one Squad-owned PMO root Issue, and support reversible Project archive/restore across server, Web/Desktop, realtime caches, Mobile defaults, and tests.

**Architecture:** Keep `pmo_sync_config`, `pmo_sync_run`, `pmo_sync_link`, snapshot validation, three-way diff, and preview/apply as the only external intake path. Add archive state to `project` and execution Squad/root-Issue linkage to `pmo_sync_config`; use existing Issue creation/assignment effects and `project:updated` events instead of a new workflow engine.

**Tech Stack:** Go 1.26, Chi, pgx/sqlc, PostgreSQL migrations, Next.js shared through `packages/views`, React Query, zod, Vitest, React Native/Expo realtime cache helpers.

---

## Execution Rules

- Work only on `feat/project-intake-archive` and keep `SY-12` assigned directly to `小码codex-fe`.
- Do not create child Issues for implementing `SY-12`.
- Read `AGENTS.md`, `CLAUDE.md`, this plan, the revised design spec, and the existing PMO design before editing.
- Before modifying any function/method/class, run GitNexus upstream impact for that exact symbol and report HIGH/CRITICAL results in `SY-12`.
- Before every commit, stage only the task files, run GitNexus `detect-changes --scope staged`, and confirm the reported files/processes match the task.
- Never archive `Multica-iworker`; handler/e2e tests create disposable Projects.
- Do not add foreign keys or cascades. Any new index must be concurrent and isolated in a single-statement migration.

### Task 1: Verify The GitHub Main Migration Baseline

GitHub `main` already contains the PMO schema as migrations `306`–`315` and
reserves prefixes `800+` for fork-local work. Do not replay the obsolete
GitLab-only `278`/`279` collision fix.

- [ ] **Step 1: Verify migration invariants**

```bash
go -C server test ./internal/migrations -count=1
```

Expected: PASS before implementation. New archive migrations use the
fork-reserved range and do not modify an already-applied migration.

### Task 2: Add Archive And PMO Orchestration Persistence

**Files:**
- Create: `server/migrations/800_project_archive.up.sql`
- Create: `server/migrations/800_project_archive.down.sql`
- Create: `server/migrations/801_pmo_orchestration.up.sql`
- Create: `server/migrations/801_pmo_orchestration.down.sql`
- Modify: `server/pkg/db/queries/project.sql`
- Modify: `server/pkg/db/queries/pmo.sql`
- Regenerate: `server/pkg/db/generated/project.sql.go`
- Regenerate: `server/pkg/db/generated/pmo.sql.go`
- Regenerate: `server/pkg/db/generated/models.go`
- Test: `server/internal/migrations/migrations_lint_test.go`

- [ ] **Step 1: Add migration tests that require the four nullable columns**

In a focused migration test, migrate a clean test database and assert these queries succeed:

```sql
SELECT archived_at, archived_by FROM project LIMIT 0;
SELECT orchestration_squad_id, orchestration_issue_id FROM pmo_sync_config LIMIT 0;
```

Run the focused test and observe FAIL before creating migrations.

- [ ] **Step 2: Add the reversible migrations**

`800_project_archive.up.sql`:

```sql
ALTER TABLE project
    ADD COLUMN archived_at timestamptz,
    ADD COLUMN archived_by uuid;
```

`800_project_archive.down.sql`:

```sql
ALTER TABLE project
    DROP COLUMN archived_by,
    DROP COLUMN archived_at;
```

`801_pmo_orchestration.up.sql`:

```sql
ALTER TABLE pmo_sync_config
    ADD COLUMN orchestration_squad_id uuid,
    ADD COLUMN orchestration_issue_id uuid;
```

`801_pmo_orchestration.down.sql`:

```sql
ALTER TABLE pmo_sync_config
    DROP COLUMN orchestration_issue_id,
    DROP COLUMN orchestration_squad_id;
```

- [ ] **Step 3: Add archive-aware Project queries**

Change `ListProjects` to require `archived_mode` with this predicate:

```sql
AND (
    sqlc.arg('archived_mode')::text = 'all'
    OR (sqlc.arg('archived_mode')::text = 'only' AND archived_at IS NOT NULL)
    OR (sqlc.arg('archived_mode')::text = 'active' AND archived_at IS NULL)
)
```

Add idempotent workspace-scoped writes:

```sql
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
```

Add `TryAutoArchivePMOProject` using `pmo_sync_link.baseline_external->>'status'`, canonical Project status, one-or-more Issues, and `NOT EXISTS` for non-terminal Issues. It returns no row when the guard is false and preserves an existing manual archive timestamp.

- [ ] **Step 4: Extend PMO config queries**

Pass nullable `orchestration_squad_id` through `CreatePMOSyncConfig` and `UpdatePMOSyncConfig`. Add:

```sql
-- name: SetPMOSyncConfigOrchestrationIssue :one
UPDATE pmo_sync_config
SET orchestration_issue_id = @orchestration_issue_id,
    updated_at = now()
WHERE id = @id
  AND workspace_id = @workspace_id
  AND orchestration_issue_id IS NULL
RETURNING *;
```

- [ ] **Step 5: Regenerate and verify**

```bash
rtk make sqlc
rtk go -C server test ./internal/migrations ./pkg/db/generated -count=1
```

Expected: PASS with no generated diff outside Project/PMO models and queries.

- [ ] **Step 6: Detect and commit**

```bash
rtk git add server/migrations/800_project_archive.* server/migrations/801_pmo_orchestration.* server/pkg/db/queries/project.sql server/pkg/db/queries/pmo.sql server/pkg/db/generated
rtk node .gitnexus/run.cjs detect-changes --scope staged
rtk git commit -m "feat(projects): persist archive and PMO orchestration state"
```

### Task 3: Implement Project Archive, Restore, And List Modes

**Files:**
- Modify: `server/internal/handler/project.go`
- Modify: `server/cmd/server/router.go`
- Create: `server/internal/handler/project_archive_test.go`
- Test: `server/internal/handler/project_dates_test.go`

- [ ] **Step 1: Write failing handler tests**

Cover these exact cases with disposable Projects:

```go
func TestProjectArchiveRestoreIsIdempotentAndPreservesChildren(t *testing.T)
func TestListProjectsArchiveModes(t *testing.T)
func TestListProjectsRejectsInvalidArchiveMode(t *testing.T)
func TestProjectArchiveRequiresOwnerOrAdmin(t *testing.T)
func TestGetArchivedProjectStillReturnsDetail(t *testing.T)
```

The first test inserts an Issue, project resource, comment, and task history row; after archive and restore it asserts their counts are unchanged.

- [ ] **Step 2: Run tests to verify RED**

```bash
rtk go -C server test ./internal/handler -run 'ProjectArchive|ListProjectsArchive|GetArchivedProject' -count=1
```

Expected: FAIL because routes, response fields, and archive filtering do not exist.

- [ ] **Step 3: Extend the response and list parser**

Add to `ProjectResponse` and `projectToResponse`:

```go
ArchivedAt *string `json:"archived_at"`
ArchivedBy *string `json:"archived_by"`
```

Parse `archived` as `active` by default, accept only `active`, `only`, or `all`, and pass it as `ArchivedMode` to `ListProjects`.

- [ ] **Step 4: Add owner/admin archive handlers**

Implement `ArchiveProject` and `RestoreProject` beside `DeleteProject`. Both must:

```go
project, err := h.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: id, WorkspaceID: wsID})
requester, ok := h.requireWorkspaceRole(w, r, uuidToString(project.WorkspaceID), "project not found", "owner", "admin")
```

Call the idempotent query, load Issue/resource counts, publish `protocol.EventProjectUpdated` with the full response, and return `200`.

- [ ] **Step 5: Register routes and keep hard delete**

Inside `/api/projects/{id}` add:

```go
r.Post("/archive", h.ArchiveProject)
r.Post("/restore", h.RestoreProject)
```

Keep `r.Delete("/", h.DeleteProject)` unchanged.

- [ ] **Step 6: Verify and commit**

```bash
rtk go -C server test ./internal/handler -run 'ProjectArchive|ListProjectsArchive|GetArchivedProject|ProjectStartDueDate' -count=1
rtk git add server/internal/handler/project.go server/internal/handler/project_archive_test.go server/cmd/server/router.go
rtk node .gitnexus/run.cjs detect-changes --scope staged
rtk git commit -m "feat(projects): add archive and restore API"
```

### Task 4: Add Archive-Aware Core API And Query Caches

**Files:**
- Modify: `packages/core/types/project.ts`
- Modify: `packages/core/api/schemas.ts`
- Modify: `packages/core/api/schemas.test.ts`
- Modify: `packages/core/api/client.ts`
- Modify: `packages/core/api/client.test.ts`
- Modify: `packages/core/projects/queries.ts`
- Modify: `packages/core/projects/mutations.ts`
- Modify: `packages/core/projects/mutations.test.tsx`

- [ ] **Step 1: Write failing wire and cache tests**

Add tests proving:

```ts
expect(parseProject({ ...minimumProject, archived_at: undefined })).toMatchObject({
  archived_at: null,
  archived_by: null,
});
expect(projectKeys.list("ws-1", "active")).not.toEqual(projectKeys.list("ws-1", "only"));
```

Also assert `archiveProject` and `restoreProject` send `POST` to the exact endpoints and invalidate all Project lists plus the detail.

- [ ] **Step 2: Add types and schema defaults**

```ts
export type ProjectArchiveMode = "active" | "only" | "all";

export interface Project {
  // existing fields
  archived_at: string | null;
  archived_by: string | null;
}
```

Use nullable/defaulted zod fields so older payloads become null:

```ts
archived_at: z.string().nullable().default(null),
archived_by: z.string().nullable().default(null),
```

- [ ] **Step 3: Extend the client and query keys**

```ts
async listProjects(params: { status?: string; archived?: ProjectArchiveMode } = {})
async archiveProject(id: string): Promise<Project>
async restoreProject(id: string): Promise<Project>
```

Define keys with a defaulted mode:

```ts
list: (wsId: string, archived: ProjectArchiveMode = "active") =>
  [...projectKeys.all(wsId), "list", archived] as const,
```

- [ ] **Step 4: Add non-optimistic archive mutations**

Archive/restore are confirmation flows. Await the server response, set the detail, and invalidate `projectKeys.all(wsId)`; do not optimistically hide a Project before success.

- [ ] **Step 5: Verify and commit**

```bash
rtk pnpm --filter @multica/core test -- api/client.test.ts api/schemas.test.ts projects/mutations.test.tsx
rtk pnpm --filter @multica/core typecheck
rtk git add packages/core/types/project.ts packages/core/api packages/core/projects
rtk node .gitnexus/run.cjs detect-changes --scope staged
rtk git commit -m "feat(core): support archived project queries"
```

### Task 5: Add Shared Web/Desktop Archive UI

**Files:**
- Modify: `packages/views/projects/components/projects-page.tsx`
- Modify: `packages/views/projects/components/projects-page.test.tsx`
- Modify: `packages/views/projects/components/project-detail.tsx`
- Modify: `packages/views/projects/components/project-detail.test.tsx`
- Modify: `packages/views/locales/en/projects.json`
- Modify: `packages/views/locales/zh-Hans/projects.json`
- Modify: `packages/views/locales/ja/projects.json`
- Modify: `packages/views/locales/ko/projects.json`

- [ ] **Step 1: Write failing shared-view tests**

Test all of these user-visible states:

```ts
it("switches between active and archived query modes")
it("archives only after confirmation and keeps history copy visible")
it("offers restore in archived rows")
it("hides archive and restore from non-admin members")
it("shows archived detail with a restore action")
```

- [ ] **Step 2: Add the list mode control**

Use the existing segmented/tabs primitive already imported by the Projects page. Keep mode local to the page:

```ts
const [archiveMode, setArchiveMode] = useState<"active" | "only">("active");
const projects = useQuery(projectListOptions(wsId, archiveMode));
```

Labels are `Active` / `Archived` and `活跃` / `已归档` per the repository glossary.

- [ ] **Step 3: Add row actions and confirmation**

For owner/admin users, active rows expose Archive and archived rows expose Restore. The confirmation body must state that Issues, resources, comments, chat, and run history remain available. Keep Delete as a separate action.

- [ ] **Step 4: Add detail archived state**

Show a compact archived indicator near Project properties. Keep related Issues/resources rendered. Restore calls `useRestoreProject`; active detail exposes Archive through the existing actions menu.

- [ ] **Step 5: Verify layout and behavior**

```bash
rtk pnpm --filter @multica/views test -- projects-page.test.tsx project-detail.test.tsx
rtk pnpm --filter @multica/views typecheck
```

Expected: PASS on shared Web/Desktop behavior with no app-specific routing imports.

- [ ] **Step 6: Detect and commit**

```bash
rtk git add packages/views/projects packages/views/locales/*/projects.json
rtk node .gitnexus/run.cjs detect-changes --scope staged
rtk git commit -m "feat(views): manage archived projects"
```

### Task 6: Keep Web/Desktop And Mobile Realtime Caches Correct

**Files:**
- Modify: `packages/core/realtime/use-realtime-sync.ts`
- Modify: `packages/core/realtime/use-realtime-sync.test.ts`
- Modify: `apps/mobile/data/schemas.ts`
- Modify: `apps/mobile/data/realtime/project-ws-updaters.ts`
- Modify: `apps/mobile/data/realtime/project-ws-updaters.test.ts`
- Modify: `apps/mobile/data/realtime/use-projects-realtime.ts`

- [ ] **Step 1: Read `apps/mobile/CLAUDE.md` and write failing tests**

Tests must prove a full `project:updated` payload with non-null `archived_at` is removed from the active list while its detail cache remains readable; a payload with null `archived_at` can be reinserted only when the active list already has enough context or after invalidation.

- [ ] **Step 2: Patch shared realtime lists by archive mode**

For every cached Project list under `projectKeys.all(wsId)`, retain the payload only when it matches that key's mode. Patch detail with the full payload. Do not write server data to Zustand.

- [ ] **Step 3: Patch Mobile active lists**

Add nullable/defaulted archive fields to the Mobile Project schema. In `project:updated`:

```ts
if (payload.project.archived_at) {
  removeFromProjectsList(qc, wsId, payload.project.id);
} else {
  patchProjectsList(qc, wsId, payload.project);
}
patchProjectDetail(qc, wsId, payload.project);
```

- [ ] **Step 4: Verify and commit**

```bash
rtk pnpm --filter @multica/core test -- use-realtime-sync.test.ts
rtk pnpm --dir apps/mobile test -- project-ws-updaters.test.ts
rtk git add packages/core/realtime apps/mobile/data
rtk node .gitnexus/run.cjs detect-changes --scope staged
rtk git commit -m "fix(realtime): filter archived projects from active caches"
```

### Task 7: Configure The PMO Execution Squad

**Files:**
- Modify: `server/internal/service/pmo.go`
- Modify: `server/internal/service/pmo_test.go`
- Modify: `server/internal/handler/pmo.go`
- Modify: `server/internal/handler/pmo_test.go`
- Modify: `packages/core/types/pmo.ts`
- Modify: `packages/core/api/schemas.ts`
- Modify: `packages/core/api/schemas.test.ts`
- Modify: `packages/views/pmo/pmo-page.tsx`
- Modify: `packages/views/pmo/pmo-page.test.tsx`
- Modify: `packages/views/locales/*/pmo.json`

- [ ] **Step 1: Write failing config validation tests**

Cover null compatibility, valid same-workspace Squad, cross-workspace rejection, missing Agent Leader rejection, and preserving `orchestration_issue_id` when only config fields change.

- [ ] **Step 2: Extend PMO request/response types**

```go
OrchestrationSquadID *string `json:"orchestration_squad_id"`
OrchestrationIssueID *string `json:"orchestration_issue_id"`
```

The create/update request accepts nullable `orchestration_squad_id`; clients default both response fields to null.

- [ ] **Step 3: Validate the Squad at the service boundary**

When a Squad ID is present, load it in the same workspace and require an active Agent Leader. Store the Squad ID only after validation. Do not require it for existing configurations or preview-only sync.

- [ ] **Step 4: Add one compact Squad selector to the PMO config dialog**

Reuse `squadListOptions(wsId)` and existing actor-picker visual patterns. Do not add a new global store or a second PMO form.

- [ ] **Step 5: Verify and commit**

```bash
rtk go -C server test ./internal/service ./internal/handler -run 'PMOConfig|OrchestrationSquad' -count=1
rtk pnpm --filter @multica/core test -- api/schemas.test.ts
rtk pnpm --filter @multica/views test -- pmo-page.test.tsx
rtk git add server/internal/service/pmo* server/internal/handler/pmo* packages/core/types/pmo.ts packages/core/api/schemas* packages/views/pmo packages/views/locales/*/pmo.json
rtk node .gitnexus/run.cjs detect-changes --scope staged
rtk git commit -m "feat(pmo): configure execution squad"
```

### Task 8: Format Imported Project Titles And Create One Root Issue

**Files:**
- Modify: `server/internal/service/pmo_diff.go`
- Modify: `server/internal/service/pmo_diff_test.go`
- Modify: `server/internal/service/pmo_apply.go`
- Modify: `server/internal/service/pmo_apply_test.go`
- Modify: `server/internal/service/issue.go`

- [ ] **Step 1: Write failing PMO apply tests**

Add tests with these assertions:

```go
project.Title == "REQ-1234 Add invoice export"
root.ProjectID == project.ID
root.Status == "todo"
root.AssigneeType.String == "squad"
root.AssigneeID == config.OrchestrationSquadID
config.OrchestrationIssueID == root.ID
```

Replay the same run/apply and a concurrent retry; assert one Project, one orchestration root, and one initial Leader task. Also prove configs with no Squad retain current PMO behavior.

- [ ] **Step 2: Put formatted title into the existing three-way diff**

Add one pure helper:

```go
func pmoProjectTitle(requirement PMORequirement) string {
	return strings.TrimSpace(requirement.DisplayNumber) + " " + strings.TrimSpace(requirement.Title)
}
```

Use it only for the parent requirement's Project external values. Child requirement/task Issue titles stay unchanged.

- [ ] **Step 3: Create the root inside `applySnapshotInTx`**

After the parent Project ID is known, lock/reload the config. If a Squad is configured and no root ID exists, call the existing `IssueService.createInTx` with:

```go
IssueCreateParams{
	WorkspaceID: workspaceID,
	Title: pmoProjectTitle(snapshot.Parent),
	Description: pgtype.Text{String: snapshot.Parent.Description, Valid: true},
	Status: "todo",
	Priority: "none",
	AssigneeType: pgtype.Text{String: "squad", Valid: true},
	AssigneeID: config.OrchestrationSquadID,
	CreatorType: "member",
	CreatorID: config.CreatedBy,
	ProjectID: projectID,
}
```

Store the Issue ID with the compare-and-set query and append it to the existing post-commit `createdIssues` effects.

- [ ] **Step 4: Validate stored linkage and reopen**

If `orchestration_issue_id` exists, require the Issue to match workspace and Project. Update its title/description without triggering a new run. When the applied parent status is non-terminal and the root is `done`/`cancelled`, update it to `todo` and run the existing assignment effect once after commit.

- [ ] **Step 5: Verify and commit**

```bash
rtk go -C server test ./internal/service -run 'PMO.*(Title|Orchestration|Retry|Concurrent|Reopen)' -count=1
rtk git add server/internal/service/pmo_diff* server/internal/service/pmo_apply* server/internal/service/issue.go
rtk node .gitnexus/run.cjs detect-changes --scope staged
rtk git commit -m "feat(pmo): start one orchestration root issue"
```

### Task 9: Reconcile Automatic Archive After PMO Apply And Issue Completion

**Files:**
- Modify: `server/internal/service/pmo_apply.go`
- Modify: `server/internal/service/pmo_apply_test.go`
- Modify: `server/internal/handler/pmo.go`
- Modify: `server/internal/handler/pmo_agent_task.go`
- Modify: `server/internal/handler/issue.go`
- Create: `server/internal/handler/project_auto_archive_test.go`

- [ ] **Step 1: Write failing guard tests**

Cover the full truth table: external active, Project active, zero Issues, one non-terminal Issue, all terminal Issues, manually restored terminal Project, external reopen, single Issue update, and batch Issue update.

- [ ] **Step 2: Reconcile inside PMO apply**

After all canonical writes and link baselines are updated, call `TryAutoArchivePMOProject` in the same transaction. For non-terminal `snapshot.Parent.Status`, call `RestoreProject` and reopen only a terminal orchestration root.

- [ ] **Step 3: Reconcile terminal Issue transitions**

After `UpdateIssue` writes a transition into `done`/`cancelled`, attempt auto archive for its valid Project ID. In `BatchUpdateIssues`, collect unique affected Project IDs and reconcile once per Project after the loop.

- [ ] **Step 4: Publish full Project updates after commit**

Extend the PMO apply result to return changed Projects alongside the run. Both manual `ApplyPMORun` and scheduled completion in `pmo_agent_task.go` publish `project:updated`. Issue handlers publish the same event when their reconciliation changed archive state.

- [ ] **Step 5: Verify and commit**

```bash
rtk go -C server test ./internal/service ./internal/handler -run 'PMO.*Archive|ProjectAutoArchive|Batch.*Archive|ExternalReopen' -count=1
rtk git add server/internal/service/pmo_apply* server/internal/handler/pmo* server/internal/handler/issue.go server/internal/handler/project_auto_archive_test.go
rtk node .gitnexus/run.cjs detect-changes --scope staged
rtk git commit -m "feat(projects): reconcile PMO archive lifecycle"
```

### Task 10: Final Regression, Browser Acceptance, And Handoff

**Files:**
- Modify if behavior changed: `server/internal/service/builtin_skills/multica-projects-and-resources/SKILL.md`
- Modify if source map changed: `server/internal/service/builtin_skills/multica-projects-and-resources/references/projects-and-resources-source-map.md`
- Modify: `docs/superpowers/specs/2026-08-10-project-intake-archive-design.md` only for verified implementation differences

- [ ] **Step 1: Run focused suites**

```bash
rtk go -C server test ./internal/migrations ./internal/service ./internal/handler -count=1
rtk pnpm --filter @multica/core test
rtk pnpm --filter @multica/views test
rtk pnpm --filter @multica/core typecheck
rtk pnpm --filter @multica/views typecheck
```

Expected: all PASS.

- [ ] **Step 2: Run repository-wide verification**

```bash
rtk pnpm typecheck
rtk pnpm test
rtk make test
rtk make check
```

Record any unrelated pre-existing failure with the exact command and output; do not mask it.

- [ ] **Step 3: Run browser acceptance with a disposable Project**

Verify Active -> Archive confirmation -> Archived -> detail history -> Restore -> Active. Then run one PMO preview/apply using a test external requirement and confirm the formatted title plus exactly one root Issue. Never select or archive `Multica-iworker`.

- [ ] **Step 4: Final scope detection**

```bash
rtk git diff --check origin/main...HEAD
rtk node .gitnexus/run.cjs detect-changes --scope compare --base-ref origin/main
rtk git status --short
```

Expected: only PMO intake/archive files and documented upstream dependency changes; clean worktree after commits.

- [ ] **Step 5: Push and update `SY-12`**

```bash
rtk git push origin feat/project-intake-archive
```

Post the final commit, verification summary, and merge-request link to `SY-12`; move it to `in_review`, never directly to `done`.
