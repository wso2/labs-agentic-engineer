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

// Which runtime this pod runs, and what happens when it is asked for one that
// does not exist here.
//
// The organization's Runtime setting reaches the pod as `AEP_AGENT_RUNTIME`, so
// this is the one place a STRING from outside becomes a `Runtime`. It is
// deliberately small and deliberately loud: an org that somehow names a runtime
// this build cannot run gets a refusal that says which runtime and why, not a
// session quietly started on a different one.

import { DEFAULT_RUNTIME, UnsupportedRuntimeError, type Runtime, type RuntimeName } from "./port.js";
import { createClaudeCodeRuntime } from "./claude/runtime.js";

/**
 * Why `opencode` is a name in the contract and not an implementation here.
 *
 * The design (rev 6) requires three live spikes before an OpenCode adapter is
 * worth merging, and none has been run:
 *
 *   1. TOOL AND PERMISSION PARITY — can every `DeniedCapability` and every guard
 *      in `RuntimePolicy` actually be enforced, pre-dispatch, by OpenCode's own
 *      hook or permission mechanism? The three guards this platform relies on
 *      are PreToolUse denials, and the Claude Code spike that established that
 *      shape ALSO established that the obvious mechanism (`canUseTool`) is never
 *      invoked for a server-executed tool. There is no reason to assume the
 *      second runtime's obvious mechanism is the right one either.
 *   2. MESSAGE-STREAM FIDELITY — run events v2 needs an agent's birth
 *      certificate: a declared start carrying the agent's id, its depth, and the
 *      call that spawned it. Claude Code emits `task_started`; nothing says
 *      OpenCode declares the same facts, and without them the crew view goes
 *      back to inferring a tree, which is the v1 failure the whole contract
 *      exists to end.
 *   3. USAGE AND COST — the platform stamps a cost from per-model token counts
 *      reported cumulatively per turn. A runtime that reports usage differently
 *      (or not at all) does not simply lose a number; `modelcost.SumCost` is
 *      all-or-nothing, so it blanks the whole cycle's cost.
 *
 * Writing an adapter before those answers would be guessing in three places at
 * once, and each guess fails silently — an unenforced guard, an inferred tree, a
 * missing cost. So this refuses by name, with the reason, and the port stands
 * ready for the day the spikes are done.
 */
const OPENCODE_NOT_IMPLEMENTED =
  "this build ships no OpenCode adapter. The runtime port is in place, but three spikes are " +
  "owed first — pre-dispatch tool/permission parity for the workspace, WebSearch and WebFetch " +
  "guards; whether the message stream declares an agent's id, depth and parent; and how per-model " +
  "usage is reported for cost stamping. Until those are run, select Claude Code.";

/**
 * The runtime named by `name`.
 *
 * Total for the runtimes this build can run and explicit for the ones it cannot
 * — a silent fallback to Claude Code would bill an organization for a runtime it
 * did not choose, and an org that picked OpenCode would never learn it had not
 * got one.
 */
export function createRuntime(name: RuntimeName = DEFAULT_RUNTIME): Runtime {
  switch (name) {
    case "claude-code":
      return createClaudeCodeRuntime();
    case "opencode":
      throw new UnsupportedRuntimeError(name, OPENCODE_NOT_IMPLEMENTED);
    default:
      // Unreachable while `RuntimeName` is closed; kept so a widened union is a
      // compile error at the switch rather than a runtime surprise in a pod.
      throw new UnsupportedRuntimeError(name, "unknown runtime");
  }
}

/**
 * `AEP_AGENT_RUNTIME`, as the dispatcher stamps it from the organization's
 * setting.
 *
 * Unset means the platform default, which is what every dispatch made before the
 * setting existed carries — so an org that never opens the page keeps exactly
 * the run it had. An unrecognised VALUE is not defaulted: the org asked for
 * something, and quietly giving it something else is the failure mode this
 * whole function exists to prevent.
 */
export function runtimeNameFromEnv(env: NodeJS.ProcessEnv = process.env): RuntimeName {
  const raw = (env.AEP_AGENT_RUNTIME ?? "").trim();
  if (raw === "") return DEFAULT_RUNTIME;
  if (raw === "claude-code" || raw === "opencode") return raw;
  throw new UnsupportedRuntimeError(raw, "no runtime by that name exists");
}

/**
 * `AEP_AGENT_MODEL`, as the dispatcher stamps it from the organization's
 * setting.
 *
 * `fallback` is the platform default, and it is the caller's to name rather than
 * this module's: the model a runtime should default to is the runtime's fact,
 * not the registry's.
 */
export function modelFromEnv(fallback: string, env: NodeJS.ProcessEnv = process.env): string {
  const raw = (env.AEP_AGENT_MODEL ?? "").trim();
  return raw === "" ? fallback : raw;
}
