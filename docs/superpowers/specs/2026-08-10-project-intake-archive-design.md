# PMO Project Intake And Archive Design

**Status:** Approved
**Date:** 2026-08-10
**Supersedes:** the original generic `/api/project-intakes` proposal in commit `1aa65c1a6`

## Summary

Multica already has a deployed PMO requirement-management flow on
`feat/pm-task-sync`: a configured Agent acquires one external requirement
snapshot, Multica validates and previews it, and an approved run materializes
the parent requirement as a Project plus its children as canonical Issues.

This change extends that flow instead of adding a second intake API or a second
external identity model. An imported Project is named
`<display_number> <title>`, for example `REQ-1234 Add invoice export`. A PMO
configuration may also select an execution Squad. On first apply, Multica
creates one durable root Issue for that Squad so its Leader can split, stage,
review, and accept implementation work through the existing Squad protocol.

Projects gain reversible archive state. Archive is independent from Project
status: status describes delivery state, while archive controls visibility in
normal working views. Archiving never deletes Issues, resources, comments,
chat context, or Agent run history.

The implementation of this feature itself is assigned directly to
`小码codex-fe` on `SY-12`. It is a single development task and must not create
Squad child Issues. That delivery choice does not change the product behavior
for future externally synchronized requirements.

## Goals

- Reuse `pmo_sync_config`, `pmo_sync_run`, `pmo_sync_link`, strict snapshot
  validation, three-way diff, preview/apply, and scheduled sync.
- Name externally imported Projects as `<display_number> <title>` while leaving
  manually created Project names unchanged.
- Optionally create exactly one PMO execution root Issue per configuration and
  assign it to a selected Squad.
- Reuse Squad Leader delegation, staged child Issues, child-completion wakeups,
  `in_review`, and human-only `done` behavior.
- Hide archived Projects from normal lists, search, and pickers; provide an
  archived view and restore action in shared Web/Desktop UI.
- Restore an imported Project when the external requirement reopens.
- Automatically archive an imported Project only when its acknowledged
  external status and canonical Project status are terminal and every Project
  Issue is terminal.
- Preserve retries and concurrent PMO apply idempotency.

## Non-Goals

- A new `/api/project-intakes` endpoint or provider-specific connector.
- External identity columns on `project`; `pmo_sync_link` remains authoritative.
- A new workflow engine, task queue, or approval system.
- Automatic `done` for the PMO root Issue. A human or trusted existing
  integration owns final completion.
- Automatic deletion when an external entity disappears.
- Archived management UI in Mobile or CLI in this increment. Their ordinary
  Project lists remain active-only.
- Replacing existing hard delete. Delete remains an exceptional owner/admin
  operation with its existing cleanup semantics.

## Existing PMO Contract

The existing PMO mapping remains canonical:

- one `pmo_sync_config` represents one external parent requirement;
- a validated `pmo_sync_run` owns one immutable external snapshot;
- the parent requirement is linked to one canonical Project;
- child requirements and scheduling tasks are linked to canonical Issues;
- `pmo_sync_link` stores stable external identity and three-way baselines;
- manual runs preview before apply; scheduled runs apply safe changes and keep
  conflicts for review.

No Project is created before the existing preview/apply boundary.

## Data Model

The `project` table gains two nullable columns:

```text
archived_at timestamptz
archived_by uuid
```

There are no foreign keys. `archived_by` contains the requesting user for a
manual archive and is null for automatic archive. Restore clears both fields.

The existing `pmo_sync_config` table gains two nullable columns:

```text
orchestration_squad_id uuid
orchestration_issue_id uuid
```

Nullable fields preserve already deployed PMO configurations. A configuration
without `orchestration_squad_id` continues to preview and apply data but does
not start development automatically. Application code validates that the Squad
belongs to the workspace, has an active Agent Leader, and that a stored root
Issue belongs to the same workspace and imported Project.

No new index is needed for these one-to-one config columns. Any later index
must use `CREATE INDEX CONCURRENTLY` in its own single-statement migration.

## External Project Naming

Only the PMO parent requirement Project title is formatted:

```text
trim(display_number) + " " + trim(title)
```

Both values are already required and normalized by the PMO snapshot contract.
Child requirement and task Issue titles remain their source titles. The
formatted Project title participates in the existing field-level three-way
diff, so an external rename is previewed and local edits still produce the
existing local-only or conflict decisions.

Manual Project create/update endpoints do not accept or infer requirement
numbers.

## PMO Execution Root Issue

When an approved PMO run first creates its Project and the configuration has an
execution Squad, the same apply transaction:

1. validates and locks the configuration and Squad;
2. creates one root Issue in the imported Project with the formatted Project
   title and parent requirement description;
3. sets `assignee_type=squad`, `assignee_id=orchestration_squad_id`, and
   `status=todo`;
4. stores the new Issue ID in `pmo_sync_config.orchestration_issue_id`;
5. commits canonical entities and PMO links together;
6. runs the existing Issue post-create effects after commit so only the Squad
   Leader is queued.

