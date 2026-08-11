package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// PMOConflictResolution is one explicit, field-scoped choice for a field the
// three-way comparison flagged as a conflict. Choice is "external" (write E1
// locally) or "local" (keep L1); either way the baselines advance to E1/L1.
type PMOConflictResolution struct {
	ExternalType string `json:"external_type"`
	ExternalKey  string `json:"external_key"`
	Field        string `json:"field"`
	Choice       string `json:"choice"` // external | local
}

const (
	pmoWorkloadPropertyName   = "External Workload"
	pmoWorkloadPropertyDesc   = "Workload synchronized from the external requirement source by PMO sync"
	pmoWorkloadPropertyType   = "number"
	pmoWorkloadPropertyIcon   = "weight"
	pmoRunStatusApplied       = "applied"
	pmoRunStatusAppliedReview = "applied_with_review"
	pmoExternalTypeAssignee   = "assignee"
	pmoLocalTypeProject       = "project"
	pmoLocalTypeIssue         = "issue"
	pmoLocalTypeMember        = "member"
	pmoPriorityDefault        = "medium"
)

// Apply-state errors: ApplyRun on a run in the wrong state / workspace maps
// to 409/404 at the handler layer.
var (
	ErrPMORunNotPreviewReady     = errors.New("pmo run is not ready to apply")
	ErrPMORunNotFound            = errors.New("pmo run not found")
	ErrPMOMemberNotFound         = errors.New("member not found in this workspace")
	ErrPMOOrchestrationIssueLink = errors.New("pmo orchestration issue does not belong to the imported project")
)

func validatePMOResolutions(resolutions []PMOConflictResolution) error {
	for _, r := range resolutions {
		switch r.Choice {
		case "external", "local":
		default:
			return fmt.Errorf("invalid conflict resolution choice %q", r.Choice)
		}
		if r.ExternalType == "" || r.ExternalKey == "" || r.Field == "" {
			return errors.New("conflict resolution must name external_type, external_key, and field")
		}
	}
	return nil
}

// pmoRunApplySummary counts what an apply did; persisted on the run's summary
// column so the review UI can render review items without recomputing.
type pmoRunApplySummary struct {
	Created             int `json:"created"`
	IncomingFields      int `json:"incoming_fields"`
	ConvergedFields     int `json:"converged_fields"`
	ConflictsResolved   int `json:"conflicts_resolved"`
	ConflictsPending    int `json:"conflicts_pending"`
	LocalOnlyFields     int `json:"local_only_fields"`
	ExternalRemoved     int `json:"external_removed"`
	ExternalReturned    int `json:"external_returned"`
	UnresolvedAssignees int `json:"unresolved_assignees"`
}

type pmoCreatedIssue struct {
	issue  db.Issue
	params IssueCreateParams
}

type pmoApplyResult struct {
	reviewItems      bool
	diffJSON         []byte
	summary          pmoRunApplySummary
	createdIssues    []pmoCreatedIssue
	reassignedIssues []db.Issue
	changedProjects  []db.Project
}

// ApplyRun applies a preview_ready run in one transaction. It re-reads the
// stored normalized snapshot and the CURRENT canonical local values, rebuilds
// the field-level three-way diff, writes incoming/converged fields and
// explicitly resolved conflicts, creates missing entities (project →
// top-level issues → child issues), upserts links with advanced baselines,
// marks entities absent from a complete snapshot externally_removed (never
// deletes canonical rows), and stamps run + config applied state. Post-commit
// it publishes create effects through the shared issue pipeline. Any failure
// rolls everything back and leaves the run preview_ready for retry.
// Scheduled auto-apply (Task 7) calls this with nil resolutions.
func (s *PMOService) ApplyRun(ctx context.Context, workspaceID, runID pgtype.UUID, resolutions []PMOConflictResolution) (db.PmoSyncRun, error) {
	run, _, err := s.ApplyRunWithProjectChanges(ctx, workspaceID, runID, resolutions)
	return run, err
}

