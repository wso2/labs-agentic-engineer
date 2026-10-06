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

import { describe, expect, it } from "vitest";
import type { components } from "../../../generated/aep-api";
import {
  buildOutcome,
  buildSessionLabel,
  externalValuesPark,
  gateDrive,
  hasMergedWork,
  isAgentStreaming,
  isDeliveryRun,
  isTerminalRun,
  runHold,
  runKind,
  runKindLabel,
  mergedCycle,
  runStateChip,
  spentBudgets,
  terminalReasonText,
  versionIsLive,
} from "./runView";

type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type RunBudgets = components["schemas"]["RunBudgets"];
type TaskView = components["schemas"]["TaskView"];

const budgets = (over: Partial<RunBudgets> = {}): RunBudgets => ({
  cyclesTotal: 0,
  cycleCeiling: 8,
  fixCycles: 0,
  conflictCycles: 0,
  buildRetriggers: 0,
  validationCycles: 0,
  ...over,
});

const run = (over: Partial<MilestoneRunView> = {}): MilestoneRunView => ({
  id: "run-1",
  milestoneNumber: 1,
  milestoneTitle: "v1",
  kind: "dev",
  origin: "spec-build",
  state: "running",
  budgets: budgets(),
  validation: {},
  cycles: [],
  createdAt: "2026-07-10T09:00:00Z",
  ...over,
});

const cycle = (
  over: Partial<components["schemas"]["RunCycleView"]> = {},
): components["schemas"]["RunCycleView"] => ({
  id: "c1",
  kind: "coding",
  attempts: 1,
  createdAt: "2026-07-10T09:05:00Z",
  ...over,
});

describe("mergedCycle / hasMergedWork", () => {
  it("finds the cycle that actually merged, not the newest one", () => {
    // The Build logs bug: the section was handed `cycles.at(-1)`, which is
    // routinely a validation cycle or a coding cycle that never merged, and the
    // cluster read answers empty for any cycle with no merge SHA.
    const r = run({
      cycles: [
        cycle({ id: "merged", mergeSha: "abc1234" }),
        cycle({ id: "retry", mergeSha: "" }),
      ],
    });
    expect(mergedCycle([r])?.id).toBe("merged");
    expect(hasMergedWork([r])).toBe(true);
  });

  it("looks across every run of the version, newest merge first", () => {
    // A version is often worked by several runs; the merge is frequently in an
    // earlier one, and a later run cannot un-merge it.
    const older = run({ id: "older", cycles: [cycle({ id: "old-merge", mergeSha: "aaa" })] });
    const newer = run({ id: "newer", kind: "task", cycles: [] });
    expect(mergedCycle([newer, older])?.id).toBe("old-merge");
  });

  it("prefers the NEWER RUN's merge when both runs merged something", () => {
    // The precedence rule itself. The two cases either side of this one pass
    // just as happily if the scan runs oldest-first: one has no merge in the
    // newer run, the other has both merges inside a single run.
    const newer = run({ id: "newer", cycles: [cycle({ id: "new-merge", mergeSha: "bbb" })] });
    const older = run({ id: "older", cycles: [cycle({ id: "old-merge", mergeSha: "aaa" })] });
    expect(mergedCycle([newer, older])?.id).toBe("new-merge");
  });

  it("prefers the newest merge when several cycles merged", () => {
    const r = run({
      cycles: [cycle({ id: "first", mergeSha: "aaa" }), cycle({ id: "second", mergeSha: "bbb" })],
    });
    expect(mergedCycle([r])?.id).toBe("second");
  });

  it("does not count a validation cycle's SHA — it names the commit it judged", () => {
    const r = run({ cycles: [cycle({ kind: "validation", mergeSha: "abc1234" })] });
    expect(mergedCycle([r])).toBeUndefined();
    expect(hasMergedWork([r])).toBe(false);
  });

  it("is undefined when nothing merged", () => {
    expect(mergedCycle([run({ cycles: [cycle({ mergeSha: "" })] })])).toBeUndefined();
    expect(mergedCycle([])).toBeUndefined();
    expect(mergedCycle(undefined)).toBeUndefined();
  });
});

