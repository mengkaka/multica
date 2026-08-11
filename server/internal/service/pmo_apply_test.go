package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// pmoApplyFixture seeds a workspace + member + agent/runtime + PMO config and
// a wired PMOService for apply tests. All external keys are fictional.
type pmoApplyFixture struct {
	pool        *pgxpool.Pool
	svc         *PMOService
	workspaceID pgtype.UUID
	ownerID     pgtype.UUID
	agentID     pgtype.UUID
	configID    pgtype.UUID
	bus         *events.Bus
}

func newPMOApplyFixture(t *testing.T) pmoApplyFixture {
	t.Helper()
	ctx := context.Background()
	pool := issueServiceTestPool(t)

	suffix := time.Now().UnixNano()
	slug := fmt.Sprintf("pmo-apply-%d", suffix)

	var ownerID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('PMO Apply Owner', $1) RETURNING id`,
		fmt.Sprintf("pmo-apply-%d@example.test", suffix)).Scan(&ownerID); err != nil {
		t.Fatalf("create owner: %v", err)
	}
	var workspaceID string
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ('PMO Apply', $1, 'temp pmo apply workspace', 'PMA') RETURNING id`, slug).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, workspaceID, ownerID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	var runtimeID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at)
		VALUES ($1, 'PMO Apply Runtime', 'cloud', 'pmo_apply_runtime', 'online', 'pmo apply runtime', '{}'::jsonb, $2, now())
		RETURNING id
	`, workspaceID, ownerID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	var agentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id)
		VALUES ($1, 'PMO Apply Agent', '', 'cloud', '{}'::jsonb, $2, 'private', 'private', 1, $3)
		RETURNING id
	`, workspaceID, runtimeID, ownerID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	queries := db.New(pool)
	config, err := queries.CreatePMOSyncConfig(ctx, db.CreatePMOSyncConfigParams{
		WorkspaceID:     parsePGUUID(t, workspaceID),
		Name:            "PMO Apply Config",
		AgentID:         parsePGUUID(t, agentID),
		RootExternalKey: "EXT-P-001",
		CreatedBy:       parsePGUUID(t, ownerID),
	})
	if err != nil {
		t.Fatalf("create config: %v", err)
	}

	bus := events.New()
	svc := NewPMOService(queries, pool, nil)
	svc.IssueSvc = NewIssueService(queries, pool, bus, nil, nil)
	svc.IssueSvc.TaskService = NewTaskService(queries, pool, nil, bus)

	t.Cleanup(func() {
		cleanup := context.Background()
		_, _ = pool.Exec(cleanup, `DELETE FROM agent_task_queue WHERE runtime_id = $1`, runtimeID)
		_, _ = pool.Exec(cleanup, `DELETE FROM issue WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(cleanup, `DELETE FROM pmo_sync_link WHERE config_id = $1`, config.ID)
		_, _ = pool.Exec(cleanup, `DELETE FROM pmo_sync_run WHERE config_id = $1`, config.ID)
		_, _ = pool.Exec(cleanup, `DELETE FROM pmo_sync_config WHERE id = $1`, config.ID)
		_, _ = pool.Exec(cleanup, `DELETE FROM project WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(cleanup, `DELETE FROM issue_property WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(cleanup, `DELETE FROM member WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(cleanup, `DELETE FROM squad WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(cleanup, `DELETE FROM agent WHERE id = $1`, agentID)
		_, _ = pool.Exec(cleanup, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
		_, _ = pool.Exec(cleanup, `DELETE FROM workspace WHERE id = $1`, workspaceID)
		_, _ = pool.Exec(cleanup, `DELETE FROM "user" WHERE id = $1`, ownerID)
	})

	return pmoApplyFixture{
		pool:        pool,
		svc:         svc,
		workspaceID: parsePGUUID(t, workspaceID),
		ownerID:     parsePGUUID(t, ownerID),
		agentID:     parsePGUUID(t, agentID),
		configID:    config.ID,
		bus:         bus,
	}
}

func configurePMOOrchestrationSquad(t *testing.T, f pmoApplyFixture) db.Squad {
	t.Helper()
	ctx := context.Background()
	squad, err := f.svc.Queries.CreateSquad(ctx, db.CreateSquadParams{
		WorkspaceID: f.workspaceID,
		Name:        "PMO Orchestration Squad",
		Description: "",
		LeaderID:    f.agentID,
		CreatorID:   f.ownerID,
	})
	if err != nil {
		t.Fatalf("create squad: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE pmo_sync_config SET orchestration_squad_id = $1 WHERE id = $2`, squad.ID, f.configID); err != nil {
		t.Fatalf("configure orchestration squad: %v", err)
	}
	return squad
}

func parsePGUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	u, err := util.ParseUUID(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return u
}

// pmoApplyTask builds one external requirement/task map for snapshot fixtures.
func pmoRequirement(key, displayNumber, title, sourceStatus, status string, numericID int64) map[string]any {
	return map[string]any{
		"key": key, "display_number": displayNumber, "numeric_id": numericID,
		"title": title, "description": "", "source_status": sourceStatus, "status": status,
	}
}

func pmoChildWithTasks(t *testing.T, key, displayNumber, title, status string, numericID int64, tasks ...map[string]any) map[string]any {
	child := pmoRequirement(key, displayNumber, title, "todo", status, numericID)
	if len(tasks) == 0 {
		child["tasks"] = []map[string]any{}
	} else {
		child["tasks"] = tasks
	}
	return child
}

func pmoTask(taskID, schemeID, title, status string) map[string]any {
	return map[string]any{
		"task_id": taskID, "scheme_id": schemeID, "title": title, "description": "",
		"source_status": "todo", "status": status,
	}
}

// buildPMOSnapshotJSON assembles a contract-valid snapshot JSON string.
func buildPMOSnapshotJSON(t *testing.T, parent map[string]any, children []map[string]any, tasks []map[string]any) string {
	t.Helper()
	if children == nil {
		children = []map[string]any{}
	}
	if tasks == nil {
		tasks = []map[string]any{}
	}
	raw, err := json.Marshal(map[string]any{
		"schema_version": "1", "snapshot_complete": true,
		"parent_requirement": parent, "child_requirements": children, "tasks": tasks,
	})
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	return string(raw)
}

// seedPMOPreview stores a run in preview_ready state for the config: creates
// the run row (no agent task needed — apply is independent of acquisition)
// and persists the snapshot as the stored source_snapshot.
func seedPMOPreview(t *testing.T, f pmoApplyFixture, snapshotJSON string) db.PmoSyncRun {
	t.Helper()
	ctx := context.Background()
	run, err := f.svc.Queries.CreatePMOSyncRun(ctx, db.CreatePMOSyncRunParams{
		WorkspaceID: f.workspaceID, ConfigID: f.configID, Trigger: "manual", RequestedBy: f.ownerID,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	run, err = f.svc.Queries.MarkPMOSyncRunRunning(ctx, db.MarkPMOSyncRunRunningParams{ID: run.ID, WorkspaceID: f.workspaceID})
	if err != nil {
		t.Fatalf("mark run running: %v", err)
	}
	run, err = f.svc.Queries.StorePMOSyncRunPreview(ctx, db.StorePMOSyncRunPreviewParams{
		ID: run.ID, WorkspaceID: f.workspaceID,
		SourceSnapshot: []byte(snapshotJSON), Diff: []byte(`{"entities":[],"warnings":[],"summary":{}}`), Summary: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("store preview: %v", err)
	}
	t.Cleanup(func() {
		_, _ = f.pool.Exec(context.Background(), `DELETE FROM pmo_sync_run WHERE id = $1`, run.ID)
	})
	return run
}

func countPMOEntities(t *testing.T, pool *pgxpool.Pool, workspaceID pgtype.UUID) (projects, issues, links int) {
	t.Helper()
	ctx := context.Background()
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM project WHERE workspace_id = $1`, workspaceID).Scan(&projects); err != nil {
		t.Fatalf("count projects: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE workspace_id = $1`, workspaceID).Scan(&issues); err != nil {
		t.Fatalf("count issues: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pmo_sync_link WHERE workspace_id = $1`, workspaceID).Scan(&links); err != nil {
		t.Fatalf("count links: %v", err)
	}
	return projects, issues, links
}

func pmoLinkByExternal(t *testing.T, f pmoApplyFixture, externalType, externalKey string) db.PmoSyncLink {
	t.Helper()
	link, err := f.svc.Queries.GetPMOSyncLink(context.Background(), db.GetPMOSyncLinkParams{
		WorkspaceID: f.workspaceID, ConfigID: f.configID, ExternalType: externalType, ExternalKey: externalKey,
	})
	if err != nil {
		t.Fatalf("get link %s/%s: %v", externalType, externalKey, err)
	}
	return link
}

func issueByID(t *testing.T, pool *pgxpool.Pool, id pgtype.UUID) db.Issue {
	t.Helper()
	var issue db.Issue
	err := pool.QueryRow(context.Background(), `
		SELECT id, workspace_id, title, description, status, priority, assignee_type, assignee_id,
		       creator_type, creator_id, parent_issue_id, acceptance_criteria, context_refs, position,
		       due_date, created_at, updated_at, number, project_id, origin_type, origin_id,
		       first_executed_at, start_date, metadata, stage, properties
		FROM issue WHERE id = $1
	`, id).Scan(&issue.ID, &issue.WorkspaceID, &issue.Title, &issue.Description, &issue.Status,
		&issue.Priority, &issue.AssigneeType, &issue.AssigneeID, &issue.CreatorType, &issue.CreatorID,
		&issue.ParentIssueID, &issue.AcceptanceCriteria, &issue.ContextRefs, &issue.Position,
		&issue.DueDate, &issue.CreatedAt, &issue.UpdatedAt, &issue.Number, &issue.ProjectID,
		&issue.OriginType, &issue.OriginID, &issue.FirstExecutedAt, &issue.StartDate,
		&issue.Metadata, &issue.Stage, &issue.Properties)
	if err != nil {
		t.Fatalf("read issue: %v", err)
	}
	return issue
}

func TestApplyPMORunFirstImportCreatesHierarchy(t *testing.T) {
	f := newPMOApplyFixture(t)
	ctx := context.Background()

	var mu sync.Mutex
	var createdEvents int
	f.bus.Subscribe(protocol.EventIssueCreated, func(e events.Event) {
		mu.Lock()
		defer mu.Unlock()
		createdEvents++
	})

	snapshot := buildPMOSnapshotJSON(t,
		pmoRequirement("EXT-P-001", "P-001", "Imported Project", "planned", "planned", 1),
		[]map[string]any{
			pmoChildWithTasks(t, "EXT-I-001", "I-001", "Child Requirement", "todo", 2,
				pmoTask("EXT-T-001", "EXT-S-001", "Child Task", "todo")),
		},
		[]map[string]any{pmoTask("EXT-T-002", "EXT-S-002", "Parent Task", "todo")})
	run := seedPMOPreview(t, f, snapshot)

	applied, err := f.svc.ApplyRun(ctx, f.workspaceID, run.ID, nil)
	if err != nil {
		t.Fatalf("ApplyRun: %v", err)
	}
	if applied.Status != "applied" {
		t.Fatalf("run status = %q, want applied", applied.Status)
	}

	// Project from parent requirement.
	projectLink := pmoLinkByExternal(t, f, "requirement", "EXT-P-001")
	if projectLink.LocalType.String != "project" || !projectLink.LocalID.Valid {
		t.Fatalf("project link = %+v", projectLink)
	}
	var projectTitle, projectStatus string
	if err := f.pool.QueryRow(ctx, `SELECT title, status FROM project WHERE id = $1`, projectLink.LocalID).Scan(&projectTitle, &projectStatus); err != nil {
		t.Fatalf("read project: %v", err)
	}
	if projectTitle != "P-001 Imported Project" || projectStatus != "planned" {
		t.Fatalf("project title/status = %q/%q", projectTitle, projectStatus)
	}

	// Child requirement → top-level issue in the project.
	childLink := pmoLinkByExternal(t, f, "requirement", "EXT-I-001")
	if childLink.LocalType.String != "issue" || !childLink.LocalID.Valid {
		t.Fatalf("child link = %+v", childLink)
	}
	childIssue := issueByID(t, f.pool, childLink.LocalID)
	if childIssue.ProjectID != projectLink.LocalID {
		t.Fatalf("child issue project_id = %v, want %v", childIssue.ProjectID, projectLink.LocalID)
	}
	if childIssue.ParentIssueID.Valid {
		t.Fatalf("child issue has parent %v, want top-level", childIssue.ParentIssueID)
	}

	// Task under child → child issue under the requirement issue.
	taskLink := pmoLinkByExternal(t, f, "task", "EXT-T-001")
	taskIssue := issueByID(t, f.pool, taskLink.LocalID)
	if taskIssue.ParentIssueID != childLink.LocalID {
		t.Fatalf("task issue parent = %v, want child issue %v", taskIssue.ParentIssueID, childLink.LocalID)
	}
	if taskIssue.ProjectID != projectLink.LocalID {
		t.Fatalf("task issue project = %v, want %v", taskIssue.ProjectID, projectLink.LocalID)
	}

	// Parent-level task → top-level issue.
	parentTaskLink := pmoLinkByExternal(t, f, "task", "EXT-T-002")
	parentTaskIssue := issueByID(t, f.pool, parentTaskLink.LocalID)
	if parentTaskIssue.ParentIssueID.Valid {
		t.Fatalf("parent task issue has parent %v, want top-level", parentTaskIssue.ParentIssueID)
	}

	// External identities preserved on links.
	if childLink.ExternalDisplayNumber.String != "I-001" || childLink.ExternalNumericID.Int64 != 2 {
		t.Fatalf("child link identities = %+v", childLink)
	}
	if taskLink.ExternalTaskID.String != "EXT-T-001" {
		t.Fatalf("task link external_task_id = %q", taskLink.ExternalTaskID.String)
	}

	// Baselines are populated snapshots, not empty blobs.
	var baseline map[string]any
	if err := json.Unmarshal(childLink.BaselineExternal, &baseline); err != nil || len(baseline) == 0 {
		t.Fatalf("baseline_external = %s", childLink.BaselineExternal)
	}
	if err := json.Unmarshal(childLink.BaselineLocal, &baseline); err != nil || len(baseline) == 0 {
		t.Fatalf("baseline_local = %s", childLink.BaselineLocal)
	}

	// Config last_applied_at stamped.
	var appliedAt *time.Time
	if err := f.pool.QueryRow(ctx, `SELECT last_applied_at FROM pmo_sync_config WHERE id = $1`, f.configID).Scan(&appliedAt); err != nil {
		t.Fatalf("read config: %v", err)
	}
	if appliedAt == nil {
		t.Fatal("config last_applied_at not set after apply")
	}

	// Post-commit create effects: one issue:created per created issue (3 issues).
	mu.Lock()
	defer mu.Unlock()
	if createdEvents != 3 {
		t.Fatalf("issue:created events = %d, want 3", createdEvents)
	}
	config, err := f.svc.Queries.GetPMOSyncConfig(ctx, db.GetPMOSyncConfigParams{ID: f.configID, WorkspaceID: f.workspaceID})
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if config.OrchestrationIssueID.Valid {
		t.Fatalf("config without squad created orchestration issue %v", config.OrchestrationIssueID)
	}
}

func TestApplyPMORunCreatesOneOrchestrationIssueAndLeaderTask(t *testing.T) {
	f := newPMOApplyFixture(t)
	squad := configurePMOOrchestrationSquad(t, f)
	ctx := context.Background()
	snapshot := buildPMOSnapshotJSON(t,
		pmoRequirement("EXT-P-001", "REQ-1234", "Add invoice export", "planned", "planned", 1),
		nil, nil)

	runs := []db.PmoSyncRun{seedPMOPreview(t, f, snapshot), seedPMOPreview(t, f, snapshot)}
	errs := make(chan error, len(runs))
	var wg sync.WaitGroup
	for _, run := range runs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.svc.ApplyRun(ctx, f.workspaceID, run.ID, nil)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent apply: %v", err)
		}
	}

	config, err := f.svc.Queries.GetPMOSyncConfig(ctx, db.GetPMOSyncConfigParams{ID: f.configID, WorkspaceID: f.workspaceID})
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if !config.OrchestrationIssueID.Valid {
		t.Fatal("orchestration issue id was not persisted")
	}
	root := issueByID(t, f.pool, config.OrchestrationIssueID)
	if root.Title != "REQ-1234 Add invoice export" || root.Status != "todo" {
		t.Fatalf("root title/status = %q/%q", root.Title, root.Status)
	}
	if !root.ProjectID.Valid || root.AssigneeType.String != "squad" || root.AssigneeID != squad.ID {
		t.Fatalf("root project/assignee = %v/%v/%v", root.ProjectID, root.AssigneeType, root.AssigneeID)
	}
	tasks, err := f.svc.Queries.ListTasksByIssue(ctx, root.ID)
	if err != nil {
		t.Fatalf("list root tasks: %v", err)
	}
	if len(tasks) != 1 || !tasks[0].IsLeaderTask || tasks[0].SquadID != squad.ID {
		t.Fatalf("root tasks = %#v", tasks)
	}
	var projects, roots int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM project WHERE workspace_id = $1`, f.workspaceID).Scan(&projects); err != nil {
		t.Fatalf("count projects: %v", err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE workspace_id = $1 AND project_id = $2 AND assignee_type = 'squad'`, f.workspaceID, root.ProjectID).Scan(&roots); err != nil {
		t.Fatalf("count orchestration roots: %v", err)
	}
	if projects != 1 || roots != 1 {
		t.Fatalf("projects/roots = %d/%d, want 1/1", projects, roots)
	}
}

func TestApplyPMORunReopensTerminalOrchestrationIssueOnce(t *testing.T) {
	f := newPMOApplyFixture(t)
	configurePMOOrchestrationSquad(t, f)
	ctx := context.Background()
	parent := pmoRequirement("EXT-P-001", "REQ-1234", "Initial title", "planned", "planned", 1)
	run := seedPMOPreview(t, f, buildPMOSnapshotJSON(t, parent, nil, nil))
	if _, err := f.svc.ApplyRun(ctx, f.workspaceID, run.ID, nil); err != nil {
		t.Fatalf("initial apply: %v", err)
	}
	config, err := f.svc.Queries.GetPMOSyncConfig(ctx, db.GetPMOSyncConfigParams{ID: f.configID, WorkspaceID: f.workspaceID})
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE issue SET status = 'done' WHERE id = $1`, config.OrchestrationIssueID); err != nil {
		t.Fatalf("complete root: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE issue_id = $1`, config.OrchestrationIssueID); err != nil {
		t.Fatalf("complete leader task: %v", err)
	}

	parent["title"] = "Reopened title"
	parent["description"] = "Updated description"
	run = seedPMOPreview(t, f, buildPMOSnapshotJSON(t, parent, nil, nil))
	if _, err := f.svc.ApplyRun(ctx, f.workspaceID, run.ID, nil); err != nil {
		t.Fatalf("reopen apply: %v", err)
	}
	root := issueByID(t, f.pool, config.OrchestrationIssueID)
	if root.Status != "todo" || root.Title != "REQ-1234 Reopened title" || root.Description.String != "Updated description" {
		t.Fatalf("reopened root = %q/%q/%q", root.Status, root.Title, root.Description.String)
	}
	tasks, err := f.svc.Queries.ListTasksByIssue(ctx, root.ID)
	if err != nil {
		t.Fatalf("list root tasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("leader tasks after reopen = %d, want 2", len(tasks))
	}
}

func TestApplyPMORunRejectsOrchestrationIssueFromAnotherProject(t *testing.T) {
	f := newPMOApplyFixture(t)
	configurePMOOrchestrationSquad(t, f)
	ctx := context.Background()
	snapshot := buildPMOSnapshotJSON(t,
		pmoRequirement("EXT-P-001", "REQ-1234", "Linked project", "planned", "planned", 1),
		nil, nil)
	run := seedPMOPreview(t, f, snapshot)
	if _, err := f.svc.ApplyRun(ctx, f.workspaceID, run.ID, nil); err != nil {
		t.Fatalf("initial apply: %v", err)
	}
	config, err := f.svc.Queries.GetPMOSyncConfig(ctx, db.GetPMOSyncConfigParams{ID: f.configID, WorkspaceID: f.workspaceID})
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	other, err := f.svc.Queries.CreateProject(ctx, db.CreateProjectParams{
		WorkspaceID: f.workspaceID,
		Title:       "Other project",
		Status:      "planned",
		Priority:    "medium",
	})
	if err != nil {
		t.Fatalf("create other project: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE issue SET project_id = $1 WHERE id = $2`, other.ID, config.OrchestrationIssueID); err != nil {
		t.Fatalf("move orchestration issue: %v", err)
	}

	run = seedPMOPreview(t, f, snapshot)
	if _, err := f.svc.ApplyRun(ctx, f.workspaceID, run.ID, nil); !errors.Is(err, ErrPMOOrchestrationIssueLink) {
		t.Fatalf("apply error = %v, want ErrPMOOrchestrationIssueLink", err)
	}
	stored, err := f.svc.Queries.GetPMOSyncRun(ctx, db.GetPMOSyncRunParams{ID: run.ID, WorkspaceID: f.workspaceID})
	if err != nil {
		t.Fatalf("get failed run: %v", err)
	}
	if stored.Status != "preview_ready" {
		t.Fatalf("failed run status = %q, want preview_ready", stored.Status)
	}
}

func TestApplyPMORunAutoArchiveTruthTable(t *testing.T) {
	cases := []struct {
		name        string
		parent      string
		child       string
		withIssue   bool
		wantArchive bool
	}{
		{name: "external active", parent: "planned", child: "done", withIssue: true},
		{name: "zero issues", parent: "completed", withIssue: false},
		{name: "nonterminal issue", parent: "completed", child: "todo", withIssue: true},
		{name: "all terminal", parent: "completed", child: "done", withIssue: true, wantArchive: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPMOApplyFixture(t)
			var children []map[string]any
			if tc.withIssue {
				children = []map[string]any{pmoChildWithTasks(t, "EXT-I-001", "I-001", "Child", tc.child, 2)}
			}
			snapshot := buildPMOSnapshotJSON(t,
				pmoRequirement("EXT-P-001", "REQ-1234", "Archive truth table", tc.parent, tc.parent, 1),
				children, nil)
			run := seedPMOPreview(t, f, snapshot)
			_, changedProjects, err := f.svc.ApplyRunWithProjectChanges(context.Background(), f.workspaceID, run.ID, nil)
			if err != nil {
				t.Fatalf("apply: %v", err)
			}
			if got := len(changedProjects); (got == 1) != tc.wantArchive || got > 1 {
				t.Fatalf("changed projects = %d, want changed=%v", got, tc.wantArchive)
			}
			projectID := pmoLinkByExternal(t, f, "requirement", "EXT-P-001").LocalID
			project, err := f.svc.Queries.GetProjectInWorkspace(context.Background(), db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: f.workspaceID})
			if err != nil {
				t.Fatalf("get project: %v", err)
			}
			if project.ArchivedAt.Valid != tc.wantArchive {
				t.Fatalf("archived = %v, want %v", project.ArchivedAt.Valid, tc.wantArchive)
			}
		})
	}
}

func TestApplyPMORunRearchivesManualRestoreAndRestoresExternalReopen(t *testing.T) {
	f := newPMOApplyFixture(t)
	ctx := context.Background()
	terminal := buildPMOSnapshotJSON(t,
		pmoRequirement("EXT-P-001", "REQ-1234", "Archive lifecycle", "completed", "completed", 1),
		[]map[string]any{pmoChildWithTasks(t, "EXT-I-001", "I-001", "Child", "done", 2)}, nil)
	run := seedPMOPreview(t, f, terminal)
	if _, err := f.svc.ApplyRun(ctx, f.workspaceID, run.ID, nil); err != nil {
		t.Fatalf("terminal apply: %v", err)
	}
	projectID := pmoLinkByExternal(t, f, "requirement", "EXT-P-001").LocalID
	if _, err := f.svc.Queries.RestoreProject(ctx, db.RestoreProjectParams{ID: projectID, WorkspaceID: f.workspaceID}); err != nil {
		t.Fatalf("manual restore: %v", err)
	}
	run = seedPMOPreview(t, f, terminal)
	if _, err := f.svc.ApplyRun(ctx, f.workspaceID, run.ID, nil); err != nil {
		t.Fatalf("terminal reapply: %v", err)
	}
	project, err := f.svc.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: f.workspaceID})
	if err != nil || !project.ArchivedAt.Valid {
		t.Fatalf("terminal project not rearchived: %+v, %v", project, err)
	}

	active := buildPMOSnapshotJSON(t,
		pmoRequirement("EXT-P-001", "REQ-1234", "Archive lifecycle", "in_progress", "in_progress", 1),
		[]map[string]any{pmoChildWithTasks(t, "EXT-I-001", "I-001", "Child", "done", 2)}, nil)
	run = seedPMOPreview(t, f, active)
	if _, err := f.svc.ApplyRun(ctx, f.workspaceID, run.ID, nil); err != nil {
		t.Fatalf("reopen apply: %v", err)
	}
	project, err = f.svc.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: f.workspaceID})
	if err != nil || project.ArchivedAt.Valid {
		t.Fatalf("reopened project still archived: %+v, %v", project, err)
	}
}