// ApplyRunWithProjectChanges also returns projects whose archive state changed
// so handler callers can publish full project:updated events after commit.
func (s *PMOService) ApplyRunWithProjectChanges(ctx context.Context, workspaceID, runID pgtype.UUID, resolutions []PMOConflictResolution) (db.PmoSyncRun, []db.Project, error) {
	if err := validatePMOResolutions(resolutions); err != nil {
		return db.PmoSyncRun{}, nil, err
	}
	if s.IssueSvc == nil {
		return db.PmoSyncRun{}, nil, errors.New("pmo apply: issue service not wired")
	}

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.PmoSyncRun{}, nil, err
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)

	run, err := qtx.GetPMOSyncRunForUpdate(ctx, db.GetPMOSyncRunForUpdateParams{ID: runID, WorkspaceID: workspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.PmoSyncRun{}, nil, ErrPMORunNotFound
		}
		return db.PmoSyncRun{}, nil, err
	}
	if run.Status != "preview_ready" {
		return db.PmoSyncRun{}, nil, ErrPMORunNotPreviewReady
	}
	config, err := qtx.GetPMOSyncConfigForUpdate(ctx, db.GetPMOSyncConfigForUpdateParams{ID: run.ConfigID, WorkspaceID: workspaceID})
	if err != nil {
		return db.PmoSyncRun{}, nil, err
	}

	// Re-validate the stored normalized snapshot through the same contract.
	snapshot, err := ParsePMOSnapshot(string(run.SourceSnapshot))
	if err != nil {
		return db.PmoSyncRun{}, nil, fmt.Errorf("pmo apply: revalidate stored snapshot: %w", err)
	}

	workloadPropertyID, err := s.ensureWorkloadProperty(ctx, qtx, workspaceID, config)
	if err != nil {
		return db.PmoSyncRun{}, nil, err
	}

	result, err := s.applySnapshotInTx(ctx, tx, qtx, workspaceID, run, snapshot, resolutions, workloadPropertyID)
	if err != nil {
		return db.PmoSyncRun{}, nil, err
	}
	// Test-only seam: inject a failure inside the transaction to prove the
	// whole hierarchy rolls back.
	if s.applyTestHook != nil {
		if hookErr := s.applyTestHook(ctx, qtx); hookErr != nil {
			return db.PmoSyncRun{}, nil, hookErr
		}
	}

	appliedStatus := pmoRunStatusApplied
	if result.reviewItems {
		appliedStatus = pmoRunStatusAppliedReview
	}
	summaryJSON, err := json.Marshal(result.summary)
	if err != nil {
		return db.PmoSyncRun{}, nil, fmt.Errorf("pmo apply: marshal summary: %w", err)
	}
	run, err = qtx.MarkPMOSyncRunApplied(ctx, db.MarkPMOSyncRunAppliedParams{
		ID: run.ID, WorkspaceID: workspaceID,
		Status:  appliedStatus,
		Diff:    result.diffJSON,
		Summary: summaryJSON,
	})
	if err != nil {
		return db.PmoSyncRun{}, nil, err
	}
	if _, err := qtx.MarkPMOSyncConfigApplied(ctx, db.MarkPMOSyncConfigAppliedParams{ID: run.ConfigID, WorkspaceID: workspaceID}); err != nil {
		return db.PmoSyncRun{}, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return db.PmoSyncRun{}, nil, err
	}

	// Post-commit create effects only; updates and removals never publish
	// create events. Nothing about snapshot content is logged here.
	for _, created := range result.createdIssues {
		s.IssueSvc.afterCreate(ctx, IssueCreateResult{Issue: created.issue}, created.params, IssueCreateOpts{})
	}
	for _, issue := range result.reassignedIssues {
		s.IssueSvc.maybeEnqueueOnAssign(ctx, issue, "member", util.UUIDToString(config.CreatedBy), time.Time{})
	}
	return run, result.changedProjects, nil
}

