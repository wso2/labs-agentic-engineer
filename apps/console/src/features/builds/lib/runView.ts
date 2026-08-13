/**
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

import type { StatusTone } from "../../../components/StatusChip";
import type { components } from "../../../generated/aep-api";
import { openGates } from "../../tasks/lib/issueRows";

type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type TaskView = components["schemas"]["TaskView"];
type RunCycleView = components["schemas"]["RunCycleView"];
type RunBudgets = components["schemas"]["RunBudgets"];
type CycleBuild = components["schemas"]["CycleBuild"];

// Pure derivations for the version's run story. The run state is the Builds
// page's single liveness driver — nothing here consults a task's derivedStatus,
// which the flip degraded to "the issue is open, or it is closed".

/**
 * The cycle kinds the BUILDS surface owns.
 *
 * Validation is deliberately absent. The deployment is what gets validated, so
 * its cycle and its verdict render on the deployment surface — the same reason
 * the issue list hides the validation issue. A validation cycle showing up
 * here would put the verdict in two places and invite them to disagree.
 */
export const BUILD_CYCLE_KINDS = ["coding", "fix", "conflict"] as const;

/** Terminal run states: the loop is over and nothing will move again.
 *  `blocked` is terminal and is NOT a failure — the org has no agent slot left. */
const TERMINAL_RUN_STATES = new Set([
  "succeeded",
  "failed",
  "cancelled",
  "blocked",
]);

/** The run's build sessions in order — the validation cycle filtered out,
 *  because the deployment is what gets validated and its verdict renders on
 *  the Validation board, not on this rail. */
export function buildCycles(cycles: RunCycleView[]): RunCycleView[] {
  return cycles.filter((c) =>
    (BUILD_CYCLE_KINDS as readonly string[]).includes(c.kind),
  );
}

export function isTerminalRun(state: string): boolean {
  return TERMINAL_RUN_STATES.has(state);
}

/**
 * Does this run belong on the BUILD rail — is it part of how the version was
 * delivered, rather than a re-judgement of it?
 *
 * The rail already filters validation cycles out (buildCycles above), on the
 * grounds that a verdict shown in two places invites the two to disagree. A run
 * that ONLY validated is that same argument one level up: it has no build session
 * to show, and its verdict belongs on the Validation board. Leading with one made
 * this page announce "No build session was ever dispatched" — copy written for a
 * run that died before dispatching, and false here, because a validation cycle was
 * dispatched, ran, and merged.
 *
 * Not a plain origin test, and that is the whole subtlety: a revalidation left at
 * the default attempt allowance REPAIRS what it finds — one issue per failed
 * criterion, then an ordinary coding cycle, then builds. Once it has done that it
 * is a build story like any other, so it is judged on what it actually did.
 *
 * The converse also has to hold: a spec build that died before dispatching has no
 * build sessions either, and it MUST stay — "nothing was ever dispatched" is the
 * true and useful thing to say about it. Hence the origin clause first.
 */
export function isDeliveryRun(run: MilestoneRunView): boolean {
  return run.origin !== "revalidate" || buildCycles(run.cycles ?? []).length > 0;
}

/**
 * Is this version still moving? Only the newest run can be live (a milestone
 * sees SEQUENTIAL runs across its life), so this is the whole page's poll
 * predicate and the gate on every GitHub-backed read.
 */
export function versionIsLive(runs: MilestoneRunView[]): boolean {
  const newest = runs[0];
  return newest !== undefined && !isTerminalRun(newest.state);
}

/** The run's own state chip. `waiting` is written only when a run actually
 *  parks, so it is worth its own warning tone — that is when cancel matters.
 *  `planning` is the platform working, so it reads like `running`. */
export function runStateChip(run: MilestoneRunView): {
  label: string;
  tone: StatusTone;
} {
  switch (run.state) {
    case "planning":
      return { label: "Planning", tone: "info" };
    case "waiting":
      return { label: "Waiting", tone: "warning" };
    case "running":
      return { label: "Running", tone: "info" };
    case "succeeded":
      return { label: "Succeeded", tone: "success" };
    case "failed":
      return { label: "Failed", tone: "error" };
    case "cancelled":
      return { label: "Cancelled", tone: "neutral" };
    case "blocked":
      // Quota, not a platform fault — warning, not error.
      return { label: "Blocked", tone: "warning" };
    default:
      // An unknown state renders raw and red rather than hiding.
      return { label: run.state, tone: "error" };
  }
}

/**
 * WHO, if anyone, has to act on an OPEN dispatch gate.
 *
 * Every path that resolves a gate admits a `provision` Execution against the
 * gate's issue before it does anything else — the drawer's config author, the
 * platform-resource provisioner, the org-service visibility request. That row
 * is therefore the answer to "is anything already working on this", and it is
 * the only honest one available: a gate issue is prose plus two labels, and the
 * platform never reads a gate back to decide anything.
 *
 *   provisioning — a run is in flight. The platform closes this gate itself,
 *                  so nothing is needed from the user. A database or an
 *                  identity app takes minutes to stand up, and calling that
 *                  wait a hold is what made a healthy build read as parked on
 *                  a human.
 *   failed       — the provisioning run broke. The gate issue carries why.
 *   idle         — no run at all: nothing is driving this gate. This is the
 *                  one case where a human genuinely has to supply something.
 *
 * A SUCCEEDED run against a still-open gate reads `idle` for the same reason a
 * missing row does — whatever was driving it is finished and the gate is still
 * open, so the next move is a person's.
 */
