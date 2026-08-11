package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func createProjectArchiveFixture(t *testing.T, title string) ProjectResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateProject(w, newRequest(http.MethodPost, "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": title,
	}))
	project := decodeProject(t, w, http.StatusCreated)
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id IN (SELECT id FROM issue WHERE project_id = $1)`, project.ID)
		_, _ = testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id IN (SELECT id FROM issue WHERE project_id = $1)`, project.ID)
		_, _ = testPool.Exec(ctx, `DELETE FROM project_resource WHERE project_id = $1`, project.ID)
		_, _ = testPool.Exec(ctx, `DELETE FROM issue WHERE project_id = $1`, project.ID)
		_, _ = testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, project.ID)
	})
	return project
}

func archiveProjectForTest(t *testing.T, projectID string, userID ...string) ProjectResponse {
	t.Helper()
	req := newRequest(http.MethodPost, "/api/projects/"+projectID+"/archive", nil)
	if len(userID) > 0 {
		req.Header.Set("X-User-ID", userID[0])
	}
	w := httptest.NewRecorder()
	testHandler.ArchiveProject(w, withURLParam(req, "id", projectID))
	return decodeProject(t, w, http.StatusOK)
}

func restoreProjectForTest(t *testing.T, projectID string) ProjectResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPost, "/api/projects/"+projectID+"/restore", nil), "id", projectID)
	testHandler.RestoreProject(w, req)
	return decodeProject(t, w, http.StatusOK)
}

func listProjectArchiveMode(t *testing.T, mode string) []ProjectResponse {
	t.Helper()
	path := "/api/projects?workspace_id=" + testWorkspaceID
	if mode != "" {
		path += "&archived=" + mode
	}
	w := httptest.NewRecorder()
	testHandler.ListProjects(w, newRequest(http.MethodGet, path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list projects status = %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Projects []ProjectResponse `json:"projects"`
	}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode project list: %v", err)
	}
	return response.Projects
}

func projectListContains(projects []ProjectResponse, id string) bool {
	for _, project := range projects {
		if project.ID == id {
			return true
		}
	}
	return false
}

func projectArchiveRelatedCounts(t *testing.T, projectID string) [4]int64 {
	t.Helper()
	var counts [4]int64
	if err := testPool.QueryRow(context.Background(), `
		SELECT
			(SELECT count(*) FROM issue WHERE project_id = $1),
			(SELECT count(*) FROM project_resource WHERE project_id = $1),
			(SELECT count(*) FROM comment WHERE issue_id IN (SELECT id FROM issue WHERE project_id = $1)),
			(SELECT count(*) FROM agent_task_queue WHERE issue_id IN (SELECT id FROM issue WHERE project_id = $1))
	`, projectID).Scan(&counts[0], &counts[1], &counts[2], &counts[3]); err != nil {
		t.Fatalf("count project children: %v", err)
	}
	return counts
}

