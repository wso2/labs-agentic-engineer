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

import type { components } from "../../../generated/aep-api";
import { startupFailureCause, startupWaitNotice } from "./agentStart";
import { buildOutcome, buildsAreSettled } from "./runView";
import type { SpineStage } from "./stage";

type RunCycleView = components["schemas"]["RunCycleView"];
type TaskView = components["schemas"]["TaskView"];
type CycleBuild = components["schemas"]["CycleBuild"];

// THE BUILD-SESSION SPINE — one build session as the sequence it actually is:
// the agent writes code, opens a pull request, the platform merges it, the merge
// fans out to builds, and a green build deploys itself.
//
// Every applicable stage exists from the moment the session does. That is the
// whole point: the surface this replaced drew a stage only AFTER it happened
// (the build list returned nothing before a merge SHA), so everything between
// "the agent pushed" and "builds exist" was rendered nowhere, and a session
// stalled on a declined merge looked exactly like one still thinking.
//
// A stage that has not been reached says WHAT IT WAITS FOR. A stage that went
// wrong says so in the platform's own recorded words — `mergeReason` — rather
// than leaving the reader to infer a stall from an absence.
//
// Every derivation here is pure and reads only facts the platform actually
// learned: the cycle row (webhook-fed), the milestone's issues, and the cluster
// read for builds. Nothing is inferred from elapsed time.

/**
 * The issues a build session worked, with how the console knows.
 *
 * `exact` is the difference between a fact and a good guess. Once a pull request
 * exists the platform has recorded the merge policy's matched set, which is
 * precisely what the merge closes. Before that, the only honest answer is the
 * milestone's open agent-work — right in the common case, wrong if the working
 * set changes mid-session — so the caption says which one the reader is looking
 * at rather than presenting both as the same claim.
 */
export interface SessionIssues {
  issues: TaskView[];
  exact: boolean;
  caption: string;
}

export interface SessionFacts {
  cycle: RunCycleView;
  /** The milestone's agent-work issues (open and closed), or [] while loading. */
  work: TaskView[];
  /** This session's builds, or undefined when the read has not answered. */
  builds: CycleBuild[] | undefined;
}

/** Short SHA, matching how the platform logs one. */
function shortSha(sha: string): string {
  return sha.slice(0, 8);
}

/**
 * The issues this session worked. Exact from `resolves`, otherwise the
 * milestone's open agent work — and never a mix of the two, because a caption
 * can only describe one of them truthfully.
 */
export function sessionIssues(cycle: RunCycleView, work: TaskView[]): SessionIssues {
  const resolves = cycle.resolves ?? [];
  if (resolves.length > 0) {
    const claimed = new Set(resolves);
    const issues = work.filter((task) => claimed.has(task.issueNumber));
    return {
      issues,
      exact: true,
      caption: cycle.prNumber
        ? `Resolved by pull request #${cycle.prNumber}`
        : "Claimed by this session's pull request",
    };
  }
  // A closed session that never recorded a matched set worked nothing this
  // console can name: its agent died before opening a pull request, or it
  // predates the record. Guessing from what is open NOW would attribute later
  // work to it.
  if (cycle.endedAt) {
    return { issues: [], exact: true, caption: "" };
  }
  return {
    issues: work.filter((task) => task.derivedStatus === "pending"),
    exact: false,
    caption: "The milestone's open work — this session's own set is recorded once it opens a pull request",
  };
}

