"use client";

import { useEffect, useMemo, useState } from "react";
import { CircleAlert, ClipboardList, RefreshCw } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { pmoConfigsOptions, pmoRunsOptions } from "@multica/core/pmo/queries";
import {
  useApplyPMORun,
  useSetPMOAssigneeMapping,
  useStartPMORun,
  useUpdatePMOConfig,
} from "@multica/core/pmo/mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions, agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { isAgentRuntimeBound } from "@multica/core/agents";
import { useWorkspacePaths } from "@multica/core/paths";
import type {
  MemberWithUser,
  PMOApplyChoice,
  PMOConfig,
  PMOConflictResolution,
  PMORun,
} from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { Alert, AlertAction, AlertDescription, AlertTitle } from "@multica/ui/components/ui/alert";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { Switch } from "@multica/ui/components/ui/switch";
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
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@multica/ui/components/ui/tabs";
import { CollectionPageState } from "../layout/collection-page";
import { BreadcrumbHeader } from "../layout/breadcrumb-header";
import { PageHeader } from "../layout/page-header";
import { AppLink, useNavigation } from "../navigation";
import { useT } from "../i18n";
import {
  conflictId,
  formatDateTime,
  historyCounts,
  latestRun,
  parseDiffView,
  SummaryChip,
  TruncatedValue,
  RUN_STATUS_ACTIVE,
  type DiffFieldRow,
  type DiffFilter,
} from "./pmo-diff";