func TestApplyPMORunDoesNotArchiveActiveCanonicalProject(t *testing.T) {
	f := newPMOApplyFixture(t)
	ctx := context.Background()
	terminal := buildPMOSnapshotJSON(t,
		pmoRequirement("EXT-P-001", "REQ-1234", "Canonical active", "completed", "completed", 1),
		[]map[string]any{pmoChildWithTasks(t, "EXT-I-001", "I-001", "Child", "done", 2)}, nil)
	run := seedPMOPreview(t, f, terminal)
	if _, err := f.svc.ApplyRun(ctx, f.workspaceID, run.ID, nil); err != nil {
		t.Fatalf("initial apply: %v", err)
	}
	projectID := pmoLinkByExternal(t, f, "requirement", "EXT-P-001").LocalID
	if _, err := f.pool.Exec(ctx, `UPDATE project SET status = 'planned', archived_at = NULL, archived_by = NULL WHERE id = $1`, projectID); err != nil {
		t.Fatalf("locally reactivate project: %v", err)
	}
	run = seedPMOPreview(t, f, terminal)
	if _, err := f.svc.ApplyRun(ctx, f.workspaceID, run.ID, nil); err != nil {
		t.Fatalf("reapply: %v", err)
	}
	project, err := f.svc.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: f.workspaceID})
	if err != nil || project.Status != "planned" || project.ArchivedAt.Valid {
		t.Fatalf("active canonical project archived: %+v, %v", project, err)
	}
}

