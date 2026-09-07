# ADR-0009 — A run reports progress per item, not just per event

**Status:** Accepted

## Context

A validation cycle is dispatched with a 7200s deadline
(`coding_executor.go:455`). Until it merges its pull request and the platform
reads `tests/validation/report.json` at that merge commit, nothing on the
Validation page can say anything about any individual acceptance criterion —
issue #669 put a row per criterion on screen and every one of them read
`Pending` for up to two hours. The only live surface was the raw agent feed, where the whole
authoring phase is Write calls and the whole test run was one `tool_use` line
with its result arriving up to ten minutes later.

**PR #307 built this and was closed** on 2026-08-20:

> After an offline discussion, decided to design a generalized event system for
> all coding agents instead of a validation-specific event system.

That generalization landed as ADR-0002 — emitter identity, tool outcomes,
durations — and is about diagnosing a run. The per-criterion capability was
never re-added, so the gap #307 opened against stayed open.

The nine kinds in `progress/schema.ts` share a shape: each reports **something
that happened once, at a moment**. A call was made, a commit landed, the run
entered a phase. Append-only, one line of text each. A checklist needs the
opposite — a fixed set of named things whose status changes repeatedly across
two hours — and no existing kind carries an identity you can key on.
`toolUseId` comes closest and identifies a single call, not a thing that
outlives it.

## Decisions

1. **A tenth kind, `progress_item`, carrying `{ itemId, status }`.** `itemId`
   is the stable key a consumer folds on; `status` reuses the existing envelope
   field, scoped by `kind` as every field here is. One new field on the wire.

   Named for the thing, not for the drawing. `checklist` was the first
   candidate and describes how one consumer renders it — the same objection
   `format.ts` already records for `LineTone` over theme tokens. `work_item` is
   on `CONTEXT.md`'s avoid-list, where it means a Task (a GitHub issue).

2. **No `group` field.** Validation binds items to acceptance criteria, and a
   coding cycle could bind them to issues — but `cycleKind` is already stamped
   on every line the BFF serves, so a field naming the domain would only repeat
   what the envelope said. This is the answer to what closed #307: the kind is
   generic without paying for genericity nobody is using yet.

3. **Inferred from the agent's own tool calls, never declared by it.** A
   `PreToolUse` hook (`validation_progress.ts`) reads statuses off calls the run
   has to make anyway — the test plan it must commit, the spec file it must
   write, the command it must run. An instruction to report progress is one the
   agent can skip with nobody noticing for a whole run; a spec file it never
   writes is a spec file that does not exist.