describe("isAgentStreaming", () => {
  it("streams only while a build session is open", () => {
    expect(isAgentStreaming([run({ cycles: [cycle({ endedAt: null })] })])).toBe(true);
    expect(
      isAgentStreaming([run({ cycles: [cycle({ endedAt: "2026-07-10T09:40:00Z" })] })]),
    ).toBe(false);
  });

  it("stops the moment the agent finishes, though the RUN is still in progress", () => {
    // The bug: the chip read the build's status, and a run stays in_progress
    // through the merge, the component builds and the deployment — long after
    // the coding agent has stopped and there is nothing left to stream.
    const settled = run({
      state: "running",
      cycles: [cycle({ endedAt: "2026-07-10T09:40:00Z", mergeSha: "abc" })],
    });
    expect(isAgentStreaming([settled])).toBe(false);
  });

  it("never streams on a terminal run", () => {
    expect(
      isAgentStreaming([run({ state: "cancelled", cycles: [cycle({ endedAt: null })] })]),
    ).toBe(false);
  });

  it("asks only the newest run — an older run's open cycle is not live", () => {
    const newest = run({ id: "newest", cycles: [cycle({ endedAt: "2026-07-10T09:40:00Z" })] });
    const older = run({ id: "older", cycles: [cycle({ endedAt: null })] });
    expect(isAgentStreaming([newest, older])).toBe(false);
  });

  it("is quiet between sessions, and with no run at all", () => {
    expect(isAgentStreaming([run({ cycles: [] })])).toBe(false);
    expect(isAgentStreaming([])).toBe(false);
    expect(isAgentStreaming(undefined)).toBe(false);
  });
});

describe("isTerminalRun / versionIsLive", () => {
  it("names the terminal states and nothing else", () => {
    expect(isTerminalRun("succeeded")).toBe(true);
    expect(isTerminalRun("failed")).toBe(true);
    expect(isTerminalRun("cancelled")).toBe(true);
    expect(isTerminalRun("blocked")).toBe(true);
    expect(isTerminalRun("waiting")).toBe(false);
    expect(isTerminalRun("running")).toBe(false);
  });

  it("a version with no runs is not live", () => {
    expect(versionIsLive([])).toBe(false);
  });

  it("only the NEWEST run decides — a milestone's runs are sequential", () => {
    // Newest first. An old succeeded run behind a live one must not settle the
    // page, and an old running row behind a terminal one must not keep it
    // polling forever.
    expect(
      versionIsLive([run({ state: "waiting" }), run({ state: "succeeded" })]),
    ).toBe(true);
    expect(
      versionIsLive([run({ state: "succeeded" }), run({ state: "running" })]),
    ).toBe(false);
  });
});

describe("isDeliveryRun", () => {
  type Cycle = MilestoneRunView["cycles"][number];
  const cycle = (kind: Cycle["kind"]): Cycle => ({
    id: `cycle-${kind}`,
    kind,
    attempts: 1,
    createdAt: "2026-07-10T10:00:00Z",
  });

  it("keeps every run that delivered the version", () => {
    expect(isDeliveryRun(run({ cycles: [cycle("coding")] }))).toBe(true);
    expect(
      isDeliveryRun(run({ kind: "task", cycles: [cycle("coding")] })),
    ).toBe(true);
  });

  // The copy this protects — "No build session was ever dispatched" — is TRUE for a
  // dev run that died before dispatching, so that run has to stay on the rail.
  it("keeps a dev run that never dispatched a session", () => {
    expect(isDeliveryRun(run({ cycles: [] }))).toBe(true);
  });

  // A run that only re-judged the version has no build session to show, and its
  // verdict belongs on the Validation board. Leading with one made the page claim
  // nothing had been dispatched — false, since a validation cycle ran and merged.
  it("drops a validation run that only validated", () => {
    expect(
      isDeliveryRun(run({ kind: "validation", cycles: [cycle("validation")] })),
    ).toBe(false);
  });

  // A row recorded before the kind column existed carries only its origin, and the
  // rail must read it the same way — otherwise every historical revalidation
  // reappears on the build rail announcing that nothing was ever dispatched.
  it("falls back to the origin on a run that predates the kind", () => {
    const legacy = run({ cycles: [cycle("validation")] }) as Record<
      string,
      unknown
    >;
    delete legacy.kind;
    legacy.origin = "revalidate";
    expect(isDeliveryRun(legacy as MilestoneRunView)).toBe(false);
  });

  // But a validation run left at the default attempt allowance repairs what it
  // finds: an issue per failed criterion, then an ordinary coding cycle, then
  // builds. Once it has done that it IS a build story, so the test is what it did.
  it("keeps a validation run that repaired and rebuilt", () => {
    expect(
      isDeliveryRun(
        run({
          kind: "validation",
          cycles: [cycle("validation"), cycle("coding")],
        }),
      ),
    ).toBe(true);
  });
});