export function PMOConfigDetailPage() {
  const { t } = useT("pmo");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { pathname } = useNavigation();
  const configId = pathname.split("/").pop() ?? "";

  const configsQuery = useQuery(pmoConfigsOptions(wsId));
  const configs: PMOConfig[] = configsQuery.data ?? [];
  const config = configs.find((c) => c.id === configId) ?? null;

  const activeConfigId = config?.id ?? "";
  const runsListQuery = useQuery({
    ...pmoRunsOptions(wsId, activeConfigId),
    enabled: Boolean(activeConfigId),
  });
  const runs: PMORun[] = runsListQuery.data ?? [];

  const run = latestRun(runs);
  const diffView = useMemo(() => parseDiffView(run?.diff ?? null), [run?.diff]);
  const runActive = run !== null && RUN_STATUS_ACTIVE.has(run.status);
  const hasConflicts = (diffView?.conflicts.length ?? 0) > 0;

  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));

  const startRun = useStartPMORun();
  const applyRun = useApplyPMORun();
  const setMapping = useSetPMOAssigneeMapping();
  const updateConfig = useUpdatePMOConfig();

  const [tab, setTab] = useState<"preview" | "assignees" | "history">("preview");
  const [filter, setFilter] = useState<DiffFilter>("all");
  const [selections, setSelections] = useState<Record<string, PMOApplyChoice>>({});
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [rootKeyDraft, setRootKeyDraft] = useState("");

  // The inline root-key editor follows the selected config; reset on switch.
  useEffect(() => {
    setRootKeyDraft(config?.root_external_key ?? "");
    setSelections({});
    setFilter("all");
  }, [activeConfigId, config?.root_external_key]);

  const unresolvedConflictCount =
    diffView?.conflicts.filter((id) => !selections[id]).length ?? 0;
  const canApply =
    run !== null &&
    run.trigger === "manual" &&
    run.status === "preview_ready" &&
    unresolvedConflictCount === 0;

  const handleSyncNow = () => {
    if (!config || runActive) return;
    startRun.mutate(config.id, {
      onError: () => toast.error(t(($) => $.config.sync_failed)),
    });
  };

  const handleChoice = (row: DiffFieldRow, choice: PMOApplyChoice) => {
    setSelections((prev) => ({
      ...prev,
      [conflictId(row)]: choice,
    }));
  };

  const handleApply = () => {
    if (!config || !run) return;
    const resolutions: PMOConflictResolution[] = (diffView?.rows ?? [])
      .filter((row) => row.decision === "conflict")
      .map((row) => ({
        external_type: row.externalType,
        external_key: row.entityKey,
        field: row.field,
        choice: selections[conflictId(row)] ?? "local",
      }));
    applyRun.mutate(
      { runId: run.id, resolutions },
      {
        onSuccess: () => toast.success(t(($) => $.preview.apply_success)),
        onError: () => toast.error(t(($) => $.preview.apply_failed)),
      },
    );
    setConfirmOpen(false);
  };

  const handleScheduleToggle = (enabled: boolean) => {
    if (!config || !config.last_applied_at) return;
    updateConfig.mutate(
      {
        id: config.id,
        name: config.name,
        agent_id: config.agent_id,
        orchestration_squad_id: config.orchestration_squad_id,
        root_external_key: config.root_external_key,
        schedule_enabled: enabled,
      },
      {
        onError: () => toast.error(t(($) => $.config.save_failed)),
      },
    );
  };

  const handleRootKeyCommit = () => {
    if (!config) return;
    const next = rootKeyDraft.trim();
    if (!next || next === config.root_external_key) {
      setRootKeyDraft(config.root_external_key);
      return;
    }
    updateConfig.mutate(
      {
        id: config.id,
        name: config.name,
        agent_id: config.agent_id,
        orchestration_squad_id: config.orchestration_squad_id,
        root_external_key: next,
        schedule_enabled: config.schedule_enabled,
      },
      {
        onError: () => {
          setRootKeyDraft(config.root_external_key);
          toast.error(t(($) => $.config.save_failed));
        },
      },
    );
  };

  const activeAgents = useMemo(() => agents.filter((a) => !a.archived_at), [agents]);
  const activeSquads = useMemo(() => squads.filter((squad) => !squad.archived_at), [squads]);

  const filteredRows = useMemo(() => {
    const rows = diffView?.rows ?? [];
    switch (filter) {
      case "all":
        return rows;
      case "creates":
        return rows.filter((r) => r.action === "create");
      case "updates":
        return rows.filter((r) => r.action === "update" && (r.decision === "incoming" || r.decision === "converged"));
      case "local_only":
        return rows.filter((r) => r.decision === "local_only");
      case "conflicts":
        return rows.filter((r) => r.decision === "conflict");
      case "external_removed":
        return rows.filter((r) => r.action === "external_removed");
      case "unresolved": {
        const ids = new Set((diffView?.warnings ?? []).map((w) => w.externalKey));
        return rows.filter((r) => ids.has(r.entityKey));
      }
      default:
        return rows;
    }
  }, [diffView, filter]);

  // ------------------------------------------------------------------ states

  if (configsQuery.isPending) {
    return <ConfigDetailSkeleton />;
  }

  if (configsQuery.isError) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <BreadcrumbHeader
          segments={[{ href: p.pmo(), label: t(($) => $.title) }]}
          leaf={<h1 className="text-body font-medium text-foreground">{t(($) => $.title)}</h1>}
        />
        <CollectionPageState
          icon={ClipboardList}
          tone="destructive"
          title={t(($) => $.config.load_failed)}
        />
      </div>
    );
  }

  if (!config) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <BreadcrumbHeader
          segments={[{ href: p.pmo(), label: t(($) => $.title) }]}
          leaf={<h1 className="text-body font-medium text-foreground">{t(($) => $.title)}</h1>}
        />
        <CollectionPageState
          icon={ClipboardList}
          title={t(($) => $.config.not_found_title)}
          description={t(($) => $.config.not_found_description)}
          actions={
            <Button size="sm" variant="outline" render={<AppLink href={p.pmo()} />} nativeButton={false}>
              {t(($) => $.config.back_to_list)}
            </Button>
          }
        />
      </div>
    );
  }

  const previewBody = !run ? (
    <CollectionPageState
      icon={ClipboardList}
      title={t(($) => $.preview.no_preview_title)}
      description={t(($) => $.preview.no_preview_description)}
    />
  ) : RUN_STATUS_ACTIVE.has(run.status) ? (
    <div className="flex items-center gap-2 px-4 py-10 text-caption text-muted-foreground">
      <Spinner className="size-3.5" />
      {t(($) => $.preview.loading)}
    </div>
  ) : run.status === "applied" || run.status === "applied_with_review" ? (
    <div className="space-y-2 px-4 py-8">
      <p className="text-body font-medium">
        {run.status === "applied" ? t(($) => $.status.applied) : t(($) => $.status.applied_with_review)}
        <span className="ml-2 text-caption font-normal text-muted-foreground">
          {formatDateTime(run.applied_at ?? run.completed_at)}
        </span>
      </p>
      <div className="flex flex-wrap gap-1">
        <SummaryChip count={run.summary?.created as number | undefined} label={(c) => t(($) => $.history.summary.creates, { count: c })} />
        <SummaryChip count={run.summary?.incoming_fields as number | undefined} label={(c) => t(($) => $.history.summary.incoming_fields, { count: c })} />
        <SummaryChip count={run.summary?.conflicts_resolved as number | undefined} label={(c) => t(($) => $.history.summary.conflicts_resolved, { count: c })} />
        <SummaryChip count={run.summary?.conflicts_pending as number | undefined} label={(c) => t(($) => $.history.summary.conflicts_pending, { count: c })} />
        <SummaryChip count={run.summary?.unresolved_assignees as number | undefined} label={(c) => t(($) => $.history.summary.unresolved_assignees, { count: c })} />
      </div>
      <p className="text-caption text-muted-foreground">{t(($) => $.preview.retry_hint)}</p>
    </div>
  ) : (diffView?.rows.length ?? 0) === 0 ? (
    <p className="px-4 py-10 text-center text-caption text-muted-foreground">{t(($) => $.preview.no_changes)}</p>
  ) : filteredRows.length === 0 ? (
    <p className="px-4 py-10 text-center text-caption text-muted-foreground">{t(($) => $.preview.filter_empty)}</p>
  ) : (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[880px] border-collapse text-left">
        <thead>
          <tr className="border-b">
            <th className="px-3 py-2 text-caption font-medium text-muted-foreground">{t(($) => $.preview.entity)}</th>
            <th className="px-3 py-2 text-caption font-medium text-muted-foreground">{t(($) => $.preview.field)}</th>
            <th className="px-3 py-2 text-caption font-medium text-muted-foreground">{t(($) => $.preview.old)}</th>
            <th className="px-3 py-2 text-caption font-medium text-muted-foreground">{t(($) => $.preview.external)}</th>
            <th className="px-3 py-2 text-caption font-medium text-muted-foreground">{t(($) => $.preview.local)}</th>
            <th className="px-3 py-2 text-caption font-medium text-muted-foreground">{t(($) => $.preview.change)}</th>
          </tr>
        </thead>
        <tbody>
          {filteredRows.map((row) => {
            const rowConflicted = row.decision === "conflict";
            const selected = selections[conflictId(row)];
            return (
              <tr
                key={`${row.externalType}:${row.entityKey}:${row.field}`}
                className={cn("border-b last:border-b-0", rowConflicted && "bg-warning/5")}
              >
                <td className="px-3 py-1.5 align-top">
                  <TruncatedValue value={row.entityKey} />
                  <span className="block text-micro text-muted-foreground">
                    {row.externalType === "requirement" ? t(($) => $.entities.requirement) : row.externalType === "task" ? t(($) => $.entities.task) : row.externalType}
                  </span>
                </td>
                <td className="max-w-36 px-3 py-1.5 align-top">
                  <TruncatedValue value={row.field} />
                </td>
                <td className="max-w-56 px-3 py-1.5 align-top text-muted-foreground">
                  <TruncatedValue value={row.decision === "local_only" ? row.baselineLocal : row.baselineExternal} />
                </td>
                <td className="max-w-56 px-3 py-1.5 align-top">
                  <TruncatedValue value={row.external} />
                </td>
                <td className="max-w-56 px-3 py-1.5 align-top">
                  <TruncatedValue value={row.local} />
                </td>
                <td className="px-3 py-1.5 align-top">
                  {rowConflicted ? (
                    <div className="flex items-center gap-1.5">
                      <Button
                        size="xs"
                        variant={selected === "external" ? "default" : "outline"}
                        className="w-[7.5rem]"
                        onClick={() => handleChoice(row, "external")}
                        aria-label={`${t(($) => $.preview.choose_external)} ${row.entityKey} ${row.field}`}
                      >
                        {t(($) => $.preview.choose_external)}
                      </Button>
                      <Button
                        size="xs"
                        variant={selected === "local" ? "default" : "outline"}
                        className="w-[7.5rem]"
                        onClick={() => handleChoice(row, "local")}
                        aria-label={`${t(($) => $.preview.choose_local)} ${row.entityKey} ${row.field}`}
                      >
                        {t(($) => $.preview.choose_local)}
                      </Button>
                    </div>
                  ) : (
                    <Badge variant={row.decision === "incoming" ? "secondary" : "outline"}>
                      {row.decision === "incoming" && t(($) => $.preview.decision_incoming)}
                      {row.decision === "local_only" && t(($) => $.preview.decision_local_only)}
                      {row.decision === "converged" && t(($) => $.preview.decision_converged)}
                      {row.decision === "unchanged" && t(($) => $.preview.decision_unchanged)}
                    </Badge>
                  )}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <BreadcrumbHeader
        segments={[{ href: p.pmo(), label: t(($) => $.title) }]}
        leaf={
          <h1 className="min-w-0 text-body font-medium text-foreground">
            <TruncatedValue value={config.name} />
          </h1>
        }
        actions={
          <Button
            size="sm"
            variant="outline"
            onClick={handleSyncNow}
            disabled={runActive || startRun.isPending}
          >
            {startRun.isPending ? <Spinner className="size-3.5" /> : <RefreshCw className="size-3.5" />}
            {t(($) => $.actions.sync_now)}
          </Button>
        }
      />

      <div className="mx-auto w-full max-w-6xl px-4 pb-8 sm:px-6">
        {/* Config context */}
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2 border-b py-3">
          <NativeSelect
            className="w-44"
            value={config.agent_id}
            onChange={(event) => {
              updateConfig.mutate(
                {
                  id: config.id,
                  name: config.name,
                  agent_id: event.target.value,
                  orchestration_squad_id: config.orchestration_squad_id,
                  root_external_key: config.root_external_key,
                  schedule_enabled: config.schedule_enabled,
                },
                { onError: () => toast.error(t(($) => $.config.save_failed)) },
              );
            }}
            aria-label={t(($) => $.config.agent_label)}
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

          <NativeSelect
            className="w-44"
            value={config.orchestration_squad_id ?? ""}
            onChange={(event) => {
              updateConfig.mutate(
                {
                  id: config.id,
                  name: config.name,
                  agent_id: config.agent_id,
                  orchestration_squad_id: event.target.value || null,
                  root_external_key: config.root_external_key,
                  schedule_enabled: config.schedule_enabled,
                },
                { onError: () => toast.error(t(($) => $.config.save_failed)) },
              );
            }}
            aria-label={t(($) => $.config.squad_label)}
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

          <div className="flex items-center gap-1.5">
            <span className="text-caption text-muted-foreground">{t(($) => $.config.root_key_label)}</span>
            <Input
              className="h-7 w-40 font-mono"
              value={rootKeyDraft}
              onChange={(e) => setRootKeyDraft(e.target.value)}
              onBlur={handleRootKeyCommit}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  (e.target as HTMLInputElement).blur();
                }
              }}
              disabled={Boolean(config.last_applied_at)}
              aria-label={t(($) => $.config.root_key_label)}
            />
          </div>

          <div className="flex items-center gap-1.5">
            <Switch
              size="sm"
              checked={config.schedule_enabled}
              disabled={!config.last_applied_at}
              onCheckedChange={handleScheduleToggle}
              aria-label={t(($) => $.config.schedule)}
            />
            <span className="text-caption text-muted-foreground">{t(($) => $.config.schedule)}</span>
          </div>

          <div className="hidden text-caption text-muted-foreground md:block">
            {run ? (
              <>
                {t(($) => $.config.last_run)} {formatDateTime(run.created_at)}
              </>
            ) : null}
          </div>
        </div>

        {!config.last_applied_at && (
          <p className="pt-2 text-caption text-muted-foreground">{t(($) => $.config.schedule_guard)}</p>
        )}
        {config.schedule_enabled && (
          <p className="pt-2 text-caption text-muted-foreground">{t(($) => $.config.schedule_hint)}</p>
        )}

        <Tabs value={tab} onValueChange={(next) => setTab(next as typeof tab)} className="mt-3 gap-0">
          <TabsList variant="line">
            <TabsTrigger value="preview" className="text-caption">
              {t(($) => $.tabs.preview)}
              {hasConflicts ? <span className="ml-1 text-warning">·</span> : null}
            </TabsTrigger>
            <TabsTrigger value="assignees" className="text-caption">
              {t(($) => $.tabs.assignees)}
              {(diffView?.warnings.length ?? 0) > 0 ? (
                <span className="ml-1">{diffView?.warnings.length}</span>
              ) : null}
            </TabsTrigger>
            <TabsTrigger value="history" className="text-caption">
              {t(($) => $.tabs.history)}
            </TabsTrigger>
          </TabsList>

          <TabsContent value="preview">
            {run?.status === "failed" && (
              <div className="pt-3">
                <Alert variant="destructive">
                  <CircleAlert className="size-4" />
                  <AlertTitle>{t(($) => $.preview.run_failed_title)}</AlertTitle>
                  <AlertDescription>
                    {run.error_code
                      ? `${run.error_code}${run.error_message ? ` — ${run.error_message}` : ""}`
                      : t(($) => $.history.error_redacted)}
                  </AlertDescription>
                  <AlertAction>
                    <Button size="sm" variant="outline" onClick={handleSyncNow} disabled={startRun.isPending}>
                      {startRun.isPending ? <Spinner className="size-3.5" /> : null}
                      {t(($) => $.preview.retry)}
                    </Button>
                  </AlertAction>
                </Alert>
              </div>
            )}
            <div className="flex flex-wrap items-center justify-between gap-2 py-3">
              <div className="flex flex-wrap items-center gap-1" role="group" aria-label={t(($) => $.filters.label)}>
                {(
                  [
                    ["all", t(($) => $.filters.all)],
                    ["creates", t(($) => $.filters.creates)],
                    ["updates", t(($) => $.filters.updates)],
                    ["local_only", t(($) => $.filters.local_only)],
                    ["conflicts", t(($) => $.filters.conflicts)],
                    ["external_removed", t(($) => $.filters.external_removed)],
                    ["unresolved", t(($) => $.filters.unresolved)],
                  ] as [DiffFilter, string][]
                ).map(([key, label]) => (
                  <Button
                    key={key}
                    size="xs"
                    variant={filter === key ? "secondary" : "ghost"}
                    onClick={() => setFilter(key)}
                    aria-pressed={filter === key}
                  >
                    {label}
                  </Button>
                ))}
              </div>
              <div className="flex items-center gap-2">
                {unresolvedConflictCount > 0 && run?.status === "preview_ready" && (
                  <span className="text-caption text-warning">
                    {t(($) => $.history.summary.conflicts_pending, { count: unresolvedConflictCount })}
                  </span>
                )}
                {run?.status === "preview_ready" && run.trigger === "manual" && (
                  <Button size="sm" disabled={!canApply || applyRun.isPending} onClick={() => setConfirmOpen(true)}>
                    {applyRun.isPending ? <Spinner className="size-3.5" /> : null}
                    {t(($) => $.preview.apply)}
                  </Button>
                )}
              </div>
            </div>
            {previewBody}
          </TabsContent>

          <TabsContent value="assignees">
            <div className="space-y-1 py-3">
              <p className="text-caption text-muted-foreground">{t(($) => $.assignees.description)}</p>
              {(diffView?.warnings.length ?? 0) === 0 ? (
                <p className="py-8 text-center text-caption text-muted-foreground">{t(($) => $.assignees.empty)}</p>
              ) : (
                <div className="divide-y">
                  {(diffView?.warnings ?? []).map((warning) => {
                    const references = (diffView?.rows ?? [])
                      .filter((row) => row.entityKey === warning.externalKey)
                      .map((row) => row.field);
                    return (
                      <div key={warning.externalId} className="flex flex-wrap items-center justify-between gap-2 py-2">
                        <div className="min-w-0">
                          <p className="truncate text-body">
                            {warning.displayName || "—"}
                            <span className="ml-2 font-mono text-caption text-muted-foreground">{warning.externalId}</span>
                          </p>
                          {references.length > 0 && (
                            <p className="truncate text-caption text-muted-foreground">
                              {warning.externalKey} · {references.join(", ")}
                            </p>
                          )}
                        </div>
                        <div className="flex items-center gap-2">
                          <span className="hidden text-caption text-muted-foreground sm:block">{t(($) => $.assignees.member)}</span>
                          <NativeSelect
                            className="w-44"
                            defaultValue=""
                            onChange={(event) => {
                              if (!event.target.value) return;
                              setMapping.mutate(
                                { configId: config.id, externalKey: warning.externalId, memberId: event.target.value },
                                { onError: () => toast.error(t(($) => $.assignees.save_failed)) },
                              );
                            }}
                            aria-label={`${t(($) => $.assignees.member)} ${warning.externalId}`}
                          >
                            <NativeSelectOption value="" disabled>
                              {t(($) => $.assignees.member_placeholder)}
                            </NativeSelectOption>
                            {(members as MemberWithUser[]).map((member) => (
                              <NativeSelectOption key={member.id} value={member.id}>
                                {member.name}
                              </NativeSelectOption>
                            ))}
                          </NativeSelect>
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}
            </div>
          </TabsContent>

          <TabsContent value="history">
            {runs.length === 0 ? (
              <CollectionPageState
                icon={ClipboardList}
                title={t(($) => $.history.empty_title)}
                description={t(($) => $.history.empty_description)}
              />
            ) : (
              <div className="divide-y py-1">
                {runs.map((historyRun) => (
                  <div key={historyRun.id} className="flex flex-wrap items-center gap-x-4 gap-y-1.5 py-2.5">
                    <Badge
                      variant={
                        historyRun.status === "failed"
                          ? "destructive"
                          : historyRun.status === "preview_ready"
                            ? "default"
                            : "secondary"
                      }
                      className={cn(historyRun.status === "applied_with_review" && "bg-warning/10 text-warning")}
                    >
                      {historyRun.status === "queued" && t(($) => $.status.queued)}
                      {historyRun.status === "running" && t(($) => $.status.running)}
                      {historyRun.status === "preview_ready" && t(($) => $.status.preview_ready)}
                      {historyRun.status === "applied" && t(($) => $.status.applied)}
                      {historyRun.status === "applied_with_review" && t(($) => $.status.applied_with_review)}
                      {historyRun.status === "failed" && t(($) => $.status.failed)}
                      {historyRun.status === "queued" ||
                      historyRun.status === "running" ||
                      historyRun.status === "preview_ready" ||
                      historyRun.status === "applied" ||
                      historyRun.status === "applied_with_review" ||
                      historyRun.status === "failed"
                        ? null
                        : t(($) => $.status.unknown)}
                    </Badge>
                    <span className="text-caption text-muted-foreground">
                      {historyRun.trigger === "scheduled" ? t(($) => $.history.trigger_scheduled) : t(($) => $.history.trigger_manual)}
                    </span>
                    <span className="text-caption text-muted-foreground">{formatDateTime(historyRun.created_at)}</span>
                    <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1">
                      {(() => {
                        const counts = historyCounts(historyRun);
                        return (
                          <>
                            <SummaryChip count={counts?.creates} label={(c) => t(($) => $.history.summary.creates, { count: c })} />
                            <SummaryChip count={counts?.incoming_fields} label={(c) => t(($) => $.history.summary.incoming_fields, { count: c })} />
                            <SummaryChip count={counts?.conflicts_resolved} label={(c) => t(($) => $.history.summary.conflicts_resolved, { count: c })} />
                            <SummaryChip count={counts?.conflicts_pending} label={(c) => t(($) => $.history.summary.conflicts_pending, { count: c })} />
                            <SummaryChip count={counts?.unresolved_assignees} label={(c) => t(($) => $.history.summary.unresolved_assignees, { count: c })} />
                          </>
                        );
                      })()}
                    </div>
                    {historyRun.status === "failed" && (
                      <span
                        className="max-w-full truncate text-caption text-destructive"
                        title={historyRun.error_code ?? ""}
                      >
                        {historyRun.error_code ?? ""}
                        {historyRun.error_message ? ` — ${historyRun.error_message}` : ` — ${t(($) => $.history.error_redacted)}`}
                      </span>
                    )}
                  </div>
                ))}
              </div>
            )}
          </TabsContent>
        </Tabs>
      </div>

      {/* Apply confirmation */}
      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t(($) => $.preview.apply_confirm_title)}</DialogTitle>
            <DialogDescription>{t(($) => $.preview.apply_confirm_description)}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" size="sm" onClick={() => setConfirmOpen(false)}>
              {t(($) => $.config.cancel)}
            </Button>
            <Button size="sm" onClick={handleApply}>
              {t(($) => $.preview.apply_confirm)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

// Initial-load skeleton — mirrors the breadcrumb + content column layout of
// the loaded page so the swap to real content doesn't shift layout.
function ConfigDetailSkeleton() {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader className="px-5">
        <Skeleton className="h-5 w-48" />
      </PageHeader>
      <div className="mx-auto w-full max-w-6xl space-y-3 px-4 py-4 sm:px-6">
        <Skeleton className="h-9 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    </div>
  );
}
