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

// The tool glossary: the one place a runtime's tool NAMES are written down for
// the agent reading them.
//
// The `aep` skill is authored in roles — "the fan-out tool", "the wait tool",
// "the task list" — because it is one library shared by every org, and a skill
// naming `Agent` and `TaskOutput` would be a Claude Code document that a second
// runtime silently mis-steers. The binding from role to tool name belongs to
// whoever started the session, which is this package. So the skill says what to
// do and the glossary says what to call, and the two move independently.
//
// It is appended LAST, after the workflow body and any pinned skill bodies, so
// the skill's "the tool glossary at the end of your instructions" is literally
// true and the model has one place to look rather than a definition buried
// mid-prompt.
//
// Each runtime is one entry in GLOSSARIES and nothing else. The runtime
// PORT — `runtime/port.ts`, one interface over starting, translating and
// settling a session — is the larger seam this sits inside: a `Runtime` answers
// `toolGlossary()` out of this table, and `runtime/claude/translate.ts` is the
// translation half of the same adapter.
//
// The table is keyed on the port's `RuntimeName`, which is also the wire value
// of the organization's Runtime setting and of `AEP_AGENT_RUNTIME`. One spelling
// across all three, so a runtime that reaches here with no glossary is a type
// error rather than a session steered by role names nothing binds.

import { DEFAULT_RUNTIME, type RuntimeName } from "../runtime/port.js";

/**
 * One glossary per runtime, keyed by the runtime's own id.
 *
 * Every role the `aep` skill names in prose has an entry here, and nothing else
 * does: this is a lookup table the agent reads under load, not a second copy of
 * the workflow. No model is named: a run has ONE model, the organization's
 * setting, which Claude Code pins every alias and the subagent model to
 * (`modelPinEnv`, `runtime/claude/runtime.ts`) and OpenCode's one subagent,
 * `general`, runs on (`runtime/opencode/config.ts`). An alias offered here would
 * be a second model the org's key may not serve or the platform cannot price.
 *
 * TOTAL over `RuntimeName`: every runtime the org setting can name has an
 * entry, so a new name without one is a type error here rather than a session
 * whose workflow names roles nothing binds.
 */
const GLOSSARIES: Record<RuntimeName, string> = {
  "claude-code": [
    "## Tool glossary (Claude Code)",
    "",
    "The roles your workflow names, and the tools that play them in this session:",
    "",
    "- **fan-out tool**: `Agent` — `run_in_background: true` for a builder",
    "- **wait tool**: `TaskOutput` with `block: true` — one call per agent you dispatched",
    "- **stop tool**: `TaskStop`, for an agent that has run away",
    "- **task list**: `TaskCreate` and `TaskUpdate`",
    "- **edit**: `Edit`, `Write` · **shell**: `Bash`",
  ].join("\n"),
  // OpenCode runs its fan-out in the FOREGROUND only (ADR-0015): the platform
  // does not set the experimental background flag, so the lead is held until
  // every builder of a wave returns. The skill's prose still says "dispatch in
  // the background … keep working while they build", which describes the Claude
  // Code shape; this entry is where a lead reading that prose on OpenCode is
  // told, in its own terms, what the words mean in this session. There is one
  // subagent, `general`, on the org's model (`runtime/opencode/config.ts`).
  opencode: [
    "## Tool glossary (OpenCode)",
    "",
    "The roles your workflow names, and the tools that play them in this session:",
    "",
    '- **fan-out tool**: `task` with `subagent_type: "general"`. There is no background mode in this session:' +
      ' "dispatch in the background" means issue every `task` call of the wave as PARALLEL tool calls' +
      " in ONE message. They run at the same time and each returns its builder's report when that" +
      " builder finishes; you are held until the slowest one does.",
    "- **wait tool**: none is needed. Each `task` call IS the wait: its result is the report.",
    "- **stop tool**: none in this session; a task you no longer need is left to finish",
    "- **task list**: `todowrite`",
    "- **edit**: `edit`, `write` · **shell**: `bash`",
  ].join("\n"),
};

/**
 * The glossary block for a runtime, ready to append to a system prompt.
 *
 * Throws for a runtime with no entry, so a caller cannot start a session whose
 * prose names roles nothing binds — that is a programming error here, not a
 * degraded run. The org setting is validated at the API, so this is the second
 * line, not the first.
 */
export function toolGlossary(runtime: RuntimeName = DEFAULT_RUNTIME): string {
  const glossary = GLOSSARIES[runtime];
  if (glossary === undefined) throw new Error(`no tool glossary for runtime ${JSON.stringify(runtime)}`);
  return glossary;
}