4. **No side channel — the skill already names specs on the command line.**
   #307 needed a Unix socket because a Playwright reporter runs in a subprocess
   whose stdout the SDK captures as a tool result and drops. None is needed: the
   criterion id is already on Bash commands the hook sees. `authoring.md`
   requires every spec to pass **alone and twice consecutively**
   (`npm test --prefix tests/e2e -- specs/<AC-ID>.spec.ts`), and `healing.md`
   re-runs the same shape. That deletes the socket, its listener, its lifecycle
   and its tests.

   The RUN step was deliberately not changed to match, on the grounds that the
   integrated pass — the suite run serially against one shared deployed
   environment — is the only thing that catches a spec depending on state
   another spec left behind (`healing.md`'s data-collision class). That still
   holds where the pass can happen. What ADR-0010 establishes is that on a suite
   which has outgrown the command timeout it cannot happen at all, so it is now
   attempted best-effort rather than required.

5. **~~Only a COMPLETE run writes `results.json`.~~ Superseded by ADR-0010.**

   > The config chooses its own reporters from whether the command narrows the
   > suite: a narrowed run is a probe and gets `line`, a complete run gets the
   > JSON reporter the report generator reads. Without this, every per-spec
   > verification in steps 6 and 8 overwrites the whole suite's results with one
   > criterion's.
   >
   > Narrowing means a spec filter *or* one of Playwright's own flags —
   > `--shard`, `--grep`, `--last-failed`, `--only-changed`. `--shard` matters
   > most: it looks like the way to fit a big suite inside the command window,
   > and treating it as complete would write a third of the suite's results as
   > if they were all of it.
   >
   > Inferred from the command rather than switched by a flag or an env var, for
   > the same reason as decision 3: naming a spec is not optional, it is *how*
   > you run one spec, so there is nothing for the agent to remember. The cost is
   > that a narrowed authoritative run writes nothing; the generator then exits 2
   > naming the missing file, which is why the skill forbids narrowing that one
   > call — loudly wrong beats a partial report read as a whole one.

   Kept verbatim above because what replaced it is only legible against it. It
   was wrong in a way that cost a run: a suite that has outgrown the command
   timeout **cannot** be run completely, so under this rule it had no path to a
   report at all (issue #701). Results are now accumulated per run and merged,
   with coverage verified against the specs on disk. The reasoning being
   defended — a partial run silently recorded as the whole suite is worse than a
   loud failure — still holds. The mistake was banning the partial run instead
   of learning to detect it.

6. **The live stream ends at `report.json`'s own words.** `pass` / `fail`, not
   `passed` / `failed`. The console overlays these statuses onto the same rows
   it later fills from that report; a second spelling for one fact would need a
   translation table between them, and `passed`/`failed` are already taken at
   the run altitude by the verdict.

7. **`healing` requires a prior pass.** It fires only for an item that reached
   `pass` and then failed — what `healing.md` scopes a heal to. `authoring.md`
   requires a spec to pass twice consecutively, so failing on the way to a
   first pass is the normal path, and reporting that as healing would make a
   healthy run read as a struggling one.

## Rejected

- **A durable store.** #307 added a `validation_criterion_statuses` table, an
  ingest endpoint and a public GET, and this ADR rejected all of it on the
  grounds that "the run-progress stream replays the whole cycle log on
  reconnect, and the archive after the pod is reaped, so a reload re-folds the
  same events."

  **That premise is false, measured.** A poll returns at most
  `defaultProgressLimit` = 200 events (`agent_progress.go`) drawn from the last
  `logPageBytes` = 64 KiB of pod stdout (`cycle_log_source.go`); `sinceMillis
  = 0` on a fresh attach means "everything in the current page", not "from the
  start of the run". Older output is unrecoverable — it is not in the database,
  not cached, and the archive is consulted only once live output is empty and
  the pod terminal, itself capped at 20k lines and then cut to 199. So a page
  opened 90 minutes into a 7200s validation cycle shows `Pending` for criteria
  that already passed, and neither a refresh nor a reconnect recovers them.

  Not building a store may still be the right call, but it is an **open gap**
  rather than a justified rejection: the events are the only record, and they
  are a sliding window. The durable artifact this wants already exists —
  `tests/validation/report.json`, which is on the Files API read allow-list —
  and what blocks reading it mid-run is that nothing records a head SHA for an
  open cycle and the skill pushes only at step 10.
- **Phase markers at the workflow's step boundaries.** A second claim about the
  same run, and wrong for most of it: `authoring.md` has the agent run tests all
  through the authoring step, so a marker reading "Authoring tests…" while rows
  read `Running…` — then flipping to "Running tests…" an hour after the first
  test ran — disagrees with the evidence beside it. The console derives its one
  run-wide line from the rows instead, which cannot contradict them.
- **Reusing `step` for the id.** What #307 did, to avoid touching the backend.
  `step` already means an ordinal stage name for `build_step`; one field
  carrying both that and an entity id is the collision the naming above rejects.

## Consequences

- **A failing multi-spec call settles nothing.** The exit code says the batch
  failed, never which member did, so those items keep `running` and are
  corrected by the report. One spec per call makes this the rare path.
- **The skill owes a stub.** Exploration is the longest unobservable stretch and
  `playwright-cli` calls name URLs, never criteria — so the spec file's
  mandatory `// spec:` header is written *before* exploring, and its existence
  is what marks a criterion as picked up. For newly created specs only: blanking
  an existing one registers as an unexplained modification and fails the report.
- **Renderers format this to no text**, as `activity` already does. The
  structure is the payload; a criterion moving through five statuses would
  otherwise be five log rows narrating what the row above already shows.
- **The integrated run settles no item.** It names no spec, so the hook
  attributes nothing to it: rows hold whatever status their step-6 verification
  left them at until `report.json` lands. That is the trade for keeping the
  integrated run inside the window where healing can still act on what it finds.
- **The report still wins.** These statuses are retired the moment `report.json`
  lands, and its digest — not this feed — is what the delivery workflow reads. A
  row wrong for a minute costs nothing; a wrong verdict mints wrong repair
  issues.

Related: ADR-0002 is what a run records to be *diagnosed*; this is what it
records to be *watched*. #307 carries the closed first attempt.
