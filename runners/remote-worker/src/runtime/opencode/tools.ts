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

// OpenCode's tool surface: which names this runtime answers to, which of them
// the platform's `DeniedCapability` classes map onto, and how a platform model
// id is spelled for it.
//
// The mirror of `runtime/claude/tools.ts`. The two tables share the REASON each
// entry is on them (`DeniedCapability`) and nothing else, which is the whole
// argument for the port stating classes rather than names: Claude Code denies
// sixteen tools across five classes, OpenCode 1.18.32 ships one tool in any of
// them. Read from the server itself (`GET /experimental/tool/ids`, no model
// call): invalid, question, bash, read, glob, grep, edit, write, task,
// webfetch, todowrite, websearch, skill, apply_patch.

import { deniedToolNames, type DeniedCapability } from "../port.js";

/**
 * OpenCode's permission keys denied by each capability class.
 *
 * `interactive_prompt` is `question` — the one tool that stops to ask a person.
 * (`plan_enter` / `plan_exit` are denied by OpenCode's own defaults, visible in
 * `GET /agent`, so they need no entry.) The other four classes have NO OpenCode
 * tool: there is no scheduler, no worktree, no peer messaging, and publishing
 * is the `share` feature, which the config switches off as a whole
 * (`share: "disabled"`, `config.ts`) rather than through a permission. An empty
 * list is a recorded fact, not a gap: a future OpenCode that adds a tool in one
 * of these classes is added here, under the class that explains it.
 */
const DENIED_TOOLS_BY_CAPABILITY: Record<DeniedCapability, readonly string[]> = {
  interactive_prompt: ["question"],
  scheduling: [],
  durable_session: [],
  peer_messaging: [],
  artifact_publishing: [],
};

/** The OpenCode permission keys denied by a set of capability classes, in a stable order. */
export function deniedTools(capabilities: readonly DeniedCapability[]): string[] {
  return deniedToolNames(DENIED_TOOLS_BY_CAPABILITY, capabilities);
}

/**
 * The tools the workflow cannot run without, checked at start (`startup.ts`).
 *
 * `task` is the fan-out tool, `skill` loads the mirrored skills, `todowrite` is
 * the task list and the other three author and build. Each one can be hidden
 * WITHOUT an error anywhere — an allowlist written in the wrong order hides the
 * whole tool (`Permission.disabled`: the LAST rule matching a permission is
 * `"*": "deny"`) — so presence is asserted, never assumed.
 */
export const REQUIRED_TOOLS = ["task", "skill", "todowrite", "edit", "write", "bash"] as const;

/** The fan-out tool, whose call is an agent's birth rather than a row. */
export const FANOUT_TOOL = "task";

/** The task-list tool; its rows come from `todo.updated`, not from the call. */
export const PLAN_TOOL = "todowrite";

/** The shell tool, whose command is read as a commit / push / `gh` call. */
export const SHELL_TOOL = "bash";

/**
 * The tools that author a file at a path the model chose, and the argument key
 * each names it by. `apply_patch` carries its paths inside the patch text and
 * is only offered for GPT-family models (`ToolRegistry.tools`), so on the
 * Anthropic models this platform prices it never reaches a session — it is
 * guarded anyway, because the guard costs nothing and a model change should not
 * be what opens the hole.
 */
export const AUTHORING_TOOLS = new Set(["edit", "write", "apply_patch"]);

/**
 * The paths an `apply_patch` call authors, read off its patch text: the patch
 * grammar names every file it touches on a header line (`*** Add File:`,
 * `*** Update File:`, `*** Delete File:`, `*** Move to:`).
 */
export function patchPaths(patchText: string): string[] {
  const out: string[] = [];
  for (const line of patchText.split("\n")) {
    const m = /^\*\*\* (?:Add File|Update File|Delete File|Move to): (.+)$/.exec(line.trim());
    if (m) out.push(m[1].trim());
  }
  return out;
}

/**
 * The parameter whose presence means the experimental background mode is on.
 * `OPENCODE_EXPERIMENTAL_BACKGROUND_SUBAGENTS=true` adds it to the task tool's
 * schema; without the flag it is absent (measured, S2b/S2c). The platform does
 * not set the flag (ADR-0015), so its presence is a leak and fails the run.
 */
export const BACKGROUND_PARAMETER = "background";

/** The one provider this runtime is configured with today (ADR-0015). */
export const PROVIDER_ID = "anthropic";

/** The platform's model id as OpenCode's config spells it: `anthropic/<id>`. */
export function opencodeModel(platformModel: string): string {
  return `${PROVIDER_ID}/${platformModel}`;
}

/**
 * An OpenCode model id back to the platform's spelling — the id `model_rates`
 * is keyed by.
 *
 * Measured: `message.updated` reports the undated alias the config named
 * (`claude-sonnet-5`, `claude-haiku-4-5`) as `modelID`, with the provider in its
 * own field, so this usually has nothing to strip. It strips anyway because the
 * config side does carry the prefix, and anything else is reported AS IS — an
 * unmapped id blanks the cycle's cost, which is the right loud failure.
 */
export function platformModel(modelId: string): string {
  return modelId.startsWith(`${PROVIDER_ID}/`) ? modelId.slice(PROVIDER_ID.length + 1) : modelId;
}

/**
 * The key the platform's MCP server is registered under. OpenCode names an MCP
 * tool `<server>_<tool>`; the adapter prints whatever the runtime spells.
 */
export const MCP_SERVER_KEY = "aep";

/** The one subagent the lead fans out to, on the run's model (`config.ts`, the glossary). */
export const SUBAGENT = "general";

/** The platform's primary agent: one workflow, no built-in prompt competing with it. */
export const PRIMARY_AGENT = "aep";
