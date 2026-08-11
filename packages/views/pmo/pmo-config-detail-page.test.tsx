/**
 * PMOConfigDetailPage (/pmo/:configId) state coverage.
 *
 * The page is tested through real DOM assertions with only the data and
 * primitive layers mocked (hooks + UI components). The config id is read
 * from the real NavigationProvider adapter's pathname; row/back navigation
 * is asserted against the adapter's push. i18n resolves through the real
 * RESOURCES bundle. The Tabs mock is CONTROLLED (only the active panel
 * mounts, mirroring the real Base UI tabs), so tab-switching is exercised.
 */
import React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { renderWithI18n } from "../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import type { PMOConfig, PMORun } from "@multica/core/types";
import { PMOConfigDetailPage } from "./pmo-config-detail-page";

// ---------------------------------------------------------------------------
// Fixtures — fictional only (no company names, domains, real identifiers).
// ---------------------------------------------------------------------------

const CONFIG: PMOConfig = {
  id: "cfg-1",
  workspace_id: "ws-1",
  name: "Platform requirements",
  agent_id: "agent-1",
  orchestration_squad_id: null,
  orchestration_issue_id: null,
  root_external_key: "EXT-P-001",
  workload_property_id: null,
  schedule_enabled: false,
  next_run_at: null,
  last_run_at: null,
  last_applied_at: null,
  created_by: "user-1",
  created_at: "2026-08-01T00:00:00Z",
  updated_at: "2026-08-01T00:00:00Z",
};

const PREVIEW_DIFF = {
  entities: [
    {
      external_type: "requirement",
      external_key: "EXT-P-001",
      local_type: "project",
      local_id: "project-1",
      action: "update",
      fields: {
        title: {
          baseline_external: "Old external title",
          baseline_local: "Old local title",
          external: "New external title",
          local: "New local title",
          decision: "conflict",
        },
        status: {
          baseline_external: "todo",
          baseline_local: "todo",
          external: "in_progress",
          local: "todo",
          decision: "incoming",
        },
      },
    },
    {
      external_type: "task",
      external_key: "TASK-001",
      local_type: "issue",
      local_id: "issue-1",
      action: "create",
      fields: {
        title: { external: "New task title", decision: "incoming" },
      },
    },
  ],
  warnings: [
    {
      code: "unresolved_assignee",
      external_id: "EXT-U-001",
      display_name: "Example User",
      external_type: "assignee",
      external_key: "EXT-P-001",
      field: "assignee_id",
    },
  ],
  summary: {
    creates: 1,
    incoming_fields: 2,
    local_only_fields: 0,
    converged_fields: 0,
    conflicts: 1,
    external_removed: 0,
    unresolved_assignees: 1,
  },
};

