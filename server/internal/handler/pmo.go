package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	maxPMOConfigNameBytes = 200
	maxPMORootKeyBytes    = 256
)

type PMOConfigResponse struct {
	ID                   string  `json:"id"`
	WorkspaceID          string  `json:"workspace_id"`
	Name                 string  `json:"name"`
	AgentID              string  `json:"agent_id"`
	RootExternalKey      string  `json:"root_external_key"`
	WorkloadPropertyID   *string `json:"workload_property_id"`
	OrchestrationSquadID *string `json:"orchestration_squad_id"`
	OrchestrationIssueID *string `json:"orchestration_issue_id"`
	ScheduleEnabled      bool    `json:"schedule_enabled"`
	NextRunAt            *string `json:"next_run_at"`
	LastRunAt            *string `json:"last_run_at"`
	LastAppliedAt        *string `json:"last_applied_at"`
	CreatedBy            string  `json:"created_by"`
	CreatedAt            string  `json:"created_at"`
	UpdatedAt            string  `json:"updated_at"`
}

type PMORunResponse struct {
	ID             string          `json:"id"`
	WorkspaceID    string          `json:"workspace_id"`
	ConfigID       string          `json:"config_id"`
	AgentTaskID    *string         `json:"agent_task_id"`
	Trigger        string          `json:"trigger"`
	Status         string          `json:"status"`
	SourceSnapshot json.RawMessage `json:"source_snapshot"`
	Diff           json.RawMessage `json:"diff"`
	Summary        json.RawMessage `json:"summary"`
	ErrorCode      *string         `json:"error_code"`
	ErrorMessage   *string         `json:"error_message"`
	RequestedBy    *string         `json:"requested_by"`
	CreatedAt      string          `json:"created_at"`
	StartedAt      *string         `json:"started_at"`
	CompletedAt    *string         `json:"completed_at"`
	AppliedAt      *string         `json:"applied_at"`
}

type createPMOConfigRequest struct {
	Name                 string  `json:"name"`
	AgentID              string  `json:"agent_id"`
	RootExternalKey      string  `json:"root_external_key"`
	OrchestrationSquadID *string `json:"orchestration_squad_id"`
}

type updatePMOConfigRequest struct {
	Name                 string  `json:"name"`
	AgentID              string  `json:"agent_id"`
	RootExternalKey      string  `json:"root_external_key"`
	ScheduleEnabled      bool    `json:"schedule_enabled"`
	OrchestrationSquadID *string `json:"orchestration_squad_id"`
}

func pmoConfigToResponse(config db.PmoSyncConfig) PMOConfigResponse {
	return PMOConfigResponse{
		ID:                   uuidToString(config.ID),
		WorkspaceID:          uuidToString(config.WorkspaceID),
		Name:                 config.Name,
		AgentID:              uuidToString(config.AgentID),
		RootExternalKey:      config.RootExternalKey,
		WorkloadPropertyID:   uuidToPtr(config.WorkloadPropertyID),
		OrchestrationSquadID: uuidToPtr(config.OrchestrationSquadID),
		OrchestrationIssueID: uuidToPtr(config.OrchestrationIssueID),
		ScheduleEnabled:      config.ScheduleEnabled,
		NextRunAt:            timestampToPtr(config.NextRunAt),
		LastRunAt:            timestampToPtr(config.LastRunAt),
		LastAppliedAt:        timestampToPtr(config.LastAppliedAt),
		CreatedBy:            uuidToString(config.CreatedBy),
		CreatedAt:            timestampToString(config.CreatedAt),
		UpdatedAt:            timestampToString(config.UpdatedAt),
	}
}

func pmoRunToResponse(run db.PmoSyncRun) PMORunResponse {
	return PMORunResponse{
		ID:             uuidToString(run.ID),
		WorkspaceID:    uuidToString(run.WorkspaceID),
		ConfigID:       uuidToString(run.ConfigID),
		AgentTaskID:    uuidToPtr(run.AgentTaskID),
		Trigger:        run.Trigger,
		Status:         run.Status,
		SourceSnapshot: json.RawMessage(run.SourceSnapshot),
		Diff:           json.RawMessage(run.Diff),
		Summary:        json.RawMessage(run.Summary),
		ErrorCode:      textToPtr(run.ErrorCode),
		ErrorMessage:   textToPtr(run.ErrorMessage),
		RequestedBy:    uuidToPtr(run.RequestedBy),
		CreatedAt:      timestampToString(run.CreatedAt),
		StartedAt:      timestampToPtr(run.StartedAt),
		CompletedAt:    timestampToPtr(run.CompletedAt),
		AppliedAt:      timestampToPtr(run.AppliedAt),
	}
}

