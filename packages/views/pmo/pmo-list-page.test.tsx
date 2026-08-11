/**
 * PMOListPage (overview at /pmo) state coverage.
 *
 * The page is tested through real DOM assertions with only the data and
 * primitive layers mocked (hooks + UI components). Navigation goes through
 * the real NavigationProvider / AppLink with a stubbed adapter so row
 * clicks can be asserted against the adapter's push. i18n resolves through
 * the real RESOURCES bundle.
 */
import React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, within } from "@testing-library/react";
import { renderWithI18n } from "../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import type { PMOConfig } from "@multica/core/types";
import { PMOListPage } from "./pmo-list-page";

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

const SCHEDULED_CONFIG: PMOConfig = {
  ...CONFIG,
  id: "cfg-2",
  name: "Weekly sync",
  schedule_enabled: true,
  last_run_at: "2026-08-03T00:00:00Z",
  last_applied_at: "2026-08-02T00:00:00Z",
};

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const createConfigMutate = vi.fn();
const push = vi.fn();

vi.mock("@multica/core/pmo/mutations", () => ({
  useStartPMORun: () => ({ mutate: vi.fn(), isPending: false }),
  useApplyPMORun: () => ({ mutate: vi.fn(), isPending: false }),
  useSetPMOAssigneeMapping: () => ({ mutate: vi.fn(), isPending: false }),
  useUpdatePMOConfig: () => ({ mutate: vi.fn(), isPending: false }),
  useCreatePMOConfig: () => ({ mutate: createConfigMutate, isPending: false }),
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
};

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey?: readonly unknown[] }) => {
    const key = options.queryKey?.[0];
    const second = options.queryKey?.[1];
    if (key === "pmo" && second === "configs") return queryState.configs;
    if (key === "members") return { data: [{ id: "member-1", name: "Example Member", user_id: "user-1" }] };
    if (key === "agents") return { data: [{ id: "agent-1", name: "Example Agent", archived_at: null, runtime_bound: true }] };
    if (key === "squads") return { data: [{ id: "squad-1", name: "Example Squad", archived_at: null }] };
    return { data: [] };
  },
}));

vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