// applySnapshotInTx performs every apply write under the caller-owned
// transaction. DiffPMOSnapshot's entity order (project, child requirements,
// parent tasks, child tasks) is also the creation order: the project exists
// before any issue, and every child requirement exists before its tasks.
func (s *PMOService) applySnapshotInTx(
	ctx context.Context,
	tx pgx.Tx,
	qtx *db.Queries,
	workspaceID pgtype.UUID,
	run db.PmoSyncRun,
	snapshot PMOSnapshot,
	resolutions []PMOConflictResolution,
	workloadPropertyID pgtype.UUID,
) (pmoApplyResult, error) {
	result := pmoApplyResult{}

	linkRows, err := qtx.ListPMOSyncLinks(ctx, db.ListPMOSyncLinksParams{WorkspaceID: workspaceID, ConfigID: run.ConfigID})
	if err != nil {
		return result, fmt.Errorf("pmo apply: load links: %w", err)
	}
	assigneeMappings := map[string]string{}
	byIdentity := map[string]db.PmoSyncLink{}
	for _, link := range linkRows {
		byIdentity[link.ExternalType+"\x00"+link.ExternalKey] = link
		if link.ExternalType == pmoExternalTypeAssignee && link.LocalID.Valid {
			assigneeMappings[link.ExternalKey] = util.UUIDToString(link.LocalID)
		}
	}

	// Apply always re-reads CURRENT local values so a local edit made after
	// the preview surfaces as the correct decision instead of being silently
	// overwritten.
	linkStates, err := s.buildLinkStates(ctx, qtx, workspaceID, linkRows, workloadPropertyID)
	if err != nil {
		return result, err
	}

	diff := DiffPMOSnapshot(PMODiffInput{Snapshot: snapshot, Links: linkStates, AssigneeMappings: assigneeMappings})
	diffJSON, err := json.Marshal(diff)
	if err != nil {
		return result, fmt.Errorf("pmo apply: marshal diff: %w", err)
	}
	result.diffJSON = diffJSON

	resolutionByKey := map[string]PMOConflictResolution{}
	for _, r := range resolutions {
		resolutionByKey[r.ExternalType+"\x00"+r.ExternalKey+"\x00"+r.Field] = r
	}

	// External identity lookups so link rows carry display_number /
	// numeric_id / task_id alongside the diff (the diff itself only holds
	// synced-field values).
	requirements := map[string]PMORequirement{snapshot.Parent.Key: snapshot.Parent}
	for _, child := range snapshot.Children {
		requirements[child.Key] = child
	}
	createdIDs := map[string]pgtype.UUID{}
	seen := map[string]struct{}{}

	for _, entity := range diff.Entities {
		identity := entity.ExternalType + "\x00" + entity.ExternalKey
		seen[identity] = struct{}{}
		link := byIdentity[identity]
		var oldBaselineExt map[string]any
		if link.ID.Valid {
			oldBaselineExt = decodeBaseline(link.BaselineExternal)
		}

		switch entity.Action {
		case PMOCreate:
			localID, issueRow, createParams, createErr := s.createEntityInTx(ctx, tx, qtx, workspaceID, run, entity, createdIDs, workloadPropertyID)
			if createErr != nil {
				return result, createErr
			}
			createdIDs[identity] = localID
			result.summary.Created++
			if issueRow != nil {
				result.createdIssues = append(result.createdIssues, pmoCreatedIssue{issue: *issueRow, params: *createParams})
			}
			localValues, verr := s.readLocalValuesAfterWrite(ctx, qtx, workspaceID, entity.LocalType, localID, workloadPropertyID)
			if verr != nil {
				return result, verr
			}
			req := requirementIdentity(entity, requirements)
			if err := s.upsertEntityLink(ctx, qtx, workspaceID, run.ConfigID, entity, localID, entityExternalFields(entity), localValues, nil, nil, req); err != nil {
				return result, err
			}
		case PMOUpdate, PMOEntityUnchanged:
			if link.ID.Valid && link.LocalID.Valid {
				pending, applyErr := s.applyEntityFields(ctx, qtx, workspaceID, workloadPropertyID, entity, link, resolutionByKey)
				if applyErr != nil {
					return result, applyErr
				}
				result.summary.IncomingFields += countDecisions(entity, PMOIncoming)
				result.summary.ConvergedFields += countDecisions(entity, PMOConverged)
				result.summary.LocalOnlyFields += countDecisions(entity, PMOLocalOnly)
				result.summary.ConflictsResolved += countDecisions(entity, PMOConflict) - pending
				if pending > 0 {
					result.summary.ConflictsPending += pending
					result.reviewItems = true
				}
				localValues, rerr := s.readLocalValuesAfterWrite(ctx, qtx, workspaceID, entity.LocalType, link.LocalID, workloadPropertyID)
				if rerr != nil {
					return result, rerr
				}
				if result.summary.LocalOnlyFields > 0 {
					result.reviewItems = true
				}
				req := requirementIdentity(entity, requirements)
				oldBaselineLoc := map[string]any{}
				if link.ID.Valid {
					oldBaselineLoc = decodeBaseline(link.BaselineLocal)
				}
				if err := s.upsertEntityLink(ctx, qtx, workspaceID, run.ConfigID, entity, link.LocalID, entityExternalFields(entity), localValues, oldBaselineExt, oldBaselineLoc, req); err != nil {
					return result, err
				}
			}
		case PMOExternalRemoved:
			if link.ID.Valid && !link.ExternallyRemovedAt.Valid {
				if _, err := qtx.MarkPMOSyncLinkExternallyRemoved(ctx, db.MarkPMOSyncLinkExternallyRemovedParams{ID: link.ID, WorkspaceID: workspaceID}); err != nil {
					return result, err
				}
			}
			result.summary.ExternalRemoved++
			result.reviewItems = true
		}
	}

	// Reappearance clears the removal marker; normal comparison resumes.
	for identity, link := range byIdentity {
		if link.ExternalType == pmoExternalTypeAssignee {
			continue
		}
		if _, present := seen[identity]; !present {
			continue
		}
		if link.ExternallyRemovedAt.Valid {
			// The entity loop's upsert already reset the marker (EXCLUDED value),
			// so a no-rows result here just means it was cleared; tolerate it and
			// still count the return so the summary stays accurate.
			if _, err := qtx.ClearPMOSyncLinkExternallyRemoved(ctx, db.ClearPMOSyncLinkExternallyRemovedParams{ID: link.ID, WorkspaceID: workspaceID}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return result, err
			}
			result.summary.ExternalReturned++
		}
	}

	// Every external owner named in this snapshot gets an assignee link row,
	// mapped or not, so the mapping queue lists exactly the outstanding
	// identities. Never infer a member by display name — only explicit
	// SetAssigneeMapping resolves local_id.
	if err := s.upsertAssigneeLinks(ctx, qtx, workspaceID, run.ConfigID, snapshot, byIdentity); err != nil {
		return result, err
	}
	projectID := createdIDs[pmoIdentity("requirement", snapshot.Parent.Key)]
	if !projectID.Valid {
		projectID = byIdentity[pmoIdentity("requirement", snapshot.Parent.Key)].LocalID
	}
	if !projectID.Valid {
		return result, errors.New("pmo apply: imported project id is missing")
	}
	createdRoot, reopenedRoot, err := s.ensurePMOOrchestrationIssue(ctx, tx, qtx, workspaceID, run.ConfigID, projectID, snapshot.Parent)
	if err != nil {
		return result, err
	}
	if createdRoot != nil {
		result.createdIssues = append(result.createdIssues, *createdRoot)
	}
	if reopenedRoot != nil {
		result.reassignedIssues = append(result.reassignedIssues, *reopenedRoot)
	}
	changedProject, err := s.reconcilePMOProjectArchive(ctx, qtx, workspaceID, projectID, snapshot.Parent.Status)
	if err != nil {
		return result, err
	}
	if changedProject != nil {
		result.changedProjects = append(result.changedProjects, *changedProject)
	}
	result.summary.UnresolvedAssignees = len(diff.Warnings)
	if result.summary.UnresolvedAssignees > 0 {
		result.reviewItems = true
	}

	return result, nil
}

