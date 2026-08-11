import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import type { Project } from "@multica/core/types";

vi.mock("@/data/api", () => ({ api: {} }));

import { projectKeys } from "@/data/queries/projects";
import {
  applyProjectUpdatedToCache,
  patchProjectsList,
} from "./project-ws-updaters";

const WS_ID = "ws-1";

function project(overrides: Partial<Project> = {}): Project {
  return {
    id: "project-1",
    workspace_id: WS_ID,
    title: "Project One",
    description: null,
    icon: null,
    status: "in_progress",
    priority: "none",
    lead_type: null,
    lead_id: null,
    start_date: null,
    due_date: null,
    created_at: "2026-08-10T00:00:00Z",
    updated_at: "2026-08-10T00:00:00Z",
    issue_count: 1,
    done_count: 1,
    resource_count: 1,
    archived_at: null,
    archived_by: null,
    ...overrides,
  };
}

describe("project archive realtime cache updates", () => {
  it("removes archived projects from the active list while retaining detail", () => {
    const qc = new QueryClient();
    qc.setQueryData(projectKeys.list(WS_ID), [project()]);
    const archived = project({
      archived_at: "2026-08-10T01:00:00Z",
      archived_by: "user-1",
    });

    applyProjectUpdatedToCache(qc, WS_ID, archived);

    expect(qc.getQueryData(projectKeys.list(WS_ID))).toEqual([]);
    expect(qc.getQueryData(projectKeys.detail(WS_ID, archived.id))).toEqual(archived);
  });

  it("patches a restored project when the active list still contains it", () => {
    const qc = new QueryClient();
    const stale = project({ title: "Stale" });
    const restored = project({ title: "Restored" });
    qc.setQueryData(projectKeys.list(WS_ID), [stale]);

    applyProjectUpdatedToCache(qc, WS_ID, restored);

    expect(qc.getQueryData(projectKeys.list(WS_ID))).toEqual([restored]);
    expect(qc.getQueryData(projectKeys.detail(WS_ID, restored.id))).toEqual(restored);
  });

  it("does not invent an active list entry after a remote restore", () => {
    const qc = new QueryClient();
    qc.setQueryData<Project[]>(projectKeys.list(WS_ID), []);

    patchProjectsList(qc, WS_ID, project());

    expect(qc.getQueryData(projectKeys.list(WS_ID))).toEqual([]);
  });
});