func TestProjectArchiveRestoreIsIdempotentAndPreservesChildren(t *testing.T) {
	project := createProjectArchiveFixture(t, "archive-preserves-"+uuid.NewString())

	w := httptest.NewRecorder()
	testHandler.CreateIssue(w, newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":      "archive child",
		"project_id": project.ID,
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create issue status = %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode issue: %v", err)
	}

	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO project_resource (project_id, workspace_id, resource_type, resource_ref, label, position, created_by)
		VALUES ($1, $2, 'github_repo', '{"url":"https://example.com/archive-test.git"}'::jsonb, 'Archive test', 0, $3)
	`, project.ID, testWorkspaceID, testUserID); err != nil {
		t.Fatalf("insert project resource: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'archive history')
	`, issue.ID, testWorkspaceID, testUserID); err != nil {
		t.Fatalf("insert comment: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, status, priority, issue_id,
			originator_source, originator_user_id, accountable_user_id
		)
		SELECT id, runtime_id, 'completed', 0, $1, 'direct_human', $2, $2
		FROM agent
		WHERE workspace_id = $3
		ORDER BY created_at
		LIMIT 1
	`, issue.ID, testUserID, testWorkspaceID); err != nil {
		t.Fatalf("insert task history: %v", err)
	}

	wantCounts := projectArchiveRelatedCounts(t, project.ID)
	archived := archiveProjectForTest(t, project.ID)
	if archived.ArchivedAt == nil || archived.ArchivedBy == nil || *archived.ArchivedBy != testUserID {
		t.Fatalf("archive metadata = (%v, %v), want timestamp and requester", archived.ArchivedAt, archived.ArchivedBy)
	}
	archivedAgain := archiveProjectForTest(t, project.ID)
	if *archivedAgain.ArchivedAt != *archived.ArchivedAt || *archivedAgain.ArchivedBy != *archived.ArchivedBy {
		t.Fatalf("second archive changed metadata: first=%+v second=%+v", archived, archivedAgain)
	}

	restored := restoreProjectForTest(t, project.ID)
	if restored.ArchivedAt != nil || restored.ArchivedBy != nil {
		t.Fatalf("restore metadata = (%v, %v), want nil", restored.ArchivedAt, restored.ArchivedBy)
	}
	restoredAgain := restoreProjectForTest(t, project.ID)
	if restoredAgain.ArchivedAt != nil || restoredAgain.ArchivedBy != nil {
		t.Fatalf("second restore metadata = (%v, %v), want nil", restoredAgain.ArchivedAt, restoredAgain.ArchivedBy)
	}
	if got := projectArchiveRelatedCounts(t, project.ID); got != wantCounts {
		t.Fatalf("related counts after archive/restore = %v, want %v", got, wantCounts)
	}
}

func TestListProjectsArchiveModes(t *testing.T) {
	active := createProjectArchiveFixture(t, "archive-list-active-"+uuid.NewString())
	archived := createProjectArchiveFixture(t, "archive-list-only-"+uuid.NewString())
	archiveProjectForTest(t, archived.ID)

	activeProjects := listProjectArchiveMode(t, "")
	if !projectListContains(activeProjects, active.ID) || projectListContains(activeProjects, archived.ID) {
		t.Fatalf("default active list has active=%v archived=%v", projectListContains(activeProjects, active.ID), projectListContains(activeProjects, archived.ID))
	}
	archivedProjects := listProjectArchiveMode(t, "only")
	if projectListContains(archivedProjects, active.ID) || !projectListContains(archivedProjects, archived.ID) {
		t.Fatalf("archived-only list has active=%v archived=%v", projectListContains(archivedProjects, active.ID), projectListContains(archivedProjects, archived.ID))
	}
	allProjects := listProjectArchiveMode(t, "all")
	if !projectListContains(allProjects, active.ID) || !projectListContains(allProjects, archived.ID) {
		t.Fatalf("all list has active=%v archived=%v", projectListContains(allProjects, active.ID), projectListContains(allProjects, archived.ID))
	}
}

func TestListProjectsRejectsInvalidArchiveMode(t *testing.T) {
	w := httptest.NewRecorder()
	testHandler.ListProjects(w, newRequest(http.MethodGet, "/api/projects?workspace_id="+testWorkspaceID+"&archived=deleted", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid archive mode status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestProjectArchiveRequiresOwnerOrAdmin(t *testing.T) {
	project := createProjectArchiveFixture(t, "archive-role-"+uuid.NewString())
	memberEmail := "archive-member-" + uuid.NewString() + "@multica.test"
	var memberID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO "user" (name, email) VALUES ('Archive Member', $1) RETURNING id
	`, memberEmail).Scan(&memberID); err != nil {
		t.Fatalf("create member user: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, testWorkspaceID, memberID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, memberID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, memberID)
	})

	req := newRequest(http.MethodPost, "/api/projects/"+project.ID+"/archive", nil)
	req.Header.Set("X-User-ID", memberID)
	w := httptest.NewRecorder()
	testHandler.ArchiveProject(w, withURLParam(req, "id", project.ID))
	if w.Code != http.StatusForbidden {
		t.Fatalf("member archive status = %d, want 403: %s", w.Code, w.Body.String())
	}
}

func TestGetArchivedProjectStillReturnsDetail(t *testing.T) {
	project := createProjectArchiveFixture(t, "archive-detail-"+uuid.NewString())
	archiveProjectForTest(t, project.ID)

	w := httptest.NewRecorder()
	testHandler.GetProject(w, withURLParam(newRequest(http.MethodGet, "/api/projects/"+project.ID, nil), "id", project.ID))
	got := decodeProject(t, w, http.StatusOK)
	if got.ArchivedAt == nil {
		t.Fatal("archived project detail is missing archived_at")
	}
}

func TestSearchProjectsHidesArchivedProjects(t *testing.T) {
	project := createProjectArchiveFixture(t, "zzarchive-search-"+uuid.NewString())
	archiveProjectForTest(t, project.ID)

	w := httptest.NewRecorder()
	testHandler.SearchProjects(w, newRequest(http.MethodGet, "/api/projects/search?q=zzarchive-search&include_closed=true", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("search status = %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Projects []ProjectResponse `json:"projects"`
	}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if projectListContains(response.Projects, project.ID) {
		t.Fatalf("archived project %s was returned by search", project.ID)
	}
}