func (s *PMOService) ensurePMOOrchestrationIssue(
	ctx context.Context,
	tx pgx.Tx,
	qtx *db.Queries,
	workspaceID, configID, projectID pgtype.UUID,
	requirement PMORequirement,
) (*pmoCreatedIssue, *db.Issue, error) {
	config, err := qtx.GetPMOSyncConfigForUpdate(ctx, db.GetPMOSyncConfigForUpdateParams{ID: configID, WorkspaceID: workspaceID})
	if err != nil {
		return nil, nil, fmt.Errorf("pmo apply: reload orchestration config: %w", err)
	}
	if !config.OrchestrationSquadID.Valid {
		return nil, nil, nil
	}
	squad, err := qtx.LockSquadForUpdate(ctx, db.LockSquadForUpdateParams{ID: config.OrchestrationSquadID, WorkspaceID: workspaceID})
	if err != nil {
		return nil, nil, ErrPMOOrchestrationSquad
	}
	leader, err := qtx.GetAgent(ctx, squad.LeaderID)
	if err != nil || !validPMOOrchestrationSquad(workspaceID, squad, leader) {
		return nil, nil, ErrPMOOrchestrationSquad
	}

	title := pmoProjectTitle(requirement)
	description := pgtype.Text{String: requirement.Description, Valid: true}
	if !config.OrchestrationIssueID.Valid {
		params := IssueCreateParams{
			WorkspaceID:  workspaceID,
			Title:        title,
			Description:  description,
			Status:       "todo",
			Priority:     "none",
			AssigneeType: pgtype.Text{String: "squad", Valid: true},
			AssigneeID:   config.OrchestrationSquadID,
			CreatorType:  "member",
			CreatorID:    config.CreatedBy,
			ProjectID:    projectID,
		}
		created, err := s.IssueSvc.createInTx(ctx, tx, qtx, params)
		if err != nil {
			return nil, nil, fmt.Errorf("pmo apply: create orchestration issue: %w", err)
		}
		if _, err := qtx.SetPMOSyncConfigOrchestrationIssue(ctx, db.SetPMOSyncConfigOrchestrationIssueParams{
			OrchestrationIssueID: created.Issue.ID,
			ID:                   config.ID,
			WorkspaceID:          workspaceID,
		}); err != nil {
			return nil, nil, fmt.Errorf("pmo apply: link orchestration issue: %w", err)
		}
		return &pmoCreatedIssue{issue: created.Issue, params: params}, nil, nil
	}

	root, err := qtx.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: config.OrchestrationIssueID, WorkspaceID: workspaceID})
	if err != nil || !root.ProjectID.Valid || root.ProjectID != projectID {
		return nil, nil, ErrPMOOrchestrationIssueLink
	}
	reopened := !pmoProjectStatusTerminal(requirement.Status) && (root.Status == "done" || root.Status == "cancelled")
	status := root.Status
	if reopened {
		status = "todo"
	}
	root, err = qtx.UpdateIssue(ctx, db.UpdateIssueParams{
		ID:            root.ID,
		Title:         pgtype.Text{String: title, Valid: true},
		Description:   description,
		Status:        pgtype.Text{String: status, Valid: true},
		Priority:      pgtype.Text{String: root.Priority, Valid: true},
		AssigneeType:  root.AssigneeType,
		AssigneeID:    root.AssigneeID,
		Position:      pgtype.Float8{Float64: root.Position, Valid: true},
		StartDate:     root.StartDate,
		DueDate:       root.DueDate,
		ParentIssueID: root.ParentIssueID,
		ProjectID:     root.ProjectID,
		Stage:         root.Stage,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("pmo apply: update orchestration issue: %w", err)
	}
	if reopened {
		return nil, &root, nil
	}
	return nil, nil, nil
}

func pmoProjectStatusTerminal(status string) bool {
	return status == "completed" || status == "cancelled"
}

func (s *PMOService) reconcilePMOProjectArchive(ctx context.Context, qtx *db.Queries, workspaceID, projectID pgtype.UUID, externalStatus string) (*db.Project, error) {
	if !pmoProjectStatusTerminal(externalStatus) {
		current, err := qtx.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: workspaceID})
		if err != nil {
			return nil, fmt.Errorf("pmo apply: load project archive state: %w", err)
		}
		restored, err := qtx.RestoreProject(ctx, db.RestoreProjectParams{ID: projectID, WorkspaceID: workspaceID})
		if err != nil {
			return nil, fmt.Errorf("pmo apply: restore project: %w", err)
		}
		if current.ArchivedAt.Valid {
			return &restored, nil
		}
		return nil, nil
	}
	project, err := qtx.TryAutoArchivePMOProject(ctx, db.TryAutoArchivePMOProjectParams{ID: projectID, WorkspaceID: workspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("pmo apply: auto archive project: %w", err)
	}
	return &project, nil
}

func countDecisions(entity PMOEntityDiff, decision PMOFieldDecision) int {
	n := 0
	for _, fieldDiff := range entity.Fields {
		if fieldDiff.Decision == decision {
			n++
		}
	}
	return n
}

func entityExternalFields(entity PMOEntityDiff) map[string]any {
	fields := make(map[string]any, len(entity.Fields))
	for field, fieldDiff := range entity.Fields {
		fields[field] = fieldDiff.External
	}
	return fields
}

func decodeBaseline(raw []byte) map[string]any {
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return map[string]any{}
	}
	return values
}

// buildLinkStates rebuilds PMOLinkState rows from stored links plus the
// current canonical project/issue values. Entities whose local row vanished
// keep an empty LocalID so the diff reads them as create candidates again.
func (s *PMOService) buildLinkStates(ctx context.Context, qtx *db.Queries, workspaceID pgtype.UUID, links []db.PmoSyncLink, workloadPropertyID pgtype.UUID) ([]PMOLinkState, error) {
	states := make([]PMOLinkState, 0, len(links))
	for _, link := range links {
		if link.ExternalType == pmoExternalTypeAssignee {
			continue
		}
		state := PMOLinkState{
			ExternalType:      link.ExternalType,
			ExternalKey:       link.ExternalKey,
			ExternallyRemoved: link.ExternallyRemovedAt.Valid,
			BaselineExternal:  decodeBaseline(link.BaselineExternal),
			BaselineLocal:     decodeBaseline(link.BaselineLocal),
		}
		if !link.LocalID.Valid {
			states = append(states, state)
			continue
		}
		state.LocalType = PMOLocalType(link.LocalType.String)
		state.LocalID = util.UUIDToString(link.LocalID)
		local, err := s.readLocalValuesAfterWrite(ctx, qtx, workspaceID, PMOLocalType(link.LocalType.String), link.LocalID, workloadPropertyID)
		if err != nil {
			return nil, err
		}
		state.CurrentLocal = local
		states = append(states, state)
	}
	return states, nil
}