func (h *Handler) ListPMOConfigs(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	configs, err := h.Queries.ListPMOSyncConfigs(r.Context(), workspaceUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list PMO configurations")
		return
	}
	response := make([]PMOConfigResponse, len(configs))
	for i, config := range configs {
		response[i] = pmoConfigToResponse(config)
	}
	writeJSON(w, http.StatusOK, map[string]any{"configs": response})
}

func (h *Handler) CreatePMOConfig(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}
	var request createPMOConfigRequest
	if !decodePMORequest(w, r, &request) || !validatePMOConfigInput(w, request.Name, request.AgentID, request.RootExternalKey) {
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, request.AgentID, "agent id")
	if !ok {
		return
	}
	if _, ok := h.requirePMOInvokableAgent(w, r, workspaceID, uuidToString(member.UserID), agentID); !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	var orchestrationSquadID pgtype.UUID
	if request.OrchestrationSquadID != nil {
		orchestrationSquadID, ok = parseUUIDOrBadRequest(w, *request.OrchestrationSquadID, "orchestration squad id")
		if !ok {
			return
		}
	}
	config, err := h.PMOService.CreateConfig(r.Context(), service.CreatePMOConfigParams{
		WorkspaceID: workspaceUUID, Name: request.Name, AgentID: agentID,
		RootExternalKey: request.RootExternalKey, CreatedBy: member.UserID,
		OrchestrationSquadID: orchestrationSquadID,
	})
	if err != nil {
		if errors.Is(err, service.ErrPMOOrchestrationSquad) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a PMO configuration already exists for this external key")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create PMO configuration")
		return
	}
	writeJSON(w, http.StatusCreated, pmoConfigToResponse(config))
}

func (h *Handler) UpdatePMOConfig(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}
	configID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "configuration id")
	if !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetPMOSyncConfig(r.Context(), db.GetPMOSyncConfigParams{ID: configID, WorkspaceID: workspaceUUID}); err != nil {
		writeError(w, http.StatusNotFound, "PMO configuration not found")
		return
	}
	var request updatePMOConfigRequest
	if !decodePMORequest(w, r, &request) || !validatePMOConfigInput(w, request.Name, request.AgentID, request.RootExternalKey) {
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, request.AgentID, "agent id")
	if !ok {
		return
	}
	if _, ok := h.requirePMOInvokableAgent(w, r, workspaceID, uuidToString(member.UserID), agentID); !ok {
		return
	}
	var orchestrationSquadID pgtype.UUID
	if request.OrchestrationSquadID != nil {
		orchestrationSquadID, ok = parseUUIDOrBadRequest(w, *request.OrchestrationSquadID, "orchestration squad id")
		if !ok {
			return
		}
	}
	config, err := h.PMOService.UpdateConfig(r.Context(), service.UpdatePMOConfigParams{
		ID: configID, WorkspaceID: workspaceUUID, Name: request.Name, AgentID: agentID,
		RootExternalKey: request.RootExternalKey, ScheduleEnabled: request.ScheduleEnabled,
		OrchestrationSquadID: orchestrationSquadID,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrPMOScheduleNeedsApply), errors.Is(err, service.ErrPMORootKeyLocked), errors.Is(err, service.ErrPMOOrchestrationSquad):
			writeError(w, http.StatusBadRequest, err.Error())
		case isUniqueViolation(err):
			writeError(w, http.StatusConflict, "a PMO configuration already exists for this external key")
		default:
			writeError(w, http.StatusInternalServerError, "failed to update PMO configuration")
		}
		return
	}
	writeJSON(w, http.StatusOK, pmoConfigToResponse(config))
}