// Keep the ui primitives as light DOM so the state logic is what is under test.
vi.mock("@multica/ui/components/ui/button", () => ({
  Button: ({ children, ...props }: React.ButtonHTMLAttributes<HTMLButtonElement> & { children?: React.ReactNode }) => (
    <button {...props}>{children}</button>
  ),
}));
vi.mock("@multica/ui/components/ui/badge", () => ({
  Badge: ({ children }: { children?: React.ReactNode }) => <span>{children}</span>,
}));
vi.mock("@multica/ui/components/ui/input", () => ({
  Input: (props: React.InputHTMLAttributes<HTMLInputElement>) => <input {...props} />,
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
    pathname: "/ws-1/pmo",
    push,
    replace: vi.fn(),
    back: vi.fn(),
    searchParams: new URLSearchParams(),
    getShareableUrl: (path) => path,
  };
  return renderWithI18n(
    <NavigationProvider value={adapter}>
      <PMOListPage />
    </NavigationProvider>,
  );
}

function listConfigs(configs: PMOConfig[]) {
  queryState.configs = { data: configs, isPending: false, isError: false, isSuccess: true };
}

function loadingConfigs() {
  queryState.configs = { data: undefined, isPending: true, isError: false, isSuccess: false };
}

function errorConfigs() {
  queryState.configs = { data: undefined, isPending: false, isError: true, isSuccess: false };
}

beforeEach(() => {
  queryState.configs = { data: [], isPending: false, isError: false, isSuccess: false };
  createConfigMutate.mockClear();
  push.mockClear();
});

describe("PMOListPage loading and empty states", () => {
  it("renders skeletons while configs load", () => {
    loadingConfigs();
    const { container } = renderPage();
    expect(container.querySelectorAll('[data-testid="skeleton"]').length).toBeGreaterThan(0);
  });

  it("shows the empty config state when the workspace has no configs", () => {
    listConfigs([]);
    renderPage();
    expect(screen.getByText("No sync config yet")).toBeInTheDocument();
    expect(screen.getByText("Create a configuration to sync one external requirement tree.")).toBeInTheDocument();
  });

  it("opens the create dialog from the empty-state CTA", () => {
    listConfigs([]);
    renderPage();
    // The header action and the empty-state CTA share the label; scope the
    // click to the CTA inside the page-state block.
    fireEvent.click(within(screen.getByTestId("page-state")).getByRole("button", { name: "New sync config" }));
    expect(screen.getByLabelText("Name")).toBeInTheDocument();
    expect(screen.getByLabelText("Agent")).toBeInTheDocument();
    expect(screen.getByLabelText("External root key")).toBeInTheDocument();
  });

  it("shows the error state when the config list fails", () => {
    errorConfigs();
    renderPage();
    expect(screen.getByText("Failed to load sync configs.")).toBeInTheDocument();
  });
});

describe("PMOListPage config list", () => {
  it("renders a row per config with name, root key, agent, schedule and last-run values", () => {
    listConfigs([CONFIG, SCHEDULED_CONFIG]);
    renderPage();
    // Name (row identity). "Scheduled sync" here is the schedule column header.
    expect(screen.getByText("Platform requirements")).toBeInTheDocument();
    expect(screen.getByText("Weekly sync")).toBeInTheDocument();
    expect(screen.getByText("Scheduled sync")).toBeInTheDocument();
    // Root key (mono cell) — both fixtures share the same root key.
    expect(screen.getAllByText("EXT-P-001").length).toBeGreaterThanOrEqual(1);
    // Agent name resolved from the agents query.
    expect(screen.getAllByText("Example Agent").length).toBeGreaterThanOrEqual(1);
    // Schedule state badges.
    expect(screen.getByText("Off")).toBeInTheDocument();
    expect(screen.getByText("On")).toBeInTheDocument();
    // No-run config shows "Never" for both last-run and last-applied columns.
    expect(screen.getAllByText("Never").length).toBeGreaterThanOrEqual(2);
  });

  it("navigates to the config detail page when a row is clicked", () => {
    listConfigs([CONFIG]);
    renderPage();
    fireEvent.click(screen.getByText("Platform requirements"));
    expect(push).toHaveBeenCalledWith("/ws-1/pmo/cfg-1");
  });

  it("opens the create dialog from the header action", () => {
    listConfigs([CONFIG]);
    renderPage();
    fireEvent.click(screen.getByRole("button", { name: "New sync config" }));
    expect(screen.getByLabelText("Name")).toBeInTheDocument();
  });
});

describe("PMOListPage create-config dialog", () => {
  it("creates a config with the filled form values", () => {
    listConfigs([CONFIG]);
    renderPage();
    fireEvent.click(screen.getByRole("button", { name: "New sync config" }));

    const nameInput = screen.getByLabelText("Name") as HTMLInputElement;
    const agentSelect = screen.getByLabelText("Agent") as HTMLSelectElement;
    const squadSelect = screen.getByLabelText("Execution squad") as HTMLSelectElement;
    const rootKeyInput = screen.getByLabelText("External root key") as HTMLInputElement;

    // Save stays disabled until every required field is filled.
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();

    fireEvent.change(nameInput, { target: { value: "My config" } });
    fireEvent.change(agentSelect, { target: { value: "agent-1" } });
    fireEvent.change(squadSelect, { target: { value: "squad-1" } });
    fireEvent.change(rootKeyInput, { target: { value: "EXT-9" } });

    expect(screen.getByRole("button", { name: "Save" })).not.toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(createConfigMutate).toHaveBeenCalledWith(
      {
        name: "My config",
        agent_id: "agent-1",
        orchestration_squad_id: "squad-1",
        root_external_key: "EXT-9",
      },
      expect.anything(),
    );
  });

  it("does not submit when a required field is empty", () => {
    listConfigs([CONFIG]);
    renderPage();
    fireEvent.click(screen.getByRole("button", { name: "New sync config" }));

    const nameInput = screen.getByLabelText("Name") as HTMLInputElement;
    const rootKeyInput = screen.getByLabelText("External root key") as HTMLInputElement;
    fireEvent.change(nameInput, { target: { value: "My config" } });
    fireEvent.change(rootKeyInput, { target: { value: "EXT-9" } });
    // Agent left on the disabled placeholder → save disabled, nothing submitted.
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(createConfigMutate).not.toHaveBeenCalled();
  });
});
