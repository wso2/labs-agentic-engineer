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

// Routes the runner's console output into the progress feed, scrubbed.
//
// `console.*` is not a private debug channel here: the BFF tails the agent
// pod's stdout/stderr and forwards every line that isn't a progress NDJSON
// envelope into the console build log as a `log` event (see
// delivery/codingagent/agent_progress.go). So console is as user-facing as
// emit() and needs the same redaction.
//
// It is also on the SAME file descriptor as the NDJSON feed, which is why this
// converts rather than merely scrubs: a bare line on that fd makes the stream
// not-NDJSON, so a strict consumer breaks on it and a watchdog cannot parse the
// feed it is supposed to be watching. Emitting a typed `notice` instead gives
// every line the same envelope, version, ts and seq as the rest. The notice
// carries no `code`: that field names closed CONDITIONS a consumer branches on,
// and arbitrary console output is not one — it is prose for a reader, which is
// what `detail` is for.
// The BFF's raw-line fallback stays as a safety net for output that never went
// through console at all (a dependency writing to process.stdout directly).
//
// This wraps the console methods once at process entry rather than asking each
// call site to remember, which also covers output we don't author — the Agent
// SDK, git's own stderr as relayed by child_process errors, and any dependency
// that logs. Scrubbing is applied per call by emit(), so literals enrolled later
// (the git token, minted mid-run) still redact earlier-wrapped methods.

import { format } from "node:util";
import { emit, primeScrubber } from "./emitter.js";
import { MIN_LITERAL_LEN } from "./scrubber.js";
import { scanCredentialEnv } from "../credential_env.js";

type ConsoleMethod = "log" | "info" | "warn" | "error" | "debug";

const METHODS: readonly ConsoleMethod[] = ["log", "info", "warn", "error", "debug"];

export type ConsoleLike = Pick<Console, ConsoleMethod>;

// Wrapping the same console twice would emit twice — and it would stack a
// wrapper per call in tests.
const wrapped = new WeakSet<object>();

// console's five levels collapse to the feed's three. `debug` joins `info`
// rather than being dropped: the runner logs its provisioning trail there, and
// a level that silently discards output is worse than a noisy feed.
const LEVELS: Record<ConsoleMethod, "info" | "warn" | "error"> = {
  log: "info",
  info: "info",
  debug: "info",
  warn: "warn",
  error: "error",
};

/**
 * The whole log-safety install, in one call — for every entrypoint.
 *
 * Two steps that only work together: wrap console so output reaches the feed as
 * scrubbed events, and ENROLL the mounted credentials so the scrubber has
 * literals to match. Wrapping without enrolling is what shipped: the feed was
 * well-formed and the git credential went through it intact, because shape
 * patterns cover only the well-known GitHub prefixes.
 *
 * One function rather than two calls per entrypoint, for the same reason
 * `requireWorkflowBodies` sits inside `runClaudeQuery`: a third entrypoint
 * cannot then forget half of it. Ordering is fixed here too — enrolling after
 * the first line is logged is a race nobody should have to remember.
 */
export function installLogRedaction(
  target: ConsoleLike = console,
  env: NodeJS.ProcessEnv = process.env,
): void {
  installConsoleScrubber(target);
  const { values, tooShort } = scanCredentialEnv(env);
  primeScrubber(values);
  if (tooShort.length > 0) {
    // NAMES only, and deliberately not fatal. A value this short cannot be
    // enrolled without the literal shredding ordinary log text, so the honest
    // outcome is an unprotected credential the operator is TOLD about — a
    // misconfiguration is not itself a disclosure, and failing a whole cycle
    // over a placeholder in a local run would be the worse trade. Routed
    // through `target` so it lands on the feed like every other line.
    target.warn(
      `[redaction] mounted credential(s) under ${MIN_LITERAL_LEN} chars cannot be enrolled; ` +
        `their values will NOT be redacted from this log: ${tooShort.join(", ")}`,
    );
  }
}

export function installConsoleScrubber(target: ConsoleLike = console): void {
  if (wrapped.has(target)) return;
  wrapped.add(target);
  for (const method of METHODS) {
    // util.format reproduces console's own rendering, including printf-style
    // specifiers and Error stacks, so nothing is lost by collapsing the args
    // to one string.
    target[method] = (...args: unknown[]): void => {
      emit({ kind: "notice", level: LEVELS[method], detail: format(...args) });
    };
  }
}