function makeRun(overrides: Partial<PMORun> = {}): PMORun {
  return {
    id: "run-1",
    workspace_id: "ws-1",
    config_id: "cfg-1",
    agent_task_id: null,
    trigger: "manual",
    status: "preview_ready",
    source_snapshot: null,
    diff: PREVIEW_DIFF,
    summary: null,
    error_code: null,
    error_message: null,
    requested_by: "user-1",
    created_at: "2026-08-03T00:00:00Z",
    started_at: "2026-08-03T00:00:05Z",
    completed_at: "2026-08-03T00:01:00Z",
    applied_at: null,
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const startRunMutate = vi.fn();
const applyRunMutate = vi.fn();
const setMappingMutate = vi.fn();
const updateConfigMutate = vi.fn();
const push = vi.fn();

vi.mock("@multica/core/pmo/mutations", () => ({
  useStartPMORun: () => ({ mutate: startRunMutate, isPending: false }),
  useApplyPMORun: () => ({ mutate: applyRunMutate, isPending: false }),
  useSetPMOAssigneeMapping: () => ({ mutate: setMappingMutate, isPending: false }),
  useUpdatePMOConfig: () => ({ mutate: updateConfigMutate, isPending: false }),
  useCreatePMOConfig: () => ({ mutate: vi.fn(), isPending: false }),
  useDeletePMOConfig: () => ({ mutate: vi.fn(), isPending: false }),
}));

vi.mock("@multica/core/pmo/queries", () => ({
  pmoConfigsOptions: () => ({ queryKey: ["pmo", "configs"] }),
  pmoRunsOptions: (_wsId: string, configId: string) => ({
    queryKey: ["pmo", "runs", configId],
    enabled: Boolean(configId),
  }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"] }),
  agentListOptions: () => ({ queryKey: ["agents"] }),
  squadListOptions: () => ({ queryKey: ["squads"] }),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/agents", () => ({
  isAgentRuntimeBound: (agent: { runtime_bound?: boolean }) => agent.runtime_bound !== false,
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    pmo: () => "/ws-1/pmo",
    pmoConfigDetail: (id: string) => `/ws-1/pmo/${id}`,
  }),
}));

// Query results controlled per test.
const queryState = {
  configs: { data: [] as PMOConfig[] | undefined, isPending: false, isError: false, isSuccess: false },
  runs: { data: [] as PMORun[] | undefined, isPending: false, isError: false, isSuccess: false },
};

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey?: readonly unknown[] }) => {
    const key = options.queryKey?.[0];
    const second = options.queryKey?.[1];
    if (key === "pmo" && second === "configs") return queryState.configs;
    if (key === "pmo" && second === "runs") return { ...queryState.runs, data: options.queryKey?.[2] ? queryState.runs.data : undefined };
    if (key === "members") return { data: [{ id: "member-1", name: "Example Member", user_id: "user-1" }] };
    if (key === "agents") return { data: [{ id: "agent-1", name: "Example Agent", archived_at: null, runtime_bound: true }] };
    if (key === "squads") return { data: [{ id: "squad-1", name: "Example Squad", archived_at: null }] };
    return { data: [] };
  },
}));

vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

// Keep the ui primitives as light DOM so the state logic is what is under test.
// Button preserves its `render` prop (a real AppLink) so link-style buttons
// (e.g. the not-found back link) stay clickable and navigable.
vi.mock("@multica/ui/components/ui/button", async () => {
  const React = await import("react");
  return {
    Button: ({ children, render, ...props }: {
      children?: React.ReactNode;
      render?: React.ReactElement;
      [key: string]: unknown;
    }) => (render ? React.cloneElement(render, undefined, children) : <button {...props}>{children}</button>),
  };
});
vi.mock("@multica/ui/components/ui/badge", () => ({
  Badge: ({ children }: { children?: React.ReactNode }) => <span>{children}</span>,
}));
vi.mock("@multica/ui/components/ui/input", () => ({
  Input: (props: React.InputHTMLAttributes<HTMLInputElement>) => <input {...props} />,
}));
vi.mock("@multica/ui/components/ui/switch", () => ({
  Switch: ({ checked, disabled, onCheckedChange, ...rest }: {
    checked?: boolean;
    disabled?: boolean;
    onCheckedChange?: (value: boolean) => void;
    [key: string]: unknown;
  }) => (
    <button
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      onClick={() => onCheckedChange?.(!checked)}
      {...rest}
    />
  ),
}));
vi.mock("@multica/ui/components/ui/skeleton", () => ({
  Skeleton: ({ className }: { className?: string }) => <div data-testid="skeleton" className={className} />,
}));
vi.mock("@multica/ui/components/ui/spinner", () => ({
  Spinner: () => <span data-testid="spinner" />,
}));
vi.mock("@multica/ui/components/ui/native-select", () => ({
  NativeSelect: ({ children, className, size: _size, ...props }: React.SelectHTMLAttributes<HTMLSelectElement> & { size?: "sm" | "default" | number }) => (
    <select className={className} {...props}>{children}</select>
  ),
  NativeSelectOption: ({ children, ...props }: React.OptionHTMLAttributes<HTMLOptionElement>) => (
    <option {...props}>{children}</option>
  ),
}));
vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ open, children }: { open?: boolean; children?: React.ReactNode }) => (open ? <>{children}</> : null),
  DialogContent: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  DialogFooter: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  DialogDescription: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
}));
vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children?: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render?: React.ReactElement }) => (render ?? null),
  TooltipContent: () => null,
}));