func TestApplyPMORunIdempotentRerun(t *testing.T) {
	f := newPMOApplyFixture(t)
	ctx := context.Background()

	snapshot := buildPMOSnapshotJSON(t,
		pmoRequirement("EXT-P-001", "P-001", "Idempotent Project", "planned", "planned", 1),
		[]map[string]any{pmoChildWithTasks(t, "EXT-I-001", "I-001", "Child", "todo", 2)},
		nil)
	run := seedPMOPreview(t, f, snapshot)
	if _, err := f.svc.ApplyRun(ctx, f.workspaceID, run.ID, nil); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	projects, issues, links := countPMOEntities(t, f.pool, f.workspaceID)

	// Same snapshot in a second preview run → zero new rows.
	run2 := seedPMOPreview(t, f, snapshot)
	applied, err := f.svc.ApplyRun(ctx, f.workspaceID, run2.ID, nil)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if applied.Status != "applied" {
		t.Fatalf("second apply status = %q, want applied", applied.Status)
	}
	projects2, issues2, links2 := countPMOEntities(t, f.pool, f.workspaceID)
	if projects != projects2 || issues != issues2 || links != links2 {
		t.Fatalf("rerun created rows: %d/%d/%d -> %d/%d/%d", projects, issues, links, projects2, issues2, links2)
	}
}

