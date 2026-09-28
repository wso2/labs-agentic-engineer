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

// What the runner and the guard plugin agree on, and nothing else.
//
// The plugin runs inside the `opencode` binary's own Bun, in another process,
// so the two halves cannot share a closure — they share these names. The
// runner sets the environment and reads the marker; the plugin reads the
// environment and writes the marker. Imported by both, so a rename is one edit.

/** The environment the runner hands the OpenCode server, for the plugin to read. */
export const GUARD_ENV = {
  /** Absolute project root: authored files outside it are refused. */
  workspace: "AEP_GUARD_WORKSPACE",
  /** A 0600 JSON file holding the run's staged-secret VALUES (a string array). */
  secretsFile: "AEP_GUARD_SECRETS_FILE",
  /** Where the plugin writes its "I loaded" marker at init. */
  readyFile: "AEP_GUARD_READY",
  /** The instructions file named in config: the appendix only the lead keeps. */
  appendixFile: "AEP_GUARD_APPENDIX",
  /** Where the plugin appends one `SessionContextRecord` per session fact (lib/run_context.ts). */
  sessionLog: "AEP_GUARD_SESSION_LOG",
  /** Where the plugin writes its marker when the startup probe reaches the system transform. */
  probeFile: "AEP_GUARD_PROBE_MARKER",
} as const;

/**
 * The text of the runner's startup probe prompt. A session whose message is
 * exactly this is the probe: the plugin marks the system transform firing and
 * refuses the model call, so the probe proves the hook is live and costs nothing.
 */
export const STARTUP_PROBE_PROMPT = "[aep-guard:startup-probe]";

/**
 * The prefix a workspace-guard denial carries in the tool's error text.
 *
 * OpenCode hands a thrown error's message to the model verbatim and puts the
 * same text on the tool part's `state.error`, which is the only place the
 * runner sees the denial at all — the plugin cannot reach the run's feed. The
 * marker is what lets the translator tell "the platform refused this write"
 * from any other failed write, and emit the same `workspace_guard` notice the
 * Claude Code hook raises. Short and bracketed so the sentence after it still
 * reads first-person to the model.
 */
export const WORKSPACE_GUARD_MARKER = "[aep-guard:workspace]";
