package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/attribution"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const PMOSyncContextType = "pmo_sync"

var (
	ErrPMOActiveRun          = errors.New("pmo sync already has an active run")
	ErrPMOScheduleNeedsApply = errors.New("pmo schedule requires a successfully applied run")
	ErrPMOAgentUnavailable   = errors.New("pmo agent is unavailable")
	ErrPMOOrchestrationSquad = errors.New("pmo orchestration squad must be active with an active agent leader in the same workspace")
	ErrPMORootKeyLocked      = errors.New("pmo external root key cannot change after the first applied run; existing links belong to that root")
)

type PMOSyncContext struct {
	Type        string `json:"type"`
	WorkspaceID string `json:"workspace_id"`
	RequesterID string `json:"requester_id,omitempty"`
	RunID       string `json:"run_id"`
	Prompt      string `json:"prompt"`
}

// ParsePMOSyncContext returns the PMO sync payload when the given task
// context JSONB carries type == "pmo_sync"; otherwise ok is false so callers
// can short-circuit. Shared by the task pipeline (workspace resolution,
// claim, completion, failure) and exported because the daemon handlers parse
// contexts they did not create.
func ParsePMOSyncContext(contextJSON []byte) (PMOSyncContext, bool) {
	if len(contextJSON) == 0 {
		return PMOSyncContext{}, false
	}
	var pmoCtx PMOSyncContext
	if err := json.Unmarshal(contextJSON, &pmoCtx); err != nil || pmoCtx.Type != PMOSyncContextType {
		return PMOSyncContext{}, false
	}
	return pmoCtx, true
}

// PreparePMOSyncRunPreview serializes a validated snapshot into the columns
// StorePMOSyncRunPreview persists: the normalized source snapshot plus a
// three-way diff and its summary. Task 5 has no linked canonical entities
// yet (the apply logic lands in Task 6), so the diff runs against empty
// local state — every snapshot entity reads as a create proposal and the
// preview changes no projects or issues.
func PreparePMOSyncRunPreview(snapshot PMOSnapshot) (sourceSnapshot, diffJSON, summaryJSON []byte, err error) {
	sourceSnapshot, err = json.Marshal(snapshot)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal pmo source snapshot: %w", err)
	}
	diff := DiffPMOSnapshot(PMODiffInput{Snapshot: snapshot})
	diffJSON, err = json.Marshal(diff)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal pmo diff: %w", err)
	}
	summaryJSON, err = json.Marshal(diff.Summary)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal pmo summary: %w", err)
	}
	return sourceSnapshot, diffJSON, summaryJSON, nil
}

type PMOService struct {
	Queries   *db.Queries
	TxStarter TxStarter
	TaskSvc   *TaskService
	// IssueSvc shares the issue-creation pipeline so PMO apply creates issues
	// with identical numbering / duplicate-guard / position semantics, inside
	// the apply transaction. Wired post-construction to break the init cycle.
	IssueSvc *IssueService

	// applyTestHook lets tests inject a failure inside the apply transaction
	// to prove rollback semantics. Never set outside tests.
	applyTestHook func(ctx context.Context, qtx *db.Queries) error
}

// SetApplyTestHook swaps the test-only ApplyRun failure hook (cross-package
// tests cannot touch the unexported field). Pass nil to clear.
func (s *PMOService) SetApplyTestHook(hook func(ctx context.Context, qtx *db.Queries) error) {
	s.applyTestHook = hook
}

type CreatePMOConfigParams struct {
	WorkspaceID          pgtype.UUID
	Name                 string
	AgentID              pgtype.UUID
	RootExternalKey      string
	CreatedBy            pgtype.UUID
	OrchestrationSquadID pgtype.UUID
}

type UpdatePMOConfigParams struct {
	ID                   pgtype.UUID
	WorkspaceID          pgtype.UUID
	Name                 string
	AgentID              pgtype.UUID
	RootExternalKey      string
	ScheduleEnabled      bool
	OrchestrationSquadID pgtype.UUID
}

func NewPMOService(queries *db.Queries, txStarter TxStarter, taskSvc *TaskService) *PMOService {
	return &PMOService{Queries: queries, TxStarter: txStarter, TaskSvc: taskSvc}
}

func (s *PMOService) CreateConfig(ctx context.Context, params CreatePMOConfigParams) (db.PmoSyncConfig, error) {
	if err := s.validateOrchestrationSquad(ctx, params.WorkspaceID, params.OrchestrationSquadID); err != nil {
		return db.PmoSyncConfig{}, err
	}
	return s.Queries.CreatePMOSyncConfig(ctx, db.CreatePMOSyncConfigParams{
		WorkspaceID:          params.WorkspaceID,
		Name:                 strings.TrimSpace(params.Name),
		AgentID:              params.AgentID,
		RootExternalKey:      strings.TrimSpace(params.RootExternalKey),
		CreatedBy:            params.CreatedBy,
		OrchestrationSquadID: params.OrchestrationSquadID,
	})
}