// Controlled Tabs mock: only the active panel mounts, mirroring the real
// Base UI tabs. The trigger click calls onValueChange so the page's own tab
// state drives which panel renders.
vi.mock("@multica/ui/components/ui/tabs", async () => {
  const React = await import("react");
  const TabContext = React.createContext<{
    value: string;
    onValueChange?: (value: string) => void;
  }>({ value: "" });
  return {
    Tabs: ({ value, onValueChange, children }: {
      value?: string;
      onValueChange?: (value: string) => void;
      children?: React.ReactNode;
    }) => (
      <TabContext.Provider value={{ value: value ?? "", onValueChange }}>
        {children}
      </TabContext.Provider>
    ),
    TabsList: ({ children }: { children?: React.ReactNode }) => (
      <div role="tablist">{children}</div>
    ),
    TabsTrigger: ({ value, children }: { value: string; children?: React.ReactNode }) => {
      const ctx = React.useContext(TabContext);
      return (
        <button
          type="button"
          role="tab"
          aria-selected={ctx.value === value}
          onClick={() => ctx.onValueChange?.(value)}
        >
          {children}
        </button>
      );
    },
    TabsContent: ({ value, children }: { value: string; children?: React.ReactNode }) => {
      const ctx = React.useContext(TabContext);
      if (ctx.value !== value) return null;
      return (
        <div role="tabpanel" data-value={value}>{children}</div>
      );
    },
  };
});
vi.mock("../layout/collection-page", () => ({
  CollectionPageHeader: ({ children, actions }: { children?: React.ReactNode; actions?: React.ReactNode }) => (
    <header>{children}{actions}</header>
  ),
  CollectionPageHeaderAction: ({ label, onClick }: { label?: React.ReactNode; onClick?: () => void }) => (
    <button type="button" onClick={onClick}>{label}</button>
  ),
  CollectionPageState: ({ title, description, actions }: {
    title?: React.ReactNode;
    description?: React.ReactNode;
    actions?: React.ReactNode;
  }) => (
    <div data-testid="page-state">
      <div>{title}</div>
      <div>{description}</div>
      <div>{actions}</div>
    </div>
  ),
}));

function renderPage() {
  const adapter: NavigationAdapter = {
    pathname: "/ws-1/pmo/cfg-1",
    push,
    replace: vi.fn(),
    back: vi.fn(),
    searchParams: new URLSearchParams(),
    getShareableUrl: (path) => path,
  };
  return renderWithI18n(
    <NavigationProvider value={adapter}>
      <PMOConfigDetailPage />
    </NavigationProvider>,
  );
}

function previewConfig(overrides: Partial<PMOConfig> = {}) {
  queryState.configs = { data: [{ ...CONFIG, ...overrides }], isPending: false, isError: false, isSuccess: true };
}

function loadingConfigs() {
  queryState.configs = { data: undefined, isPending: true, isError: false, isSuccess: false };
}

function errorConfigs() {
  queryState.configs = { data: undefined, isPending: false, isError: true, isSuccess: false };
}

function noConfig() {
  queryState.configs = { data: [], isPending: false, isError: false, isSuccess: true };
}

function setRuns(runs: PMORun[]) {
  queryState.runs = { data: runs, isPending: false, isError: false, isSuccess: true };
}

const previewPanel = () =>
  document.querySelector<HTMLElement>('[role="tabpanel"][data-value="preview"]') as HTMLElement;

beforeEach(() => {
  queryState.configs = { data: [], isPending: false, isError: false, isSuccess: false };
  queryState.runs = { data: [], isPending: false, isError: false, isSuccess: false };
  startRunMutate.mockClear();
  applyRunMutate.mockClear();
  setMappingMutate.mockClear();
  updateConfigMutate.mockClear();
  push.mockClear();
});

describe("PMOConfigDetailPage routing and states", () => {
  it("reads the config id from the pathname and renders the config name", () => {
    previewConfig();
    setRuns([]);
    renderPage();
    // Breadcrumb leaf shows the matched config name.
    expect(screen.getByText("Platform requirements")).toBeInTheDocument();
  });

  it("renders skeletons while configs load", () => {
    loadingConfigs();
    const { container } = renderPage();
    expect(container.querySelectorAll('[data-testid="skeleton"]').length).toBeGreaterThan(0);
  });

  it("shows the error state when the config list fails", () => {
    errorConfigs();
    renderPage();
    expect(screen.getByText("Failed to load sync configs.")).toBeInTheDocument();
  });

  it("shows the not-found state with a back link when the config id is unknown", () => {
    noConfig();
    renderPage();
    expect(screen.getByText("Sync config not found")).toBeInTheDocument();
    expect(screen.getByText("This config may have been deleted.")).toBeInTheDocument();
    const backLink = screen.getByRole("link", { name: "Back to requirements" });
    expect(backLink).toHaveAttribute("href", "/ws-1/pmo");
    fireEvent.click(backLink);
    expect(push).toHaveBeenCalledWith("/ws-1/pmo");
  });
});