func (h *Handler) DeletePMOConfig(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	configID, workspaceUUID, ok := parsePMOResourceIDs(w, r, workspaceID, "configuration id")
	if !ok {
		return
	}
	err := h.PMOService.DeleteConfig(r.Context(), workspaceUUID, configID)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			writeError(w, http.StatusNotFound, "PMO configuration not found")
		case errors.Is(err, service.ErrPMOActiveRun):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to delete PMO configuration")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) StartPMORun(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}
	configID, workspaceUUID, ok := parsePMOResourceIDs(w, r, workspaceID, "configuration id")
	if !ok {
		return
	}
	config, err := h.Queries.GetPMOSyncConfig(r.Context(), db.GetPMOSyncConfigParams{ID: configID, WorkspaceID: workspaceUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "PMO configuration not found")
		return
	}
	if _, ok := h.requirePMOInvokableAgent(w, r, workspaceID, uuidToString(member.UserID), config.AgentID); !ok {
		return
	}
	run, err := h.PMOService.StartRun(r.Context(), workspaceUUID, configID, member.UserID, "manual")
	if err != nil {
		switch {
		case errors.Is(err, service.ErrPMOActiveRun):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, service.ErrPMOAgentUnavailable):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to start PMO run")
		}
		return
	}
	writeJSON(w, http.StatusCreated, pmoRunToResponse(run))
}

func (h *Handler) ListPMORuns(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	var configID pgtype.UUID
	if raw := r.URL.Query().Get("config_id"); raw != "" {
		configID, ok = parseUUIDOrBadRequest(w, raw, "configuration id")
		if !ok {
			return
		}
	}
	limit := int32(50)
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 100")
			return
		}
		limit = int32(parsed)
	}
	runs, err := h.Queries.ListPMOSyncRuns(r.Context(), db.ListPMOSyncRunsParams{
		WorkspaceID: workspaceUUID, Limit: limit, Offset: 0, ConfigID: configID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list PMO runs")
		return
	}
	response := make([]PMORunResponse, len(runs))
	for i, run := range runs {
		response[i] = pmoRunToResponse(run)
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": response})
}

func (h *Handler) GetPMORun(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	runID, workspaceUUID, ok := parsePMOResourceIDs(w, r, workspaceID, "run id")
	if !ok {
		return
	}
	run, err := h.Queries.GetPMOSyncRun(r.Context(), db.GetPMOSyncRunParams{ID: runID, WorkspaceID: workspaceUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "PMO run not found")
		return
	}
	writeJSON(w, http.StatusOK, pmoRunToResponse(run))
}

type applyPMORunRequest struct {
	ConflictResolutions []applyPMOConflictResolution `json:"conflict_resolutions"`
}

type applyPMOConflictResolution struct {
	ExternalType string `json:"external_type"`
	ExternalKey  string `json:"external_key"`
	Field        string `json:"field"`
	Choice       string `json:"choice"` // external | local
}

// ApplyPMORun applies a preview_ready run in one transaction. Owner/admin
// only; manual apply of a scheduled run is not distinguished here — both
// flow through PMOService.ApplyRun (Task 7 also calls it with nil
// resolutions).
func (h *Handler) ApplyPMORun(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}
	runID, workspaceUUID, ok := parsePMOResourceIDs(w, r, workspaceID, "run id")
	if !ok {
		return
	}

	var request applyPMORunRequest
	if r.ContentLength != 0 {
		if !decodePMORequest(w, r, &request) {
			return
		}
	}
	resolutions := make([]service.PMOConflictResolution, 0, len(request.ConflictResolutions))
	for _, raw := range request.ConflictResolutions {
		resolutions = append(resolutions, service.PMOConflictResolution{
			ExternalType: raw.ExternalType,
			ExternalKey:  raw.ExternalKey,
			Field:        raw.Field,
			Choice:       raw.Choice,
		})
	}

	run, changedProjects, err := h.PMOService.ApplyRunWithProjectChanges(r.Context(), workspaceUUID, runID, resolutions)
	if err != nil {
		switch {
		case err == service.ErrPMORunNotFound:
			writeError(w, http.StatusNotFound, "PMO run not found")
		case err == service.ErrPMORunNotPreviewReady:
			writeError(w, http.StatusConflict, "PMO run is not ready to apply")
		case errors.Is(err, service.ErrPMOOrchestrationIssueLink), errors.Is(err, service.ErrPMOOrchestrationSquad):
			writeError(w, http.StatusConflict, err.Error())
		case isBadRequestInput(err):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			// Apply failures roll back entirely; the run stays preview_ready
			// for retry. Never leak stored error text to the client.
			slog.Warn("apply pmo run failed", "run_id", chi.URLParam(r, "id"), "error", err)
			writeError(w, http.StatusInternalServerError, "failed to apply PMO run")
		}
		return
	}
	for _, project := range changedProjects {
		h.publishProjectUpdated(r.Context(), project, "member", uuidToString(member.UserID))
	}
	writeJSON(w, http.StatusOK, pmoRunToResponse(run))
}