/** The coding agent's stage: the runner Job, up to the pull request it opens. */
function agentStage(cycle: RunCycleView): SpineStage {
  const stage: SpineStage = {
    id: "agent",
    name: "Coding agent",
    actor: "runner",
    state: "active",
    note: "Reading the milestone's issues and writing code.",
    // The per-session re-dispatch budget: 2 dispatches, reset at every session
    // boundary. A second attempt means the FIRST agent died, which is a fact
    // about this stage — so it rides the stage rather than a chip in a session
    // header that the flat rail no longer has.
    ...(cycle.attempts > 1 && { fact: `attempt ${cycle.attempts}/2` }),
  };
  if (cycle.prNumber) {
    // The Job exits the moment it opens its pull request, so a PR IS the
    // agent's exit — whatever the cycle row says about being open.
    return { ...stage, state: "done", note: "Finished and opened its pull request." };
  }
  if (cycle.endedAt) {
    // The watcher closed it before its pod ever ran: it did not "end", it
    // never began, and the cause is the cluster's.
    const cause = startupFailureCause(cycle.agentReason);
    if (cause) return { ...stage, state: "failed", note: `Could not start. ${cause}` };
    return {
      ...stage,
      state: "failed",
      note:
        cycle.attempts > 1
          ? "Ended without opening a pull request, on its second attempt — the per-session re-dispatch budget is spent."
          : "Ended without opening a pull request.",
    };
  }
  if (cycle.attempts === 0) {
    return { ...stage, state: "waiting", note: "Waiting for its runner Job to be dispatched." };
  }
  // Dispatched, but the cluster has not started the pod: "writing code" would
  // be false for the whole startup grace. The run card's notice says why.
  const wait = startupWaitNotice(cycle);
  if (wait) return { ...stage, state: "waiting", note: `${wait.title}.` };
  return stage;
}

/** The pull request the agent opens — the handover from agent to platform. */
function prStage(cycle: RunCycleView): SpineStage {
  const stage: SpineStage = { id: "pr", name: "Pull request", actor: "agent", state: "waiting", note: "" };
  if (!cycle.prNumber) {
    return {
      ...stage,
      note: cycle.endedAt
        ? "None was opened, so this session landed nothing."
        : "The agent opens one when its work builds.",
    };
  }
  const fact = `#${cycle.prNumber}`;
  // The host's own link, as the webhook reported it. A cycle recorded before the
  // platform learned to keep it has the number and no URL, so the link is
  // conditional and the number is not.
  const href = cycle.prUrl ? { factHref: cycle.prUrl } : {};
  if (cycle.prDraft) {
    // The one stall with no deadline behind it: the merge policy never runs on a
    // draft, so the session waits until the agent marks it ready.
    return {
      ...stage,
      state: "attention",
      fact,
      ...href,
      note: "Still a draft — the agent has not marked it ready, and nothing merges while it is one.",
    };
  }
  return {
    ...stage,
    state: "done",
    fact,
    ...href,
    note: "Open and ready, claiming this session's issues.",
  };
}

/** The auto-merge policy's stage — the platform's decision, and its outcome. */
function mergeStage(cycle: RunCycleView): SpineStage {
  const stage: SpineStage = { id: "merge", name: "Merge", actor: "platform", state: "waiting", note: "" };
  if (cycle.mergeSha) {
    // A merge SHA is the merge. A stale verdict from an earlier decline on the
    // same pull request is not consulted — see the contract note on mergeVerdict.
    return {
      ...stage,
      state: "done",
      fact: shortSha(cycle.mergeSha),
      note: "Squash-merged into main, closing the issues it claimed.",
    };
  }
  if (cycle.mergeVerdict === "refused") {
    return {
      ...stage,
      state: "failed",
      note: cycle.mergeReason
        ? `Refused: ${cycle.mergeReason}. A conflict issue is filed, and the next build session rebases.`
        : "Refused by GitHub. A conflict issue is filed, and the next build session rebases.",
    };
  }
  if (cycle.mergeVerdict === "declined") {
    return {
      ...stage,
      state: "attention",
      note: cycle.mergeReason
        ? `Not merged — ${cycle.mergeReason}. This pull request is left for a human.`
        : "Not merged: the auto-merge policy declined it, so it is left for a human.",
    };
  }
  if (cycle.prNumber && !cycle.prDraft) {
    return { ...stage, state: "active", note: "Deciding whether the pull request is this run's work." };
  }
  return { ...stage, note: "Runs as soon as a pull request is ready." };
}