describe("PMOConfigDetailPage preview tab", () => {
  it("renders a preview_ready manual run's field-level diff", () => {
    previewConfig();
    setRuns([makeRun()]);
    renderPage();
    // EXT-P-001 appears once per diff row for that entity (title + status).
    expect(screen.getAllByText("EXT-P-001").length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText("New external title")).toBeInTheDocument();
    expect(screen.getByText("New local title")).toBeInTheDocument();
    expect(screen.getByText("TASK-001")).toBeInTheDocument();
  });

  it("shows an empty preview when there are no runs", () => {
    previewConfig();
    setRuns([]);
    renderPage();
    expect(screen.getByText("No preview yet")).toBeInTheDocument();
  });

  it("shows a compact failure banner with retry while keeping the page visible", () => {
    previewConfig();
    setRuns([makeRun({ status: "failed", error_code: "agent_unavailable", error_message: "agent unreachable" })]);
    renderPage();
    // Banner present.
    expect(screen.getByText("The last run failed")).toBeInTheDocument();
    expect(screen.getByText(/agent_unavailable/)).toBeInTheDocument();
    // The rest of the page (config context + tabs + filters) stays visible.
    expect(screen.getByRole("switch")).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /Preview/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "All" })).toBeInTheDocument();
    // Retry triggers a new run.
    fireEvent.click(screen.getByRole("button", { name: "Retry run" }));
    expect(startRunMutate).toHaveBeenCalledWith(CONFIG.id, expect.anything());
  });

  it("shows a loading indicator for queued/running runs", () => {
    previewConfig();
    setRuns([makeRun({ status: "running" })]);
    renderPage();
    expect(screen.getByTestId("spinner")).toBeInTheDocument();
    expect(screen.getByText("Loading the latest preview...")).toBeInTheDocument();
  });

  it("shows the applied state with summary counts", () => {
    previewConfig();
    setRuns([makeRun({ status: "applied", applied_at: "2026-08-03T00:02:00Z", summary: { created: 1, incoming_fields: 2, conflicts_resolved: 1 } })]);
    renderPage();
    expect(screen.getByText("Applied")).toBeInTheDocument();
    const preview = previewPanel();
    expect(preview.textContent).toContain("1 create");
    expect(preview.textContent).toContain("2 incoming fields");
  });

  it("applies a preview with conflict resolutions", async () => {
    previewConfig();
    setRuns([makeRun()]);
    renderPage();
    fireEvent.click(screen.getByRole("button", { name: "Use external EXT-P-001 title" }));
    fireEvent.click(screen.getByRole("button", { name: "Apply preview" }));
    await waitFor(() => expect(screen.getByText("Apply this preview?")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    expect(applyRunMutate).toHaveBeenCalledWith(
      {
        runId: "run-1",
        resolutions: [
          { external_type: "requirement", external_key: "EXT-P-001", field: "title", choice: "external" },
        ],
      },
      expect.anything(),
    );
  });

  it("keeps the apply button disabled when conflicts are unresolved", () => {
    previewConfig();
    setRuns([makeRun()]);
    renderPage();
    const applyButton = screen.getByRole("button", { name: "Apply preview" });
    expect(applyButton).toBeDisabled();
  });

  it("hides the filter rows that do not match", () => {
    previewConfig();
    setRuns([makeRun()]);
    renderPage();
    fireEvent.click(screen.getByRole("button", { name: "Conflicts" }));
    expect(screen.getByText("New external title")).toBeInTheDocument();
    // The incoming-only status/task rows are filtered out under "Conflicts".
    expect(screen.queryByText("Incoming")).toBeNull();
    expect(screen.queryByText("New task title")).toBeNull();
  });
});