// isBadRequestInput classifies the validation errors ApplyRun returns before
// any database write.
func isBadRequestInput(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "invalid conflict resolution choice") ||
		strings.Contains(err.Error(), "conflict resolution must name"))
}

type setPMOAssigneeMappingRequest struct {
	MemberID string `json:"member_id"`
}

// SetPMOAssigneeMapping maps an external assignee identity to a workspace
// member BY MEMBER ID. Never matched by display name. Owner/admin only.
func (h *Handler) SetPMOAssigneeMapping(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	configID, workspaceUUID, ok := parsePMOResourceIDs(w, r, workspaceID, "configuration id")
	if !ok {
		return
	}
	externalKey := strings.TrimSpace(chi.URLParam(r, "externalKey"))
	if externalKey == "" {
		writeError(w, http.StatusBadRequest, "external assignee key is required")
		return
	}

	var request setPMOAssigneeMappingRequest
	if !decodePMORequest(w, r, &request) {
		return
	}
	memberUserID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(request.MemberID), "member_id")
	if !ok {
		return
	}

	link, err := h.PMOService.SetAssigneeMapping(r.Context(), workspaceUUID, configID, externalKey, memberUserID)
	if err != nil {
		switch {
		case err == service.ErrPMOMemberNotFound:
			writeError(w, http.StatusNotFound, "member not found in this workspace")
		case err == service.ErrPMORunNotFound:
			writeError(w, http.StatusNotFound, "PMO configuration not found")
		default:
			writeError(w, http.StatusInternalServerError, "failed to map PMO assignee")
		}
		return
	}
	writeJSON(w, http.StatusOK, pmoSyncLinkToResponse(link))
}

func pmoSyncLinkToResponse(link db.PmoSyncLink) map[string]any {
	return map[string]any{
		"id":            uuidToString(link.ID),
		"workspace_id":  uuidToString(link.WorkspaceID),
		"config_id":     uuidToString(link.ConfigID),
		"external_type": link.ExternalType,
		"external_key":  link.ExternalKey,
		"local_type":    textToPtr(link.LocalType),
		"local_id":      uuidToPtr(link.LocalID),
		"external_ids": map[string]any{
			"display_number": textToPtr(link.ExternalDisplayNumber),
			"numeric_id":     int64ToPtr(link.ExternalNumericID),
			"task_id":        textToPtr(link.ExternalTaskID),
		},
		"parent_external_key":   textToPtr(link.ParentExternalKey),
		"externally_removed_at": timestampToPtr(link.ExternallyRemovedAt),
	}
}

// int64ToPtr renders a nullable Int8 without leaking the pgtype wrapper.
func int64ToPtr(v pgtype.Int8) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}

func decodePMORequest(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	// Reject trailing JSON: a valid PMO request body is exactly one JSON value,
	// matching the strict one-object contract enforced on Agent snapshots.
	var trailing any
	if err := decoder.Decode(&trailing); err == nil || !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func validatePMOConfigInput(w http.ResponseWriter, name, agentID, rootExternalKey string) bool {
	name = strings.TrimSpace(name)
	rootExternalKey = strings.TrimSpace(rootExternalKey)
	if name == "" || len(name) > maxPMOConfigNameBytes {
		writeError(w, http.StatusBadRequest, "name is required and must not exceed 200 bytes")
		return false
	}
	if strings.TrimSpace(agentID) == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return false
	}
	if rootExternalKey == "" || len(rootExternalKey) > maxPMORootKeyBytes {
		writeError(w, http.StatusBadRequest, "root_external_key is required and must not exceed 256 bytes")
		return false
	}
	return true
}

func (h *Handler) requirePMOInvokableAgent(w http.ResponseWriter, r *http.Request, workspaceID, userID string, agentID pgtype.UUID) (db.Agent, bool) {
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.Agent{}, false
	}
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: workspaceUUID})
	if err != nil || agent.ArchivedAt.Valid || !agent.RuntimeID.Valid {
		writeError(w, http.StatusBadRequest, "agent is unavailable")
		return db.Agent{}, false
	}
	if !h.canInvokeAgent(r.Context(), agent, "member", userID, userID, workspaceID) {
		writeError(w, http.StatusForbidden, "agent cannot be invoked by this member")
		return db.Agent{}, false
	}
	return agent, true
}

func parsePMOResourceIDs(w http.ResponseWriter, r *http.Request, workspaceID, resourceName string) (pgtype.UUID, pgtype.UUID, bool) {
	resourceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), resourceName)
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	return resourceID, workspaceUUID, true
}