func TestApplyPMORunIncomingFieldUpdate(t *testing.T) {
	f := newPMOApplyFixture(t)
	ctx := context.Background()

	first := buildPMOSnapshotJSON(t,
		pmoRequirement("EXT-P-001", "P-001", "Field Project", "planned", "planned", 1),
		[]map[string]any{pmoChildWithTasks(t, "EXT-I-001", "I-001", "Original Title", "todo", 2)},
		nil)
	run := seedPMOPreview(t, f, first)
	if _, err := f.svc.ApplyRun(ctx, f.workspaceID, run.ID, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	childLink := pmoLinkByExternal(t, f, "requirement", "EXT-I-001")

	// External title changes; local untouched → safe incoming update.
	updated := buildPMOSnapshotJSON(t,
		pmoRequirement("EXT-P-001", "P-001", "Field Project", "planned", "planned", 1),
		[]map[string]any{pmoChildWithTasks(t, "EXT-I-001", "I-001", "Updated Title", "todo", 2)},
		nil)
	run2 := seedPMOPreview(t, f, updated)
	if _, err := f.svc.ApplyRun(ctx, f.workspaceID, run2.ID, nil); err != nil {
		t.Fatalf("re-apply: %v", err)
	}

	issue := issueByID(t, f.pool, childLink.LocalID)
	if issue.Title != "Updated Title" {
		t.Fatalf("issue title = %q, want Updated Title", issue.Title)
	}
	// Baseline advanced to the new acknowledged external value.
	link := pmoLinkByExternal(t, f, "requirement", "EXT-I-001")
	var baselineExt map[string]any
	if err := json.Unmarshal(link.BaselineExternal, &baselineExt); err != nil {
		t.Fatalf("unmarshal baseline: %v", err)
	}
	if baselineExt["title"] != "Updated Title" {
		t.Fatalf("baseline_external.title = %v, want Updated Title", baselineExt["title"])
	}
}

func TestApplyPMORunPreservesLocalOnlyEdit(t *testing.T) {
	f := newPMOApplyFixture(t)
	ctx := context.Background()

	snapshot := buildPMOSnapshotJSON(t,
		pmoRequirement("EXT-P-001", "P-001", "Local Edit Project", "planned", "planned", 1),
		[]map[string]any{pmoChildWithTasks(t, "EXT-I-001", "I-001", "Snapshot Title", "todo", 2)},
		nil)
	run := seedPMOPreview(t, f, snapshot)
	if _, err := f.svc.ApplyRun(ctx, f.workspaceID, run.ID, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	childLink := pmoLinkByExternal(t, f, "requirement", "EXT-I-001")
	if _, err := f.pool.Exec(ctx, `UPDATE issue SET title = 'Locally Edited' WHERE id = $1`, childLink.LocalID); err != nil {
		t.Fatalf("local edit: %v", err)
	}

	// Re-apply the UNCHANGED snapshot: local edit survives, no overwrite.
	run2 := seedPMOPreview(t, f, snapshot)
	if _, err := f.svc.ApplyRun(ctx, f.workspaceID, run2.ID, nil); err != nil {
		t.Fatalf("re-apply: %v", err)
	}
	issue := issueByID(t, f.pool, childLink.LocalID)
	if issue.Title != "Locally Edited" {
		t.Fatalf("local-only edit overwritten: %q", issue.Title)
	}
}

func TestApplyPMORunConflictResolutions(t *testing.T) {
	f := newPMOApplyFixture(t)
	ctx := context.Background()

	first := buildPMOSnapshotJSON(t,
		pmoRequirement("EXT-P-001", "P-001", "Conflict Project", "planned", "planned", 1),
		[]map[string]any{pmoChildWithTasks(t, "EXT-I-001", "I-001", "Original Title", "todo", 2)},
		nil)
	run := seedPMOPreview(t, f, first)
	if _, err := f.svc.ApplyRun(ctx, f.workspaceID, run.ID, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	childLink := pmoLinkByExternal(t, f, "requirement", "EXT-I-001")

	// Diverge both sides: external title changes AND local title edited.
	if _, err := f.pool.Exec(ctx, `UPDATE issue SET title = 'Local Side' WHERE id = $1`, childLink.LocalID); err != nil {
		t.Fatalf("local edit: %v", err)
	}
	conflicted := buildPMOSnapshotJSON(t,
		pmoRequirement("EXT-P-001", "P-001", "Conflict Project", "planned", "planned", 1),
		[]map[string]any{pmoChildWithTasks(t, "EXT-I-001", "I-001", "External Side", "todo", 2)},
		nil)

	// Use external: resolves to E1 and writes it locally.
	runExternal := seedPMOPreview(t, f, conflicted)
	if _, err := f.svc.ApplyRun(ctx, f.workspaceID, runExternal.ID, []PMOConflictResolution{
		{ExternalType: "requirement", ExternalKey: "EXT-I-001", Field: "title", Choice: "external"},
	}); err != nil {
		t.Fatalf("apply external choice: %v", err)
	}
	issue := issueByID(t, f.pool, childLink.LocalID)
	if issue.Title != "External Side" {
		t.Fatalf("external choice not applied: %q", issue.Title)
	}

	// Keep local: diverge again with a NEW external value (so this is a real
	// three-way conflict, not local-only), resolve local; value stays and
	// both baselines advance to E1/L1.
	if _, err := f.pool.Exec(ctx, `UPDATE issue SET title = 'Local Side Two' WHERE id = $1`, childLink.LocalID); err != nil {
		t.Fatalf("local edit: %v", err)
	}
	localConflicted := buildPMOSnapshotJSON(t,
		pmoRequirement("EXT-P-001", "P-001", "Conflict Project", "planned", "planned", 1),
		[]map[string]any{pmoChildWithTasks(t, "EXT-I-001", "I-001", "External Side Two", "todo", 2)},
		nil)
	runLocal := seedPMOPreview(t, f, localConflicted)
	if _, err := f.svc.ApplyRun(ctx, f.workspaceID, runLocal.ID, []PMOConflictResolution{
		{ExternalType: "requirement", ExternalKey: "EXT-I-001", Field: "title", Choice: "local"},
	}); err != nil {
		t.Fatalf("apply local choice: %v", err)
	}
	issue = issueByID(t, f.pool, childLink.LocalID)
	if issue.Title != "Local Side Two" {
		t.Fatalf("local choice overwritten: %q", issue.Title)
	}
	link := pmoLinkByExternal(t, f, "requirement", "EXT-I-001")
	var baseExt, baseLocal map[string]any
	if err := json.Unmarshal(link.BaselineExternal, &baseExt); err != nil {
		t.Fatalf("unmarshal baseline_external: %v", err)
	}
	if err := json.Unmarshal(link.BaselineLocal, &baseLocal); err != nil {
		t.Fatalf("unmarshal baseline_local: %v", err)
	}
	if baseExt["title"] != "External Side Two" || baseLocal["title"] != "Local Side Two" {
		t.Fatalf("baselines not advanced: ext=%v local=%v", baseExt["title"], baseLocal["title"])
	}
}

func TestApplyPMORunLocalEditAfterPreviewBecomesConflict(t *testing.T) {
	f := newPMOApplyFixture(t)
	ctx := context.Background()

	preview := buildPMOSnapshotJSON(t,
		pmoRequirement("EXT-P-001", "P-001", "Late Edit Project", "planned", "planned", 1),
		[]map[string]any{pmoChildWithTasks(t, "EXT-I-001", "I-001", "Preview Title", "todo", 2)},
		nil)
	run := seedPMOPreview(t, f, preview)
	if _, err := f.svc.ApplyRun(ctx, f.workspaceID, run.ID, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	childLink := pmoLinkByExternal(t, f, "requirement", "EXT-I-001")

	// Diverge BOTH sides after the preview is stored: external changes title,
	// then a local edit lands before apply. Apply re-reads local values, so the
	// field is a conflict and is NOT overwritten without an explicit choice.
	if _, err := f.pool.Exec(ctx, `UPDATE issue SET title = 'Edited After Preview' WHERE id = $1`, childLink.LocalID); err != nil {
		t.Fatalf("local edit: %v", err)
	}
	late := buildPMOSnapshotJSON(t,
		pmoRequirement("EXT-P-001", "P-001", "Late Edit Project", "planned", "planned", 1),
		[]map[string]any{pmoChildWithTasks(t, "EXT-I-001", "I-001", "Late External Title", "todo", 2)},
		nil)
	runLate := seedPMOPreview(t, f, late)
	if _, err := f.svc.ApplyRun(ctx, f.workspaceID, runLate.ID, nil); err != nil {
		t.Fatalf("apply with unresolved conflict: %v", err)
	}
	issue := issueByID(t, f.pool, childLink.LocalID)
	if issue.Title != "Edited After Preview" {
		t.Fatalf("late local edit overwritten: %q", issue.Title)
	}
}

func TestApplyPMORunExternalRemovalOnlyMarksLink(t *testing.T) {
	f := newPMOApplyFixture(t)
	ctx := context.Background()

	withTwo := buildPMOSnapshotJSON(t,
		pmoRequirement("EXT-P-001", "P-001", "Removal Project", "planned", "planned", 1),
		[]map[string]any{
			pmoChildWithTasks(t, "EXT-I-001", "I-001", "Staying Child", "todo", 2),
			pmoChildWithTasks(t, "EXT-I-002", "I-002", "Removed Child", "todo", 3),
		},
		nil)
	run := seedPMOPreview(t, f, withTwo)
	if _, err := f.svc.ApplyRun(ctx, f.workspaceID, run.ID, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	removedLink := pmoLinkByExternal(t, f, "requirement", "EXT-I-002")
	removedIssue := issueByID(t, f.pool, removedLink.LocalID)

	// Second snapshot drops EXT-I-002: marker set, canonical row untouched.
	withoutSecond := buildPMOSnapshotJSON(t,
		pmoRequirement("EXT-P-001", "P-001", "Removal Project", "planned", "planned", 1),
		[]map[string]any{pmoChildWithTasks(t, "EXT-I-001", "I-001", "Staying Child", "todo", 2)},
		nil)
	run2 := seedPMOPreview(t, f, withoutSecond)
	if _, err := f.svc.ApplyRun(ctx, f.workspaceID, run2.ID, nil); err != nil {
		t.Fatalf("re-apply: %v", err)
	}
	marked := pmoLinkByExternal(t, f, "requirement", "EXT-I-002")
	if !marked.ExternallyRemovedAt.Valid {
		t.Fatal("externally_removed_at not set for absent entity")
	}
	after := issueByID(t, f.pool, removedLink.LocalID)
	if after.Title != removedIssue.Title || after.Status != removedIssue.Status {
		t.Fatalf("removed entity mutated: %q/%q -> %q/%q", removedIssue.Title, removedIssue.Status, after.Title, after.Status)
	}

	// Reappearance clears the marker.
	run3 := seedPMOPreview(t, f, withTwo)
	if _, err := f.svc.ApplyRun(ctx, f.workspaceID, run3.ID, nil); err != nil {
		t.Fatalf("third apply: %v", err)
	}
	cleared := pmoLinkByExternal(t, f, "requirement", "EXT-I-002")
	if cleared.ExternallyRemovedAt.Valid {
		t.Fatal("externally_removed_at not cleared on reappearance")
	}
}

func TestApplyPMORunUnresolvedAndMappedAssignees(t *testing.T) {
	f := newPMOApplyFixture(t)
	ctx := context.Background()

	owner := map[string]any{"external_id": "EXT-U-001", "display_name": "Fictional Owner"}
	parent := pmoRequirement("EXT-P-001", "P-001", "Assignee Project", "planned", "planned", 1)
	parent["owner"] = owner
	child := pmoChildWithTasks(t, "EXT-I-001", "I-001", "Assigned Child", "todo", 2)
	child["owner"] = owner

	snapshot := buildPMOSnapshotJSON(t, parent, []map[string]any{child}, nil)
	run := seedPMOPreview(t, f, snapshot)
	applied, err := f.svc.ApplyRun(ctx, f.workspaceID, run.ID, nil)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	// Unresolved assignees leave the run reviewable and entities unassigned.
	if applied.Status != "applied_with_review" {
		t.Fatalf("status = %q, want applied_with_review", applied.Status)
	}
	childLink := pmoLinkByExternal(t, f, "requirement", "EXT-I-001")
	issue := issueByID(t, f.pool, childLink.LocalID)
	if issue.AssigneeID.Valid {
		t.Fatalf("issue assigned without mapping: %v", issue.AssigneeID)
	}
	assigneeLink := pmoLinkByExternal(t, f, "assignee", "EXT-U-001")
	if assigneeLink.LocalID.Valid {
		t.Fatalf("assignee link resolved without mapping: %+v", assigneeLink)
	}

	// Map the external identity to the workspace member (by ID, never name).
	if _, err := f.svc.SetAssigneeMapping(ctx, f.workspaceID, f.configID, "EXT-U-001", f.ownerID); err != nil {
		t.Fatalf("SetAssigneeMapping: %v", err)
	}
	run2 := seedPMOPreview(t, f, snapshot)
	if _, err := f.svc.ApplyRun(ctx, f.workspaceID, run2.ID, nil); err != nil {
		t.Fatalf("re-apply: %v", err)
	}
	issue = issueByID(t, f.pool, childLink.LocalID)
	if issue.AssigneeID != f.ownerID || issue.AssigneeType.String != "member" {
		t.Fatalf("issue assignee = %v/%q, want member %v", issue.AssigneeID, issue.AssigneeType.String, f.ownerID)
	}
	mapped := pmoLinkByExternal(t, f, "assignee", "EXT-U-001")
	if mapped.LocalType.String != "member" || mapped.LocalID != f.ownerID {
		t.Fatalf("assignee mapping link = %+v", mapped)
	}
}

func TestApplyPMORunWorkloadPropertyOnce(t *testing.T) {
	f := newPMOApplyFixture(t)
	ctx := context.Background()

	workload := 2.5
	child := pmoChildWithTasks(t, "EXT-I-001", "I-001", "Workload Child", "todo", 2)
	child["workload"] = workload
	snapshot := buildPMOSnapshotJSON(t,
		pmoRequirement("EXT-P-001", "P-001", "Workload Project", "planned", "planned", 1),
		[]map[string]any{child}, nil)
	run := seedPMOPreview(t, f, snapshot)
	if _, err := f.svc.ApplyRun(ctx, f.workspaceID, run.ID, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}

	var propCount int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM issue_property WHERE workspace_id = $1`, f.workspaceID).Scan(&propCount); err != nil {
		t.Fatalf("count properties: %v", err)
	}
	if propCount != 1 {
		t.Fatalf("issue_property rows = %d, want exactly 1", propCount)
	}
	var configProp pgtype.UUID
	if err := f.pool.QueryRow(ctx, `SELECT workload_property_id FROM pmo_sync_config WHERE id = $1`, f.configID).Scan(&configProp); err != nil {
		t.Fatalf("read config property: %v", err)
	}
	if !configProp.Valid {
		t.Fatal("config workload_property_id not set")
	}

	childLink := pmoLinkByExternal(t, f, "requirement", "EXT-I-001")
	issue := issueByID(t, f.pool, childLink.LocalID)
	var props map[string]any
	if err := json.Unmarshal(issue.Properties, &props); err != nil {
		t.Fatalf("unmarshal properties: %v", err)
	}
	if props[util.UUIDToString(configProp)] != workload {
		t.Fatalf("workload property value = %v, want %v", props[util.UUIDToString(configProp)], workload)
	}

	// Re-apply: still exactly one property definition (reuse, not recreate).
	run2 := seedPMOPreview(t, f, snapshot)
	if _, err := f.svc.ApplyRun(ctx, f.workspaceID, run2.ID, nil); err != nil {
		t.Fatalf("re-apply: %v", err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM issue_property WHERE workspace_id = $1`, f.workspaceID).Scan(&propCount); err != nil {
		t.Fatalf("count properties: %v", err)
	}
	if propCount != 1 {
		t.Fatalf("issue_property rows after re-apply = %d, want 1", propCount)
	}
}

func TestApplyPMORunRollsBackWholeHierarchy(t *testing.T) {
	f := newPMOApplyFixture(t)
	ctx := context.Background()

	snapshot := buildPMOSnapshotJSON(t,
		pmoRequirement("EXT-P-001", "P-001", "Rollback Project", "planned", "planned", 1),
		[]map[string]any{pmoChildWithTasks(t, "EXT-I-001", "I-001", "Child", "todo", 2)},
		nil)
	run := seedPMOPreview(t, f, snapshot)

	projects, issues, links := countPMOEntities(t, f.pool, f.workspaceID)

	// Force a failure mid-apply via the test hook: the apply must roll back
	// every project/issue/link written by this run.
	f.svc.applyTestHook = func(ctx context.Context, qtx *db.Queries) error {
		return fmt.Errorf("injected apply failure")
	}
	t.Cleanup(func() { f.svc.applyTestHook = nil })
	if _, err := f.svc.ApplyRun(ctx, f.workspaceID, run.ID, nil); err == nil {
		t.Fatal("expected apply error")
	}

	projects2, issues2, links2 := countPMOEntities(t, f.pool, f.workspaceID)
	if projects != projects2 || issues != issues2 || links != links2 {
		t.Fatalf("rollback leaked rows: %d/%d/%d -> %d/%d/%d", projects, issues, links, projects2, issues2, links2)
	}
	// Run remains preview_ready for retry — the failure must not terminalize it.
	var status string
	if err := f.pool.QueryRow(ctx, `SELECT status FROM pmo_sync_run WHERE id = $1`, run.ID).Scan(&status); err != nil {
		t.Fatalf("read run: %v", err)
	}
	if status != "preview_ready" {
		t.Fatalf("run status = %q, want preview_ready after rollback", status)
	}
}

func TestApplyPMORunRejectsWrongState(t *testing.T) {
	f := newPMOApplyFixture(t)
	ctx := context.Background()

	// A run that never reached preview_ready cannot be applied.
	run, err := f.svc.Queries.CreatePMOSyncRun(ctx, db.CreatePMOSyncRunParams{
		WorkspaceID: f.workspaceID, ConfigID: f.configID, Trigger: "manual", RequestedBy: f.ownerID,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), `DELETE FROM pmo_sync_run WHERE id = $1`, run.ID) })
	if _, err := f.svc.ApplyRun(ctx, f.workspaceID, run.ID, nil); err != ErrPMORunNotPreviewReady {
		t.Fatalf("apply queued run: want ErrPMORunNotPreviewReady, got %v", err)
	}
}