/** The build fan-out one merge causes, one build per component the diff touched. */
function buildsStage(cycle: RunCycleView, builds: CycleBuild[] | undefined): SpineStage {
  const stage: SpineStage = { id: "builds", name: "Builds", actor: "platform", state: "waiting", note: "" };
  if (!cycle.mergeSha) {
    return { ...stage, note: "The merge is what triggers them — one per component the diff touches." };
  }
  if (builds === undefined) {
    return { ...stage, state: "active", note: "Reading this merge's builds…" };
  }
  if (builds.length === 0) {
    return { ...stage, state: "active", note: "The merge landed; its builds have not appeared in the cluster yet." };
  }
  const red = builds.filter((build) => buildOutcome(build) === "failed");
  if (red.length > 0 && buildsAreSettled(builds)) {
    return {
      ...stage,
      state: "failed",
      fact: `${red.length} of ${builds.length} red`,
      note: `${red.map((build) => build.component).join(", ")} did not go green. A red build is re-triggered once, then a fix issue comes back into the milestone.`,
    };
  }
  if (!buildsAreSettled(builds)) {
    return { ...stage, state: "active", fact: `${builds.length}`, note: "Building each component the merge touched." };
  }
  // Settled, nothing red — but "not red" is not the same as "green". A terminal
  // Reason this console cannot classify must not be counted as a success, or the
  // deployment stage below would claim a rollout on the strength of a string
  // nobody recognised.
  const green = builds.filter((build) => buildOutcome(build) === "succeeded");
  if (green.length < builds.length) {
    const odd = builds.filter((build) => buildOutcome(build) === "unknown");
    return {
      ...stage,
      state: "attention",
      fact: `${green.length} of ${builds.length} green`,
      note: `${odd
        .map((build) => `${build.component} finished as ${build.status || "unknown"}`)
        .join(", ")} — an outcome this console does not recognise. The build's own log says what happened.`,
    };
  }
  return {
    ...stage,
    state: "done",
    fact: `${builds.length} green`,
    note: "Every component the merge touched built cleanly.",
  };
}

/**
 * Deployment — the honest end of a session.
 *
 * It reads NOTHING from the cluster. A Deployment carries no commit or image, so
 * no rollout can be attributed to the merge that caused it; chips here would
 * show a later session's rollout under an earlier session. What this stage owns
 * is the CONSEQUENCE — components carry auto-deploy, so a green build deploys
 * itself — plus the way to the board that does know.
 */
function deployStage(builds: SpineStage): SpineStage {
  const stage: SpineStage = { id: "deploy", name: "Deployment", actor: "cluster", state: "waiting", note: "" };
  switch (builds.state) {
    case "done":
      return {
        ...stage,
        state: "done",
        note: "Components carry auto-deploy, so this merge rolls out on its own.",
      };
    case "failed":
      return {
        ...stage,
        state: "failed",
        note: "Nothing deploys from this build session — a component did not go green.",
      };
    case "attention":
      // The builds settled on an outcome the console cannot classify, so it can
      // neither promise a rollout nor report a failure.
      return {
        ...stage,
        state: "attention",
        note: "Whether this merge rolled out depends on a build outcome the console could not read.",
      };
    default:
      return { ...stage, note: "A green build deploys itself." };
  }
}

/**
 * How many stages one build session contributes to the rail. It is a CONSTANT,
 * which is what lets the rail number every stage straight through without first
 * fetching each session's cluster-derived facts: step numbers are assigned from
 * the session count alone. `sessionStages` is asserted against it.
 */
export const SESSION_STAGE_COUNT = 5;

/** One build session's whole spine, in the order it happens. */
export function sessionStages(facts: SessionFacts): SpineStage[] {
  const builds = buildsStage(facts.cycle, facts.builds);
  return [agentStage(facts.cycle), prStage(facts.cycle), mergeStage(facts.cycle), builds, deployStage(builds)];
}
