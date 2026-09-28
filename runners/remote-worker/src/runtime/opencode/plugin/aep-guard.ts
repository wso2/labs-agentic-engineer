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

// The AEP guard, as an OpenCode plugin: the workspace write guard, the
// WebSearch DLP guard and the WebFetch SSRF + secret guard, enforced through
// `tool.execute.before`.
//
// NOT imported by the runner. `plugin/build.ts` bundles this file (and the
// platform modules it reaches) into ONE dependency-free `index.js` beside a
// `package.json {"type": "module", "main": "index.js"}`, and the image ships
// that DIRECTORY at /app/runtime/opencode/aep-guard. Two packaging facts are
// measured and both fail silently (spikes S1/S1b):
//
//   - the binary imports a plugin with its own Bun and cannot see the runner's
//     node_modules, so the bundle carries everything it needs;
//   - a bare-file plugin spec is resolved through the nearest package.json and
//     skipped WITHOUT A WORD when that yields no server entry, so the plugin is
//     a package directory, referenced as a directory.
//
// Because a skipped plugin is silent, this one ANNOUNCES itself: it writes the
// ready marker the runner waits for at start (`startup.ts`). A run whose guard
// did not load is failed before its prompt is sent — no guard is the exact
// defect (a component built into the run directory) the guard exists to stop.
//
// A thrown error aborts the tool and reaches the model as the tool's error text,
// verbatim (S1c: both guard sentences appeared in the lead's report). A tool its
// rules hide is never offered, so never reaches this; a path-scoped permission
// (`external_directory`, a pattern under `edit`) is asked INSIDE the tool, after
// this hook (1.18.32 `session/tools.ts`), so for an authored path this hook's
// sentence is the one the agent reads.
//
// It also keeps the prompt appendix to the lead and records what each session
// was given (`context.ts`), and answers the runner's startup probe
// (`startup_probe.ts`).

import fs from "node:fs";
import { appendSessionContext } from "../../../lib/run_context.js";
import { obj, str } from "../../fields.js";
import { PRIMARY_AGENT } from "../tools.js";
import { createSessionContext, type SessionContext } from "./context.js";
import { createGuardDecision, type GuardDecision } from "./guard.js";
import { GUARD_ENV } from "./protocol.js";
import { createStartupProbe } from "./startup_probe.js";

/** The slice of OpenCode's plugin hook input this guard reads. */
interface ToolExecuteInput {
  tool: string;
  sessionID: string;
  callID: string;
}

/** The slice of OpenCode's plugin hook output this guard reads. */
interface ToolExecuteOutput {
  args: unknown;
}

/** The slices of `chat.message` and `experimental.chat.system.transform` this plugin reads. */
interface ChatMessageInput {
  sessionID: string;
}
interface ChatMessageOutput {
  message: { agent?: string };
  parts: unknown[];
}
interface SystemTransformInput {
  sessionID?: string;
}
interface SystemTransformOutput {
  system: string[];
}
interface ChatParamsInput {
  sessionID: string;
}

interface GuardHooks {
  "tool.execute.before": (input: ToolExecuteInput, output: ToolExecuteOutput) => Promise<void>;
  "chat.message": (input: ChatMessageInput, output: ChatMessageOutput) => Promise<void>;
  "experimental.chat.system.transform": (input: SystemTransformInput, output: SystemTransformOutput) => Promise<void>;
  "chat.params": (input: ChatParamsInput) => Promise<void>;
}

function required(name: string): string {
  const value = process.env[name];
  if (!value) throw new Error(`aep-guard: ${name} is not set`);
  return value;
}

/**
 * Read the run's inputs and build the decision. Throws when anything is
 * missing — and a plugin that throws at init is one OpenCode does not load, so
 * no marker is written and the run fails at start instead of running unguarded.
 */
function loadDecision(): GuardDecision {
  const workspace = required(GUARD_ENV.workspace);
  const parsed: unknown = JSON.parse(fs.readFileSync(required(GUARD_ENV.secretsFile), "utf8"));
  if (!Array.isArray(parsed) || !parsed.every((s) => typeof s === "string")) {
    throw new Error(`aep-guard: ${GUARD_ENV.secretsFile} does not hold a string array`);
  }
  return createGuardDecision({ workspace, secrets: parsed });
}

function loadSessionContext(): SessionContext {
  const appendix = fs.readFileSync(required(GUARD_ENV.appendixFile), "utf8");
  const log = required(GUARD_ENV.sessionLog);
  return createSessionContext({ appendix, lead: PRIMARY_AGENT, record: (r) => appendSessionContext(log, r) });
}

/**
 * The plugin entry point. OpenCode calls every exported function of the module
 * with its plugin context; this guard needs none of it.
 */
export const AepGuard = async (): Promise<GuardHooks> => {
  const decide = loadDecision();
  const context = loadSessionContext();
  const probeFile = required(GUARD_ENV.probeFile);
  const probe = createStartupProbe(() => fs.writeFileSync(probeFile, "system.transform\n", { mode: 0o600 }));
  fs.writeFileSync(required(GUARD_ENV.readyFile), JSON.stringify({ pid: process.pid }) + "\n", { mode: 0o600 });
  return {
    "tool.execute.before": async (input, output) => {
      if (input.tool === "skill") context.noteSkill(input.sessionID, str(obj(output.args).name));
      const reason = decide(input.tool, output.args);
      if (reason) throw new Error(reason);
    },
    "chat.message": async (input, output) => {
      probe.noteMessage(input.sessionID, output.parts ?? []);
      if (!probe.isProbe(input.sessionID)) context.noteAgent(input.sessionID, output.message.agent);
    },
    "experimental.chat.system.transform": async (input, output) => {
      const sessionID = input.sessionID ?? "";
      if (probe.isProbe(sessionID)) return probe.markTransform(sessionID);
      context.shapeSystem(sessionID, output.system);
    },
    "chat.params": async (input) => {
      probe.refuseModelCall(input.sessionID);
    },
  };
};