// readLocalValuesAfterWrite loads the canonical synced-field values of one
// project or issue row. Returns nil (create candidate) when the row is gone.
func (s *PMOService) readLocalValuesAfterWrite(ctx context.Context, qtx *db.Queries, workspaceID pgtype.UUID, localType PMOLocalType, localID pgtype.UUID, workloadPropertyID pgtype.UUID) (map[string]any, error) {
	switch localType {
	case PMOLocalProject:
		project, err := qtx.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: localID, WorkspaceID: workspaceID})
		if err != nil {
			return nil, nil
		}
		return projectLocalValues(project), nil
	case PMOLocalIssue:
		issue, err := qtx.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: localID, WorkspaceID: workspaceID})
		if err != nil || !issue.ID.Valid {
			return nil, nil
		}
		return issueLocalValues(issue, workloadPropertyID), nil
	default:
		return nil, nil
	}
}

func projectLocalValues(project db.Project) map[string]any {
	return map[string]any{
		"title":       project.Title,
		"description": pmoTextOrEmpty(project.Description),
		"status":      project.Status,
		"lead_id":     pmoUUIDOrNull(project.LeadID),
		"start_date":  pmoDateOrNull(project.StartDate),
		"due_date":    pmoDateOrNull(project.DueDate),
	}
}

func issueLocalValues(issue db.Issue, workloadPropertyID pgtype.UUID) map[string]any {
	values := map[string]any{
		"title":           issue.Title,
		"description":     pmoTextOrEmpty(issue.Description),
		"status":          issue.Status,
		"assignee_id":     pmoUUIDOrNull(issue.AssigneeID),
		"start_date":      pmoDateOrNull(issue.StartDate),
		"due_date":        pmoDateOrNull(issue.DueDate),
		"project_id":      pmoUUIDOrNull(issue.ProjectID),
		"parent_issue_id": pmoUUIDOrNull(issue.ParentIssueID),
		"workload":        issuePMOWorkload(issue, workloadPropertyID),
	}
	return values
}

func issuePMOWorkload(issue db.Issue, workloadPropertyID pgtype.UUID) any {
	if !workloadPropertyID.Valid {
		return nil
	}
	var props map[string]any
	if err := json.Unmarshal(issue.Properties, &props); err != nil {
		return nil
	}
	if value, ok := props[util.UUIDToString(workloadPropertyID)]; ok {
		return value
	}
	return nil
}

func pmoTextOrEmpty(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return t.String
}

func pmoUUIDOrNull(u pgtype.UUID) any {
	if !u.Valid {
		return nil
	}
	return util.UUIDToString(u)
}

func pmoDateOrNull(d pgtype.Date) any {
	if !d.Valid {
		return nil
	}
	return d.Time.Format("2006-01-02")
}

func pmoToPGDate(v any) pgtype.Date {
	text, _ := v.(string)
	if text == "" {
		return pgtype.Date{}
	}
	parsed, err := time.Parse("2006-01-02", text)
	if err != nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: parsed, Valid: true}
}

// createEntityInTx creates one canonical entity (project or issue) for an
// unlinked diff entity inside the apply transaction, reusing the shared
// issue pipeline for issue rows. Returns the new local id; issue rows are
// returned so post-commit effects can fire after the apply commit.
func (s *PMOService) createEntityInTx(
	ctx context.Context,
	tx pgx.Tx,
	qtx *db.Queries,
	workspaceID pgtype.UUID,
	run db.PmoSyncRun,
	entity PMOEntityDiff,
	createdIDs map[string]pgtype.UUID,
	workloadPropertyID pgtype.UUID,
) (pgtype.UUID, *db.Issue, *IssueCreateParams, error) {
	nothing := pgtype.UUID{}
	// Flatten the diff's external values into plain values for creation.
	values := entityExternalFields(entity)

	if entity.LocalType == PMOLocalProject {
		var leadID pgtype.UUID
		if lead, ok := values["lead_id"].(string); ok && lead != "" {
			if parsed, err := util.ParseUUID(lead); err == nil {
				leadID = parsed
			}
		}
		project, err := qtx.CreateProject(ctx, db.CreateProjectParams{
			WorkspaceID: workspaceID,
			Title:       pmoAnyToString(values["title"]),
			Description: pgtype.Text{String: pmoAnyToString(values["description"]), Valid: true},
			Status:      pmoAnyToString(values["status"]),
			Priority:    pmoPriorityDefault,
			StartDate:   pmoToPGDate(values["start_date"]),
			DueDate:     pmoToPGDate(values["due_date"]),
		})
		if err != nil {
			return nothing, nil, nil, fmt.Errorf("pmo apply: create project: %w", err)
		}
		if leadID.Valid {
			if _, err := qtx.UpdateProject(ctx, db.UpdateProjectParams{
				ID:          project.ID,
				Description: project.Description,
				Icon:        project.Icon,
				LeadType:    pgtype.Text{String: pmoLocalTypeMember, Valid: true},
				LeadID:      leadID,
				StartDate:   project.StartDate,
				DueDate:     project.DueDate,
			}); err != nil {
				return nothing, nil, nil, err
			}
		}
		return project.ID, nil, nil, nil
	}

	// Issue entity: resolve project + parent through already-created links so
	// the hierarchy wiring follows the external tree, not guesswork.
	var projectID, parentIssueID pgtype.UUID
	if entity.ProjectExternalKey != "" {
		if id, ok := createdIDs["requirement\x00"+entity.ProjectExternalKey]; ok {
			projectID = id
		}
	}
	if entity.ParentExternalKey != "" && entity.ParentExternalKey != entity.ProjectExternalKey {
		if id, ok := createdIDs["requirement\x00"+entity.ParentExternalKey]; ok {
			parentIssueID = id
		}
	}
	var assigneeID pgtype.UUID
	if assignee, ok := values["assignee_id"].(string); ok && assignee != "" {
		if parsed, err := util.ParseUUID(assignee); err == nil {
			assigneeID = parsed
		}
	}
	params := IssueCreateParams{
		WorkspaceID:    workspaceID,
		Title:          pmoAnyToString(values["title"]),
		Description:    pgtype.Text{String: pmoAnyToString(values["description"]), Valid: true},
		Status:         pmoAnyToString(values["status"]),
		Priority:       pmoPriorityDefault,
		CreatorType:    "member",
		CreatorID:      run.RequestedBy,
		ProjectID:      projectID,
		AllowDuplicate: true,
	}
	if parentIssueID.Valid {
		params.ParentIssueID = parentIssueID
	}
	if assigneeID.Valid {
		params.AssigneeType = pgtype.Text{String: pmoLocalTypeMember, Valid: true}
		params.AssigneeID = assigneeID
	}
	res, err := s.IssueSvc.createInTx(ctx, tx, qtx, params)
	if err != nil {
		return nothing, nil, nil, fmt.Errorf("pmo apply: create issue: %w", err)
	}
	issue := res.Issue
	if workload, ok := values["workload"].(float64); ok && workloadPropertyID.Valid {
		if _, werr := qtx.SetIssuePropertyValue(ctx, db.SetIssuePropertyValueParams{
			ID:          issue.ID,
			WorkspaceID: workspaceID,
			Key:         util.UUIDToString(workloadPropertyID),
			Value:       pmoJSONNumber(workload),
		}); werr != nil {
			return nothing, nil, nil, fmt.Errorf("pmo apply: set workload: %w", werr)
		}
	}
	return issue.ID, &issue, &params, nil
}

