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
import { resetStamp } from "../../../lib/stamp";

type RunCycleView = components["schemas"]["RunCycleView"];

/**
 * The words for an agent the cluster has not started, or never started.
 *
 * The platform sends the pod's own Kubernetes waiting reason, verbatim: on an
 * open cycle as `startupWait.reason`, and on a cycle the watcher closed inside
 * `agentReason` as `startup_failed:<reason>[: <message>]`. Before OpenChoreo
 * has applied the agent's Job there is no pod to have a reason, and the
 * platform sends its own instead: `NotYetApplied` while it waits, and
 * `not_applied` when that outlasted its cap. This module owns
 * the plain words for the reasons the console knows, so the waiting notice,
 * the failure card and the run's reason line say one cause one way (lexicon,
 * *An agent that has not started*). A reason it has no words for is shown as
 * the cluster gave it rather than hidden.
 */

/**
 * The scheduler found no node for the pod: no free CPU or memory, or a
 * scheduling rule (taints, affinity, limits) nothing satisfies. The one cause
 * that is about room in the cluster.
 */
const UNSCHEDULABLE = "Unschedulable";

interface CauseWords {
  /** While the agent waits, about "the agent". */
  waiting: string;
  /** After it never started, after a title that already named the agent. */
  failed: string;
}

/**
 * The platform's own reasons, not the cluster's: the agent's Job has not been
 * applied yet. Said in the voice of AE Studio's slow start ("taking longer
 * than usual"), and never as "the cluster reported".
 */
const PLATFORM_PREPARING: CauseWords = {
  waiting: "The platform is still preparing the agent.",
  failed: "The platform did not start it within 30 minutes.",
};
const NOT_APPLIED = "not_applied";
/** The platform has not applied the agent's Job yet: on Cloud the normal path for 8-13 minutes. */
const NOT_YET_APPLIED = "NotYetApplied";

const CAUSES: Record<string, CauseWords> = {
  [NOT_YET_APPLIED]: PLATFORM_PREPARING,
  [NOT_APPLIED]: PLATFORM_PREPARING,
  [UNSCHEDULABLE]: {
    waiting: "The cluster has no room for the agent right now (CPU, memory or a scheduling rule).",
    failed: "The cluster had no room for it (CPU, memory or a scheduling rule).",
  },
  ImagePullBackOff: {
    waiting: "The cluster cannot pull the agent's container image.",
    failed: "The cluster could not pull its container image.",
  },
  ErrImagePull: {
    waiting: "The cluster cannot pull the agent's container image.",
    failed: "The cluster could not pull its container image.",
  },
  CreateContainerConfigError: {
    waiting: "A secret or setting the agent needs is not ready yet.",
    failed: "A secret or setting it needed was not ready.",
  },
};

/** The `agentReason` prefix of a cycle whose agent never started. */
const STARTUP_FAILED = "startup_failed:";

export interface StartupWaitCopy {
  title: string;
  body: string;
}

/** How the waiting notice reads: `neutral` is the platform's normal wait, `warning` a stuck cause. */
export interface StartupWaitNoticeCopy extends StartupWaitCopy {
  tone: "neutral" | "warning";
}


/**
 * The notice for an OPEN cycle whose agent the cluster has not started, or
 * `undefined` when nothing holds it up. The deadline is the platform's own
 * (`failsAt`), never a guess: past it the cycle closes and the run fails.
 *
 * `now` decides whether the deadline needs its date; tests pin it.
 */
export function startupWaitNotice(
  cycle: RunCycleView | undefined,
  now: Date = new Date(),
): StartupWaitNoticeCopy | undefined {
  const wait = cycle?.startupWait;
  if (!cycle || cycle.endedAt || !wait) return undefined;
  // Healthy and expected, so no deadline line and no warning: only a cause the
  // cluster reported can need a person.
  if (wait.reason === NOT_YET_APPLIED) {
    return { title: "Preparing the agent", body: PLATFORM_PREPARING.waiting, tone: "neutral" };
  }
  const cause =
    CAUSES[wait.reason]?.waiting ?? `The cluster reports the agent as waiting: ${wait.reason}.`;
  return {
    title:
      wait.reason === UNSCHEDULABLE
        ? "Waiting for room in the cluster to start the agent"
        : "Waiting to start the agent",
    body: `${cause} If it has not started by ${resetStamp(wait.failsAt, now)}, this run fails.`,
    tone: "warning",
  };
}

