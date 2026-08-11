import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { ProjectArchiveMode } from "../types";

export const projectKeys = {
  all: (wsId: string) => ["projects", wsId] as const,
  list: (wsId: string, archived: ProjectArchiveMode = "active") =>
    [...projectKeys.all(wsId), "list", archived] as const,
  detail: (wsId: string, id: string) =>
    [...projectKeys.all(wsId), "detail", id] as const,
};

export function projectListOptions(wsId: string, archived: ProjectArchiveMode = "active") {
  return queryOptions({
    queryKey: projectKeys.list(wsId, archived),
    queryFn: () => api.listProjects({ archived }),
    select: (data) => data.projects,
  });
}

export function projectDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: projectKeys.detail(wsId, id),
    queryFn: () => api.getProject(id),
  });
}
