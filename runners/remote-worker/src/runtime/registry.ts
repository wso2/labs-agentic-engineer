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
import { createOpencodeRuntime } from "./opencode/runtime.js";

/**
 * The runtime named by `name`.
 *
 * Total for the runtimes this build can run and explicit for anything else — a
 * silent fallback to Claude Code would bill an organization for a runtime it
 * did not choose, and the org would never learn it had not got one.
 */
export function createRuntime(name: RuntimeName = DEFAULT_RUNTIME): Runtime {
  switch (name) {
    case "claude-code":
      return createClaudeCodeRuntime();
    case "opencode":
      return createOpencodeRuntime();
    default:
      // Unreachable while `RuntimeName` is closed; kept so a widened union is a
      // compile error at the switch rather than a runtime surprise in a pod.
      throw new UnsupportedRuntimeError(name, "unknown runtime");
  }
}

/**
 * An organization setting the dispatcher stamps as env (`AEP_AGENT_MODEL`),
 * trimmed; unset or blank means `fallback`.
 *
 * `fallback` is the platform default, and it is the caller's to name rather than
 * this module's: the model a runtime should default to is the runtime's fact
 * (`Runtime.defaultModel`), not the registry's.
 */
export function envOr(
  key: "AEP_AGENT_MODEL",
  fallback: string,
  env: NodeJS.ProcessEnv = process.env,
): string {
  const raw = (env[key] ?? "").trim();
  return raw === "" ? fallback : raw;
}