The config row lock and stored Issue ID are the idempotency boundary. A retry
must reload the stored Issue and must never create a duplicate root Issue or
Agent task. An inconsistent cross-workspace or cross-Project stored link fails
the apply with a conflict error rather than repairing by duplication.

Routine sync updates the root title and description without enqueuing another
run while it is active. If the external requirement reopens and the root is
`done` or `cancelled`, apply restores it to `todo` and invokes the existing
assignment trigger once. If it is already active, apply only restores the
Project and updates fields.

The imported child requirement Issues remain canonical PMO-synced Issues. The
PMO Leader may create separate execution children under the orchestration root;
the sync service does not reinterpret those local execution Issues as external
requirements.

## Development And Acceptance Loop

No new runner is added:

1. The root Issue assignment resolves to the selected Squad Leader.
2. The PMO Leader sizes the decomposition to the requirement.
3. Independent work may run in one stage; dependent integration and acceptance
   work use later stages.
4. A `todo` Agent child starts through the existing trigger. A `backlog` child
   waits for its stage.
5. Closing a stage wakes the PMO Leader through the existing child-done path.
6. The PMO Leader reviews results and moves the root to `in_review`.
7. A human or trusted integration moves it to `done`, or returns it for another
   cycle.

Small work may be delegated to one Agent without creating multiple children.
The code does not force a minimum child count.

## Archive And Restore API

Archive operations are idempotent:

```http
POST /api/projects/{id}/archive
POST /api/projects/{id}/restore
```

Both require workspace owner/admin access and return the current full Project
response with `200 OK`. Manual archive sets `archived_at=now()` and
`archived_by=<user>` without changing status. Restore clears both fields and
does not change Project or Issue status or trigger an Agent.

Direct `GET /api/projects/{id}` continues returning archived Projects so saved
links and history remain usable.

## Automatic Archive And Reopen

The auto-archive query uses the applied parent requirement link and succeeds
only when all of these are true:

```text
an active pmo_sync_link maps the parent requirement to this Project
AND acknowledged external status is completed or cancelled
AND canonical Project status is completed or cancelled
AND the Project has at least one Issue
AND no Project Issue has a status outside done or cancelled
```

It runs after a PMO apply and after a Project Issue transitions into a terminal
status. The update and Issue count check execute in the same transaction as the
calling mutation. A successful change publishes the existing
`project:updated` event after commit.

An applied PMO snapshot whose parent status is non-terminal always clears
archive state. Reopen also changes a terminal orchestration root Issue to
`todo`; it does not duplicate the root or touch locally created child Issues.

Manual restore alone does not restart work. A later terminal sync may archive
the Project again if the full guard is true.

## Listing, Search, And UI

`GET /api/projects` accepts:

```text
archived=active  (default)
archived=only
archived=all
```

Invalid values return `400`. Status and priority filters compose with archive
mode. Search and Project pickers use the active-only default.

The shared Web/Desktop Projects page adds an `Active | Archived` segmented
control. Active rows expose Archive; archived rows expose Restore. The detail
view keeps Issues and resources readable, shows archived state, and exposes
Restore for owner/admin users. Archive confirmation states that history is
kept.

Project responses add nullable `archived_at` and `archived_by`. API schemas use
fallback defaults so older installed clients tolerate newer responses. Query
keys include archive mode. `project:updated` removes archived Projects from
active caches and restored Projects from archived caches.

Mobile and CLI ordinary lists inherit the active-only server default. Mobile
realtime removes a Project from its active cache when `archived_at` becomes
non-null. Dedicated archive management can be added later.

## Authorization And Data Safety

- PMO config writes, apply, archive, and restore use existing workspace role
  checks.
- Every Project, Squad, Issue, config, run, and link lookup is workspace-scoped.
- No database foreign keys or cascading actions are introduced.
- No archive path deletes or detaches related rows.
- Events and Agent triggers occur only after transaction commit.
- Existing PMO validation, payload limits, conflict handling, and redacted
  errors remain unchanged.
- Tests must use a temporary Project; `Multica-iworker` must never be archived.

## Verification

Focused server tests cover Project name formatting, config migration
compatibility, Squad validation, one root Issue under retries/concurrency,
reopen, manual archive/restore, active/only/all listing, and the full automatic
archive guard.

Core and shared-view tests cover response fallback, archive-aware query keys,
mutations, cache removal, segmented list mode, permissions, confirmation, and
detail restore. Mobile tests cover active-cache removal. Integration checks run
the migration linter, PMO tests, Project/Issue handler tests, TypeScript tests,
typecheck, and the repository's broader verification commands.

## Deployment Configuration

For each external root requirement configuration:

1. select the existing acquisition Agent;
2. optionally select an execution Squad whose Leader is the PMO Agent;
3. keep implementation and acceptance Agents as Squad members;
4. give the Squad instructions for right-sized decomposition, explicit
   acceptance criteria, staged dependencies, and review before `in_review`;
5. keep human approval for the root `done` transition.

The current `SY-12` delivery remains directly assigned to `小码codex-fe`; it
does not use `实习生小队`.