describe("externalValuesPark", () => {
  // ADR-0023: the deploy gate parks the run in `waiting` and names what it is
  // short of. The build page renders those names, so the helper must return
  // them rather than a boolean.
  it("names the dependencies a deploy-gate park is waiting on", () => {
    expect(
      externalValuesPark(
        run({
          state: "waiting",
          waitingReason: "external-values",
          blockingDependencies: ["stripe", "sendgrid"],
        }),
      ),
    ).toEqual(["stripe", "sendgrid"]);
  });

  // An empty array is a real answer — parked, naming nothing — and must stay
  // distinguishable from null, or the one run that most needs an explanation
  // renders none.
  it("still reports a park that names nothing", () => {
    expect(
      externalValuesPark(run({ state: "waiting", waitingReason: "external-values" })),
    ).toEqual([]);
  });

  // A reasonless `waiting` is the ordinary between-cycles park. It is bounded
  // and nobody has to do anything about it, so it must not borrow the gate's
  // call to action.
  it("ignores a wait with no reason on it", () => {
    expect(externalValuesPark(run({ state: "waiting" }))).toBeNull();
  });

  it("ignores a run that is not waiting", () => {
    expect(
      externalValuesPark(
        run({ state: "running", waitingReason: "external-values" }),
      ),
    ).toBeNull();
  });

  // The build page has no run at all until the runs read answers, and while it
  // is in flight the page must not claim the version is parked.
  it("is null when there is no run", () => {
    expect(externalValuesPark(undefined)).toBeNull();
  });
});

describe("runStateChip", () => {
  it("gives waiting its own warning tone — that is when cancel matters", () => {
    expect(runStateChip(run({ state: "waiting" }))).toEqual({
      label: "Waiting",
      tone: "warning",
    });
  });

  it("gives blocked a warning tone — quota, not a platform fault", () => {
    expect(runStateChip(run({ state: "blocked" }))).toEqual({
      label: "Blocked",
      tone: "warning",
    });
  });

  // Planning is the platform working, not a run held on somebody.
  it("reads planning as activity, not as a warning", () => {
    expect(runStateChip(run({ state: "planning" }))).toEqual({
      label: "Planning",
      tone: "info",
    });
  });

  it("renders an unknown state raw and red rather than hiding it", () => {
    expect(runStateChip(run({ state: "quantum" as never }))).toEqual({
      label: "quantum",
      tone: "error",
    });
  });
});

// A gate issue, with or without the provisioning run that drives it. `status`
// omitted = no `provision` execution row at all, i.e. nothing is working on it.
const gate = (issueNumber: number, title: string, status?: string): TaskView =>
  ({
    issueNumber,
    title,
    issueUrl: `https://github.com/o/r/issues/${issueNumber}`,
    executorClass: "provision",
    derivedStatus: "pending",
    executions: status
      ? {
          provision: {
            id: `x${issueNumber}`,
            kind: "provision",
            status,
            createdAt: "2026-07-10T09:01:00Z",
          },
        }
      : {},
  }) as TaskView;

describe("gateDrive", () => {
  it("reads an in-flight provisioning run as the platform working", () => {
    expect(gateDrive(gate(1, "Provision resource: db", "running"))).toBe(
      "provisioning",
    );
    // Admitted but not yet dispatched is still nobody's problem but ours.
    expect(gateDrive(gate(1, "Provision resource: db", "queued"))).toBe(
      "provisioning",
    );
  });

  it("reads a gate with no run at all as stalled on a human", () => {
    expect(gateDrive(gate(1, "Provide configuration: stripe"))).toBe("idle");
  });

  // Whatever was driving it is finished and the gate is STILL open, so the next
  // move is a person's — same as never having run.
  it("reads a finished run against a still-open gate as stalled too", () => {
    expect(gateDrive(gate(1, "Provision resource: db", "succeeded"))).toBe(
      "idle",
    );
  });

  it("names a broken provisioning run for what it is", () => {
    expect(gateDrive(gate(1, "Provision resource: db", "failed"))).toBe(
      "failed",
    );
    expect(gateDrive(gate(1, "Provision resource: db", "canceled"))).toBe(
      "failed",
    );
  });
});

