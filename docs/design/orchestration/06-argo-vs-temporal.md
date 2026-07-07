# Argo vs. Temporal — why Temporal drives the dev flow

> TL;DR: **Temporal** orchestrates the long-lived, human-paced development *flow*
> (requirements → design → implement → merge → complete, with approvals and
> iterate-back loops). **Argo** (via OpenChoreo) still runs the ephemeral
> **build/deploy pods** — invoked from a Temporal activity. They are layered, not
> competitors. We use Temporal for the *orchestration* layer because Argo is the
> wrong shape for a months-long, human-gated, looping process.

## What each is built for

- **Argo Workflows** — a Kubernetes-native workflow engine. You declare a DAG of
  **steps, each of which runs as a pod/container**. Excellent for CI/CD pipelines,
  batch, and ML — "run this graph of containers to completion." Workflow state is a
  Kubernetes CR in etcd, reconciled by the Argo controller. Human approval exists
  as a *suspend* node.
- **Temporal** — a durable execution engine. You write the workflow as **ordinary
  code** (Go/TS SDK); the engine persists every step as an event log and replays it
  to resume after any crash. Side effects are **activities** (retried automatically
  on failure). Built for long-running, stateful, human-paced **processes**.

## Side-by-side

| Dimension | Argo Workflows | Temporal |
|---|---|---|
| Unit of work | a **pod/container** per step | an **activity** (a function call) per side effect |
| Programming model | YAML DAG (or Hera/Python) — declarative graph | real code — loops, conditionals, `if/for`, back-edges |
| Human-in-the-loop | `suspend` node (operator-oriented) | `await signal` — a first-class, durable wait |
| Long waits (days/weeks) | suspended CR sits in etcd; controller reconciles it | **zero compute while waiting** (see below) |
| Loops / iterate-back | awkward (DAGs are acyclic; re-runs are new objects) | natural — it's just a `for` loop in code |
| Durability / resume | CR status in etcd | event-sourced history; deterministic replay |
| Retry / compensation | per-step retry | per-activity retry + saga patterns |
| State ownership | Kubernetes object | Temporal datastore (SQLite dev / Postgres prod) |
| Scaling cost | scales with **running pods** + CR count | scales with **event throughput**, not in-flight count |
| Coupling | tightly Kubernetes-coupled | runs anywhere; workers are plain processes |
| Sweet spot | "run a container pipeline to done" | "model a long, branching, human-paced process" |

## Why Temporal for *our* orchestration

The development flow is exactly the shape Argo is bad at:

1. **Human-paced and long-lived.** A cycle can sit at "awaiting design approval" for
   days. Temporal waits durably at ~zero cost; an Argo suspended workflow is a
   long-lived etcd object the controller keeps reconciling.
2. **Loops / iterate-back.** "Back to requirements" is a back-edge in a state
   machine — a `for` loop in Temporal. Argo DAGs are acyclic; re-doing a phase means
   minting new objects and bolting on external state.
3. **Branching as code.** Per-stage gate modes (human vs auto), DAG-ordered task
   scheduling, per-task retries — all natural in code, painful in YAML DAGs.
4. **Per-task addressable signals.** A GitHub webhook routes to the exact task
   workflow by ID and advances its state machine. Argo has no equivalent
   fine-grained, externally-addressable signal model.
5. **One state machine, two modes.** Interactive and autonomous share the same
   workflow — only *who satisfies the gate* changes. (See `00-overview.md`.)

## Why we still use Argo (complementary)

Building and deploying a component **is** "run a container to completion" — Argo's
sweet spot, and what OpenChoreo already uses. So the **build/deploy stays on Argo**,
kicked off by the orchestrator's `DispatchTask` / `DispatchDeploy` activities; the
workflow then `await`s the result signal (`BuildSucceeded`, `DeploySucceeded`). The
durable orchestration brain (Temporal) drives the ephemeral pod-running muscle
(Argo). Neither is removed.

```
Temporal DevelopmentFlow/Task workflow   ── activity ──▶  Argo ClusterWorkflow (build/deploy pods)
        (durable position, human gates,  ◀── signal ───   (ephemeral, runs to completion)
         loops, retries)
```

## Does an active workflow burn compute while it waits? — No.

This is the crux. **A Temporal workflow that is waiting consumes essentially no
compute.** A workflow blocked on a signal, a timer, or an activity result is **not
running**:

- Its state is **persisted in the datastore** and the workflow code is **evicted
  from worker memory**. It is rows in a database, not a parked thread or a held pod.
- A worker only **loads and briefly executes** the workflow for the moment it
  processes a *new event* — a signal arrives, a timer fires, an activity completes —
  then persists and evicts it again.
- `workflow.Sleep(30 * 24 * time.Hour)` does **not** hold a thread; it's a durable
  timer in the service that fires a month later.

So a cycle can sit at "awaiting approval" for **weeks at ~zero CPU/RAM**, and wake
instantly when you click Approve.

**Cost model:** you pay per **event processed** (each signal/timer/activity ≈ a few
datastore writes + a brief slice of worker CPU), **not** per in-flight workflow or
per wait-duration. Workers are sized for **concurrent active processing**, not for
the total number of open workflows. A million idle workflows cost storage, not CPU.

**Honest caveats:**
- The Temporal **Server** (frontend/history/matching) + its **datastore** have a
  baseline footprint regardless — but it's shared across all workflows.
- Idle workflows still consume **storage** (their event history). Retention +
  archival bound this, and very long histories use **`ContinueAsNew`** to keep
  replay cheap.
- Activities that *do* run pods (build/deploy on Argo) consume real compute — but
  that's the pod doing actual work, not the orchestrator waiting.

Contrast: an Argo workflow suspended for two weeks is a live `Workflow` CR the
controller keeps reconciling, adding to etcd/object pressure; a thread-blocked or
pod-parked design would hold real resources the whole time. Temporal's
evict-and-replay model is precisely what makes long human waits free.

## When Argo *would* be the right primary choice
Short-lived, fully-automated container pipelines with no human gates and no
long waits (a nightly batch, an ML training DAG, a pure CI pipeline). For those,
Argo's pod-per-step model is simpler and Kubernetes-native. Our dev flow is the
opposite: long, human-paced, and branching — so Temporal leads and Argo executes
the pod steps underneath.