func pmoJSONNumber(v float64) []byte {
	return []byte(fmt.Sprintf("%v", v))
}

// applyEntityFields writes the incoming / resolved-conflict fields of one
// linked entity and never touches local_only or unresolved-conflict values.
// Returns the number of unresolved conflicts left for review.
func (s *PMOService) applyEntityFields(
	ctx context.Context,
	qtx *db.Queries,
	workspaceID pgtype.UUID,
	workloadPropertyID pgtype.UUID,
	entity PMOEntityDiff,
	link db.PmoSyncLink,
	resolutions map[string]PMOConflictResolution,
) (int, error) {
	pending := 0
	writes := map[string]any{}
	for field, fieldDiff := range entity.Fields {
		switch fieldDiff.Decision {
		case PMOIncoming:
			writes[field] = fieldDiff.External
		case PMOConflict:
			key := entity.ExternalType + "\x00" + entity.ExternalKey + "\x00" + field
			if res, ok := resolutions[key]; ok && res.Choice == "external" {
				writes[field] = fieldDiff.External
			}
			// Choice "local" is resolved: L1 stays as-is (no write) and the
			// next baseline upsert acknowledges E1/L1. Only UNRESOLVED
			// conflicts stay pending for review.
			if _, ok := resolutions[key]; !ok {
				pending++
			}
		}
	}
	if len(writes) == 0 {
		return pending, nil
	}

	switch entity.LocalType {
	case PMOLocalProject:
		if err := s.applyProjectFields(ctx, qtx, link.LocalID, workloadPropertyID, writes); err != nil {
			return pending, err
		}
	case PMOLocalIssue:
		if err := s.applyIssueFields(ctx, qtx, workspaceID, link.LocalID, workloadPropertyID, writes); err != nil {
			return pending, err
		}
	}
	return pending, nil
}

// applyProjectFields writes one UpdateProject carrying the unchanged current
// values for every field it does NOT explicitly change, so a partial write
// never nulls an untouched column.
func (s *PMOService) applyProjectFields(ctx context.Context, qtx *db.Queries, projectID pgtype.UUID, _ pgtype.UUID, writes map[string]any) error {
	current, err := qtx.GetProject(ctx, projectID)
	if err != nil {
		return fmt.Errorf("pmo apply: reload project: %w", err)
	}
	params := db.UpdateProjectParams{
		ID:          projectID,
		Title:       pgtype.Text{String: current.Title, Valid: true},
		Description: current.Description,
		Icon:        current.Icon,
		Status:      pgtype.Text{String: current.Status, Valid: true},
		Priority:    pgtype.Text{String: current.Priority, Valid: true},
		LeadType:    current.LeadType,
		LeadID:      current.LeadID,
		StartDate:   current.StartDate,
		DueDate:     current.DueDate,
	}
	if v, ok := writes["title"]; ok {
		params.Title = pgtype.Text{String: pmoAnyToString(v), Valid: true}
	}
	if v, ok := writes["description"]; ok {
		params.Description = pgtype.Text{String: pmoAnyToString(v), Valid: true}
	}
	if v, ok := writes["status"]; ok {
		params.Status = pgtype.Text{String: pmoAnyToString(v), Valid: true}
	}
	if v, ok := writes["start_date"]; ok {
		params.StartDate = pmoToPGDate(v)
	}
	if v, ok := writes["due_date"]; ok {
		params.DueDate = pmoToPGDate(v)
	}
	if v, ok := writes["lead_id"]; ok {
		leadID, valid := pmoAnyToUUID(v)
		params.LeadID = leadID
		if valid {
			params.LeadType = pgtype.Text{String: pmoLocalTypeMember, Valid: true}
		} else {
			params.LeadType = pgtype.Text{}
		}
	}
	_, err = qtx.UpdateProject(ctx, params)
	return err
}