describe("runHold", () => {
  const loaded = (over: { gates?: TaskView[]; openWork?: number } = {}) => ({
    gates: [],
    openWork: 3,
    ...over,
  });

  // The bug this whole distinction exists for: a build that is busy writing
  // its milestone used to announce itself as parked on a human.
  it("names the plan window as work in progress, not a hold", () => {
    const hold = runHold(
      run({ state: "planning", milestoneTitle: "v3" }),
      undefined,
    );
    expect(hold?.kind).toBe("planning");
    expect(hold?.tone).toBe("info");
    expect(hold?.title).toBe("Planning v3");
    // It must not claim anything is needed from the user.
    expect(hold?.body).toMatch(/Nothing is held/);
  });

  it("does not need the issue plane to explain planning", () => {
    expect(runHold(run({ state: "planning" }), undefined)).not.toBeNull();
  });

  // The gate story is the PROVISIONING STAGE's now: it sits first on the run's
  // rail, names each connection and says who is acting on it. A notice here as
  // well put two warnings on one page competing to explain one fact.
  it("defers an open gate to the provisioning stage rather than warning twice", () => {
    const hold = runHold(
      run({ state: "waiting" }),
      loaded({
        gates: [
          gate(1, "Provide configuration: stripe"),
          gate(2, "Provision resource: db", "running"),
        ],
      }),
    );
    expect(hold).toBeNull();
  });

  // A RESOLVED gate holds nothing, so it must not silence the run's own reason
  // for standing still.
  it("still explains a park when every gate is resolved", () => {
    const resolved = {
      ...gate(1, "Provide configuration: stripe"),
      derivedStatus: "merged",
    };
    expect(
      runHold(run({ state: "waiting" }), loaded({ gates: [resolved] }))?.kind,
    ).toBe("parked");
  });

  // Gates and an empty milestone are both `waiting`, and the fix for one is
  // nothing like the fix for the other.
  it("tells an empty milestone apart from a gate hold", () => {
    const hold = runHold(run({ state: "waiting" }), loaded({ openWork: 0 }));
    expect(hold?.kind).toBe("no-work");
    expect(hold?.tone).toBe("info");
  });

  it("falls back to the unbounded park when work is dispatchable", () => {
    expect(runHold(run({ state: "waiting" }), loaded())?.kind).toBe("parked");
  });

  // Until the GitHub-backed list lands, every waiting run would look like it
  // had no work — an accusation made from an answer that has not arrived.
  it("stays silent about a waiting run while the issue plane is loading", () => {
    expect(runHold(run({ state: "waiting" }), undefined)).toBeNull();
  });

  it("explains nothing about a run that is moving or over", () => {
    expect(runHold(run({ state: "running" }), loaded())).toBeNull();
    expect(runHold(run({ state: "succeeded" }), loaded())).toBeNull();
    expect(
      runHold(
        run({ state: "failed" }),
        loaded({ gates: [gate(1, "Provide configuration: stripe")] }),
      ),
    ).toBeNull();
  });
});

describe("terminalReasonText", () => {
  it("spells each failure class as a sentence", () => {
    expect(terminalReasonText("no-progress")).toMatch(/closed no issues/);
    expect(terminalReasonText("cycle-ceiling")).toMatch(
      /ceiling on total build sessions/,
    );
  });

  // Both reasons the validating phase can settle under. They are separate
  // sentences because they are separate failures, and the fallback below means a
  // gap here degrades silently to a raw slug rather than to anything noisy.
  it("spells both of the validating phase's reasons", () => {
    expect(terminalReasonText("validation-failed")).toMatch(
      /validation criteria/,
    );
    expect(terminalReasonText("validation-unreported")).toMatch(
      /without committing a report/,
    );
    expect(terminalReasonText("agent-quota-blocked")).toMatch(
      /maximum number of agent runs/,
    );
    expect(terminalReasonText("agent-quota-blocked")).toMatch(
      /Wait for one to finish/,
    );
  });

  it("spells a project with no write target", () => {
    expect(terminalReasonText("no-write-target")).toMatch(/deployment pipeline/);
  });

  it("passes an unmapped reason through so it still reaches the user", () => {
    expect(terminalReasonText("a-reason-from-the-future")).toBe(
      "a-reason-from-the-future",
    );
  });

  it("is empty for a run that has no reason", () => {
    expect(terminalReasonText("")).toBe("");
  });

  // A cycle the pod-truth watcher closed is never re-dispatched, so a run can
  // settle redispatch-budget on its FIRST launch. "Twice" described an attempt
  // nobody made.
  it("counts the dispatches the newest cycle actually had", () => {
    const once = terminalReasonText("redispatch-budget", cycle({ attempts: 1 }));
    expect(once).toBe(
      "The coding agent stopped without opening a pull request after the platform dispatched it once.",
    );
    expect(once).not.toMatch(/twice|budget is spent/);
    expect(terminalReasonText("redispatch-budget", cycle({ attempts: 2 }))).toMatch(
      /dispatched it twice\.$/,
    );
    expect(terminalReasonText("redispatch-budget")).toMatch(/dispatched it once\.$/);
  });

  it("says which agent could not start, and why", () => {
    expect(
      terminalReasonText(
        "agent-start-failed",
        cycle({ agentReason: "startup_failed:Unschedulable: 0/1 nodes are available" }),
      ),
    ).toBe("The coding agent could not start. The cluster had no free CPU or memory for it.");
    expect(
      terminalReasonText(
        "agent-start-failed",
        cycle({ kind: "validation", agentReason: "startup_failed:ImagePullBackOff" }),
      ),
    ).toBe("The validation agent could not start. The cluster could not pull its container image.");
    expect(terminalReasonText("agent-start-failed")).toBe("The agent could not start.");
  });
});

