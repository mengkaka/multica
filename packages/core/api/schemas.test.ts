import { describe, expect, it } from "vitest";
import {
  AppConfigSchema,
  WecomInstallationSchema,
  ListWecomInstallationsResponseSchema,
  RedeemWecomBindingTokenResponseSchema,
  EMPTY_WECOM_INSTALLATION,
  EMPTY_LIST_WECOM_INSTALLATIONS_RESPONSE,
  EMPTY_REDEEM_WECOM_BINDING_TOKEN_RESPONSE,
  AgentTaskListSchema,
  AutopilotRunSchema,
  FALLBACK_AUTOPILOT_RUN,
  CommentTriggerPreviewSchema,
  DashboardAgentRunTimeListSchema,
  DashboardRunTimeDailyListSchema,
  DashboardFailureByAgentListSchema,
  DashboardFailureDailyListSchema,
  DashboardUsageByAgentListSchema,
  DashboardUsageDailyListSchema,
  ChatDraftRestoresResponseSchema,
  ChatPendingTaskSchema,
  PrioritizeQueuedChatTaskResponseSchema,
  CreateFeedbackResponseSchema,
  BatchDeleteIssuesResponseSchema,
  BatchUpdateIssuesResponseSchema,
  DesignRestoreTaskSchema,
  TestCaseSchema,
  ListTestCasesResponseSchema,
  ListTestCaseModulesResponseSchema,
  ListTestCaseRevisionsResponseSchema,
  EMPTY_TEST_CASE,
  EMPTY_LIST_TEST_CASES_RESPONSE,
  EMPTY_LIST_TEST_CASE_MODULES_RESPONSE,
  EMPTY_LIST_TEST_CASE_REVISIONS_RESPONSE,
  TestGenerationJobSchema,
  TestGenerationPlanSchema,
  TestCaseProposalSchema,
  ListTestGenerationJobsResponseSchema,
  ListTestCaseProposalsResponseSchema,
  EMPTY_TEST_GENERATION_JOB,
  EMPTY_TEST_GENERATION_PLAN,
  EMPTY_TEST_CASE_PROPOSAL,
  EMPTY_LIST_TEST_GENERATION_JOBS_RESPONSE,
  EMPTY_LIST_TEST_CASE_PROPOSALS_RESPONSE,
  ListDesignDeliveriesResponseSchema,
  ListDesignRestoreTasksResponseSchema,
  DuplicateIssueErrorBodySchema,
  EMPTY_BATCH_DELETE_ISSUES_RESPONSE,
  EMPTY_BATCH_UPDATE_ISSUES_RESPONSE,
  EMPTY_CHAT_DRAFT_RESTORES,
  EMPTY_CHAT_PENDING_TASK,
  EMPTY_PRIORITIZE_QUEUED_CHAT_TASK_RESPONSE,
  EMPTY_CREATE_FEEDBACK_RESPONSE,
  EMPTY_INBOX_ITEMS,
  EMPTY_INBOX_UNREAD_SUMMARY,
  EMPTY_SEARCH_PROJECTS_RESPONSE,
  EMPTY_USER,
  InboxItemListSchema,
  InboxUnreadSummarySchema,
  IssueTriggerPreviewSchema,
  ListIssuesResponseSchema,
  ListPropertiesResponseSchema,
  MALFORMED_RUNTIME_MODEL_LIST_REQUEST,
  RuntimeModelListRequestSchema,
  SearchProjectsResponseSchema,
  RuntimeHourlyActivityListSchema,
  RuntimeUsageByAgentListSchema,
  RuntimeUsageByHourListSchema,
  RuntimeUsageListSchema,
  SendChatMessageResponseSchema,
  SquadListSchema,
  SquadSchema,
  TimelineEntriesSchema,
  UserSchema,
  parsePMORun,
  parsePMOConfig,
  parsePMOSyncLink,
  parseProject,
  EMPTY_PMO_RUN,
  EMPTY_PMO_SYNC_LINK,
  ListPMOConfigsResponseSchema,
  ListPMORunsResponseSchema,
  EMPTY_LIST_PMO_CONFIGS_RESPONSE,
  EMPTY_LIST_PMO_RUNS_RESPONSE,
  TestPlanSchema,
  TestRunSchema,
  TestRunCaseSchema,
  TestCaseResultTimelineEntrySchema,
  ListTestPlansResponseSchema,
  ListTestRunsResponseSchema,
  ListTestRunCasesResponseSchema,
  TestCaseResultTimelineResponseSchema,
  ListTestCapabilitiesResponseSchema,
  EMPTY_TEST_PLAN,
  EMPTY_TEST_RUN,
  EMPTY_TEST_RUN_CASE,
  EMPTY_LIST_TEST_PLANS_RESPONSE,
  EMPTY_LIST_TEST_RUNS_RESPONSE,
  EMPTY_LIST_TEST_RUN_CASES_RESPONSE,
  EMPTY_TEST_CASE_RESULT_TIMELINE_RESPONSE,
  EMPTY_LIST_TEST_CAPABILITIES_RESPONSE,
} from "./schemas";
import { IssueViewSchema, IssueViewListSchema } from "./schemas";
import { parseWithFallback } from "./schema";

