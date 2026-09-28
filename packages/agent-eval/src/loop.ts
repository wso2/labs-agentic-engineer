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

import type { Verdict } from "./verdict.js";

export interface LoopResult {
  body: string;
  rounds: number;
  best: Verdict;
  history: Verdict[];
}

// The hard iteration cap from the plan's global constraints: unbounded (or
// caller-raised) iteration on a probabilistic system spends the org's key
// with no guarantee of converging. `maxRounds` may only ever LOWER this, so
// it is a ceiling on the caller's input, never a default that a caller can
// raise past it.
const HARD_CAP = 3;

/**
 * Resolves the round cap and validates it in one place, so every rejection
 * path — "too high", "not an integer", "not finite", "below 1" — shares one
 * error shape naming the cap. Silently clamping an over-high request (e.g.
 * `Math.min(n, HARD_CAP)`) would hide a caller's misunderstanding of the
 * contract instead of surfacing it, so this throws instead of coercing.
 */
function resolveMax(maxRounds: number | undefined): number {
  if (maxRounds === undefined) return HARD_CAP;
  if (!Number.isInteger(maxRounds) || maxRounds < 1 || maxRounds > HARD_CAP) {
    throw new Error(
      `runFixLoop: maxRounds must be an integer between 1 and ${HARD_CAP} ` +
        `(the hard iteration cap), got ${maxRounds}`,
    );
  }
  return maxRounds;
}

/**
 * Evaluate, revise, repeat — within bounds that exist because the system is
 * probabilistic and the model calls are on the org's key.
 *
 * Three rules, each earning its place:
 *  - a CAP, because "iterate until it passes" may never converge, and the
 *    cap is a ceiling the caller can only lower, never raise;
 *  - keep the BEST-scoring body, because a revision can make the agent worse
 *    and the last attempt is not automatically the right one to ship. On a
 *    tie the EARLIEST body wins — an equal score is no evidence a later
 *    revision helped, so there is no reason to prefer the more expensive one;
 *  - stop early when a round regresses, because a loop that has started going
 *    backwards has no reason to find its way forward by spending more.
 *
 * The first round always runs (a validated cap is never below 1), so `best`
 * is a real `Verdict` from the first call onward — never a null stand-in for
 * "nothing evaluated yet", which would make a downstream `.overall` read
 * unsound.
 */
export async function runFixLoop(opts: {
  evaluate: () => Promise<Verdict>;
  // Receives the FULL verdict, `ungraded` included, because building the
  // revision prompt is this callback's job, not the loop's. Whoever
  // implements `revise` must feed `failed` into the prompt and leave
  // `ungraded` out of it: a rubric line the grader never returned a verdict
  // on is not something a prompt fix can address, and citing it as an
  // instruction would be citing a guess.
  revise: (verdict: Verdict, body: string) => Promise<string>;
  body: string;
  maxRounds?: number;
}): Promise<LoopResult> {
  const max = resolveMax(opts.maxRounds);
  const history: Verdict[] = [];
  let body = opts.body;
  let bestBody = opts.body;

  let verdict = await opts.evaluate();
  history.push(verdict);
  let best = verdict;

  for (let round = 2; round <= max; round++) {
    if (verdict.passed) break;
    body = await opts.revise(verdict, body);
    verdict = await opts.evaluate();
    history.push(verdict);

    if (verdict.overall > best.overall) {
      best = verdict;
      bestBody = body;
    } else if (verdict.overall < history[history.length - 2]!.overall) {
      // Regressed: keep what was better and stop.
      break;
    }
  }

  return { body: bestBody, rounds: history.length, best, history };
}