/** The Kubernetes reason inside a `startup_failed:` agent reason, if it is one. */
function startupFailureReason(agentReason: string | undefined): string | undefined {
  if (!agentReason?.startsWith(STARTUP_FAILED)) return undefined;
  const rest = agentReason.slice(STARTUP_FAILED.length);
  const end = rest.indexOf(":");
  return (end < 0 ? rest : rest.slice(0, end)).trim() || undefined;
}

/** Why a cycle's agent never started, as one sentence; `undefined` for any other stop. */
export function startupFailureCause(agentReason: string | undefined): string | undefined {
  const reason = startupFailureReason(agentReason);
  if (!reason) return undefined;
  return CAUSES[reason]?.failed ?? `The cluster reported ${reason}.`;
}

/** Is the cause a matter of room, which frees up without anyone fixing anything? */
export function isRoomShortage(agentReason: string | undefined): boolean {
  return startupFailureReason(agentReason) === UNSCHEDULABLE;
}

/** What the cluster said, without the platform's `startup_failed:` prefix;
 *  nothing when the reason is the platform's own (`not_applied`). */
export function clusterReport(agentReason: string | undefined): string | undefined {
  if (!agentReason?.startsWith(STARTUP_FAILED)) return undefined;
  if (startupFailureReason(agentReason) === NOT_APPLIED) return undefined;
  return agentReason.slice(STARTUP_FAILED.length).trim() || undefined;
}

/** Which agent a cycle ran: validation cycles run the validation agent, every
 *  other kind the coding agent. No cycle at all names neither. */
export function agentNoun(cycle: RunCycleView | undefined): string {
  if (!cycle) return "agent";
  return cycle.kind === "validation" ? "validation agent" : "coding agent";
}

/** Is this the reason the platform gives a cycle whose agent never started? */
export function isStartupFailure(agentReason: string | undefined): boolean {
  return startupFailureReason(agentReason) !== undefined;
}

/**
 * "<who>: <text>." as one sentence. The text is the cluster's or the runner's
 * own words, which may already end the sentence (a scheduler message ends in
 * "."), so a period is added only when it does not.
 */
export function reportSentence(who: string, text: string): string {
  const said = text.trim();
  return `${who}: ${said}${/[.!?]$/.test(said) ? "" : "."}`;
}

/** What a coding agent that never started did not do, naming a pull request an earlier session of the run opened. */
function noPullRequestSentence(earlier: readonly RunCycleView[]): string {
  const opened = [...earlier].reverse().find((c) => c.prNumber !== undefined);
  if (!opened) return "Nothing ran; no pull request was opened.";
  return `Nothing ran this time, so no new pull request was opened; #${opened.prNumber}, opened earlier in this build, is unchanged.`;
}

/**
 * Why the newest cycle's agent never started, what that left undone, and what
 * trying again does. `retry` names how, in the words of the button on the
 * caller's screen (the Build card's Retry; the Validation card's label from
 * `validateLabel`), so the copy never names a button that is not there.
 * `earlier` are the run's cycles before it.
 */
export function agentStartFailedCopy(
  cycle: RunCycleView | undefined,
  earlier: readonly RunCycleView[],
  retry: string,
): StartupWaitCopy {
  const validating = cycle?.kind === "validation";
  const cause = startupFailureCause(cycle?.agentReason);
  const notDone = validating ? "Nothing ran; the version was not validated." : noPullRequestSentence(earlier);
  const when = isRoomShortage(cycle?.agentReason)
    ? "once the cluster has room"
    : "once that is fixed";
  const retryLine =
    startupFailureReason(cycle?.agentReason) === NOT_APPLIED
      ? `${retry} starts a new attempt.`
      : `${retry} ${when}.`;
  const reported = clusterReport(cycle?.agentReason);
  return {
    title: `The ${agentNoun(cycle)} could not start`,
    body: [cause, notDone, retryLine, reported ? reportSentence("The cluster reported", reported) : undefined]
      .filter(Boolean)
      .join(" "),
  };
}