func (s *PMOService) UpdateConfig(ctx context.Context, params UpdatePMOConfigParams) (db.PmoSyncConfig, error) {
	current, err := s.Queries.GetPMOSyncConfig(ctx, db.GetPMOSyncConfigParams{ID: params.ID, WorkspaceID: params.WorkspaceID})
	if err != nil {
		return db.PmoSyncConfig{}, err
	}
	if params.ScheduleEnabled && !current.LastAppliedAt.Valid {
		return db.PmoSyncConfig{}, ErrPMOScheduleNeedsApply
	}
	// After the first successful apply, pmo_sync_link rows are bound to the
	// root key. Switching roots would orphan every link's baseline and make
	// the three-way comparison meaningless, so the root is immutable from then on.
	if current.LastAppliedAt.Valid && strings.TrimSpace(params.RootExternalKey) != current.RootExternalKey {
		return db.PmoSyncConfig{}, ErrPMORootKeyLocked
	}
	if err := s.validateOrchestrationSquad(ctx, params.WorkspaceID, params.OrchestrationSquadID); err != nil {
		return db.PmoSyncConfig{}, err
	}
	return s.Queries.UpdatePMOSyncConfig(ctx, db.UpdatePMOSyncConfigParams{
		ID:                   params.ID,
		WorkspaceID:          params.WorkspaceID,
		Name:                 strings.TrimSpace(params.Name),
		AgentID:              params.AgentID,
		RootExternalKey:      strings.TrimSpace(params.RootExternalKey),
		ScheduleEnabled:      params.ScheduleEnabled,
		OrchestrationSquadID: params.OrchestrationSquadID,
	})
}

func (s *PMOService) validateOrchestrationSquad(ctx context.Context, workspaceID, squadID pgtype.UUID) error {
	if !squadID.Valid {
		return nil
	}
	squad, err := s.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
		ID: squadID, WorkspaceID: workspaceID,
	})
	if err != nil || squad.ArchivedAt.Valid {
		return ErrPMOOrchestrationSquad
	}
	leader, err := s.Queries.GetAgent(ctx, squad.LeaderID)
	if err != nil || !validPMOOrchestrationSquad(workspaceID, squad, leader) {
		return ErrPMOOrchestrationSquad
	}
	return nil
}

func validPMOOrchestrationSquad(workspaceID pgtype.UUID, squad db.Squad, leader db.Agent) bool {
	return !squad.ArchivedAt.Valid &&
		!leader.ArchivedAt.Valid &&
		squad.WorkspaceID == workspaceID &&
		leader.WorkspaceID == workspaceID &&
		squad.LeaderID == leader.ID
}

func (s *PMOService) DeleteConfig(ctx context.Context, workspaceID, configID pgtype.UUID) error {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)
	if _, err := qtx.GetPMOSyncConfigForUpdate(ctx, db.GetPMOSyncConfigForUpdateParams{ID: configID, WorkspaceID: workspaceID}); err != nil {
		return err
	}
	if _, err := qtx.GetActivePMOSyncRun(ctx, db.GetActivePMOSyncRunParams{WorkspaceID: workspaceID, ConfigID: configID}); err == nil {
		return ErrPMOActiveRun
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err := qtx.DeletePMOSyncLinksByConfig(ctx, db.DeletePMOSyncLinksByConfigParams{WorkspaceID: workspaceID, ConfigID: configID}); err != nil {
		return err
	}
	if err := qtx.DeletePMOSyncRunsByConfig(ctx, db.DeletePMOSyncRunsByConfigParams{WorkspaceID: workspaceID, ConfigID: configID}); err != nil {
		return err
	}
	rows, err := qtx.DeletePMOSyncConfig(ctx, db.DeletePMOSyncConfigParams{ID: configID, WorkspaceID: workspaceID})
	if err != nil {
		return err
	}
	if rows != 1 {
		return pgx.ErrNoRows
	}
	return tx.Commit(ctx)
}