// applyIssueFields mirrors applyProjectFields for issue rows, writing the
// workload property separately when it is among the changed fields.
func (s *PMOService) applyIssueFields(ctx context.Context, qtx *db.Queries, workspaceID pgtype.UUID, issueID pgtype.UUID, workloadPropertyID pgtype.UUID, writes map[string]any) error {
	current, err := qtx.GetIssue(ctx, issueID)
	if err != nil {
		return fmt.Errorf("pmo apply: reload issue: %w", err)
	}
	params := db.UpdateIssueParams{
		ID:            issueID,
		Title:         pgtype.Text{String: current.Title, Valid: true},
		Description:   current.Description,
		Status:        pgtype.Text{String: current.Status, Valid: true},
		Priority:      pgtype.Text{String: current.Priority, Valid: true},
		AssigneeType:  current.AssigneeType,
		AssigneeID:    current.AssigneeID,
		Position:      pgtype.Float8{Float64: current.Position, Valid: true},
		StartDate:     current.StartDate,
		DueDate:       current.DueDate,
		ParentIssueID: current.ParentIssueID,
		ProjectID:     current.ProjectID,
		Stage:         current.Stage,
	}
	if v, ok := writes["title"]; ok {
		params.Title = pgtype.Text{String: pmoAnyToString(v), Valid: true}
	}
	if v, ok := writes["description"]; ok {
		params.Description = pgtype.Text{String: pmoAnyToString(v), Valid: true}
	}
	if v, ok := writes["status"]; ok {
		params.Status = pgtype.Text{String: pmoAnyToString(v), Valid: true}
	}
	if v, ok := writes["start_date"]; ok {
		params.StartDate = pmoToPGDate(v)
	}
	if v, ok := writes["due_date"]; ok {
		params.DueDate = pmoToPGDate(v)
	}
	if v, ok := writes["assignee_id"]; ok {
		assigneeID, valid := pmoAnyToUUID(v)
		params.AssigneeID = assigneeID
		if valid {
			params.AssigneeType = pgtype.Text{String: pmoLocalTypeMember, Valid: true}
		} else {
			params.AssigneeType = pgtype.Text{}
		}
	}
	if v, ok := writes["project_id"]; ok {
		projectID, _ := pmoAnyToUUID(v)
		params.ProjectID = projectID
	}
	if v, ok := writes["parent_issue_id"]; ok {
		parentID, _ := pmoAnyToUUID(v)
		params.ParentIssueID = parentID
	}
	if _, err := qtx.UpdateIssue(ctx, params); err != nil {
		return err
	}
	if v, ok := writes["workload"]; ok && workloadPropertyID.Valid {
		if _, err := qtx.SetIssuePropertyValue(ctx, db.SetIssuePropertyValueParams{
			ID:          issueID,
			WorkspaceID: workspaceID,
			Key:         util.UUIDToString(workloadPropertyID),
			Value:       pmoJSONNumber(pmoAnyToFloat(v)),
		}); err != nil {
			return err
		}
	}
	return nil
}

func pmoAnyToString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func pmoAnyToUUID(v any) (pgtype.UUID, bool) {
	s, _ := v.(string)
	if s == "" {
		return pgtype.UUID{}, false
	}
	parsed, err := util.ParseUUID(s)
	if err != nil {
		return pgtype.UUID{}, false
	}
	return parsed, true
}

func pmoAnyToFloat(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}

// upsertEntityLink persists the entity binding: canonical local id, advanced
// baselines (E1 external; freshly-read L1 local — except local-only fields,
// which keep their old local baseline so the local edit keeps surfacing),
// and the external identity columns (display_number / numeric_id / task_id).
func (s *PMOService) upsertEntityLink(
	ctx context.Context,
	qtx *db.Queries,
	workspaceID, configID pgtype.UUID,
	entity PMOEntityDiff,
	localID pgtype.UUID,
	baselineExternal, baselineLocal map[string]any,
	oldBaselineExternal, oldBaselineLocal map[string]any,
	identity PMORequirement,
) error {
	// Local-only fields: the local edit wins and the baselines must NOT
	// advance — keep the previously acknowledged local value so the local
	// edit keeps surfacing as local_only until the external side catches up.
	for field, fieldDiff := range entity.Fields {
		if fieldDiff.Decision == PMOLocalOnly && oldBaselineLocal != nil {
			if old, ok := oldBaselineLocal[field]; ok {
				baselineLocal[field] = old
			}
		}
	}
	externalJSON, err := json.Marshal(baselineExternal)
	if err != nil {
		return fmt.Errorf("pmo apply: marshal baseline_external: %w", err)
	}
	localJSON, err := json.Marshal(baselineLocal)
	if err != nil {
		return fmt.Errorf("pmo apply: marshal baseline_local: %w", err)
	}

	params := db.UpsertPMOSyncLinkParams{
		WorkspaceID:      workspaceID,
		ConfigID:         configID,
		ExternalType:     entity.ExternalType,
		ExternalKey:      entity.ExternalKey,
		BaselineExternal: externalJSON,
		BaselineLocal:    localJSON,
		ExternalMetadata: []byte(`{}`),
	}
	if entity.ExternalType == "requirement" && identity.Key == entity.ExternalKey {
		if identity.DisplayNumber != "" {
			params.ExternalDisplayNumber = pgtype.Text{String: identity.DisplayNumber, Valid: true}
		}
		if identity.NumericID > 0 {
			params.ExternalNumericID = pgtype.Int8{Int64: identity.NumericID, Valid: true}
		}
	}
	if entity.ExternalType == "task" {
		params.ExternalTaskID = pgtype.Text{String: entity.ExternalKey, Valid: true}
	}
	if entity.ParentExternalKey != "" {
		params.ParentExternalKey = pgtype.Text{String: entity.ParentExternalKey, Valid: true}
	}
	if localID.Valid {
		params.LocalType = pgtype.Text{String: string(entity.LocalType), Valid: true}
		params.LocalID = localID
	}
	_, err = qtx.UpsertPMOSyncLink(ctx, params)
	return err
}

