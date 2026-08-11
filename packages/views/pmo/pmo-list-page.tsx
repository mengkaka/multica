"use client";

import { useMemo, useState } from "react";
import { ClipboardList, Plus } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { pmoConfigsOptions } from "@multica/core/pmo/queries";
import { useCreatePMOConfig } from "@multica/core/pmo/mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { isAgentRuntimeBound } from "@multica/core/agents";
import { useWorkspacePaths } from "@multica/core/paths";
import type { PMOConfig } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Spinner } from "@multica/ui/components/ui/spinner";
import {
  NativeSelect,
  NativeSelectOption,
} from "@multica/ui/components/ui/native-select";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  CollectionPageHeader,
  CollectionPageState,
} from "../layout/collection-page";
import { AppLink } from "../navigation";
import { useT } from "../i18n";
import { formatDateTime, TruncatedValue } from "./pmo-diff";

// ---------------------------------------------------------------------------
// List — same plain-table shape as the tests library (see testing pages):
// caption header row, hover-highlighted body rows, name is the detail link.
// Configs are the fewest entity (like squads, 1-5 rows), so there is no
// checkbox, batch action, or virtualization.
// ---------------------------------------------------------------------------

/** Table header cell, mirroring the tests library list. */
function Th({ children }: { children: React.ReactNode }) {
  return <th className="px-3 py-2 text-left font-normal">{children}</th>;
}