describe("runKindLabel", () => {
  it("names each kind — it is what tells two runs of one milestone apart", () => {
    expect(runKindLabel("dev")).toBe("Spec build");
    expect(runKindLabel("task")).toBe("Incident");
    expect(runKindLabel("validation")).toBe("Revalidation");
  });

  it("shows an unknown kind raw rather than mislabelling it", () => {
    expect(runKindLabel("some-future-kind")).toBe("some-future-kind");
  });
});

describe("runKind", () => {
  it("reads the run's own kind", () => {
    expect(runKind(run({ kind: "task" }))).toBe("task");
  });

  // The whole reason origin is still read: rows admitted before the kind column
  // existed carry one, and they still have to render as what they are.
  it("derives the kind from the origin when the row predates it", () => {
    for (const [origin, kind] of [
      ["spec-build", "dev"],
      ["incident-adoption", "task"],
      ["revalidate", "validation"],
    ]) {
      const legacy = run() as Record<string, unknown>;
      delete legacy.kind;
      legacy.origin = origin;
      expect(runKind(legacy as MilestoneRunView)).toBe(kind);
    }
  });
});

describe("buildOutcome", () => {
  const b = (status: string, completed = true) => ({
    component: "api",
    buildName: "n",
    status,
    completed,
    attempt: 1,
  });

  // The CLUSTER's own reason constants (controller_conditions.go) are what
  // arrive in practice; the bare spellings are the contract's examples and the
  // mock layer's. Recognising only the bare ones read a real red build as green.
  it("classifies the cluster's reason strings and the bare ones alike", () => {
    expect(buildOutcome(b("WorkflowSucceeded"))).toBe("succeeded");
    expect(buildOutcome(b("Succeeded"))).toBe("succeeded");
    expect(buildOutcome(b("WorkflowFailed"))).toBe("failed");
    expect(buildOutcome(b("Failed"))).toBe("failed");
  });

  // The Reason set is OPEN, so an unrecognised one is a fact this console has
  // not learned — not a failure, and emphatically not a success.
  it("calls an unrecognised terminal reason unknown, never a verdict", () => {
    expect(buildOutcome(b("WorkflowTimedOut"))).toBe("unknown");
    expect(buildOutcome(b(""))).toBe("unknown");
  });

  it("has no verdict for a build that has not completed", () => {
    expect(buildOutcome(b("WorkflowSucceeded", false))).toBe("unknown");
  });
});

describe("spentBudgets", () => {
  it("reports nothing for a healthy run — an unspent allowance is not the user's business", () => {
    expect(
      spentBudgets(
        budgets({
          cyclesTotal: 3,
          cycleCeiling: 5,
          fixCycles: 1,
          conflictCycles: 1,
        }),
      ),
    ).toEqual([]);
  });

  it("reports only the budgets at their ceiling", () => {
    const spent = spentBudgets(
      budgets({ cyclesTotal: 8, fixCycles: 2, conflictCycles: 1 }),
    );
    expect(spent.map((b) => b.label)).toEqual([
      "Build sessions",
      "Fix sessions",
    ]);
  });

  it("measures cycles against the run's own snapshotted ceiling, not a hardcoded one", () => {
    const [cycles] = spentBudgets(budgets({ cyclesTotal: 5, cycleCeiling: 5 }));
    expect(cycles?.text).toBe("5 / 5");
    // A run whose ceiling has not been snapshotted yet cannot be "at" it.
    expect(spentBudgets(budgets({ cyclesTotal: 0, cycleCeiling: 0 }))).toEqual(
      [],
    );
  });

  it("never reports build re-triggers — the real guard is per component per SHA", () => {
    const spent = spentBudgets(budgets({ buildRetriggers: 3 }));
    expect(spent.map((b) => b.label)).not.toContain("Build re-triggers");
  });
});

describe("buildSessionLabel", () => {
  it("numbers a build session from 1 and names its kind", () => {
    expect(
      buildSessionLabel(
        { id: "c", kind: "fix", attempts: 1, createdAt: "" },
        2,
      ),
    ).toBe("Build session 3 · fix");
  });
});