// requirementIdentity resolves the snapshot requirement row for one diff
// entity so the link row can carry display_number / numeric_id (fields the
// diff intentionally does not compare).
func requirementIdentity(entity PMOEntityDiff, requirements map[string]PMORequirement) PMORequirement {
	if entity.ExternalType == "requirement" {
		return requirements[entity.ExternalKey]
	}
	return PMORequirement{}
}

// upsertAssigneeLinks ensures one assignee link row per external owner in
// the snapshot, preserving any existing explicit member mapping.
func (s *PMOService) upsertAssigneeLinks(ctx context.Context, qtx *db.Queries, workspaceID, configID pgtype.UUID, snapshot PMOSnapshot, byIdentity map[string]db.PmoSyncLink) error {
	owners := map[string]*PMOExternalOwner{}
	addOwner := func(o *PMOExternalOwner) {
		if o != nil && o.ExternalID != "" {
			owners[o.ExternalID] = o
		}
	}
	addOwner(snapshot.Parent.Owner)
	for _, child := range snapshot.Children {
		addOwner(child.Owner)
		for i := range child.Tasks {
			addOwner(child.Tasks[i].Owner)
		}
	}
	for i := range snapshot.Tasks {
		addOwner(snapshot.Tasks[i].Owner)
	}

	for externalID, owner := range owners {
		identity := pmoExternalTypeAssignee + "\x00" + externalID
		existing := byIdentity[identity]
		externalJSON, _ := json.Marshal(map[string]any{"external_id": externalID, "display_name": owner.DisplayName})
		localJSON := []byte(`{}`)
		params := db.UpsertPMOSyncLinkParams{
			WorkspaceID:      workspaceID,
			ConfigID:         configID,
			ExternalType:     pmoExternalTypeAssignee,
			ExternalKey:      externalID,
			BaselineExternal: externalJSON,
			BaselineLocal:    localJSON,
			ExternalMetadata: []byte(`{}`),
		}
		if existing.ID.Valid && existing.LocalID.Valid {
			params.LocalType = existing.LocalType
			params.LocalID = existing.LocalID
		}
		if _, err := qtx.UpsertPMOSyncLink(ctx, params); err != nil {
			return err
		}
	}
	return nil
}

// SetAssigneeMapping maps an external owner identity to a workspace member
// BY MEMBER ID (external display names are never matched). Stored as an
// assignee-type pmo_sync_link row; takes effect on the next apply.
func (s *PMOService) SetAssigneeMapping(ctx context.Context, workspaceID, configID pgtype.UUID, externalKey string, memberUserID pgtype.UUID) (db.PmoSyncLink, error) {
	config, err := s.Queries.GetPMOSyncConfig(ctx, db.GetPMOSyncConfigParams{ID: configID, WorkspaceID: workspaceID})
	if err != nil || !config.ID.Valid {
		return db.PmoSyncLink{}, ErrPMORunNotFound
	}
	member, err := s.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: memberUserID, WorkspaceID: workspaceID})
	if err != nil || !member.ID.Valid {
		return db.PmoSyncLink{}, ErrPMOMemberNotFound
	}
	externalJSON, _ := json.Marshal(map[string]any{"external_id": externalKey})
	localJSON, _ := json.Marshal(map[string]any{"member_id": util.UUIDToString(member.UserID)})
	return s.Queries.UpsertPMOSyncLink(ctx, db.UpsertPMOSyncLinkParams{
		WorkspaceID:      workspaceID,
		ConfigID:         configID,
		ExternalType:     pmoExternalTypeAssignee,
		ExternalKey:      externalKey,
		BaselineExternal: externalJSON,
		BaselineLocal:    localJSON,
		ExternalMetadata: []byte(`{}`),
		LocalType:        pgtype.Text{String: pmoLocalTypeMember, Valid: true},
		LocalID:          member.UserID,
	})
}

// ensureWorkloadProperty creates (once per workspace) or reuses the numeric
// issue-property definition PMO stores external workload under, recording it
// on the config so later applies skip the lookup. The definition is shared
// across every PMO configuration in the workspace.
func (s *PMOService) ensureWorkloadProperty(ctx context.Context, qtx *db.Queries, workspaceID pgtype.UUID, config db.PmoSyncConfig) (pgtype.UUID, error) {
	if config.WorkloadPropertyID.Valid {
		return config.WorkloadPropertyID, nil
	}
	if existing, err := qtx.GetIssuePropertyByWorkspaceAndName(ctx, db.GetIssuePropertyByWorkspaceAndNameParams{
		WorkspaceID: workspaceID, Name: pmoWorkloadPropertyName,
	}); err == nil && existing.ID.Valid {
		if _, err := qtx.SetPMOSyncConfigWorkloadProperty(ctx, db.SetPMOSyncConfigWorkloadPropertyParams{
			ID: config.ID, WorkspaceID: workspaceID, WorkloadPropertyID: existing.ID,
		}); err != nil {
			return pgtype.UUID{}, err
		}
		return existing.ID, nil
	}
	prop, err := qtx.CreateIssueProperty(ctx, db.CreateIssuePropertyParams{
		WorkspaceID: workspaceID,
		Name:        pmoWorkloadPropertyName,
		Type:        pmoWorkloadPropertyType,
		Description: pmoWorkloadPropertyDesc,
		Icon:        pmoWorkloadPropertyIcon,
		Config:      []byte(`{}`),
	})
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("pmo apply: create workload property: %w", err)
	}
	if _, err := qtx.SetPMOSyncConfigWorkloadProperty(ctx, db.SetPMOSyncConfigWorkloadPropertyParams{
		ID: config.ID, WorkspaceID: workspaceID, WorkloadPropertyID: prop.ID,
	}); err != nil {
		return pgtype.UUID{}, err
	}
	return prop.ID, nil
}
