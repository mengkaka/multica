package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type projectAutoArchiveFixture struct {
	project ProjectResponse
	issues  []string
}

func newProjectAutoArchiveFixture(t *testing.T, statuses ...string) projectAutoArchiveFixture {
	t.Helper()
	ctx := context.Background()
	project := createProjectArchiveFixture(t, "auto-archive-"+uuid.NewString())
	if _, err := testPool.Exec(ctx, `UPDATE project SET status = 'completed' WHERE id = $1`, project.ID); err != nil {
		t.Fatalf("complete project: %v", err)
	}
	var agentID, configID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load PMO agent: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO pmo_sync_config (workspace_id, name, agent_id, root_external_key, created_by)
		VALUES ($1, 'Auto archive fixture', $2, $3, $4)
		RETURNING id
	`, testWorkspaceID, agentID, "EXT-"+uuid.NewString(), testUserID).Scan(&configID); err != nil {
		t.Fatalf("create PMO config: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO pmo_sync_link (
			workspace_id, config_id, external_type, external_key,
			local_type, local_id, baseline_external, baseline_local
		) VALUES ($1, $2, 'requirement', $3, 'project', $4, '{"status":"completed"}', '{}')
	`, testWorkspaceID, configID, "ROOT-"+uuid.NewString(), project.ID); err != nil {
		t.Fatalf("create PMO project link: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM pmo_sync_link WHERE config_id = $1`, configID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM pmo_sync_config WHERE id = $1`, configID)
	})

	fixture := projectAutoArchiveFixture{project: project}
	for i, status := range statuses {
		w := httptest.NewRecorder()
		testHandler.CreateIssue(w, newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title":      "auto archive issue " + uuid.NewString(),
			"project_id": project.ID,
		}))
		if w.Code != http.StatusCreated {
			t.Fatalf("create issue %d: %d %s", i, w.Code, w.Body.String())
		}
		var issue IssueResponse
		if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
			t.Fatalf("decode issue: %v", err)
		}
		if _, err := testPool.Exec(ctx, `UPDATE issue SET status = $1 WHERE id = $2`, status, issue.ID); err != nil {
			t.Fatalf("set issue status: %v", err)
		}
		fixture.issues = append(fixture.issues, issue.ID)
	}
	return fixture
}

func projectArchivedForTest(t *testing.T, projectID string) bool {
	t.Helper()
	var archived bool
	if err := testPool.QueryRow(context.Background(), `SELECT archived_at IS NOT NULL FROM project WHERE id = $1`, projectID).Scan(&archived); err != nil {
		t.Fatalf("read project archive state: %v", err)
	}
	return archived
}

func TestProjectAutoArchiveAfterSingleIssueUpdate(t *testing.T) {
	fixture := newProjectAutoArchiveFixture(t, "todo")
	var published *ProjectResponse
	testHandler.Bus.Subscribe(protocol.EventProjectUpdated, func(event events.Event) {
		payload, ok := event.Payload.(map[string]any)
		if !ok {
			return
		}
		project, ok := payload["project"].(ProjectResponse)
		if ok && project.ID == fixture.project.ID {
			published = &project
		}
	})
	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPatch, "/api/issues/"+fixture.issues[0], map[string]any{"status": "done"}), "id", fixture.issues[0])
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update issue: %d %s", w.Code, w.Body.String())
	}
	if !projectArchivedForTest(t, fixture.project.ID) {
		t.Fatal("project was not archived after its last issue completed")
	}
	if published == nil || published.ArchivedAt == nil || published.IssueCount != 1 || published.DoneCount != 1 {
		t.Fatalf("project:updated payload = %+v", published)
	}
}

func TestProjectAutoArchiveAfterBatchIssueUpdate(t *testing.T) {
	fixture := newProjectAutoArchiveFixture(t, "todo", "todo")
	w := httptest.NewRecorder()
	testHandler.BatchUpdateIssues(w, newRequest(http.MethodPost, "/api/issues/batch-update", map[string]any{
		"issue_ids": fixture.issues,
		"updates":   map[string]any{"status": "done"},
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("batch update: %d %s", w.Code, w.Body.String())
	}
	if !projectArchivedForTest(t, fixture.project.ID) {
		t.Fatal("project was not archived after batch completion")
	}
}