export type GateDrive = "provisioning" | "failed" | "idle";

/** The Execution statuses that still hold the admission mutex, i.e. work in
 *  flight (mirrors taskmeta.ExecutionStatus.IsActive). */
const ACTIVE_EXEC_STATES = new Set(["queued", "running"]);

export function gateDrive(gate: TaskView): GateDrive {
  const status = gate.executions?.["provision"]?.status;
  if (status === undefined) return "idle";
  if (ACTIVE_EXEC_STATES.has(status)) return "provisioning";
  return status === "failed" || status === "canceled" ? "failed" : "idle";
}

/**
 * WHY NOTHING IS MOVING — the one thing a user asks of a run that is not
 * running, and the one answer the page owes them.
 *
 * The reasons are genuinely different, and conflating them is what makes a
 * healthy build announce itself as parked: the platform writing the milestone
 * is not a hold, a gate the platform is actively provisioning is not a hold
 * either, a gate nothing is driving IS one only a human can release, and an
 * empty milestone is the loop declining to call a version delivered that was
 * never planned. Each has a different thing to do about it — including
 * "nothing" — so each gets its own notice.
 *
 * `milestone` is the issue plane's answer, or undefined while it is still
 * loading — a run is never accused of having no work on the strength of a list
 * that has not arrived. Its `gates` are the OPEN ones; a resolved gate holds
 * nothing.
 */
export interface RunHold {
  kind: "planning" | "no-work" | "parked";
  /** Warning is reserved for a hold only a human can release; error for one
   *  that already went wrong. The platform doing its own bounded work is
   *  information, not an alarm. */
  tone: "info" | "warning" | "error";
  title: string;
  body: string;
}

export function runHold(
  run: MilestoneRunView,
  milestone: { gates: TaskView[]; openWork: number } | undefined,
): RunHold | null {
  if (run.state === "planning") {
    return {
      kind: "planning",
      tone: "info",
      title: `Planning ${run.milestoneTitle}`,
      body:
        "Creating this version's issues in GitHub. Nothing is held and nothing " +
        "is needed from you — the first build session dispatches as soon as " +
        "the milestone is written.",
    };
  }
  if (run.state !== "waiting" || milestone === undefined) return null;

  // A gate hold is answered by the PROVISIONING SECTION on the run's rail, not
  // here: it names each connection, who is acting on it, and what is needed —
  // and it sits where the sequence actually stopped. Saying it twice put two
  // notices on one page competing to explain one fact.
  if (openGates(milestone.gates).length > 0) return null;

  if (milestone.openWork === 0) {
    return {
      kind: "no-work",
      tone: "info",
      title: "Waiting for work",
      body:
        "This version's milestone holds no open issue to dispatch. The run " +
        "waits rather than settling — a milestone nothing was ever planned " +
        "into is not a version that was delivered.",
    };
  }
  return {
    kind: "parked",
    tone: "warning",
    title: "Parked between build sessions",
    body:
      "The wait is unbounded — cancel is its only expiry, and cancelling " +
      "abandons the increment: the way forward is the next build.",
  };
}

// Each terminal reason names exactly ONE failure class (that is the point of
// the vocabulary), so each gets a sentence rather than a re-worded enum.
const TERMINAL_REASONS: Record<string, string> = {
  "redispatch-budget":
    "The coding agent died twice in the same build session — the per-session re-dispatch budget is spent.",
  "build-retrigger-budget":
    "A component's build stayed red after its automatic re-trigger, and no fix issue came back.",
  "fix-chain-budget": "The run spent both of its fix sessions.",
  "conflict-budget": "The run spent both of its conflict sessions.",
  "no-progress":
    "A build session closed no issues and minted none — the run stopped rather than loop.",
  "cycle-ceiling": "The run hit its ceiling on total build sessions.",
  "validation-failed": "One or more validation criteria failed.",
  // The validating phase's OTHER reason, and a different failure: nothing was
  // asserted against the software at all, so this names the missing artifact
  // rather than an outcome the criteria produced.
  "validation-unreported":
    "The validation agent finished without committing a report, so the run has no results.",
  "agent-quota-blocked":
    "This organization is already running the maximum number of agent runs allowed by its plan. " +
    "Wait for one to finish, or stop a running run, then start this one again.",
};

/** A sentence for the run's terminal reason; the raw value when unmapped, so
 *  a reason this console has not learned yet still reaches the user. */
export function terminalReasonText(reason: string): string {
  if (!reason) return "";
  return TERMINAL_REASONS[reason] ?? reason;
}

