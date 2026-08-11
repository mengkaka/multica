/** Discriminator for how a PMO run was started. */
export type PMORunTrigger = "manual" | "scheduled";

/**
 * Processing state of one PMO sync run. Mirrors the server status enum —
 * keep in sync with server/internal/service/pmo_apply.go and the run
 * state machine in the PMO design doc.
 */
export type PMORunStatus =
  | "queued"
  | "running"
  | "preview_ready"
  | "applied"
  | "applied_with_review"
  | "failed";

/** Conflict resolution choice, scoped to one (entity, field) pair. */
export type PMOApplyChoice = "external" | "local";

/**
 * An external entity type a sync link or resolution can address.
 * Kept loose (`string`) at the wire boundary since the server may add types;
 * the typed unions below are for authoring convenience.
 */
export type PMOExternalType = string;

/** One explicit, field-scoped resolution for a conflicted diff entry. */
export interface PMOConflictResolution {
  external_type: PMOExternalType;
  external_key: string;
  /** Synced field name, e.g. "title" | "status" | "assignee_id". */
  field: string;
  choice: PMOApplyChoice;
}

/** One PMO sync configuration (an external root requirement). */
export interface PMOConfig {
  id: string;
  workspace_id: string;
  name: string;
  agent_id: string;
  root_external_key: string;
  /** Numeric issue-property definition backing workload; null until first apply. */
  workload_property_id: string | null;
  orchestration_squad_id: string | null;
  orchestration_issue_id: string | null;
  schedule_enabled: boolean;
  next_run_at: string | null;
  last_run_at: string | null;
  last_applied_at: string | null;
  created_by: string;
  created_at: string;
  updated_at: string;
}

/** One external assignee identity and its optional member mapping. */
export interface PMOSyncLink {
  id: string;
  workspace_id: string;
  config_id: string;
  external_type: PMOExternalType;
  external_key: string;
  local_type: string | null;
  local_id: string | null;
  external_ids: {
    display_number: string | null;
    numeric_id: number | null;
    task_id: string | null;
  };
  parent_external_key: string | null;
  externally_removed_at: string | null;
}

/** Raw normalized snapshot as stored on the run (shape is backend-owned). */
export type PMOSnapshot = Record<string, unknown>;

/** Computed three-way diff stored on the run (shape is backend-owned). */
export type PMODiff = Record<string, unknown>;

/** Count summary stored on the run (shape is backend-owned). */
export type PMOSummary = Record<string, unknown>;

/** One PMO sync run (an immutable acquisition + its processing state). */
export interface PMORun {
  id: string;
  workspace_id: string;
  config_id: string;
  agent_task_id: string | null;
  trigger: PMORunTrigger;
  status: PMORunStatus;
  source_snapshot: PMOSnapshot | null;
  diff: PMODiff | null;
  summary: PMOSummary | null;
  error_code: string | null;
  error_message: string | null;
  requested_by: string | null;
  created_at: string;
  started_at: string | null;
  completed_at: string | null;
  applied_at: string | null;
}

/** Request body for POST /api/pmo/configs. */
export interface CreatePMOConfigRequest {
  name: string;
  agent_id: string;
  root_external_key: string;
  orchestration_squad_id: string | null;
}

/** Request body for PUT /api/pmo/configs/:id. */
export interface UpdatePMOConfigRequest
  extends CreatePMOConfigRequest {
  schedule_enabled: boolean;
}

/** Request body for POST /api/pmo/runs/:id/apply. */
export interface ApplyPMORunRequest {
  conflict_resolutions?: PMOConflictResolution[];
}

/** Request body for PUT /api/pmo/configs/:id/assignees/:externalKey. */
export interface SetPMOAssigneeMappingRequest {
  member_id: string;
}

/** List envelope for GET /api/pmo/configs. */
export interface ListPMOConfigsResponse {
  configs: PMOConfig[];
}

/** List envelope for GET /api/pmo/runs. */
export interface ListPMORunsResponse {
  runs: PMORun[];
}
