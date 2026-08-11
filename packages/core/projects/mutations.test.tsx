/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { setCurrentWorkspace } from "../platform/workspace-storage";
import {
  getIssueSurfaceViewStore,
  pruneIssueSurfaceViewStates,
} from "../issues/stores/surface-view-store";
import type { Project } from "../types";
import { projectKeys } from "./queries";
import { useArchiveProject, useDeleteProject, useRestoreProject } from "./mutations";

vi.mock("../hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

function createWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

describe("useDeleteProject", () => {
  let qc: QueryClient;
  let deleteProject: ReturnType<typeof vi.fn<() => Promise<void>>>;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    deleteProject = vi.fn().mockResolvedValue(undefined);
    setApiInstance({ deleteProject } as unknown as ApiClient);
    setCurrentWorkspace("acme", "ws-1");
  });

  afterEach(() => {
    qc.clear();
    pruneIssueSurfaceViewStates([]);
    setCurrentWorkspace(null, null);
    vi.restoreAllMocks();
  });

  it("clears the deleted project's issue surface view state", async () => {
    const store = getIssueSurfaceViewStore("project:p1");
    store.getState().setViewMode("list");
    expect(store.getState().viewMode).toBe("list");

    const { result } = renderHook(() => useDeleteProject(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync("p1");
    });

    expect(deleteProject).toHaveBeenCalledWith("p1");
    expect(store.getState().viewMode).toBe("board");
  });
});

describe("project archive mutations", () => {
  const project: Project = {
    id: "p1",
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
    archived_at: null,
    archived_by: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    issue_count: 0,
    done_count: 0,
    resource_count: 0,
  };

  it("uses archive mode in list keys", () => {
    expect(projectKeys.list("ws-1", "active")).not.toEqual(
      projectKeys.list("ws-1", "only"),
    );
  });

  it.each([
    ["archive", useArchiveProject, "archiveProject", { ...project, archived_at: "2026-08-10T00:00:00Z" }],
    ["restore", useRestoreProject, "restoreProject", project],
  ] as const)("%s writes detail and invalidates all project caches", async (_name, useHook, method, resultProject) => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const mutation = vi.fn().mockResolvedValue(resultProject);
    setApiInstance({ [method]: mutation } as unknown as ApiClient);
    const invalidate = vi.spyOn(qc, "invalidateQueries");

    const { result } = renderHook(() => useHook(), { wrapper: createWrapper(qc) });
    await act(async () => {
      await result.current.mutateAsync("p1");
    });

    expect(mutation).toHaveBeenCalledWith("p1");
    expect(qc.getQueryData(projectKeys.detail("ws-1", "p1"))).toEqual(resultProject);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: projectKeys.all("ws-1") });
    qc.clear();
  });
});
