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
// A second runtime is a second entry in GLOSSARIES and nothing else. The runtime
// PORT — `runtime/port.ts`, one interface over starting, translating and
// settling a session — is the larger seam this sits inside: a `Runtime` answers
// `toolGlossary()` out of this table, and `progress/claude_adapter.ts` is the
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
 * the workflow. The model aliases are listed because the skill tells the lead to
 * pick one ("the fast model", "the default one") and a lead that guesses an
 * alias spends a turn on a schema error.
 *
 * **Only models the platform can PRICE are offered.** `modelcost.SumCost` is
 * all-or-nothing by design — one slice whose model has no `model_rates` row
 * makes the WHOLE cycle's cost null, on the argument that a partial dollar
 * figure under-reports spend more dangerously than an absent one. So a single
 * subagent dispatched to an unpriced model blanks the cost of everything else
 * in that cycle, and it does it silently. This list offered `opus` while only
 * `claude-sonnet-5` and `claude-haiku-4-5` were seeded, which made the skill's
 * own "pick the model for the job" the way to lose a cycle's cost. Keep this in
 * step with `CodingAgentModel` in the contract, which is narrowed to the priced
 * set for the same reason; adding an alias here means seeding its rate row
 * first.
 *
 * PARTIAL on purpose: `RuntimeName` carries every runtime the org setting can
 * name, and only the ones this build can actually run have an entry. A missing
 * entry throws below rather than silently shipping a session whose workflow
 * names roles nothing binds.
 */
const GLOSSARIES: Partial<Record<RuntimeName, string>> = {
  "claude-code": [
    "## Tool glossary (Claude Code)",
    "",
    "The roles your workflow names, and the tools that play them in this session:",
    "",
    "- **fan-out tool**: `Agent` — `run_in_background: true` for a builder;" +
      " `model:` `haiku` (the fast model) or `sonnet` (the default)",
    "- **wait tool**: `TaskOutput` with `block: true` — one call per agent you dispatched",
    "- **stop tool**: `TaskStop`, for an agent that has run away",
    "- **task list**: `TaskCreate` and `TaskUpdate`",
    "- **edit**: `Edit`, `Write` · **shell**: `Bash`",
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
