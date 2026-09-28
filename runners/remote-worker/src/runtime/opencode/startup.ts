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

// The four start-time assertions (ADR-0015), stated as a pure function of
// what the server reports, so each failure mode is a unit test.
//
// Silence is OpenCode's failure mode. Each of these degrades a run without an
// error anywhere on the wire:
//
//   1. THE GUARD PLUGIN LOADED. A mis-packaged plugin is skipped without a log
//      line; the plugin writes a marker at init and a missing marker means no
//      workspace guard — the exact defect (a component built into the run
//      directory) the guard exists to stop.
//   2. THE WORKFLOW'S TOOLS ARE VISIBLE to the `aep` agent — `task`, `skill`,
//      `todowrite`, `edit`, `write`, `bash` — and `question` is NOT. An
//      allowlist written in the wrong order hides the whole tool.
//   3. FAN-OUT IS FOREGROUND: the `task` tool's schema has no `background`
//      parameter. Its presence means the experimental flag leaked into the
//      pod's environment and the run would take a shape the glossary does not
//      describe.
//   4. THE APPENDIX IS KEPT TO THE LEAD: the plugin's
//      `experimental.chat.system.transform` hook fires. It is an experimental
//      hook; were it dropped, every subagent would read the lead's workflow and
//      glossary. Proven by a probe prompt the plugin refuses before its model
//      call (plugin/startup_probe.ts).
//
// The runtime proves all four before it sends the prompt, so a failing run
// costs no model call.
//
// Where (2) reads from, precisely: `GET /experimental/tool` lists what the
// registry offers this provider and model BEFORE permissions (ToolRegistry.tools
// filters by model only), and `GET /agent` returns the `aep` agent's merged
// rule set. What the model is actually offered is the first minus the tools
// `Permission.disabled` removes from it, so that is computed here — the same
// rule, ported below, over the server's own rule set rather than the config the
// adapter meant to write.

import { BACKGROUND_PARAMETER, FANOUT_TOOL, REQUIRED_TOOLS } from "./tools.js";

/** One permission rule as `GET /agent` reports it. */
export interface PermissionRuleRecord {
  permission: string;
  pattern: string;
  action: string;
}

/** What the server said, gathered by the runtime. */
export interface StartupFacts {
  /** The plugin wrote its ready marker. */
  guardReady: boolean;
  /** `GET /experimental/tool` ids for the run's provider and model. */
  toolIds: readonly string[];
  /** The `aep` agent's rule set from `GET /agent`, or undefined when there is no such agent. */
  agentRules: readonly PermissionRuleRecord[] | undefined;
  /** The `task` tool's parameter names, from the same listing. */
  taskParameters: readonly string[];
  /** The startup probe reached the plugin's system transform. */
  systemTransformLive: boolean;
}

/** OpenCode's `Wildcard.match`: `*` any run of characters, `?` one, the rest literal. */
function wildcard(value: string, pattern: string): boolean {
  const source = pattern.replace(/[.+^${}()|[\]\\]/g, "\\$&").replace(/\*/g, ".*").replace(/\?/g, ".");
  return new RegExp(`^${source}$`, "s").test(value);
}

/**
 * The tools a rule set hides from the model — OpenCode 1.18.32's
 * `Permission.disabled`, ported: a tool is hidden when the LAST rule matching
 * its permission has pattern `*` and action `deny`. The authoring tools share
 * the `edit` permission.
 */
export function hiddenTools(tools: readonly string[], rules: readonly PermissionRuleRecord[]): Set<string> {
  const edits = new Set(["edit", "write", "apply_patch"]);
  const hidden = new Set<string>();
  for (const tool of tools) {
    const permission = edits.has(tool) ? "edit" : tool;
    const rule = [...rules].reverse().find((r) => wildcard(permission, r.permission));
    if (rule && rule.pattern === "*" && rule.action === "deny") hidden.add(tool);
  }
  return hidden;
}

/** What the `aep` agent is actually offered. */
export function visibleTools(facts: Pick<StartupFacts, "toolIds" | "agentRules">): string[] {
  const hidden = hiddenTools(facts.toolIds, facts.agentRules ?? []);
  return facts.toolIds.filter((t) => !hidden.has(t));
}

/** Every broken assertion, as a sentence; empty when the server is fit to run. */
export function startupProblems(facts: StartupFacts): string[] {
  const problems: string[] = [];
  if (!facts.guardReady) {
    problems.push(
      "the aep-guard plugin did not announce itself (no ready marker) — the workspace, WebSearch and WebFetch guards " +
        "are not loaded; check that the plugin directory exists and holds package.json + index.js",
    );
  } else if (!facts.systemTransformLive) {
    problems.push(
      "the aep-guard plugin's experimental.chat.system.transform hook did not fire on the startup probe — every " +
        "subagent would receive the lead's prompt appendix; this OpenCode build no longer calls that hook",
    );
  }
  if (!facts.agentRules) {
    problems.push("the server has no `aep` agent — the run's config was not applied");
  } else {
    const visible = new Set(visibleTools(facts));
    const missing = REQUIRED_TOOLS.filter((t) => !visible.has(t));
    if (missing.length > 0) {
      problems.push(
        `the aep agent is not offered ${missing.join(", ")} — a permission rule hides ${missing.length === 1 ? "it" : "them"} ` +
          '(an allowlist whose "*": "deny" is not FIRST hides the whole tool)',
      );
    }
    if (visible.has("question")) {
      problems.push("the aep agent is offered `question` — a one-shot run has nobody to answer it");
    }
  }
  if (!facts.toolIds.includes(FANOUT_TOOL)) {
    problems.push("the server offers no `task` tool for this model");
  } else if (facts.taskParameters.includes(BACKGROUND_PARAMETER)) {
    problems.push(
      "the task tool accepts `background` — OPENCODE_EXPERIMENTAL_BACKGROUND_SUBAGENTS is set in this environment, and " +
        "this platform runs OpenCode fan-out in the foreground only",
    );
  }
  return problems;
}

/** A start-time assertion failed: the run is refused before its prompt is sent. */
export class OpencodeStartupError extends Error {
  readonly problems: readonly string[];
  constructor(problems: readonly string[]) {
    super(`OpenCode refused to start this run: ${problems.join("; ")}`);
    this.name = "OpencodeStartupError";
    this.problems = problems;
  }
}