func (s *PMOService) StartRun(ctx context.Context, workspaceID, configID, requesterID pgtype.UUID, trigger string) (db.PmoSyncRun, error) {
	if trigger != "manual" && trigger != "scheduled" {
		return db.PmoSyncRun{}, fmt.Errorf("invalid pmo trigger %q", trigger)
	}

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.PmoSyncRun{}, err
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)
	config, err := qtx.GetPMOSyncConfigForUpdate(ctx, db.GetPMOSyncConfigForUpdateParams{ID: configID, WorkspaceID: workspaceID})
	if err != nil {
		return db.PmoSyncRun{}, err
	}
	if _, err := qtx.GetActivePMOSyncRun(ctx, db.GetActivePMOSyncRunParams{WorkspaceID: workspaceID, ConfigID: configID}); err == nil {
		return db.PmoSyncRun{}, ErrPMOActiveRun
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return db.PmoSyncRun{}, err
	}

	agent, err := qtx.LockAgentForAutopilotAssignment(ctx, db.LockAgentForAutopilotAssignmentParams{ID: config.AgentID, WorkspaceID: workspaceID})
	if err != nil || agent.ArchivedAt.Valid || !agent.RuntimeID.Valid {
		return db.PmoSyncRun{}, ErrPMOAgentUnavailable
	}
	requestedBy := requesterID
	if !requestedBy.Valid {
		// Scheduled dispatches carry no requesting member; attribute the audit
		// chain to the config creator, which mirrors the trigger_owner policy
		// (accountable = the member who created the trigger).
		requestedBy = config.CreatedBy
	}
	run, err := qtx.CreatePMOSyncRun(ctx, db.CreatePMOSyncRunParams{
		WorkspaceID: workspaceID,
		ConfigID:    configID,
		Trigger:     trigger,
		RequestedBy: requesterID,
	})
	if err != nil {
		return db.PmoSyncRun{}, err
	}

	payload := PMOSyncContext{
		Type:        PMOSyncContextType,
		WorkspaceID: util.UUIDToString(workspaceID),
		RequesterID: util.UUIDToString(requestedBy),
		RunID:       util.UUIDToString(run.ID),
		Prompt:      BuildPMOSyncPrompt(config.RootExternalKey),
	}
	contextJSON, err := json.Marshal(payload)
	if err != nil {
		return db.PmoSyncRun{}, fmt.Errorf("marshal pmo context: %w", err)
	}
	attr := attribution.DirectHumanRun(requestedBy, "", pgtype.UUID{})
	// A scheduled dispatch has no authorizing human: degrade to the workspace
	// owner-fallback policy (or fail closed) exactly like every other enqueue path.
	if trigger == "scheduled" {
		attr = attribution.TriggerOwner(requestedBy, "", pgtype.UUID{})
		if s.TaskSvc != nil {
			if attr, err = s.TaskSvc.applyAttributionFallback(ctx, attr, agent); err != nil {
				return db.PmoSyncRun{}, err
			}
		}
	}
	attrSource, _, evidenceKind, evidenceRef := attributionCreateParams(attr)
	// Stamp the optional runtime MCP overlay exactly like the other Enqueue*
	// paths. The overlay is a pure function of (originator, agent), so
	// scheduled runs with no authorizing human simply get no overlay.
	var overlayData runtimeMCPOverlayData
	if s.TaskSvc != nil {
		overlayData = s.TaskSvc.buildRuntimeMCPOverlay(ctx, requestedBy, agent)
	}
	task, err := qtx.CreateQuickCreateTask(ctx, db.CreateQuickCreateTaskParams{
		AgentID:              agent.ID,
		RuntimeID:            agent.RuntimeID,
		Priority:             priorityToInt("high"),
		Context:              contextJSON,
		OriginatorUserID:     attr.UserID,
		AccountableUserID:    attr.AccountableUserID,
		RuntimeMcpOverlay:    overlayData.Overlay,
		RuntimeConnectedApps: overlayData.ConnectedApps,
		OriginatorSource:     attrSource,
		TriggerEvidenceKind:  evidenceKind,
		TriggerEvidenceRefID: evidenceRef,
	})
	if err != nil {
		return db.PmoSyncRun{}, fmt.Errorf("create pmo agent task: %w", err)
	}
	run, err = qtx.SetPMOSyncRunAgentTask(ctx, db.SetPMOSyncRunAgentTaskParams{
		ID: run.ID, WorkspaceID: workspaceID, AgentTaskID: task.ID,
	})
	if err != nil {
		return db.PmoSyncRun{}, err
	}
	if _, err := qtx.MarkPMOSyncConfigRunStarted(ctx, db.MarkPMOSyncConfigRunStartedParams{ID: configID, WorkspaceID: workspaceID}); err != nil {
		return db.PmoSyncRun{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.PmoSyncRun{}, err
	}
	if s.TaskSvc != nil {
		s.TaskSvc.NotifyTaskEnqueued(ctx, task)
	}
	return run, nil
}

func BuildPMOSyncPrompt(rootExternalKey string) string {
	keyJSON, _ := json.Marshal(strings.TrimSpace(rootExternalKey))
	return fmt.Sprintf(`Fetch the complete external requirement snapshot rooted at %s using the tools already configured for this Agent.
Return JSON only: one object, with no Markdown fence or prose.
Use exactly this structure:
{"schema_version":"1","snapshot_complete":true,"parent_requirement":{"key":"","display_number":"","numeric_id":1,"title":"","description":"","source_status":"","status":"planned","owner":null,"start_date":null,"due_date":null,"workload":null},"child_requirements":[{"key":"","display_number":"","numeric_id":2,"title":"","description":"","source_status":"","status":"todo","owner":null,"start_date":null,"due_date":null,"workload":null,"tasks":[]}],"tasks":[]}
Each owner is null or {"external_id":"","display_name":""}. Each task contains task_id, scheme_id, title, description, source_status, status, owner, start_date, due_date, workload, and updated_at.
Project status must be one of planned, in_progress, paused, completed, cancelled. Issue and task status must be one of backlog, todo, in_progress, in_review, done, blocked, cancelled. Dates use YYYY-MM-DD and updated_at uses RFC3339. Set snapshot_complete to true only when the snapshot is complete.`, keyJSON)
}
