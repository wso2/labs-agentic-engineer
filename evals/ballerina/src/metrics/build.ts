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
 * The OUTCOME half: what the compiler said, and which errors `bal library` was
 * supposed to prevent.
 *
 * The split is the whole point of this module. A run that fumbles a tuple
 * destructuring and a run that invents a field on `s3:ConnectionConfig` both
 * report "compile errors", and only the second one is evidence about the tool.
 * Measured: the 2026-08-15 playground run had 10 errors and zero signature
 * errors, which a blended count would have read as a 10x regression against a
 * run that had one.
 */

import { BUILD_METRICS } from "../config.js";

/** Claims about another package's API — the class `bal library` exists to prevent. */
const SIGNATURE_ERROR = new RegExp(BUILD_METRICS.signaturePatterns.join("|"), "i");
const FOREIGN_COORDINATE = BUILD_METRICS.foreignCoordinate;

export interface BuildAttempt {
  /** `bal build` exit code. */
  exitCode: number;
  /** Every `ERROR [...]` line's message, in order. */
  errors: string[];
  /** The subset that claims something about another package's API. */
  signatureErrors: string[];
}

export interface BuildMetrics {
  /** How many times `bal build` ran before the session stopped. */
  cycles: number;
  /** The last attempt exited 0. */
  green: boolean;
  /** Errors on the FIRST attempt — what the agent wrote before any feedback. */
  firstAttemptErrors: number;
  firstAttemptSignatureErrors: number;
  /** Across every attempt. */
  totalErrors: number;
  totalSignatureErrors: number;
}

/**
 * Split one build's output.
 *
 * Reads `ERROR [file:(l:c,l:c)] message` lines. Warnings and HINTs are ignored
 * on purpose: a run is green with hundreds of `concurrent calls will not be
 * made` hints, and counting them would swamp the signal.
 */
export function readBuildAttempt(output: string, exitCode: number): BuildAttempt {
  const errors: string[] = [];
  for (const line of output.split("\n")) {
    const match = /^ERROR \[[^\]]*\]\s*(.*)$/.exec(line.trim());
    if (match?.[1]) errors.push(match[1].trim());
  }
  return {
    exitCode,
    errors,
    signatureErrors: errors.filter(isSignatureError),
  };
}

export function isSignatureError(message: string): boolean {
  if (FOREIGN_COORDINATE.test(message)) return true;
  return SIGNATURE_ERROR.test(message);
}

export function summarize(attempts: BuildAttempt[]): BuildMetrics {
  const first = attempts[0];
  const last = attempts[attempts.length - 1];
  return {
    cycles: attempts.length,
    // No attempt at all is NOT green. A session that never ran `bal build`
    // produced unverified code, and reporting that as a pass is the one
    // outcome this harness must never do.
    green: last !== undefined && last.exitCode === 0,
    firstAttemptErrors: first?.errors.length ?? 0,
    firstAttemptSignatureErrors: first?.signatureErrors.length ?? 0,
    totalErrors: attempts.reduce((n, a) => n + a.errors.length, 0),
    totalSignatureErrors: attempts.reduce((n, a) => n + a.signatureErrors.length, 0),
  };
}

/**
 * Recover the agent's own `bal build` runs from the transcript.
 *
 * The harness runs its OWN verifying build afterwards (`verify.ts`), but the
 * agent's attempts are the interesting ones: they say how many rounds of
 * compiler feedback it needed, which is the number the tool is supposed to
 * drive down. An exit code is rarely in the output, so a build is counted as
 * failed when it printed an ERROR line — which is what `error: compilation
 * contains errors` accompanies.
 */
export function readAgentBuilds(commands: { command: string; output: string }[]): BuildAttempt[] {
  const out: BuildAttempt[] = [];
  for (const { command, output } of commands) {
    if (!/\bbal build\b/.test(command)) continue;
    const attempt = readBuildAttempt(output, /^ERROR \[/m.test(output) ? 1 : 0);
    out.push(attempt);
  }
  return out;
}