export function PMOListPage() {
  const { t } = useT("pmo");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();

  const configsQuery = useQuery(pmoConfigsOptions(wsId));
  const configs: PMOConfig[] = configsQuery.data ?? [];

  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const agentsById = useMemo(() => {
    const m = new Map<string, { name: string }>();
    for (const a of agents) m.set(a.id, { name: a.name });
    return m;
  }, [agents]);

  const createConfig = useCreatePMOConfig();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [formName, setFormName] = useState("");
  const [formAgentId, setFormAgentId] = useState("");
  const [formSquadId, setFormSquadId] = useState("");
  const [formRootKey, setFormRootKey] = useState("");

  const openCreateDialog = () => {
    setFormName("");
    setFormAgentId("");
    setFormSquadId("");
    setFormRootKey("");
    setDialogOpen(true);
  };

  const handleFormSave = () => {
    const name = formName.trim();
    const rootKey = formRootKey.trim();
    if (!name || !formAgentId || !rootKey) return;
    createConfig.mutate(
      {
        name,
        agent_id: formAgentId,
        orchestration_squad_id: formSquadId || null,
        root_external_key: rootKey,
      },
      {
        onSuccess: () => {
          setDialogOpen(false);
          toast.success(t(($) => $.config.toast_saved));
        },
        onError: () => toast.error(t(($) => $.config.save_failed)),
      },
    );
  };

  const activeAgents = useMemo(() => agents.filter((a) => !a.archived_at), [agents]);
  const activeSquads = useMemo(() => squads.filter((squad) => !squad.archived_at), [squads]);

  const createConfigDialog = (
    <Dialog open={dialogOpen} onOpenChange={(open) => setDialogOpen(open)}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.config.create)}</DialogTitle>
          <DialogDescription>{t(($) => $.subtitle)}</DialogDescription>
        </DialogHeader>
        <div className="space-y-3 py-2">
          <div className="space-y-1">
            <label className="text-caption text-muted-foreground" htmlFor="pmo-config-name">
              {t(($) => $.config.name_label)}
            </label>
            <Input
              id="pmo-config-name"
              value={formName}
              onChange={(e) => setFormName(e.target.value)}
              placeholder={t(($) => $.config.name_placeholder)}
            />
          </div>
          <div className="space-y-1">
            <label className="text-caption text-muted-foreground" htmlFor="pmo-config-agent">
              {t(($) => $.config.agent_label)}
            </label>
            <NativeSelect
              id="pmo-config-agent"
              className="w-full"
              value={formAgentId}
              onChange={(e) => setFormAgentId(e.target.value)}
            >
              <NativeSelectOption value="" disabled>
                {t(($) => $.config.agent_placeholder)}
              </NativeSelectOption>
              {activeAgents.map((agent) => (
                <NativeSelectOption key={agent.id} value={agent.id} disabled={!isAgentRuntimeBound(agent)}>
                  {agent.name}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </div>
          <div className="space-y-1">
            <label className="text-caption text-muted-foreground" htmlFor="pmo-config-squad">
              {t(($) => $.config.squad_label)}
            </label>
            <NativeSelect
              id="pmo-config-squad"
              className="w-full"
              value={formSquadId}
              onChange={(e) => setFormSquadId(e.target.value)}
            >
              <NativeSelectOption value="">
                {t(($) => $.config.no_squad)}
              </NativeSelectOption>
              {activeSquads.map((squad) => (
                <NativeSelectOption key={squad.id} value={squad.id}>
                  {squad.name}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </div>
          <div className="space-y-1">
            <label className="text-caption text-muted-foreground" htmlFor="pmo-config-root-key">
              {t(($) => $.config.root_key_label)}
            </label>
            <Input
              id="pmo-config-root-key"
              className="font-mono"
              value={formRootKey}
              onChange={(e) => setFormRootKey(e.target.value)}
              placeholder={t(($) => $.config.root_key_placeholder)}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={() => setDialogOpen(false)}>
            {t(($) => $.config.cancel)}
          </Button>
          <Button
            size="sm"
            onClick={handleFormSave}
            disabled={!formName.trim() || !formAgentId || !formRootKey.trim() || createConfig.isPending}
          >
            {createConfig.isPending ? <Spinner className="size-3.5" /> : null}
            {t(($) => $.config.save)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );

  const header = (
    <CollectionPageHeader
      icon={ClipboardList}
      title={t(($) => $.title)}
      description={t(($) => $.subtitle)}
      count={configs.length}
      actions={
        <Button size="sm" onClick={openCreateDialog}>
          <Plus className="size-4" />
          {t(($) => $.config.create)}
        </Button>
      }
    />
  );

  if (configsQuery.isPending) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        {header}
        <div className="min-h-0 flex-1 overflow-auto">
          <table className="w-full text-body">
            <thead>
              <tr className="border-b border-border text-caption text-muted-foreground">
                <Th>{t(($) => $.config.name_label)}</Th>
                <Th>{t(($) => $.config.root_key_label)}</Th>
                <Th>{t(($) => $.config.agent_label)}</Th>
                <Th>{t(($) => $.config.schedule)}</Th>
                <Th>{t(($) => $.config.last_run)}</Th>
                <Th>{t(($) => $.config.last_applied)}</Th>
              </tr>
            </thead>
            <tbody>
              {Array.from({ length: 4 }).map((_, i) => (
                <tr key={i} className="border-b border-border">
                  <td className="px-3 py-2">
                    <Skeleton className="h-3.5 w-32 max-w-full" />
                  </td>
                  <td className="px-3 py-2">
                    <Skeleton className="h-3 w-20" />
                  </td>
                  <td className="px-3 py-2">
                    <Skeleton className="h-3 w-16" />
                  </td>
                  <td className="px-3 py-2">
                    <Skeleton className="h-5 w-12" />
                  </td>
                  <td className="px-3 py-2">
                    <Skeleton className="h-3 w-16" />
                  </td>
                  <td className="px-3 py-2">
                    <Skeleton className="h-3 w-16" />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    );
  }

  if (configsQuery.isError) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        {header}
        <CollectionPageState
          icon={ClipboardList}
          tone="destructive"
          title={t(($) => $.config.load_failed)}
        />
      </div>
    );
  }

  if (configs.length === 0) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        {header}
        <CollectionPageState
          icon={ClipboardList}
          title={t(($) => $.config.empty_title)}
          description={t(($) => $.config.empty_description)}
          actions={
            <Button size="sm" onClick={openCreateDialog}>
              {t(($) => $.config.create)}
            </Button>
          }
        />
        {createConfigDialog}
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {header}
      <div className="min-h-0 flex-1 overflow-auto">
        <table className="w-full text-body">
          <thead>
            <tr className="border-b border-border text-caption text-muted-foreground">
              <Th>{t(($) => $.config.name_label)}</Th>
              <Th>{t(($) => $.config.root_key_label)}</Th>
              <Th>{t(($) => $.config.agent_label)}</Th>
              <Th>{t(($) => $.config.schedule)}</Th>
              <Th>{t(($) => $.config.last_run)}</Th>
              <Th>{t(($) => $.config.last_applied)}</Th>
            </tr>
          </thead>
          <tbody>
            {configs.map((config) => (
              <tr key={config.id} className="border-b border-border hover:bg-accent">
                <td className="max-w-xs px-3 py-2">
                  <AppLink
                    href={p.pmoConfigDetail(config.id)}
                    className="block truncate font-medium"
                    title={config.name}
                  >
                    {config.name}
                  </AppLink>
                </td>
                <td className="px-3 py-2">
                  <TruncatedValue value={config.root_external_key} className="font-mono text-caption" />
                </td>
                <td className="px-3 py-2 text-muted-foreground">
                  <span className="min-w-0 truncate text-caption">
                    {agentsById.get(config.agent_id)?.name ?? config.agent_id.slice(0, 8)}
                  </span>
                </td>
                <td className="px-3 py-2">
                  <Badge variant={config.schedule_enabled ? "default" : "outline"}>
                    {config.schedule_enabled
                      ? t(($) => $.config.schedule_on)
                      : t(($) => $.config.schedule_off)}
                  </Badge>
                </td>
                <td className="px-3 py-2 text-muted-foreground">
                  <span className="whitespace-nowrap text-caption tabular-nums">
                    {formatDateTime(config.last_run_at) || t(($) => $.config.never)}
                  </span>
                </td>
                <td className="px-3 py-2 text-muted-foreground">
                  <span className="whitespace-nowrap text-caption tabular-nums">
                    {formatDateTime(config.last_applied_at) || t(($) => $.config.never)}
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {createConfigDialog}
    </div>
  );
}
