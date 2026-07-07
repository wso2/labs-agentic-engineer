# Full flow — sequence diagram

End-to-end runtime sequence for one development cycle, including the per-task lifecycle, gate modes
(human / auto), the DAG + per-org cap, GitHub-webhook→signal routing, and the read-only state poll.

**Key relationship:** `DevelopmentFlowWorkflow` (the cycle) **starts the `TaskLifecycleWorkflow`s**
itself via `ExecuteChildWorkflow` during IMPLEMENT. `aep-api` starts only the cycle workflow and relays
signals/queries; GitHub webhooks for a task are relayed by `aep-api` as **signals to the existing task
workflow** (routed by deterministic ID, e.g. `task:<org>:<proj>:<taskId>`).

```mermaid
sequenceDiagram
    autonumber
    actor U as User (Browser)
    participant CON as Console
    participant API as aep-api (thin client)
    participant TMP as Temporal
    participant DF as DevelopmentFlow WF
    participant TK as TaskLifecycle WF (per task)
    participant JOB as Coding-agent k8s Job
    participant GH as GitHub
    participant DB as database (read-models)

    Note over DF,TK: DevelopmentFlow & Task workflows execute in the orchestrator worker

    %% ---------- START ----------
    U->>CON: approve requirement
    CON->>API: POST /cycles
    API->>TMP: StartWorkflow(devflow:org:proj:cycle)
    TMP->>DF: start (phase = REQUIREMENTS)
    DF->>DB: upsert read-model (cycle: REQUIREMENTS)

    %% ---------- REQUIREMENTS gate ----------
    Note over U,JOB: requirements authored via direct SSE (Console ↔ agents) — NOT through the workflow
    alt GatePolicy.Requirements = human
        U->>CON: click "Approve Requirements"
        CON->>API: POST /cycles/{id}/approve
        API->>TMP: SignalWorkflow(ApproveRequirements)
        TMP->>DF: signal
    else auto (autonomous)
        DF->>JOB: activity: run checks (tests / lint / agent self-review)
        JOB-->>DF: pass
    end
    DF->>DB: upsert read-model (phase: DESIGN)

    %% ---------- DESIGN gate ----------
    Note over DF: DESIGN gate — identical human/auto pattern → on approve, phase = IMPLEMENT

    %% ---------- IMPLEMENT: spawn task children (DAG; per-org cap = k8s quota) ----------
    loop each task whose deps are all "deployed"
        DF->>TK: ExecuteChildWorkflow(task:org:proj:T)
        TK->>JOB: activity: create coding-agent k8s Job
        Note over TK,JOB: if org namespace at ResourceQuota → Job rejected → activity retries (backoff) until a slot frees
        JOB->>GH: open PR
    end

    %% ---------- PER-TASK lifecycle (GitHub webhooks → signals) ----------
    GH->>API: webhook: PR opened
    API->>TMP: SignalWorkflow(task:..:T, PRReady)
    TMP->>TK: signal → ready_for_review

    alt GatePolicy.CodeReview = human
        U->>GH: review & merge PR
    else auto
        TK->>GH: activity: auto-merge (after checks pass)
    end
    GH->>API: webhook: PR merged
    API->>TMP: SignalWorkflow(task:..:T, PRMerged)
    TMP->>TK: signal → merged

    GH->>API: webhook: build started → succeeded
    API->>TMP: SignalWorkflow(task:..:T, BuildSucceeded)
    TMP->>TK: signal → deployed
    TK->>DB: upsert read-model (task: deployed)
    TK-->>DF: child completes (deployed)
    Note over DF: Job already finished & GC'd (ttlSecondsAfterFinished) → namespace quota slot freed automatically

    Note over DF: "deployed" unblocks dependents → DF spawns the next DAG layer (repeat IMPLEMENT loop)

    %% ---------- MERGE & COMPLETE ----------
    DF->>DF: all tasks deployed → MERGE (integrate / release activity)
    alt GatePolicy = human
        U->>CON: MarkComplete
        CON->>API: POST /cycles/{id}/complete
        API->>TMP: SignalWorkflow(MarkComplete)
        TMP->>DF: signal
    else auto
        DF->>DF: auto-complete
    end
    DF->>DB: upsert read-model (cycle: COMPLETE)

    %% ---------- Frontend polling (throughout) ----------
    Note over CON,TMP: meanwhile, the Console polls state (read-only, ~2s)
    CON->>API: GET /cycles/{id}/state
    API->>TMP: QueryWorkflow(GetCycleState)
    TMP-->>CON: {phase, gatesPassed, tasks:[{id,status}]}
```

## Loop-back (iterate) — what changes mid-flight

```mermaid
sequenceDiagram
    autonumber
    actor U as User
    participant API as aep-api
    participant TMP as Temporal
    participant DF as DevelopmentFlow WF
    participant TK as TaskLifecycle WF (running)
    participant JOB as Coding-agent k8s Job
    participant GH as GitHub

    U->>API: "Back to Requirements" (during IMPLEMENT)
    API->>TMP: SignalWorkflow(BackToRequirements)
    TMP->>DF: signal
    DF->>TK: cancel child workflow(s)
    TK->>JOB: delete Job / cancel build
    TK->>GH: comment "superseded — requirements revised" (PR left open)
    Note over DF: deployed components stay running (re-plan, not rollback)
    DF->>DF: phase = REQUIREMENTS
    Note over DF: next IMPLEMENT pass re-derives tasks; idempotent dispatch acts only on what changed
```

## Notes / invariants
- **Only the orchestrator runs a worker.** `aep-api` dials Temporal for `StartWorkflow` /
  `SignalWorkflow` / `SignalWithStartWorkflow` / `QueryWorkflow` only.
- **Parent starts children.** `DevelopmentFlowWorkflow` → `ExecuteChildWorkflow(TaskLifecycleWorkflow)`.
  `aep-api` never starts a task workflow; it only signals existing ones.
- **GitHub issue fast-path:** an issue webhook starts a `DevelopmentFlowWorkflow` with
  `startPhase=tasks` (skips requirements/design) — same diagram from the IMPLEMENT loop onward.
- **All I/O is in activities** (dispatch, build, auto-merge, read-model writes); activities are
  idempotent (Temporal retries). Workflow code stays deterministic.
- **Parallel tasks:** the IMPLEMENT loop runs concurrently per ready task (bounded by the per-org cap);
  the diagram shows one task for clarity.