/**
 * One build session's section label — "Build session 3 · fix".
 *
 * "Build session" is the CONSOLE's name for a RunCycle: same object, a name that
 * reads as a unit of work rather than as loop machinery. The wire, the model and
 * the budgets stay `cycle` (see docs/glossary.md), and the bare word "session"
 * is never used on its own here — that belongs to the spec-collaboration Room.
 */
export function buildSessionLabel(cycle: RunCycleView, index: number): string {
  return `Build session ${index + 1} · ${cycle.kind}`;
}

/**
 * What started this run. A milestone can see several SEQUENTIAL runs — the
 * spec build, then an incident adoption — and every run card is titled with the
 * same milestone title, so the origin is the only thing that tells them apart.
 * An origin this build predates is shown raw rather than mislabelled.
 */
export function runOriginLabel(origin: string): string {
  switch (origin) {
    case "spec-build":
      return "Spec build";
    case "incident-adoption":
      return "Incident";
    default:
      return origin;
  }
}

/**
 * A completed build's OUTCOME — the one place the console decides what
 * OpenChoreo's condition Reason means.
 *
 * The cluster writes `WorkflowSucceeded` / `WorkflowFailed` (its
 * `controller_conditions.go` reason constants, carried verbatim through the
 * contract); the bare `Succeeded` / `Failed` spellings are accepted alongside
 * them because that is what the contract's own examples and the mock layer have
 * always used. Anything else is `unknown` and shows itself rather than being
 * flattened into a verdict — the Reason set is open, so an unrecognised terminal
 * Reason is a fact this console has not learned, not a failure.
 *
 * `unknown` deliberately does NOT read as red: a build the console cannot
 * classify has not been shown to have failed. It does not read as green either,
 * which is what keeps a session from claiming a deployment it cannot vouch for.
 */
export type BuildOutcome = "succeeded" | "failed" | "unknown";

const SUCCEEDED_REASONS = new Set(["WorkflowSucceeded", "Succeeded"]);
const FAILED_REASONS = new Set(["WorkflowFailed", "Failed"]);

export function buildOutcome(build: CycleBuild): BuildOutcome {
  if (!build.completed) return "unknown";
  if (SUCCEEDED_REASONS.has(build.status)) return "succeeded";
  if (FAILED_REASONS.has(build.status)) return "failed";
  return "unknown";
}

/**
 * One build's chip. The status string is a LABEL and never a predicate; the
 * verdict comes from `buildOutcome`, and a Reason with no known verdict renders
 * raw so nothing hides behind a guess.
 */
export function buildStatusChip(build: CycleBuild): {
  label: string;
  tone: StatusTone;
} {
  if (!build.completed) {
    // "Pending" is OpenChoreo's word for a run that exists but has not started.
    return {
      label: build.status || "Running",
      tone: build.status === "Pending" ? "neutral" : "info",
    };
  }
  switch (buildOutcome(build)) {
    case "succeeded":
      return { label: "Succeeded", tone: "success" };
    case "failed":
      return { label: "Failed", tone: "error" };
    default:
      return { label: build.status || "Completed", tone: "neutral" };
  }
}

/** Does this fan-out still have a build that could change? */
export function buildsAreSettled(builds: CycleBuild[]): boolean {
  return builds.length > 0 && builds.every((b) => b.completed);
}

export interface SpentBudget {
  label: string;
  /** The count against the ceiling it reached: "2 / 2". */
  text: string;
}

// The §7 budgets that are per-RUN constants; the per-cycle re-dispatch budget
// belongs to a cycle row, not here.
const FIX_CYCLE_BUDGET = 2;
const CONFLICT_CYCLE_BUDGET = 2;

/**
 * The budgets this run has SPENT — a counter sitting at its ceiling, and
 * nothing else. A healthy run returns [], and the card shows no numbers at
 * all: how much allowance is left is loop machinery, and it only becomes the
 * user's business once an allowance runs out and explains why the run stopped.
 * How many cycles ran is already legible from the cycle timeline.
 *
 * `cycleCeiling` is snapshotted on the run row, so a config change cannot
 * retroactively re-scale a live run's gauge. Build re-triggers are absent by
 * design: they have no run-wide ceiling — the real guard is one per component
 * per SHA, derived at the trigger site — so there is nothing here to spend.
 */
export function spentBudgets(budgets: RunBudgets): SpentBudget[] {
  const spent: SpentBudget[] = [];
  if (budgets.cycleCeiling > 0 && budgets.cyclesTotal >= budgets.cycleCeiling) {
    spent.push({
      label: "Build sessions",
      text: `${budgets.cyclesTotal} / ${budgets.cycleCeiling}`,
    });
  }
  if (budgets.fixCycles >= FIX_CYCLE_BUDGET) {
    spent.push({
      label: "Fix sessions",
      text: `${budgets.fixCycles} / ${FIX_CYCLE_BUDGET}`,
    });
  }
  if (budgets.conflictCycles >= CONFLICT_CYCLE_BUDGET) {
    spent.push({
      label: "Conflict sessions",
      text: `${budgets.conflictCycles} / ${CONFLICT_CYCLE_BUDGET}`,
    });
  }
  return spent;
}
