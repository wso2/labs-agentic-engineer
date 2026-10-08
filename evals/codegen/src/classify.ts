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

/**
 * WHOSE FAILURE an attempt's phase was, decided from structured facts — the
 * rules `attempt.ts` applies.
 *
 *   app          — the generated app (or the coding agent that wrote it)
 *                  failed. A hard fail: score 0, counted.
 *   environment  — anything else: docker, the runner, the model provider,
 *                  `wire`'s machine, the browser, the harness. A harness
 *                  error: reported with its reason, excluded from statistics.
 */

import type { WireCause, WireFailure } from "@aep/playground/src/engine/wire/failure.js";
import type { RunSettled } from "./metrics.js";

export type FailureCause = WireCause;
export type Classified = WireFailure;

export interface CodingFacts {
  /** The harness killed it at the phase limit. */
  timedOut: boolean;
  limitMinutes: number;
  exitCode: number | null;
  /** Events in the progress feed. */
  events: number;
  /** A `run_started` arrived: the agent's session began. */
  agentStarted: boolean;
  settled: RunSettled | null;
  /** Some component's App Path exists. */
  builtAnything: boolean;
}

/** The coding phase's failure; first matching rule wins. */
export function codingFailure(facts: CodingFacts): Classified | null {
  const exit = `exit ${String(facts.exitCode)}`;
  if (!facts.agentStarted) {
    const why = facts.settled?.error ?? (facts.events === 0 ? "nothing reached the progress feed" : "it settled before the agent's session began");
    return { cause: "environment", reason: `the coding agent never started (${exit}): ${why}` };
  }
  if (facts.settled?.code === "provider_limit") {
    return { cause: "environment", reason: `the model provider refused the run's calls (provider_limit)${facts.settled.error ? `: ${facts.settled.error}` : ""}` };
  }
  if (facts.timedOut) return { cause: "app", reason: `coding run timed out after ${String(facts.limitMinutes)} min` };
  if (!facts.settled) {
    return { cause: "environment", reason: `the coding run ended without settling (${exit}) — its process or container died` };
  }
  if (facts.settled.outcome === "cancelled") return { cause: "environment", reason: "the coding run was cancelled" };
  if (facts.settled.outcome !== "success") {
    return { cause: "app", reason: `coding run outcome: ${facts.settled.outcome}${facts.settled.error ? ` — ${facts.settled.error}` : ""}` };
  }
  if (!facts.builtAnything) return { cause: "app", reason: "the coding run produced no component directory" };
  return null;
}

/** How waiting for `wire`'s `READY` ended (`waitForLine`), and what it said on the way. */
export interface WireFacts {
  ended: "timeout" | "exited";
  exitCode: number | null;
  /** Its `FAILED <cause> <reason>` line, when it printed one. */
  failed: WireFailure | null;
  limitMinutes: number;
  /** Its last lines, for a reason that has nothing better. */
  tail: string;
}

/**
 * The wire phase, when no `READY` came. An unclassified wire end is the
 * environment's: it is no evidence against the app.
 */
export function wireFailure(facts: WireFacts): Classified {
  if (facts.failed) return facts.failed;
  if (facts.ended === "timeout") {
    return { cause: "environment", reason: `no READY within ${String(facts.limitMinutes)} min and no FAILED line — ${facts.tail}` };
  }
  return { cause: "environment", reason: `wire exited (code ${String(facts.exitCode)}) without classifying the failure — ${facts.tail}` };
}
