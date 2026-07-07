# Do we need Temporal? — DBOS vs. Temporal

> **TL;DR.** No engine is *strictly* required — the platform runs today on a
> DB-backed state machine (`main`). The real question is which durable-execution
> approach fits a **multi-tenant, human-gated, looping, autonomous SDLC platform on
> OpenChoreo**. Of the two serious options, **Temporal is the recommended default**
> (maturity, observability, proven long human-gated orchestration, strong Go+TS
> SDKs, safe versioning) — but **DBOS is the right pick if operational simplicity
> becomes the overriding constraint** (it needs no separate cluster, just Postgres).
> Our architecture keeps that switch contained.

## First: do we *need* a workflow engine at all?
No. `main` already runs the dev flow with a hand-rolled DB state machine + webhook
projector + polling watchers. It works — it's just more brittle code we own
(derived position, lost-approval risk, advisory locks, polling goroutines). A
durable-execution engine *removes* that machinery. So the choice is **build-and-own
vs. buy-the-durability**, and if we buy, **which engine**.

## What DBOS is (vs. Temporal)
- **DBOS** — durable execution as a **library embedded in your service**, backed by
  **Postgres**. Workflows/steps are decorated functions; their state and each step's
  result are checkpointed in Postgres tables. On restart the library scans Postgres
  and resumes incomplete workflows. **No separate orchestrator cluster** — the
  "engine" is your Postgres plus your app process.
- **Temporal** — durable execution as a **separate service** (frontend/history/
  matching) with its own datastore. Workflows are code run by **stateless workers**;
  durability is an **event-sourced history** that is **replayed** to resume. Rich
  Web UI, CLI, search, explicit versioning.

The deepest difference is the recovery model: **DBOS memoizes completed steps**
(skip-on-replay) while **Temporal replays the full event history** (deterministic
re-execution). DBOS's model is generally *easier to reason about*; Temporal's is
more battle-tested at extreme scale.

## Side-by-side

| Dimension | DBOS | Temporal |
|---|---|---|
| Infra footprint | **just Postgres** (library in-process) | a **Temporal cluster** + its datastore |
| Recovery model | step memoization (skip completed) | event-sourced **replay** |
| Determinism burden | lower (nondeterminism → put in steps) | higher (replay discipline, `GetVersion`) — ADR-0004 |
| Human-await / long waits | durable `recv`/events + sleep (✅, younger story) | first-class **signals** + durable timers (✅, proven) |
| Idle-wait compute | ~zero (checkpointed in Postgres) | ~zero (evicted from worker memory) |
| Observability | DBOS console / query Postgres (improving) | **mature Web UI**: timeline, history, search attrs |
| Maturity for this workload | younger (2023→), growing fast | **battle-tested** (Uber-origin), large deployments |
| SDKs | TS, Python, **Go (newer)** | **Go + TS both mature** |
| Versioning of long-running flows | forgiving (checkpoint model) | explicit APIs (`GetVersion`, Build IDs) |
| Scale ceiling | bound by Postgres (plenty for us) | horizontally sharded, very high |
| Ops / learning curve | **low** (no cluster, simpler model) | higher (run+operate the cluster) |
| Multi-tenant isolation | app-level keys in Postgres | workflow-id + namespaces + search attrs |

## For *this* project — the factors that actually decide it

1. **OpenChoreo already gives us Postgres.** DBOS would ride it → **no new
   cluster**. Temporal adds a Helm release + Postgres DBs + Web UI behind the
   gateway (our O5 is explicitly platform-touching). This is DBOS's biggest point.
2. **The flow is long, human-paced, looping, multi-tenant.** Both handle it.
   Temporal's signals/timers are the more proven story for **long human-gated**
   orchestration with **many concurrent cycles**; DBOS can do it via messages/events
   but the long-human-workflow track record is younger.
3. **It's a platform you operate and debug.** Operators/devs need to *see* a cycle,
   find a stuck one, audit who approved what. Temporal's **Web UI is a real
   advantage** here; DBOS observability is catching up but less turnkey today.
4. **Polyglot Go + TS.** The orchestrator is Go; agents/collab/runner are TS.
   Temporal's Go **and** TS SDKs are both mature. DBOS's Go SDK is newer.
5. **Autonomous + human modes share one engine.** Already proven on Temporal here
   (one state machine, gate mode = who acts). DBOS could express the same.
6. **Safe workflow evolution.** As the flow changes over a long-lived platform,
   Temporal's explicit versioning (and our replay-test guard) protect in-flight
   cycles. DBOS's checkpoint model is more forgiving of code changes but less
   explicit about it.

## Recommendation
**Default to Temporal** for this platform. The workload is precisely its sweet spot
(long-lived, human-gated, looping, multi-tenant), and the things a *platform team*
values most here — **observability/Web UI, maturity for this exact shape, a strong
Go+TS story, and explicit versioning** — favour it. It is also already built here,
cleanly abstracted, and tested.

**Choose DBOS instead if operational simplicity is the deciding constraint** — a
small team that does not want to run and monitor a Temporal cluster, and is happy to
lean on the existing Postgres. DBOS delivers ~80% of the durability win with a
fraction of the ops/cognitive cost; it was the documented runner-up for exactly this
reason. The trade you accept: a younger ecosystem, weaker turnkey observability, and
a newer Go SDK.

**Honest tie-breaker:** if I weight *"a platform we run, operate, and debug for
years, across many tenants"* highest → **Temporal**. If I weight *"ship fast with
the fewest moving parts on infra we already have"* highest → **DBOS**. For an
ambitious, long-lived, multi-tenant SDLC product, the first weighting is the right
one, so the recommendation is **Temporal**.

## Why the decision is low-risk either way
The orchestrator is isolated behind the `packages/contracts/orchestration` boundary
and the activity dependency interfaces. The durable-execution engine is an
implementation detail of `services/orchestrator`; `aep-api` only starts/signals/
queries through that boundary. Swapping Temporal for DBOS (or even back to a DB
state machine) is a **contained change to one service**, not a platform rewrite. So
we can commit to Temporal now and still keep DBOS as a credible fallback.

## Suggested next step
If the team wants to *validate* the lighter path before fully committing, a
time-boxed spike — re-implement just the `TaskLifecycleWorkflow` on DBOS behind the
same boundary and compare ops + DX — would settle it with evidence. Otherwise,
proceed with Temporal (O4-real + O5) as planned.
