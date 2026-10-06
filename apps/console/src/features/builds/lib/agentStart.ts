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
import { resetStamp } from "../../../lib/resetStamp";

type RunCycleView = components["schemas"]["RunCycleView"];

/**
 * The words for an agent the cluster has not started, or never started.
 *
 * The platform sends the pod's own Kubernetes waiting reason, verbatim: on an
 * open cycle as `startupWait.reason`, and on a cycle the watcher closed inside
 * `agentReason` as `startup_failed:<reason>[: <message>]`. This module owns
 * the plain words for the reasons the console knows, so the waiting notice,
 * the failure card and the run's reason line say one cause one way (lexicon,
 * *An agent that has not started*). A reason it has no words for is shown as
 * the cluster gave it rather than hidden.
 */

/** The cluster has no free CPU or memory: the one cause that is about room. */
const UNSCHEDULABLE = "Unschedulable";

interface CauseWords {
  /** While the agent waits, about "the agent". */
  waiting: string;
  /** After it never started, after a title that already named the agent. */
  failed: string;
}

const CAUSES: Record<string, CauseWords> = {
  [UNSCHEDULABLE]: {
    waiting: "The cluster has no free CPU or memory for the agent right now.",
    failed: "The cluster had no free CPU or memory for it.",
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
): StartupWaitCopy | undefined {
  const wait = cycle?.startupWait;
  if (!cycle || cycle.endedAt || !wait) return undefined;
  const cause =
    CAUSES[wait.reason]?.waiting ?? `The cluster reports the agent as waiting: ${wait.reason}.`;
  return {
    title:
      wait.reason === UNSCHEDULABLE
        ? "Waiting for room in the cluster to start the agent"
        : "Waiting to start the agent",
    body: `${cause} If it has not started by ${resetStamp(wait.failsAt, now)}, this run fails.`,
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

/** What the cluster said, without the platform's `startup_failed:` prefix. */
export function clusterReport(agentReason: string | undefined): string | undefined {
  if (!agentReason?.startsWith(STARTUP_FAILED)) return undefined;
  return agentReason.slice(STARTUP_FAILED.length).trim() || undefined;
}

/** Which agent a cycle ran: validation cycles run the validation agent, every
 *  other kind the coding agent. No cycle at all names neither. */
export function agentNoun(cycle: RunCycleView | undefined): string {
  if (!cycle) return "agent";
  return cycle.kind === "validation" ? "validation agent" : "coding agent";
}