const baseIssue = {
  id: "11111111-1111-1111-1111-111111111111",
  workspace_id: "ws-1",
  number: 1,
  identifier: "MUL-1",
  title: "Test",
  description: null,
  status: "todo",
  priority: "medium",
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "user-1",
  parent_issue_id: null,
  project_id: null,
  position: 0,
  stage: null,
  start_date: null,
  due_date: null,
  metadata: {},
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

describe("IssueSchema (via ListIssuesResponseSchema)", () => {
  it("accepts a primitive metadata KV map", () => {
    const payload = {
      issues: [
        {
          ...baseIssue,
          metadata: { pipeline_status: "waiting", pr_number: 3, is_blocked: true },
        },
      ],
      total: 1,
    };
    const parsed = ListIssuesResponseSchema.parse(payload);
    expect(parsed.issues[0]?.metadata).toEqual({
      pipeline_status: "waiting",
      pr_number: 3,
      is_blocked: true,
    });
  });

  it("defaults metadata to {} when the server omits it (older backend)", () => {
    const { metadata: _omit, ...issueWithoutMetadata } = baseIssue;
    const payload = { issues: [issueWithoutMetadata], total: 1 };
    const parsed = ListIssuesResponseSchema.parse(payload);
    expect(parsed.issues[0]?.metadata).toEqual({});
  });

  it("rejects metadata with non-primitive values (nested object)", () => {
    const payload = {
      issues: [{ ...baseIssue, metadata: { nested: { x: 1 } } }],
      total: 1,
    };
    expect(ListIssuesResponseSchema.safeParse(payload).success).toBe(false);
  });

  it("accepts a numeric stage", () => {
    const payload = { issues: [{ ...baseIssue, stage: 2 }], total: 1 };
    const parsed = ListIssuesResponseSchema.parse(payload);
    expect(parsed.issues[0]?.stage).toBe(2);
  });

  it("defaults stage to null when the server omits it (older backend)", () => {
    const { stage: _omit, ...issueWithoutStage } = baseIssue;
    const payload = { issues: [issueWithoutStage], total: 1 };
    const parsed = ListIssuesResponseSchema.parse(payload);
    expect(parsed.issues[0]?.stage).toBeNull();
  });

  it("accepts custom property values including multi_select arrays", () => {
    const payload = {
      issues: [
        {
          ...baseIssue,
          properties: { "def-1": "opt-a", "def-2": ["opt-x", "opt-y"], "def-3": 3.5, "def-4": true },
        },
      ],
      total: 1,
    };
    const parsed = ListIssuesResponseSchema.parse(payload);
    expect(parsed.issues[0]?.properties).toEqual({
      "def-1": "opt-a",
      "def-2": ["opt-x", "opt-y"],
      "def-3": 3.5,
      "def-4": true,
    });
  });

  it("defaults properties to {} when the server omits it (older backend)", () => {
    const parsed = ListIssuesResponseSchema.parse({ issues: [baseIssue], total: 1 });
    expect(parsed.issues[0]?.properties).toEqual({});
  });

  it("drops unknown-shaped property values instead of failing the issue parse", () => {
    // Forward compat: a future server type (actor/relation) may ship object
    // values. That one entry must disappear; the issue and its other
    // properties must survive — a full parse failure would blank the whole
    // list through parseWithFallback on installed desktop builds.
    const payload = {
      issues: [
        {
          ...baseIssue,
          properties: { "def-1": { nested: 1 }, "def-2": "opt-a" },
        },
      ],
      total: 1,
    };
    const parsed = ListIssuesResponseSchema.parse(payload);
    expect(parsed.issues[0]?.properties).toEqual({ "def-2": "opt-a" });
  });
});

describe("IssuePropertySchema (via ListPropertiesResponseSchema)", () => {
  const baseProperty = {
    id: "22222222-2222-2222-2222-222222222222",
    workspace_id: "ws-1",
    name: "Severity",
    type: "select",
    description: "",
    icon: "flag",
    config: { options: [{ id: "opt-1", name: "Critical", color: "#ef4444" }] },
    position: 1,
    archived: false,
    archived_at: null,
    usage_count: 2,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };

  it("parses a full definition", () => {
    const parsed = ListPropertiesResponseSchema.parse({ properties: [baseProperty], total: 1 });
    expect(parsed.properties[0]?.config.options?.[0]?.name).toBe("Critical");
    expect(parsed.properties[0]?.icon).toBe("flag");
  });

  it("survives a malformed response by defaulting the list", () => {
    const parsed = ListPropertiesResponseSchema.parse({});
    expect(parsed.properties).toEqual([]);
    expect(parsed.total).toBe(0);
  });

  it("keeps unknown property types as strings (forward compat)", () => {
    const parsed = ListPropertiesResponseSchema.parse({
      properties: [{ ...baseProperty, type: "relation", config: {} }],
      total: 1,
    });
    expect(parsed.properties[0]?.type).toBe("relation");
  });

  it("defaults config when the server sends none", () => {
    const { config: _omit, ...withoutConfig } = baseProperty;
    const parsed = ListPropertiesResponseSchema.parse({ properties: [withoutConfig], total: 1 });
    expect(parsed.properties[0]?.config).toEqual({});
  });

  it("defaults icon for an older server response", () => {
    const { icon: _omit, ...withoutIcon } = baseProperty;
    const parsed = ListPropertiesResponseSchema.parse({ properties: [withoutIcon], total: 1 });
    expect(parsed.properties[0]?.icon).toBe("");
  });
});

// POST /api/issues/preview-trigger feeds this schema through parseWithFallback
// in client.previewIssueTrigger with fallback { triggers: [], total_count: 0 }
// (MUL-3375). The four entry points read it to decide "will this start a run",
// so malformed / missing / null drift must degrade to "nothing will start"
// rather than throw into the picker/modal.
const PREVIEW_FALLBACK = { triggers: [], total_count: 0 };
const PREVIEW_ENDPOINT = { endpoint: "POST /api/issues/preview-trigger" };

describe("IssueTriggerPreviewSchema", () => {
  it("parses a well-formed response", () => {
    const parsed = IssueTriggerPreviewSchema.parse({
      triggers: [
        { issue_id: "i1", agent_id: "a1", source: "assign", handoff_supported: true },
        { issue_id: "i2", agent_id: "a2", source: "status", handoff_supported: false },
      ],
      total_count: 2,
    });
    expect(parsed.total_count).toBe(2);
    expect(parsed.triggers).toHaveLength(2);
    expect(parsed.triggers[0]).toMatchObject({ issue_id: "i1", agent_id: "a1", source: "assign", handoff_supported: true });
  });

  it("defaults missing top-level fields (empty / older backend)", () => {
    const parsed = IssueTriggerPreviewSchema.parse({});
    expect(parsed.triggers).toEqual([]);
    expect(parsed.total_count).toBe(0);
  });

  it("defaults missing optional item fields, keeping required issue_id", () => {
    const parsed = IssueTriggerPreviewSchema.parse({ triggers: [{ issue_id: "i1" }], total_count: 1 });
    expect(parsed.triggers[0]).toEqual({
      issue_id: "i1",
      agent_id: "",
      source: "",
      handoff_supported: false,
    });
  });

  it("parseWithFallback returns the fallback for a malformed shape (triggers not an array)", () => {
    const parsed = parseWithFallback(
      { triggers: "nope", total_count: 1 },
      IssueTriggerPreviewSchema,
      PREVIEW_FALLBACK,
      PREVIEW_ENDPOINT,
    );
    expect(parsed).toEqual(PREVIEW_FALLBACK);
  });

  it("parseWithFallback returns the fallback when an item drops the required issue_id", () => {
    const parsed = parseWithFallback(
      { triggers: [{ agent_id: "a1", source: "assign" }], total_count: 1 },
      IssueTriggerPreviewSchema,
      PREVIEW_FALLBACK,
      PREVIEW_ENDPOINT,
    );
    expect(parsed).toEqual(PREVIEW_FALLBACK);
  });

  it("parseWithFallback returns the fallback for a wrong-typed total_count", () => {
    const parsed = parseWithFallback(
      { triggers: [], total_count: "5" },
      IssueTriggerPreviewSchema,
      PREVIEW_FALLBACK,
      PREVIEW_ENDPOINT,
    );
    expect(parsed).toEqual(PREVIEW_FALLBACK);
  });

  it("parseWithFallback returns the fallback for null / non-object bodies", () => {
    expect(parseWithFallback(null, IssueTriggerPreviewSchema, PREVIEW_FALLBACK, PREVIEW_ENDPOINT)).toEqual(PREVIEW_FALLBACK);
    expect(parseWithFallback("oops", IssueTriggerPreviewSchema, PREVIEW_FALLBACK, PREVIEW_ENDPOINT)).toEqual(PREVIEW_FALLBACK);
  });
});

describe("TimelineEntriesSchema", () => {
  it("preserves source_task_id for agent failure comments", () => {
    const parsed = TimelineEntriesSchema.parse([
      {
        type: "comment",
        id: "comment-1",
        actor_type: "agent",
        actor_id: "agent-1",
        created_at: "2026-01-01T00:00:00Z",
        content: "API Error: 500 Internal server error",
        comment_type: "system",
        source_task_id: "task-1",
      },
    ]);

    expect(parsed[0]?.source_task_id).toBe("task-1");
  });
});

describe("AgentTaskListSchema", () => {
  const task = {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status: "queued",
    priority: 0,
    dispatched_at: null,
    started_at: null,
    completed_at: null,
    result: null,
    error: null,
    created_at: "2026-07-10T00:00:00Z",
    trigger_comment_id: "comment-3",
  };

  it("preserves planned and delivered comment IDs for a task run", () => {
    const parsed = AgentTaskListSchema.parse([
      {
        ...task,
        coalesced_comment_ids: ["comment-1", "comment-2"],
        delivered_comment_ids: ["comment-1", "comment-2", "comment-3"],
      },
    ]);

    expect(parsed[0]?.trigger_comment_id).toBe("comment-3");
    expect(parsed[0]?.coalesced_comment_ids).toEqual([
      "comment-1",
      "comment-2",
    ]);
    expect(parsed[0]?.delivered_comment_ids).toEqual([
      "comment-1",
      "comment-2",
      "comment-3",
    ]);
  });

  it("accepts task payloads from older backends without comment coverage", () => {
    const parsed = AgentTaskListSchema.parse([task]);
    expect(parsed[0]?.coalesced_comment_ids).toBeUndefined();
    expect(parsed[0]?.delivered_comment_ids).toBeUndefined();
  });

  it("degrades malformed optional coverage without dropping task rows", () => {
    const parsed = AgentTaskListSchema.parse([
      {
        ...task,
        coalesced_comment_ids: ["comment-1", 2],
        delivered_comment_ids: "not-an-array",
      },
      {
        ...task,
        id: "task-2",
        delivered_comment_ids: ["comment-2", "comment-3"],
      },
    ]);

    expect(parsed).toHaveLength(2);
    expect(parsed[0]?.coalesced_comment_ids).toBeUndefined();
    expect(parsed[0]?.delivered_comment_ids).toBeUndefined();
    expect(parsed[1]?.delivered_comment_ids).toEqual([
      "comment-2",
      "comment-3",
    ]);
  });
});

describe("ChatDraftRestoresResponseSchema", () => {
  it("parses a well-formed response with attachments", () => {
    const parsed = parseWithFallback(
      {
        restores: [
          {
            id: "msg-1",
            chat_session_id: "s-1",
            task_id: "t-1",
            content: "run the thing",
            attachments: [{ id: "att-1", filename: "notes.txt" }],
            created_at: "2026-07-01T00:00:00Z",
          },
        ],
      },
      ChatDraftRestoresResponseSchema,
      EMPTY_CHAT_DRAFT_RESTORES,
      { endpoint: "test" },
    );
    expect(parsed.restores).toHaveLength(1);
    expect(parsed.restores[0]?.content).toBe("run the thing");
    expect(parsed.restores[0]?.attachments?.[0]?.id).toBe("att-1");
  });

  it("defaults a missing restores array instead of crashing the composer", () => {
    const parsed = parseWithFallback(
      {},
      ChatDraftRestoresResponseSchema,
      EMPTY_CHAT_DRAFT_RESTORES,
      { endpoint: "test" },
    );
    expect(parsed.restores).toEqual([]);
  });

  it("falls back to the empty response on a malformed row", () => {
    // A row without the consume key (id) is unusable — the whole response
    // falls back and the durable rows simply stay pending server-side.
    const parsed = parseWithFallback(
      { restores: [{ chat_session_id: "s-1", content: 42 }] },
      ChatDraftRestoresResponseSchema,
      EMPTY_CHAT_DRAFT_RESTORES,
      { endpoint: "test" },
    );
    expect(parsed).toEqual(EMPTY_CHAT_DRAFT_RESTORES);
  });
});

describe("ChatPendingTaskSchema", () => {
  const ENDPOINT = { endpoint: "GET /api/chat/sessions/:id/pending-task" };

  it("keeps legacy responses compatible when queued_tasks is absent", () => {
    const parsed = parseWithFallback(
      {
        task_id: "task-active",
        status: "running",
        created_at: "2026-07-01T00:00:00Z",
      },
      ChatPendingTaskSchema,
      EMPTY_CHAT_PENDING_TASK,
      ENDPOINT,
    );

    expect(parsed).toMatchObject({
      task_id: "task-active",
      status: "running",
    });
    expect(parsed.queued_tasks).toBeUndefined();
  });

  it("parses queued task summaries", () => {
    const parsed = ChatPendingTaskSchema.parse({
      task_id: "task-active",
      status: "running",
      queued_tasks: [
        {
          task_id: "task-queued",
          status: "queued",
          content: "Follow up after the current task",
          created_at: "2026-07-01T00:01:00Z",
        },
      ],
    });

    expect(parsed.queued_tasks).toEqual([
      expect.objectContaining({
        task_id: "task-queued",
        content: "Follow up after the current task",
      }),
    ]);
  });

  it("keeps a valid head and ignores only malformed queued rows", () => {
    const parsed = parseWithFallback(
      {
        task_id: "task-active",
        queued_tasks: [{ task_id: 42, status: "queued" }],
      },
      ChatPendingTaskSchema,
      EMPTY_CHAT_PENDING_TASK,
      ENDPOINT,
    );

    expect(parsed).toEqual({
      task_id: "task-active",
      queued_tasks: [],
    });
  });
});

describe("SendChatMessageResponseSchema", () => {
  const base = {
    message_id: "message-1",
    task_id: "task-1",
    created_at: "2026-08-05T00:00:00Z",
  };

  it("parses the server-authoritative queue position", () => {
    expect(SendChatMessageResponseSchema.parse({ ...base, queued: false }).queued).toBe(false);
  });

  it("ignores a malformed additive queue position without losing the accepted send", () => {
    expect(SendChatMessageResponseSchema.parse({ ...base, queued: "no" }).queued).toBeUndefined();
  });
});

describe("PrioritizeQueuedChatTaskResponseSchema", () => {
  const ENDPOINT = {
    endpoint: "POST /api/chat/sessions/:id/queued-tasks/:taskId/prioritize",
  };

  it("parses the prioritized task id", () => {
    expect(
      PrioritizeQueuedChatTaskResponseSchema.parse({
        task_id: "task-queued",
        active_task_id: "task-active",
      }),
    ).toEqual({
      task_id: "task-queued",
      active_task_id: "task-active",
    });
  });

  it("falls back when task_id is malformed", () => {
    expect(
      parseWithFallback(
        { task_id: 42 },
        PrioritizeQueuedChatTaskResponseSchema,
        EMPTY_PRIORITIZE_QUEUED_CHAT_TASK_RESPONSE,
        ENDPOINT,
      ),
    ).toBe(EMPTY_PRIORITIZE_QUEUED_CHAT_TASK_RESPONSE);
  });
});

describe("CreateFeedbackResponseSchema", () => {
  const ENDPOINT = { endpoint: "POST /api/feedback" };

  it("parses a well-formed response and preserves extra fields", () => {
    const parsed = parseWithFallback(
      { id: "feedback-1", created_at: "2026-06-26T00:00:00Z", future_field: true },
      CreateFeedbackResponseSchema,
      EMPTY_CREATE_FEEDBACK_RESPONSE,
      ENDPOINT,
    );
    expect(parsed).toMatchObject({
      id: "feedback-1",
      created_at: "2026-06-26T00:00:00Z",
      future_field: true,
    });
  });

  it("returns the empty fallback for malformed feedback responses", () => {
    expect(
      parseWithFallback(
        { id: 123, created_at: "2026-06-26T00:00:00Z" },
        CreateFeedbackResponseSchema,
        EMPTY_CREATE_FEEDBACK_RESPONSE,
        ENDPOINT,
      ),
    ).toBe(EMPTY_CREATE_FEEDBACK_RESPONSE);
    expect(
      parseWithFallback(null, CreateFeedbackResponseSchema, EMPTY_CREATE_FEEDBACK_RESPONSE, ENDPOINT),
    ).toBe(EMPTY_CREATE_FEEDBACK_RESPONSE);
  });
});

describe("batch issue response schemas", () => {
  it("defaults missing counts to zero", () => {
    expect(BatchUpdateIssuesResponseSchema.parse({}).updated).toBe(0);
    expect(BatchUpdateIssuesResponseSchema.parse({}).skipped).toEqual([]);
    expect(BatchDeleteIssuesResponseSchema.parse({}).deleted).toBe(0);
  });

  it("preserves skipped issue details for partial batch updates", () => {
    const parsed = BatchUpdateIssuesResponseSchema.parse({
      updated: 1,
      skipped: [{
        issue_id: "issue-1",
        identifier: "MUL-1",
        title: "UI设计",
        reason: "UI design issue requires completed UI restore or raw design fallback handoff before completion",
      }],
    });

    expect(parsed.skipped).toEqual([{
      issue_id: "issue-1",
      identifier: "MUL-1",
      title: "UI设计",
      reason: "UI design issue requires completed UI restore or raw design fallback handoff before completion",
    }]);
  });

  it("falls back when counts drift to the wrong type", () => {
    const update = parseWithFallback(
      { updated: "1" },
      BatchUpdateIssuesResponseSchema,
      EMPTY_BATCH_UPDATE_ISSUES_RESPONSE,
      { endpoint: "POST /api/issues/batch-update" },
    );
    const deleted = parseWithFallback(
      { deleted: "1" },
      BatchDeleteIssuesResponseSchema,
      EMPTY_BATCH_DELETE_ISSUES_RESPONSE,
      { endpoint: "POST /api/issues/batch-delete" },
    );

    expect(update.updated).toBe(0);
    expect(update.skipped).toEqual([]);
    expect(deleted.deleted).toBe(0);
  });
});

describe("ListDesignDeliveriesResponseSchema", () => {
  const delivery = {
    id: "delivery-1",
    workspace_id: "ws-1",
    project_id: null,
    source_issue_id: "issue-ui",
    target_issue_id: "issue-fe",
    file_id: "file-1",
    revision_id: "revision-1",
    scope: {
      version: "1.0",
      items: [{ frameId: "frame-1", frameName: "Main" }],
    },
    status: "active",
    delivered_by: null,
    delivered_at: "2026-06-30T00:00:00Z",
    cancelled_by: null,
    cancelled_at: null,
    cancel_reason: null,
    audit_metadata: {},
    created_at: "2026-06-30T00:00:00Z",
    updated_at: "2026-06-30T00:00:00Z",
  };

  it("defaults deliveries to [] when an older backend omits the field", () => {
    const parsed = ListDesignDeliveriesResponseSchema.parse({});
    expect(parsed.deliveries).toEqual([]);
  });

  it("preserves unknown delivery fields for forward compatibility", () => {
    const parsed = ListDesignDeliveriesResponseSchema.parse({
      deliveries: [{ ...delivery, handoff_summary: "Ready for frontend" }],
    });
    expect(parsed.deliveries[0]?.handoff_summary).toBe("Ready for frontend");
  });

  it("defaults nullable fields that older backends may omit", () => {
    const {
      project_id: _projectId,
      delivered_by: _deliveredBy,
      cancelled_by: _cancelledBy,
      cancelled_at: _cancelledAt,
      cancel_reason: _cancelReason,
      audit_metadata: _auditMetadata,
      ...legacyDelivery
    } = delivery;
    const parsed = ListDesignDeliveriesResponseSchema.parse({ deliveries: [legacyDelivery] });
    expect(parsed.deliveries[0]?.project_id).toBe(null);
    expect(parsed.deliveries[0]?.delivered_by).toBe(null);
    expect(parsed.deliveries[0]?.cancelled_by).toBe(null);
    expect(parsed.deliveries[0]?.cancelled_at).toBe(null);
    expect(parsed.deliveries[0]?.cancel_reason).toBe(null);
    expect(parsed.deliveries[0]?.audit_metadata).toEqual({});
  });

  it("accepts cancellation audit fields", () => {
    const parsed = ListDesignDeliveriesResponseSchema.parse({
      deliveries: [{
        ...delivery,
        status: "cancelled",
        cancelled_by: "user-1",
        cancelled_at: "2026-06-30T01:00:00Z",
        cancel_reason: "设计稿需要重新确认",
        audit_metadata: { cancel_reason: "设计稿需要重新确认" },
      }],
    });
    expect(parsed.deliveries[0]?.cancelled_by).toBe("user-1");
    expect(parsed.deliveries[0]?.cancel_reason).toBe("设计稿需要重新确认");
    expect(parsed.deliveries[0]?.audit_metadata.cancel_reason).toBe("设计稿需要重新确认");
  });
});

describe("DesignRestoreTaskSchema", () => {
  const task = {
    id: "task-1",
    workspace_id: "ws-1",
    file_id: "file-1",
    revision_id: "revision-1",
    issue_id: "issue-fe",
    agent_task_id: null,
    status: "queued",
    input: { version: "1.0" },
    result: {},
    error: null,
    created_by: "user-1",
    created_at: "2026-06-30T00:00:00Z",
    updated_at: "2026-06-30T00:00:00Z",
  };

  it("defaults delivery_id to null for older restore task responses", () => {
    const parsed = DesignRestoreTaskSchema.parse(task);
    expect(parsed.delivery_id).toBe(null);
  });

  it("preserves delivery_id on task list responses", () => {
    const parsed = ListDesignRestoreTasksResponseSchema.parse({
      tasks: [{ ...task, delivery_id: "delivery-1" }],
    });
    expect(parsed.tasks[0]?.delivery_id).toBe("delivery-1");
  });

  it("defaults execution_status to null for older restore task responses", () => {
    const parsed = DesignRestoreTaskSchema.parse(task);
    expect(parsed.execution_status).toBe(null);
  });

  it("preserves execution_status diagnostics on task responses", () => {
    const parsed = DesignRestoreTaskSchema.parse({
      ...task,
      execution_status: {
        agent_task_id: "agent-task-1",
        agent_task_status: "queued",
        agent_task_created_at: "2026-07-03T01:00:00Z",
        agent_task_dispatched_at: null,
        agent_task_started_at: null,
        agent_task_completed_at: null,
        agent_task_error: null,
        agent_task_wait_reason: null,
        runtime_id: "runtime-1",
        runtime_status: "offline",
        runtime_last_seen_at: "2026-07-03T00:50:00Z",
        last_message_seq: null,
        last_message_at: null,
        phase: "waiting_runtime",
        reason: "runtime_offline",
        severity: "warning",
      },
    });

    expect(parsed.execution_status?.phase).toBe("waiting_runtime");
    expect(parsed.execution_status?.reason).toBe("runtime_offline");
    expect(parsed.execution_status?.runtime_status).toBe("offline");
  });
});

// The duplicate-issue branch in create-issue.tsx feeds ApiError.body
// (typed as `unknown`) through this schema. Any future server drift that
// loses the contract MUST fail the parse so the UI falls back to a normal
// error toast instead of rendering an empty / partial duplicate card.
describe("DuplicateIssueErrorBodySchema", () => {
  const valid = {
    code: "active_duplicate_issue",
    error: "An active issue with this title already exists: MUL-12 – Login bug",
    issue: {
      id: "11111111-1111-1111-1111-111111111111",
      identifier: "MUL-12",
      title: "Login bug",
    },
  };

  it("accepts a well-formed body", () => {
    expect(DuplicateIssueErrorBodySchema.safeParse(valid).success).toBe(true);
  });

  it("accepts unknown extra fields via .loose()", () => {
    const forwardCompat = {
      ...valid,
      hint: "Try a different title",
      issue: { ...valid.issue, workspace_id: "ws-1", status: "todo" },
    };
    expect(DuplicateIssueErrorBodySchema.safeParse(forwardCompat).success).toBe(true);
  });

  it("rejects a renamed code (so renames degrade to the generic toast)", () => {
    const renamed = { ...valid, code: "duplicate_issue" };
    expect(DuplicateIssueErrorBodySchema.safeParse(renamed).success).toBe(false);
  });

  it("rejects a missing issue object", () => {
    const { issue: _omit, ...without } = valid;
    expect(DuplicateIssueErrorBodySchema.safeParse(without).success).toBe(false);
  });

  it("rejects a non-string issue.id", () => {
    const broken = { ...valid, issue: { ...valid.issue, id: 42 } };
    expect(DuplicateIssueErrorBodySchema.safeParse(broken).success).toBe(false);
  });

  it("accepts a missing error field (it is optional)", () => {
    const { error: _omit, ...without } = valid;
    expect(DuplicateIssueErrorBodySchema.safeParse(without).success).toBe(true);
  });
});

// `user.timezone` (Viewing tz) was added in the timezone-architecture RFC.
// A desktop build older than the server — or a server predating the
// `user.timezone` migration — will return a `/api/me` body with no
// `timezone` key. The schema must not fail closed on that: the field
// defaults to `null`, which the frontend resolves to the browser-detected
// tz at render time.
describe("UserSchema timezone drift", () => {
  const base = {
    id: "11111111-1111-1111-1111-111111111111",
    name: "Ada",
    email: "ada@example.com",
  };

  it("defaults timezone to null when the field is absent", () => {
    const parsed = UserSchema.parse(base);
    expect(parsed.timezone).toBe(null);
  });

  it("preserves an explicit IANA timezone", () => {
    const parsed = UserSchema.parse({ ...base, timezone: "Asia/Tokyo" });
    expect(parsed.timezone).toBe("Asia/Tokyo");
  });

  it("accepts an explicit null timezone", () => {
    const parsed = UserSchema.parse({ ...base, timezone: null });
    expect(parsed.timezone).toBe(null);
  });

  // Wrong-type drift: a future server bug sending `timezone` as a number
  // must not throw into the UI. parseWithFallback degrades the whole user
  // object to the explicit fallback (EMPTY_USER) so /api/me callers keep a
  // valid shape instead of white-screening.
  it("falls back to EMPTY_USER when timezone is the wrong type", () => {
    const parsed = parseWithFallback(
      { ...base, timezone: 42 },
      UserSchema,
      EMPTY_USER,
      { endpoint: "GET /api/me" },
    );
    expect(parsed).toBe(EMPTY_USER);
  });
});

describe("SquadListSchema member preview drift", () => {
  const baseSquad = {
    id: "squad-1",
    workspace_id: "ws-1",
    name: "Frontend Squad",
    description: "",
    instructions: "",
    avatar_url: null,
    leader_id: "agent-1",
    creator_id: "user-1",
    created_at: "2026-05-01T00:00:00Z",
    updated_at: "2026-05-01T00:00:00Z",
    archived_at: null,
    archived_by: null,
  };

  it("defaults preview fields when an older backend omits them", () => {
    const parsed = SquadListSchema.parse([baseSquad]);
    expect(parsed[0]?.member_count).toBe(0);
    expect(parsed[0]?.member_preview).toEqual([]);
  });

  it("defaults preview fields on a single squad response", () => {
    const parsed = SquadSchema.parse(baseSquad);
    expect(parsed.member_count).toBe(0);
    expect(parsed.member_preview).toEqual([]);
  });

  it("preserves lightweight member preview rows", () => {
    const parsed = SquadListSchema.parse([
      {
        ...baseSquad,
        member_count: 2,
        member_preview: [
          { member_type: "agent", member_id: "agent-1", role: "leader" },
          { member_type: "member", member_id: "user-2", role: "member" },
        ],
      },
    ]);
    expect(parsed[0]?.member_count).toBe(2);
    expect(parsed[0]?.member_preview).toHaveLength(2);
    expect(parsed[0]?.member_preview?.[0]?.role).toBe("leader");
  });
});

// The workspace dashboard and runtime-detail pages were re-pointed at the
// unified `task_usage_hourly` rollup. Every numeric field drives chart /
// KPI math, and string keys (date / agent_id / model) bucket the series.
// The contract these schemas must hold: a row missing a field degrades
// that field to a sane default rather than dropping the WHOLE array to
// the `[]` fallback — one drifted row must not blank the entire chart.
describe("dashboard + runtime usage schema drift", () => {
  it("coerces a missing numeric field to 0 instead of dropping the array", () => {
    const parsed = DashboardUsageDailyListSchema.parse([
      { date: "2026-05-19", model: "claude-opus-4-7", input_tokens: 100 },
    ]);
    expect(parsed).toHaveLength(1);
    expect(parsed[0]?.output_tokens).toBe(0);
    expect(parsed[0]?.cache_read_tokens).toBe(0);
    expect(parsed[0]?.cache_write_tokens).toBe(0);
  });

  it("coerces a missing date key to \"\" so the rest of the series survives", () => {
    const parsed = DashboardUsageDailyListSchema.parse([
      { model: "claude-opus-4-7", input_tokens: 5 },
    ]);
    expect(parsed).toHaveLength(1);
    expect(parsed[0]?.date).toBe("");
  });

  it("coerces a missing agent_id key to \"\" for the agent-runtime panel", () => {
    const parsed = DashboardAgentRunTimeListSchema.parse([
      { total_seconds: 42, task_count: 3, failed_count: 0 },
    ]);
    expect(parsed).toHaveLength(1);
    expect(parsed[0]?.agent_id).toBe("");
  });

  it("defaults a missing cancelled_count to 0 so a pre-cancelled-count server still renders", () => {
    // cancelled_count was added when the run-time rollups started counting
    // runs the user stopped mid-flight. A backend predating it omits the
    // field; the row must survive with a 0 segment rather than drop the
    // whole series (installed desktop clients hit older backends).
    expect(
      DashboardAgentRunTimeListSchema.parse([
        { agent_id: "a", total_seconds: 42, task_count: 3, failed_count: 0 },
      ])[0]?.cancelled_count,
    ).toBe(0);
    expect(
      DashboardRunTimeDailyListSchema.parse([
        { date: "2026-05-19", total_seconds: 42, task_count: 3, failed_count: 0 },
      ])[0]?.cancelled_count,
    ).toBe(0);
  });

  it("coerces a missing agent_id key to \"\" for the usage-by-agent panel", () => {
    const parsed = DashboardUsageByAgentListSchema.parse([
      { model: "claude-opus-4-7", input_tokens: 7 },
    ]);
    expect(parsed[0]?.agent_id).toBe("");
  });

  it("coerces missing fields on every runtime usage schema", () => {
    expect(RuntimeUsageListSchema.parse([{ date: "2026-05-19" }])[0]?.input_tokens).toBe(0);
    expect(RuntimeHourlyActivityListSchema.parse([{ hour: 9 }])[0]?.count).toBe(0);
    expect(RuntimeUsageByAgentListSchema.parse([{ model: "x" }])[0]?.agent_id).toBe("");
    expect(RuntimeUsageByHourListSchema.parse([{ hour: 9 }])[0]?.model).toBe("");
  });

  it("defaults a missing provider to \"\" so an older server's rows still price by bare model", () => {
    // provider was added for cross-provider model disambiguation; a server
    // predating it omits the field. The schema must fill "" (→ bare-model
    // pricing lookup) rather than drop the row.
    expect(
      DashboardUsageDailyListSchema.parse([{ date: "2026-05-19", model: "claude-opus-4-7" }])[0]
        ?.provider,
    ).toBe("");
    expect(
      DashboardUsageByAgentListSchema.parse([{ model: "claude-opus-4-7" }])[0]?.provider,
    ).toBe("");
    expect(RuntimeUsageByAgentListSchema.parse([{ model: "x" }])[0]?.provider).toBe("");
  });

  it("rejects a non-array body so parseWithFallback can return its fallback", () => {
    expect(DashboardUsageDailyListSchema.safeParse(null).success).toBe(false);
    expect(DashboardFailureDailyListSchema.safeParse(null).success).toBe(false);
    expect(DashboardFailureByAgentListSchema.safeParse({ rows: [] }).success).toBe(
      false,
    );
    expect(RuntimeUsageListSchema.safeParse({ rows: [] }).success).toBe(false);
  });

  it("keeps a failure_reason the client build has never heard of", () => {
    // failure_reason is an open string, not an enum: the backend taxonomy
    // grows, and an installed desktop client must still count a reason its
    // build predates rather than dropping the row (and with it the day's
    // error total).
    const parsed = DashboardFailureDailyListSchema.parse([
      { date: "2026-05-19", failure_reason: "agent_error.brand_new", task_count: 3 },
    ]);
    expect(parsed).toHaveLength(1);
    expect(parsed[0]?.failure_reason).toBe("agent_error.brand_new");
    expect(parsed[0]?.task_count).toBe(3);
  });

  it("coerces a missing failure row field without dropping the array", () => {
    const daily = DashboardFailureDailyListSchema.parse([{ date: "2026-05-19" }]);
    expect(daily).toHaveLength(1);
    // "" is the succeeded bucket, so a reason-less row lands in the
    // denominator instead of inventing a failure that never happened.
    //
    // Defaulting to a failure bucket instead was considered and rejected: the
    // realistic drift here is someone adding `omitempty` to the Go struct
    // tag, which would strip the field from exactly the SUCCESS rows and turn
    // every window into a 100% error rate. Deflating a rate under drift is
    // the milder failure. TestDashboardFailureWireContractKeepsEmptyReason
    // (server/internal/handler/dashboard_test.go) guards the other side by
    // pinning that the server always emits the field.
    expect(daily[0]?.failure_reason).toBe("");
    expect(daily[0]?.task_count).toBe(0);

    const byAgent = DashboardFailureByAgentListSchema.parse([
      { failure_reason: "timeout", task_count: 2 },
    ]);
    expect(byAgent[0]?.agent_id).toBe("");
  });

  it("keeps unknown server-side fields via .loose()", () => {
    const parsed = RuntimeUsageListSchema.parse([
      { date: "2026-05-19", region: "us-east" },
    ]);
    expect((parsed[0] as Record<string, unknown>).region).toBe("us-east");
  });
});

describe("AppConfigSchema cdn_signed drift", () => {
  it("defaults cdn_signed to false when the server omits it (pre-MUL-3254 servers)", () => {
    const parsed = AppConfigSchema.parse({ cdn_domain: "cdn.example.com" });
    expect(parsed.cdn_signed).toBe(false);
  });

  it("coerces a malformed cdn_signed to false instead of failing the whole config", () => {
    const parsed = AppConfigSchema.parse({
      cdn_domain: "cdn.example.com",
      cdn_signed: "yes",
    });
    expect(parsed.cdn_signed).toBe(false);
    expect(parsed.cdn_domain).toBe("cdn.example.com");
  });

  it("keeps cdn_signed=true from a signing-enabled server", () => {
    const parsed = AppConfigSchema.parse({ cdn_signed: true });
    expect(parsed.cdn_signed).toBe(true);
  });

  it("parses frontend feature flag decisions", () => {
    const parsed = AppConfigSchema.parse({
      feature_flags: {
        composio_mcp_apps: true,
        malformed_future_flag: "yes",
      },
    });
    expect(parsed.feature_flags).toEqual({
      composio_mcp_apps: true,
      malformed_future_flag: false,
    });
  });

  it("defaults malformed feature_flags to an empty object", () => {
    const parsed = AppConfigSchema.parse({ feature_flags: ["not", "an", "object"] });
    expect(parsed.feature_flags).toEqual({});
  });

  it("parses server_version and leaves it undefined when the server omits it", () => {
    expect(AppConfigSchema.parse({ server_version: "1.2.3" }).server_version).toBe("1.2.3");
    expect(AppConfigSchema.parse({}).server_version).toBeUndefined();
  });
});

describe("InboxUnreadSummarySchema", () => {
  const ENDPOINT = { endpoint: "GET /api/inbox/unread-summary" };

  it("parses a well-formed summary and tolerates extra fields", () => {
    const parsed = parseWithFallback(
      [
        { workspace_id: "ws-1", count: 2 },
        { workspace_id: "ws-2", count: 0, future_field: "ignored" },
      ],
      InboxUnreadSummarySchema,
      EMPTY_INBOX_UNREAD_SUMMARY,
      ENDPOINT,
    );
    expect(parsed).toEqual([
      { workspace_id: "ws-1", count: 2 },
      { workspace_id: "ws-2", count: 0, future_field: "ignored" },
    ]);
  });

  it("returns the empty fallback (dot hidden) for a non-array body", () => {
    expect(
      parseWithFallback({ rows: [] }, InboxUnreadSummarySchema, EMPTY_INBOX_UNREAD_SUMMARY, ENDPOINT),
    ).toBe(EMPTY_INBOX_UNREAD_SUMMARY);
    expect(
      parseWithFallback(null, InboxUnreadSummarySchema, EMPTY_INBOX_UNREAD_SUMMARY, ENDPOINT),
    ).toBe(EMPTY_INBOX_UNREAD_SUMMARY);
  });

  it("returns the empty fallback when an entry has a wrong-typed count", () => {
    expect(
      parseWithFallback(
        [{ workspace_id: "ws-1", count: "lots" }],
        InboxUnreadSummarySchema,
        EMPTY_INBOX_UNREAD_SUMMARY,
        ENDPOINT,
      ),
    ).toBe(EMPTY_INBOX_UNREAD_SUMMARY);
  });
});

describe("InboxItemListSchema", () => {
  const ENDPOINT = { endpoint: "GET /api/inbox/archived" };

  const row = (overrides: Record<string, unknown> = {}) => ({
    id: "inbox-1",
    workspace_id: "ws-1",
    recipient_type: "member",
    recipient_id: "member-1",
    type: "new_comment",
    severity: "info",
    issue_id: "issue-1",
    title: "Issue title",
    body: null,
    read: false,
    archived: true,
    created_at: "2026-06-15T08:00:00Z",
    ...overrides,
  });

  it("parses a well-formed archived list and tolerates extra fields", () => {
    const parsed = parseWithFallback(
      [row({ issue_status: "in_progress", details: { comment_id: "c-1" }, future_field: 1 })],
      InboxItemListSchema,
      EMPTY_INBOX_ITEMS,
      ENDPOINT,
    );
    expect(parsed).toHaveLength(1);
    expect(parsed[0]).toMatchObject({ id: "inbox-1", archived: true });
  });

  it("keeps a notification type this client doesn't know yet", () => {
    // Enums stay lenient on purpose: a backend that ships a new inbox type
    // must not blank the whole archived list on older clients.
    const parsed = parseWithFallback(
      [row({ type: "some_future_type", severity: "future_severity" })],
      InboxItemListSchema,
      EMPTY_INBOX_ITEMS,
      ENDPOINT,
    );
    expect(parsed).toHaveLength(1);
  });

  it("accepts rows that omit the nullable optional fields", () => {
    const { body, issue_id, ...withoutOptionals } = row();
    void body;
    void issue_id;
    expect(
      parseWithFallback([withoutOptionals], InboxItemListSchema, EMPTY_INBOX_ITEMS, ENDPOINT),
    ).toHaveLength(1);
  });

  it("returns the empty fallback for a non-array body", () => {
    expect(
      parseWithFallback({ items: [] }, InboxItemListSchema, EMPTY_INBOX_ITEMS, ENDPOINT),
    ).toBe(EMPTY_INBOX_ITEMS);
    expect(
      parseWithFallback(null, InboxItemListSchema, EMPTY_INBOX_ITEMS, ENDPOINT),
    ).toBe(EMPTY_INBOX_ITEMS);
  });

  it("returns the empty fallback when a row is missing a required field", () => {
    const { id, ...withoutId } = row();
    void id;
    expect(
      parseWithFallback([withoutId], InboxItemListSchema, EMPTY_INBOX_ITEMS, ENDPOINT),
    ).toBe(EMPTY_INBOX_ITEMS);
  });

  it("returns the empty fallback when `archived` is wrong-typed", () => {
    expect(
      parseWithFallback(
        [row({ archived: "yes" })],
        InboxItemListSchema,
        EMPTY_INBOX_ITEMS,
        ENDPOINT,
      ),
    ).toBe(EMPTY_INBOX_ITEMS);
  });
});

describe("SearchProjectsResponseSchema date drift", () => {
  const ENDPOINT = { endpoint: "GET /api/projects/search" };

  const baseProject = {
    id: "p-1",
    workspace_id: "ws-1",
    title: "Launch",
    description: null,
    icon: null,
    status: "in_progress",
    priority: "high",
    lead_type: null,
    lead_id: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    issue_count: 0,
    done_count: 0,
    resource_count: 0,
    match_source: "title",
  };

  it("parses start_date / due_date when the backend returns them", () => {
    const parsed = parseWithFallback(
      { projects: [{ ...baseProject, start_date: "2026-03-01", due_date: "2026-03-31" }], total: 1 },
      SearchProjectsResponseSchema,
      EMPTY_SEARCH_PROJECTS_RESPONSE,
      ENDPOINT,
    );
    expect(parsed.projects[0]?.start_date).toBe("2026-03-01");
    expect(parsed.projects[0]?.due_date).toBe("2026-03-31");
  });

  // Frontend deploys before backend: an older backend omits the new keys. The
  // .default(null) must keep the whole batch parseable (→ null), not degrade
  // it to the empty fallback and blank the search results.
  it("defaults missing start_date / due_date to null without dropping results", () => {
    const parsed = parseWithFallback(
      { projects: [baseProject], total: 1 },
      SearchProjectsResponseSchema,
      EMPTY_SEARCH_PROJECTS_RESPONSE,
      ENDPOINT,
    );
    expect(parsed).not.toBe(EMPTY_SEARCH_PROJECTS_RESPONSE);
    expect(parsed.projects).toHaveLength(1);
    expect(parsed.projects[0]?.start_date).toBeNull();
    expect(parsed.projects[0]?.due_date).toBeNull();
  });
});

describe("ProjectSchema archive drift", () => {
  const project = {
    id: "p-1",
    workspace_id: "ws-1",
    title: "Launch",
    description: null,
    icon: null,
    status: "in_progress",
    priority: "high",
    lead_type: null,
    lead_id: null,
    start_date: null,
    due_date: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };

  it("defaults archive fields omitted by an older backend to null", () => {
    expect(parseProject(project)).toMatchObject({ archived_at: null, archived_by: null });
  });

  it("falls back when archive metadata is malformed", () => {
    expect(parseProject({ ...project, archived_at: true })).toMatchObject({
      id: "",
      archived_at: null,
      archived_by: null,
    });
  });
});

// The "run now" flow branches on run.status/reason_code to avoid a false-success
// toast (MUL-4525), so the trigger response must survive backend drift.
describe("AutopilotRunSchema", () => {
  const ENDPOINT = { endpoint: "POST /api/autopilots/:id/trigger" };
  const baseRun = {
    id: "run-1",
    autopilot_id: "ap-1",
    trigger_id: null,
    source: "manual",
    status: "issue_created",
    issue_id: "issue-1",
    task_id: null,
    triggered_at: "2026-07-14T00:00:00Z",
    completed_at: null,
    failure_reason: null,
    trigger_payload: null,
    result: null,
    created_at: "2026-07-14T00:00:00Z",
  };

  it("preserves a blocked run's status and reason_code", () => {
    const parsed = parseWithFallback(
      { ...baseRun, status: "skipped", failure_reason: "you are not allowed to trigger this autopilot's assignee agent", reason_code: "invocation_not_allowed" },
      AutopilotRunSchema,
      FALLBACK_AUTOPILOT_RUN,
      ENDPOINT,
    );
    expect(parsed.status).toBe("skipped");
    expect(parsed.reason_code).toBe("invocation_not_allowed");
  });

  it("tolerates an older server omitting reason_code", () => {
    const parsed = parseWithFallback(baseRun, AutopilotRunSchema, FALLBACK_AUTOPILOT_RUN, ENDPOINT);
    expect(parsed.status).toBe("issue_created");
    expect(parsed.reason_code).toBeUndefined();
  });

  it("degrades a malformed response to a non-success fallback (never a false success)", () => {
    const parsed = parseWithFallback("not-an-object", AutopilotRunSchema, FALLBACK_AUTOPILOT_RUN, ENDPOINT);
    expect(parsed).toBe(FALLBACK_AUTOPILOT_RUN);
    expect(parsed.status).toBe("failed");
  });
});

// The comment composer branches on preview.blocked to warn before sending
// (MUL-4525 §2), so the additive field must parse and degrade gracefully.
describe("CommentTriggerPreviewSchema.blocked", () => {
  it("parses blocked mention outcomes alongside agents", () => {
    const parsed = CommentTriggerPreviewSchema.parse({
      agents: [{ id: "a1", source: "mention_agent", reason: "" }],
      blocked: [
        { target_type: "squad", target_id: "s1", status: "blocked", reason_code: "invocation_not_allowed" },
      ],
    });
    expect(parsed.agents).toHaveLength(1);
    expect(parsed.blocked).toEqual([
      { target_type: "squad", target_id: "s1", status: "blocked", reason_code: "invocation_not_allowed" },
    ]);
  });

  it("defaults blocked to [] when an older server omits it", () => {
    const parsed = CommentTriggerPreviewSchema.parse({ agents: [] });
    expect(parsed.blocked).toEqual([]);
  });

  it("degrades a malformed blocked field to [] without dropping agents", () => {
    const parsed = CommentTriggerPreviewSchema.parse({
      agents: [{ id: "a1", source: "mention_agent", reason: "" }],
      blocked: "nope",
    });
    expect(parsed.agents).toHaveLength(1);
    expect(parsed.blocked).toEqual([]);
  });

  it("drops a single malformed blocked entry without discarding the valid ones", () => {
    const parsed = CommentTriggerPreviewSchema.parse({
      agents: [],
      blocked: [
        { target_type: "squad", target_id: "s1", status: "blocked", reason_code: "invocation_not_allowed" },
        { status: "blocked" }, // missing target_id → dropped individually
        { target_type: "agent", target_id: "a1", status: "blocked", reason_code: "runtime_offline" },
      ],
    });
    expect(parsed.blocked.map((b) => b.target_id)).toEqual(["s1", "a1"]);
  });
});

describe("RuntimeModelListRequestSchema", () => {
  const completed = {
    id: "req-1",
    runtime_id: "rt-1",
    status: "completed",
    supported: true,
    created_at: "2026-07-29T00:00:00Z",
    updated_at: "2026-07-29T00:00:01Z",
    models: [
      {
        id: "gpt-5.6-sol",
        label: "GPT-5.6-Sol",
        provider: "openai",
        default: true,
        thinking: {
          supported_levels: [{ value: "high", label: "High" }],
          default_level: "low",
        },
        service_tiers: [{ id: "fast", name: "Fast" }],
      },
    ],
  };

  it("parses a live completed discovery, keeping the fields the UI branches on", () => {
    const parsed = parseWithFallback(
      completed,
      RuntimeModelListRequestSchema,
      MALFORMED_RUNTIME_MODEL_LIST_REQUEST,
      { endpoint: "test" },
    );
    expect(parsed.status).toBe("completed");
    expect(parsed.supported).toBe(true);
    expect(parsed.models?.[0]?.default).toBe(true);
    expect(parsed.models?.[0]?.thinking?.supported_levels).toEqual([
      { value: "high", label: "High" },
    ]);
    expect(parsed.models?.[0]?.service_tiers).toEqual([{ id: "fast", name: "Fast" }]);
    expect(parsed.cached).toBeUndefined();
  });

  it("keeps the additive cache markers when the server serves a snapshot", () => {
    const parsed = parseWithFallback(
      { ...completed, cached: true, cached_at: "2026-07-29T00:00:00Z" },
      RuntimeModelListRequestSchema,
      MALFORMED_RUNTIME_MODEL_LIST_REQUEST,
      { endpoint: "test" },
    );
    expect(parsed.cached).toBe(true);
    expect(parsed.cached_at).toBe("2026-07-29T00:00:00Z");
  });

  // A backend that predates MUL-5444 sends neither marker; an even older one
  // may omit `supported`. Both must stay usable rather than reading as
  // "runtime manages the model itself" off an undefined.
  it("defaults supported to true on an older backend that omits it", () => {
    const { supported: _omitted, ...withoutSupported } = completed;
    const parsed = parseWithFallback(
      withoutSupported,
      RuntimeModelListRequestSchema,
      MALFORMED_RUNTIME_MODEL_LIST_REQUEST,
      { endpoint: "test" },
    );
    expect(parsed.supported).toBe(true);
    expect(parsed.cached).toBeUndefined();
  });

  it("passes an unknown status through instead of failing the whole response", () => {
    const parsed = parseWithFallback(
      { ...completed, status: "superseded" },
      RuntimeModelListRequestSchema,
      MALFORMED_RUNTIME_MODEL_LIST_REQUEST,
      { endpoint: "test" },
    );
    expect(parsed.status).toBe("superseded");
  });

  // Malformed bodies must land on the "failed" fallback: `completed` would
  // fabricate an empty catalog and `pending` would spin the picker until the
  // client-side poll timeout.
  it("falls back to an explicit failure on a malformed body", () => {
    for (const malformed of [
      null,
      "nope",
      42,
      {},
      { status: 7 },
      { ...completed, status: undefined },
      { ...completed, supported: "yes" },
      { ...completed, models: "nope" },
      { ...completed, models: [{ label: "no id" }] },
    ]) {
      const parsed = parseWithFallback(
        malformed,
        RuntimeModelListRequestSchema,
        MALFORMED_RUNTIME_MODEL_LIST_REQUEST,
        { endpoint: "test" },
      );
      expect(parsed.status).toBe("failed");
      expect(parsed.supported).toBe(true);
      expect(parsed.error).toBe("invalid model discovery response");
    }
  });

  it("keeps unknown server fields instead of stripping them", () => {
    const parsed = parseWithFallback(
      { ...completed, future_field: "keep me" },
      RuntimeModelListRequestSchema,
      MALFORMED_RUNTIME_MODEL_LIST_REQUEST,
      { endpoint: "test" },
    );
    expect((parsed as unknown as { future_field?: string }).future_field).toBe(
      "keep me",
    );
  });
});

describe("PMO schemas", () => {
  const baseRun = {
    id: "run-1",
    workspace_id: "ws-1",
    config_id: "cfg-1",
    agent_task_id: "task-1",
    trigger: "scheduled",
    status: "preview_ready",
    source_snapshot: { schema_version: "1", snapshot_complete: true },
    diff: { entities: [] },
    summary: { created: 1 },
    error_code: null,
    error_message: null,
    requested_by: "user-1",
    created_at: "2026-08-07T00:00:00Z",
    started_at: "2026-08-07T00:01:00Z",
    completed_at: null,
    applied_at: null,
  };

  const baseConfig = {
    id: "cfg-1",
    workspace_id: "ws-1",
    name: "Example import",
    agent_id: "agent-1",
    root_external_key: "EXT-P-001",
    workload_property_id: null,
    schedule_enabled: false,
    next_run_at: null,
    last_run_at: null,
    last_applied_at: null,
    created_by: "user-1",
    created_at: "2026-08-07T00:00:00Z",
    updated_at: "2026-08-07T00:00:00Z",
  };

  it("parses a complete run and config", () => {
    expect(parsePMORun(baseRun)).toMatchObject({
      id: "run-1",
      trigger: "scheduled",
      status: "preview_ready",
      source_snapshot: { schema_version: "1" },
    });
    expect(parsePMOConfig(baseConfig)).toMatchObject({
      id: "cfg-1",
      root_external_key: "EXT-P-001",
      schedule_enabled: false,
      orchestration_squad_id: null,
      orchestration_issue_id: null,
    });
  });

  it("parses PMO orchestration links", () => {
    expect(
      parsePMOConfig({
        ...baseConfig,
        orchestration_squad_id: "squad-1",
        orchestration_issue_id: "issue-1",
      }),
    ).toMatchObject({
      orchestration_squad_id: "squad-1",
      orchestration_issue_id: "issue-1",
    });
  });

  it("keeps unknown future fields instead of stripping them", () => {
    const parsed = parsePMORun({ ...baseRun, future_field: "keep" });
    expect((parsed as unknown as { future_field?: string }).future_field).toBe("keep");
  });

  // A newer server may add a status; an installed client must not crash,
  // spin forever, or claim a preview is available. "failed" is the only
  // honest read-only degradation.
  it("degrades an unknown status to failed", () => {
    expect(parsePMORun({ ...baseRun, status: "future_status" }).status).toBe("failed");
  });

  it("degrades an unknown trigger to manual", () => {
    expect(parsePMORun({ ...baseRun, trigger: "future_trigger" }).trigger).toBe("manual");
  });

  it("falls back to EMPTY_ constants on malformed bodies", () => {
    expect(parsePMORun(null)).toEqual(EMPTY_PMO_RUN);
    expect(parsePMORun("nope")).toEqual(EMPTY_PMO_RUN);
    expect(parsePMOConfig({})).toMatchObject({ id: "", name: "" });
    expect(parsePMOSyncLink(null)).toEqual(EMPTY_PMO_SYNC_LINK);
  });

  it("applies safe defaults to optional newer fields", () => {
    const parsed = parsePMORun({ id: "run-1", workspace_id: "ws-1", config_id: "cfg-1" });
    expect(parsed.trigger).toBe("manual");
    expect(parsed.status).toBe("failed");
    expect(parsed.source_snapshot).toBeNull();
    expect(parsed.requested_by).toBeNull();
    expect(parsed.applied_at).toBeNull();
  });

  it("defaults a link's external_ids when absent", () => {
    const parsed = parsePMOSyncLink({
      id: "link-1",
      workspace_id: "ws-1",
      config_id: "cfg-1",
      external_type: "assignee",
      external_key: "EXT-U-001",
    });
    expect(parsed.external_ids).toEqual({ display_number: null, numeric_id: null, task_id: null });
  });

  it("defaults list envelopes when fields are missing or malformed", () => {
    expect(
      parseWithFallback({}, ListPMOConfigsResponseSchema, EMPTY_LIST_PMO_CONFIGS_RESPONSE, {
        endpoint: "GET /api/pmo/configs",
      }),
    ).toEqual({ configs: [] });
    expect(
      parseWithFallback(
        { runs: "not-an-array" },
        ListPMORunsResponseSchema,
        EMPTY_LIST_PMO_RUNS_RESPONSE,
        { endpoint: "GET /api/pmo/runs" },
      ),
    ).toEqual(EMPTY_LIST_PMO_RUNS_RESPONSE);
  });
});

describe("TestCaseSchema", () => {
  it("fills defaults when the backend omits fields", () => {
    const parsed = parseWithFallback(
      { id: "c1", title: "下单成功" },
      TestCaseSchema,
      EMPTY_TEST_CASE,
      { endpoint: "test" },
    );
    expect(parsed.id).toBe("c1");
    expect(parsed.title).toBe("下单成功");
    expect(parsed.steps).toEqual([]);
    expect(parsed.repos).toEqual([]);
    expect(parsed.status).toBe("draft");
    expect(parsed.version).toBe(1);
    expect(parsed.generation_job_id).toBeNull();
  });

  it("falls back when the payload is not an object", () => {
    const parsed = parseWithFallback("nope", TestCaseSchema, EMPTY_TEST_CASE, {
      endpoint: "test",
    });
    expect(parsed).toBe(EMPTY_TEST_CASE);
  });

  it("keeps an unknown status rather than dropping the case", () => {
    const parsed = parseWithFallback(
      { id: "c1", status: "quarantined", case_type: "chaos" },
      TestCaseSchema,
      EMPTY_TEST_CASE,
      { endpoint: "test" },
    );
    expect(parsed.status).toBe("quarantined");
    expect(parsed.case_type).toBe("chaos");
  });

  it("drops a malformed step array to empty instead of failing the case", () => {
    const parsed = parseWithFallback(
      { id: "c1", steps: "not-an-array" },
      TestCaseSchema,
      EMPTY_TEST_CASE,
      { endpoint: "test" },
    );
    expect(parsed).toBe(EMPTY_TEST_CASE);
  });

  it("parses repo bindings with their role and path globs", () => {
    const parsed = parseWithFallback(
      {
        id: "c1",
        repos: [
          { project_resource_id: "r1", alias: "admin-web", role: "driver" },
          { project_resource_id: "r2", alias: "mobile-app", path_globs: ["src/**"] },
        ],
      },
      TestCaseSchema,
      EMPTY_TEST_CASE,
      { endpoint: "test" },
    );
    expect(parsed.repos).toHaveLength(2);
    expect(parsed.repos[0]?.role).toBe("driver");
    expect(parsed.repos[1]?.role).toBe("under_test");
    expect(parsed.repos[1]?.path_globs).toEqual(["src/**"]);
  });

  it("keeps unknown server fields instead of stripping them", () => {
    const parsed = parseWithFallback(
      { id: "c1", future_field: "keep me" },
      TestCaseSchema,
      EMPTY_TEST_CASE,
      { endpoint: "test" },
    );
    expect((parsed as unknown as { future_field?: string }).future_field).toBe("keep me");
  });
});

describe("test case list schemas", () => {
  it("recovers a list response missing the array", () => {
    const parsed = parseWithFallback(
      {},
      ListTestCasesResponseSchema,
      EMPTY_LIST_TEST_CASES_RESPONSE,
      { endpoint: "test" },
    );
    expect(parsed.test_cases).toEqual([]);
    expect(parsed.total).toBe(0);
  });

  it("recovers a modules response missing the array", () => {
    const parsed = parseWithFallback(
      {},
      ListTestCaseModulesResponseSchema,
      EMPTY_LIST_TEST_CASE_MODULES_RESPONSE,
      { endpoint: "test" },
    );
    expect(parsed.modules).toEqual([]);
  });

  it("recovers a revisions response missing the array", () => {
    const parsed = parseWithFallback(
      {},
      ListTestCaseRevisionsResponseSchema,
      EMPTY_LIST_TEST_CASE_REVISIONS_RESPONSE,
      { endpoint: "test" },
    );
    expect(parsed.revisions).toEqual([]);
  });

  it("defaults a module row that omits its count", () => {
    const parsed = parseWithFallback(
      { modules: [{ module: "订单" }] },
      ListTestCaseModulesResponseSchema,
      EMPTY_LIST_TEST_CASE_MODULES_RESPONSE,
      { endpoint: "test" },
    );
    expect(parsed.modules[0]).toEqual({ module: "订单", case_count: 0 });
  });
});

describe("TestGenerationJobSchema", () => {
  it("fills defaults when the backend omits optional fields", () => {
    const parsed = parseWithFallback(
      { id: "job-1", workspace_id: "ws-1", project_id: "p-1", status: "queued", created_at: "2024-01-01T00:00:00Z", updated_at: "2024-01-01T00:00:00Z" },
      TestGenerationJobSchema,
      EMPTY_TEST_GENERATION_JOB,
      { endpoint: "test" },
    );
    expect(parsed.id).toBe("job-1");
    expect(parsed.agent_id).toBeNull();
    expect(parsed.agent_task_id).toBeNull();
    expect(parsed.input).toEqual({});
    expect(parsed.result).toEqual({});
    expect(parsed.error).toBeNull();
  });

  it("falls back when the payload is not an object", () => {
    const parsed = parseWithFallback("nope", TestGenerationJobSchema, EMPTY_TEST_GENERATION_JOB, {
      endpoint: "test",
    });
    expect(parsed).toBe(EMPTY_TEST_GENERATION_JOB);
  });

  it("keeps an unknown status rather than dropping the job", () => {
    const parsed = parseWithFallback(
      { id: "job-1", status: "paused", created_at: "", updated_at: "" },
      TestGenerationJobSchema,
      EMPTY_TEST_GENERATION_JOB,
      { endpoint: "test" },
    );
    expect(parsed.status).toBe("paused");
  });

  it("keeps unknown server fields instead of stripping them", () => {
    const parsed = parseWithFallback(
      { id: "job-1", future_field: "keep me", created_at: "", updated_at: "" },
      TestGenerationJobSchema,
      EMPTY_TEST_GENERATION_JOB,
      { endpoint: "test" },
    );
    expect((parsed as unknown as { future_field?: string }).future_field).toBe("keep me");
  });
});

describe("TestGenerationPlanSchema", () => {
  it("fills defaults for missing optional fields", () => {
    const parsed = parseWithFallback(
      { id: "plan-1", workspace_id: "ws-1", job_id: "job-1", status: "draft", created_at: "", updated_at: "" },
      TestGenerationPlanSchema,
      EMPTY_TEST_GENERATION_PLAN,
      { endpoint: "test" },
    );
    expect(parsed.id).toBe("plan-1");
    expect(parsed.plan).toEqual({});
    expect(parsed.review_notes).toBe("");
    expect(parsed.approved_by).toBeNull();
    expect(parsed.approved_at).toBeNull();
  });

  it("falls back when the payload is not an object", () => {
    const parsed = parseWithFallback(null, TestGenerationPlanSchema, EMPTY_TEST_GENERATION_PLAN, {
      endpoint: "test",
    });
    expect(parsed).toBe(EMPTY_TEST_GENERATION_PLAN);
  });

  it("parses plan JSON as a record", () => {
    const plan = { version: "1.0", repos: [], issues: [], modules: [] };
    const parsed = parseWithFallback(
      { id: "plan-1", plan, created_at: "", updated_at: "" },
      TestGenerationPlanSchema,
      EMPTY_TEST_GENERATION_PLAN,
      { endpoint: "test" },
    );
    expect(parsed.plan).toEqual(plan);
  });
});

describe("TestCaseProposalSchema", () => {
  it("fills defaults for missing optional fields", () => {
    const parsed = parseWithFallback(
      { id: "prop-1", workspace_id: "ws-1", job_id: "job-1", target_case_id: "case-1", kind: "update", status: "pending", created_at: "" },
      TestCaseProposalSchema,
      EMPTY_TEST_CASE_PROPOSAL,
      { endpoint: "test" },
    );
    expect(parsed.id).toBe("prop-1");
    expect(parsed.payload).toEqual({});
    expect(parsed.rationale).toBe("");
    expect(parsed.reviewed_by).toBeNull();
    expect(parsed.reviewed_at).toBeNull();
  });

  it("falls back when the payload is not an object", () => {
    const parsed = parseWithFallback(42, TestCaseProposalSchema, EMPTY_TEST_CASE_PROPOSAL, {
      endpoint: "test",
    });
    expect(parsed).toBe(EMPTY_TEST_CASE_PROPOSAL);
  });

  it("keeps an unknown kind rather than dropping the proposal", () => {
    const parsed = parseWithFallback(
      { id: "prop-1", kind: "future_kind", status: "pending", created_at: "" },
      TestCaseProposalSchema,
      EMPTY_TEST_CASE_PROPOSAL,
      { endpoint: "test" },
    );
    expect(parsed.kind).toBe("future_kind");
  });
});

describe("test generation list schemas", () => {
  it("recovers a jobs list response missing the array", () => {
    const parsed = parseWithFallback(
      {},
      ListTestGenerationJobsResponseSchema,
      EMPTY_LIST_TEST_GENERATION_JOBS_RESPONSE,
      { endpoint: "test" },
    );
    expect(parsed.jobs).toEqual([]);
    expect(parsed.total).toBe(0);
  });

  it("recovers a proposals list response missing the array", () => {
    const parsed = parseWithFallback(
      {},
      ListTestCaseProposalsResponseSchema,
      EMPTY_LIST_TEST_CASE_PROPOSALS_RESPONSE,
      { endpoint: "test" },
    );
    expect(parsed.proposals).toEqual([]);
    expect(parsed.total).toBe(0);
  });
});


// ---------------------------------------------------------------------------
// Test plan / run / capability schemas — Phase 3/4
// ---------------------------------------------------------------------------

describe("TestPlanSchema", () => {
  it("parses a full plan row correctly", () => {
    const plan = {
      id: "plan-1",
      workspace_id: "ws-1",
      project_id: "proj-1",
      title: "Smoke tests",
      description: "Daily smoke run",
      status: "active",
      created_by: "user-1",
      created_at: "2026-08-01T00:00:00Z",
      updated_at: "2026-08-01T00:00:00Z",
    };
    const parsed = TestPlanSchema.parse(plan);
    expect(parsed.id).toBe("plan-1");
    expect(parsed.status).toBe("active");
  });

  it("defaults status to draft when the field is absent", () => {
    const { status: _omit, ...withoutStatus } = {
      id: "plan-1",
      workspace_id: "ws-1",
      project_id: "proj-1",
      title: "My plan",
      description: "",
      status: "draft",
      created_by: null,
      created_at: "",
      updated_at: "",
    };
    const parsed = TestPlanSchema.parse(withoutStatus);
    expect(parsed.status).toBe("draft");
  });

  it("accepts an unknown future status (string passthrough)", () => {
    const plan = { ...EMPTY_TEST_PLAN, status: "future_status" };
    const parsed = TestPlanSchema.parse(plan);
    expect(parsed.status).toBe("future_status");
  });

  it("recovers from malformed plan response via parseWithFallback", () => {
    const parsed = parseWithFallback(
      null,
      TestPlanSchema,
      EMPTY_TEST_PLAN,
      { endpoint: "GET /api/test-plans/:id" },
    );
    expect(parsed.id).toBe("");
  });
});

describe("ListTestPlansResponseSchema", () => {
  it("defaults to empty array when test_plans is absent", () => {
    const parsed = parseWithFallback(
      {},
      ListTestPlansResponseSchema,
      EMPTY_LIST_TEST_PLANS_RESPONSE,
      { endpoint: "GET /api/test-plans" },
    );
    expect(parsed.test_plans).toEqual([]);
    expect(parsed.total).toBe(0);
  });
});

describe("TestRunSchema", () => {
  const BASE_RUN = {
    id: "run-1",
    workspace_id: "ws-1",
    project_id: "proj-1",
    plan_id: null,
    title: "Sprint 1 run",
    executor_type: "member",
    executor_id: "user-1",
    agent_task_id: null,
    environment: "staging",
    build_ref: "v1.2.3",
    capability_binding: {},
    status: "pending",
    source_run_id: null,
    retry_scope: null,
    error: null,
    started_at: null,
    completed_at: null,
    created_by: "user-1",
    created_at: "2026-08-01T00:00:00Z",
    updated_at: "2026-08-01T00:00:00Z",
  };

  it("parses a standard run row", () => {
    const parsed = TestRunSchema.parse(BASE_RUN);
    expect(parsed.id).toBe("run-1");
    expect(parsed.status).toBe("pending");
    expect(parsed.capability_binding).toEqual({});
  });

  it("accepts execution_status when present", () => {
    const parsed = TestRunSchema.parse({
      ...BASE_RUN,
      execution_status: { phase: "running", reason: null, severity: null },
    });
    expect(parsed.execution_status?.phase).toBe("running");
  });

  it("defaults status to pending when the field is absent", () => {
    const { status: _omit, ...withoutStatus } = BASE_RUN;
    const parsed = TestRunSchema.parse(withoutStatus);
    expect(parsed.status).toBe("pending");
  });

  it("recovers from malformed run response via parseWithFallback", () => {
    const parsed = parseWithFallback(
      null,
      TestRunSchema,
      EMPTY_TEST_RUN,
      { endpoint: "GET /api/test-runs/:id" },
    );
    expect(parsed.id).toBe("");
  });
});

describe("ListTestRunsResponseSchema", () => {
  it("defaults to empty array when test_runs is absent", () => {
    const parsed = parseWithFallback(
      {},
      ListTestRunsResponseSchema,
      EMPTY_LIST_TEST_RUNS_RESPONSE,
      { endpoint: "GET /api/test-runs" },
    );
    expect(parsed.test_runs).toEqual([]);
  });
});

describe("TestRunCaseSchema", () => {
  it("parses a run case with all required fields", () => {
    const rc = {
      id: "rc-1",
      workspace_id: "ws-1",
      run_id: "run-1",
      test_case_id: "tc-1",
      case_snapshot: { title: "Login test" },
      position: 0,
      result: "passed",
      notes: "All good",
      evidence: [],
      step_results: [],
      duration_ms: 1500,
      executed_by_type: "member",
      executed_by_id: "user-1",
      executed_at: "2026-08-01T12:00:00Z",
      defect_issue_id: null,
      created_at: "2026-08-01T00:00:00Z",
      updated_at: "2026-08-01T12:00:00Z",
    };
    const parsed = TestRunCaseSchema.parse(rc);
    expect(parsed.result).toBe("passed");
    expect(parsed.duration_ms).toBe(1500);
  });

  it("defaults result to pending when absent", () => {
    const { result: _omit, ...withoutResult } = {
      id: "rc-1",
      workspace_id: "ws-1",
      run_id: "run-1",
      test_case_id: "tc-1",
      case_snapshot: {},
      position: 0,
      result: "pending",
      notes: "",
      evidence: [],
      step_results: [],
      duration_ms: null,
      executed_by_type: null,
      executed_by_id: null,
      executed_at: null,
      defect_issue_id: null,
      created_at: "",
      updated_at: "",
    };
    const parsed = TestRunCaseSchema.parse(withoutResult);
    expect(parsed.result).toBe("pending");
  });

  it("recovers from malformed run case via parseWithFallback", () => {
    const parsed = parseWithFallback(
      null,
      TestRunCaseSchema,
      EMPTY_TEST_RUN_CASE,
      { endpoint: "PUT /api/test-run-cases/:id/result" },
    );
    expect(parsed.id).toBe("");
    expect(parsed.result).toBe("pending");
  });
});

describe("ListTestRunCasesResponseSchema", () => {
  it("defaults to empty array when cases is absent", () => {
    const parsed = parseWithFallback(
      {},
      ListTestRunCasesResponseSchema,
      EMPTY_LIST_TEST_RUN_CASES_RESPONSE,
      { endpoint: "GET /api/test-runs/:id/cases" },
    );
    expect(parsed.cases).toEqual([]);
  });
});

describe("TestCaseResultTimelineEntrySchema", () => {
  it("parses a timeline entry correctly", () => {
    const entry = {
      id: "rc-1",
      run_id: "run-1",
      run_title: "Sprint 1",
      environment: "prod",
      build_ref: "v1.0.0",
      result: "failed",
      executed_at: "2026-08-01T12:00:00Z",
      executed_by_type: "agent",
      executed_by_id: "agent-1",
      defect_issue_id: "issue-1",
      run_created_at: "2026-08-01T00:00:00Z",
    };
    const parsed = TestCaseResultTimelineEntrySchema.parse(entry);
    expect(parsed.result).toBe("failed");
    expect(parsed.defect_issue_id).toBe("issue-1");
  });
});

describe("TestCaseResultTimelineResponseSchema", () => {
  it("defaults to empty timeline when absent", () => {
    const parsed = parseWithFallback(
      {},
      TestCaseResultTimelineResponseSchema,
      EMPTY_TEST_CASE_RESULT_TIMELINE_RESPONSE,
      { endpoint: "GET /api/test-cases/:ref/results" },
    );
    expect(parsed.timeline).toEqual([]);
  });
});

describe("ListTestCapabilitiesResponseSchema", () => {
  it("defaults to empty capabilities when absent", () => {
    const parsed = parseWithFallback(
      {},
      ListTestCapabilitiesResponseSchema,
      EMPTY_LIST_TEST_CAPABILITIES_RESPONSE,
      { endpoint: "GET /api/test-capabilities" },
    );
    expect(parsed.capabilities).toEqual([]);
  });
});

describe("IssueViewSchema", () => {
  const valid = {
    id: "v1",
    workspace_id: "ws1",
    owner_id: "u1",
    name: "Needs review",
    scope_type: "workspace",
    scope_id: null,
    scope_variant: null,
    visibility: "workspace",
    definition_version: 1,
    query: { statusFilters: ["in_review"] },
    display: { viewMode: "board" },
    revision: 3,
    created_at: "2026-08-06T00:00:00Z",
    updated_at: "2026-08-06T00:00:00Z",
  };

  it("parses a well-formed view and keeps unknown future fields", () => {
    const parsed = IssueViewSchema.parse({ ...valid, future_field: "keep me" });
    expect(parsed.name).toBe("Needs review");
    expect(parsed.query).toEqual({ statusFilters: ["in_review"] });
    expect((parsed as unknown as { future_field?: string }).future_field).toBe("keep me");
  });

  it("defaults missing definition blobs instead of failing", () => {
    const parsed = IssueViewSchema.parse({ id: "v2" });
    expect(parsed.query).toEqual({});
    expect(parsed.display).toEqual({});
    expect(parsed.revision).toBe(1);
  });

  it("degrades a malformed list response to [] via parseWithFallback", () => {
    expect(
      parseWithFallback({ nonsense: true }, IssueViewListSchema, [], {
        endpoint: "GET /api/issue-views",
      }),
    ).toEqual([]);
    expect(
      parseWithFallback(null, IssueViewListSchema, [], {
        endpoint: "GET /api/issue-views",
      }),
    ).toEqual([]);
  });

  it("degrades a malformed detail response to null — NOT an error", () => {
    // The sidebar's pinned view rows hinge on this distinction: a parse
    // fallback (null, no error) hides the row, while only a REAL 404
    // error may ever unpin. A malformed body must never destroy a pin.
    expect(
      parseWithFallback({ nonsense: true }, IssueViewSchema.nullable(), null, {
        endpoint: "GET /api/issue-views/{id}",
      }),
    ).toBeNull();
  });
});

// WeCom smart-bot installation schemas. These gate UI affordances (the Connect
// dialog, the "ask your operator" state, the revoked-vs-active badge), so a
// malformed response must degrade to the safe state rather than a broken one.
describe("WeCom installation schemas", () => {
  it("parses a well-formed installation", () => {
    const parsed = WecomInstallationSchema.parse({
      id: "i1",
      workspace_id: "w1",
      agent_id: "a1",
      bot_id: "aibot_xyz",
      installer_user_id: "u1",
      status: "active",
    });
    expect(parsed.bot_id).toBe("aibot_xyz");
    expect(parsed.status).toBe("active");
  });

  it("defaults a missing status to 'revoked', never 'active'", () => {
    // A broken read must not render a bot as connected when it may not be.
    const parsed = WecomInstallationSchema.parse({ id: "i1" });
    expect(parsed.status).toBe("revoked");
    expect(parsed.bot_id).toBe("");
  });

  it("keeps unknown forward-compat fields (loose) instead of failing the parse", () => {
    const parsed = WecomInstallationSchema.parse({ id: "i1", future_field: "keep" });
    expect((parsed as unknown as { future_field?: string }).future_field).toBe("keep");
  });

  it("defaults 'configured' to false so a malformed list renders the operator state", () => {
    const parsed = ListWecomInstallationsResponseSchema.parse({});
    expect(parsed.configured).toBe(false);
    expect(parsed.installations).toEqual([]);
  });

  it("falls back to the empty list when the response is not an object", () => {
    const parsed = parseWithFallback(
      "not json",
      ListWecomInstallationsResponseSchema,
      EMPTY_LIST_WECOM_INSTALLATIONS_RESPONSE,
      { endpoint: "GET /api/workspaces/:id/wecom/installations" },
    );
    expect(parsed).toEqual(EMPTY_LIST_WECOM_INSTALLATIONS_RESPONSE);
    expect(parsed.configured).toBe(false);
  });

  it("falls back on a malformed installation and redeem response", () => {
    const inst = parseWithFallback(42, WecomInstallationSchema, EMPTY_WECOM_INSTALLATION, {
      endpoint: "POST /api/workspaces/:id/wecom/install/byo",
    });
    expect(inst).toEqual(EMPTY_WECOM_INSTALLATION);

    const redeem = parseWithFallback(
      null,
      RedeemWecomBindingTokenResponseSchema,
      EMPTY_REDEEM_WECOM_BINDING_TOKEN_RESPONSE,
      { endpoint: "POST /api/wecom/binding/redeem" },
    );
    expect(redeem).toEqual(EMPTY_REDEEM_WECOM_BINDING_TOKEN_RESPONSE);
  });
});