describe("PMOConfigDetailPage assignee tab", () => {
  it("lists unresolved external owners with member selection", () => {
    previewConfig();
    setRuns([makeRun()]);
    renderPage();
    fireEvent.click(screen.getByRole("tab", { name: /Assignee mappings/ }));
    expect(screen.getByText("Example User")).toBeInTheDocument();
    expect(screen.getByText(/EXT-U-001/)).toBeInTheDocument();
    const memberSelect = screen.getByLabelText(/Workspace member EXT-U-001/) as HTMLSelectElement;
    expect(memberSelect).toBeInTheDocument();
    fireEvent.change(memberSelect, { target: { value: "member-1" } });
    expect(setMappingMutate).toHaveBeenCalledWith(
      { configId: CONFIG.id, externalKey: "EXT-U-001", memberId: "member-1" },
      expect.anything(),
    );
  });
});

describe("PMOConfigDetailPage header controls", () => {
  it("keeps the schedule switch disabled until last_applied_at exists", () => {
    previewConfig({ last_applied_at: null });
    setRuns([makeRun()]);
    renderPage();
    const scheduleSwitch = screen.getByRole("switch");
    expect(scheduleSwitch).toBeDisabled();
    expect(screen.getByText("Enable the schedule after applying your first preview.")).toBeInTheDocument();
  });

  it("enables the schedule once last_applied_at exists and calls update", () => {
    previewConfig({ last_applied_at: "2026-08-02T00:00:00Z" });
    setRuns([makeRun()]);
    renderPage();
    const scheduleSwitch = screen.getByRole("switch");
    expect(scheduleSwitch).not.toBeDisabled();
    fireEvent.click(scheduleSwitch);
    expect(updateConfigMutate).toHaveBeenCalledWith(
      expect.objectContaining({ schedule_enabled: true, id: CONFIG.id }),
      expect.anything(),
    );
  });

  it("triggers a manual sync from the header", () => {
    previewConfig();
    setRuns([]);
    renderPage();
    fireEvent.click(screen.getByRole("button", { name: "Sync now" }));
    expect(startRunMutate).toHaveBeenCalledWith(CONFIG.id, expect.anything());
  });

  it("saves the external root key on blur", () => {
    previewConfig();
    setRuns([makeRun()]);
    renderPage();
    const rootKeyInput = screen.getByLabelText("External root key") as HTMLInputElement;
    fireEvent.change(rootKeyInput, { target: { value: "EXT-P-002" } });
    fireEvent.blur(rootKeyInput);
    expect(updateConfigMutate).toHaveBeenCalledWith(
      expect.objectContaining({ root_external_key: "EXT-P-002" }),
      expect.anything(),
    );
  });

  it("updates the execution squad", () => {
    previewConfig();
    setRuns([]);
    renderPage();
    fireEvent.change(screen.getByLabelText("Execution squad"), {
      target: { value: "squad-1" },
    });
    expect(updateConfigMutate).toHaveBeenCalledWith(
      expect.objectContaining({ orchestration_squad_id: "squad-1" }),
      expect.anything(),
    );
  });

  it("locks the root key editor once the config has been applied", () => {
    previewConfig({ last_applied_at: "2026-08-02T00:00:00Z" });
    setRuns([makeRun()]);
    renderPage();
    const rootKeyInput = screen.getByLabelText("External root key") as HTMLInputElement;
    expect(rootKeyInput).toBeDisabled();
  });
});

describe("PMOConfigDetailPage history tab", () => {
  it("shows the empty history state when there are no runs", () => {
    previewConfig();
    setRuns([]);
    renderPage();
    fireEvent.click(screen.getByRole("tab", { name: /Run history/ }));
    expect(screen.getByText("No runs yet")).toBeInTheDocument();
  });

  it("renders run rows with status, trigger and timestamp", () => {
    previewConfig();
    setRuns([makeRun(), makeRun({ id: "run-2", status: "failed", error_code: "sync_error", error_message: "boom" })]);
    renderPage();
    fireEvent.click(screen.getByRole("tab", { name: /Run history/ }));
    expect(screen.getByText("Preview ready")).toBeInTheDocument();
    expect(screen.getByText("Failed")).toBeInTheDocument();
    expect(screen.getAllByText("Manual").length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText(/sync_error/)).toBeInTheDocument();
  });
});
