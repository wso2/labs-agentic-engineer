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
 * THE BOUNDARY the two wired-mode agent tasks run inside.
 *
 * The rule the whole design rests on is: **the agent proposes text or a file;
 * the harness executes.** No agent starts a process, edits the application, or
 * touches `specs/`. A seed task writes exactly one file — a shell script the
 * harness replays afterwards with no model in the loop — and a triage task
 * writes nothing at all.
 *
 * That is enforced MECHANICALLY, in a PreToolUse hook, and never by asking the
 * prompt nicely. The runner's own workspace guard
 * (`runners/remote-worker/src/lib/workspace_guard.ts`) is the same shape and was
 * written after a run that ignored the prose; this one is narrower because these
 * tasks need far less than a coding run does.
 *
 * Containment is lexical, after `path.resolve`. A symlink out of the state dir
 * would defeat it; that is acceptable here for the same reason it is there —
 * this stops the mistake, and the developer's own machine is not a sandbox.
 */

import path from "node:path";
import type { HookCallback, PreToolUseHookInput } from "@anthropic-ai/claude-agent-sdk";

/** Tools that author a file at a path the model chose. */
const WRITE_TOOLS = new Set(["Write", "Edit", "MultiEdit", "NotebookEdit"]);

/** Where those tools name their target. */
const PATH_KEYS = ["file_path", "notebook_path"] as const;

/** Tools no wired-mode task has any use for, whatever it is doing. */
const NEVER = new Set(["WebFetch", "WebSearch", "Task", "Agent"]);

export interface AgentBoundary {
  /** The task's name, as the refusal message says it. */
  task: string;
  /** A filename-safe name for the transcript. */
  slug: string;
  /** The exact files this task may write. Empty for a read-only task. */
  mayWrite: string[];
  /**
   * The one base URL Bash may `curl`. Absent forbids Bash entirely.
   *
   * A base URL rather than a command prefix, because a prefix cannot say the
   * thing that matters: `curl ` admits any host, and `curl -o` writes any file,
   * which walks straight through `mayWrite`.
   */
  mayCurl?: string;
}

/** Flags that turn `curl` from a reader into a writer, or into a second request. */
const CURL_WRITES = [" -o", " --output", " -O", " --remote-name", " --upload-file", " -T", " --config", " -K"];

/**
 * Whether this is one `curl` at the session's own base URL, and nothing else.
 *
 * Shell metacharacters are refused outright: `;`, `&&`, a pipe or a redirect
 * turn one allowed command into two, and the second is unconstrained. The model
 * loses nothing it needs — it makes one call and reads the answer.
 */
export function isAllowedCurl(command: string, baseUrl: string): boolean {
  const trimmed = command.trim();
  if (!/^curl(\s|$)/.test(trimmed)) return false;
  if (/[;|&><`$]|\n/.test(trimmed)) return false;
  if (CURL_WRITES.some((flag) => trimmed.includes(flag))) return false;
  return trimmed.includes(baseUrl);
}

/**
 * The decision, separated from the hook plumbing so it can be driven with plain
 * objects — every forbidden call in the design's table has a test, and a table
 * that is only asserted through a live model is a table nobody can trust.
 *
 * Returns the refusal, or undefined to allow.
 */
export function boundaryDenial(toolName: string, toolInput: unknown, boundary: AgentBoundary): string | undefined {
  if (NEVER.has(toolName)) {
    return `${boundary.task} may not use ${toolName}. It works from the project's own design files and the running services, and nothing else.`;
  }

  if (WRITE_TOOLS.has(toolName)) {
    const input = asRecord(toolInput);
    const raw = PATH_KEYS.map((key) => input[key]).find((value) => typeof value === "string" && value !== "");
    if (typeof raw !== "string") return undefined; // a malformed call is the SDK's to report
    if (boundary.mayWrite.length === 0) {
      return `${boundary.task} writes nothing. Put your answer in the reply instead of a file.`;
    }
    const target = path.resolve(raw);
    if (!boundary.mayWrite.some((allowed) => path.resolve(allowed) === target)) {
      return (
        `${boundary.task} may write exactly one file: ${boundary.mayWrite.join(", ")}. ` +
        `${toolName} named ${target}. The application, its specs and everything else are read-only here — ` +
        `a change to them belongs to a coding run against an issue, not to this task.`
      );
    }
    return undefined;
  }

  if (toolName === "Bash") {
    const command = String(asRecord(toolInput).command ?? "").trim();
    if (!boundary.mayCurl) {
      return `${boundary.task} runs no commands. It reads what it was given and answers.`;
    }
    if (!isAllowedCurl(command, boundary.mayCurl)) {
      return (
        `${boundary.task} may run exactly one kind of command: a single \`curl\` against ${boundary.mayCurl}, ` +
        `with no shell operators and no flag that writes a file. It may not start, stop or rebuild anything — ` +
        `the harness owns every process in this session.`
      );
    }
    return undefined;
  }

  return undefined;
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" ? (value as Record<string, unknown>) : {};
}

/**
 * The PreToolUse hook.
 *
 * `onDeny` exists because a denial nobody can see is a mystery later: the
 * session prints it, the transcript keeps it.
 */
export function createBoundaryGuard(boundary: AgentBoundary, onDeny?: (reason: string) => void): HookCallback {
  const announced = new Set<string>();
  return async (input) => {
    const hookInput = input as PreToolUseHookInput;
    if (hookInput.hook_event_name !== "PreToolUse") return {};
    const reason = boundaryDenial(hookInput.tool_name, hookInput.tool_input, boundary);
    if (!reason) return {};
    if (!announced.has(hookInput.tool_use_id)) {
      announced.add(hookInput.tool_use_id);
      onDeny?.(reason);
    }
    return {
      hookSpecificOutput: {
        hookEventName: "PreToolUse" as const,
        permissionDecision: "deny" as const,
        permissionDecisionReason: reason,
      },
    };
  };
}

/** Register one matcher per tool name: the matcher grammar is unspecified, and a silent non-match is not safe. */
export function guardMatchers(guard: HookCallback): { matcher: string; hooks: HookCallback[] }[] {
  return [...WRITE_TOOLS, ...NEVER, "Bash"].map((tool) => ({ matcher: tool, hooks: [guard] }));
}

/**
 * The tools this boundary would refuse anyway, dropped before the model is
 * offered them. Derived rather than listed per task: a hook table and a hand-kept
 * `disallowedTools` beside it drift, and the one that drifts open is the hook's.
 */
export function deniedTools(boundary: AgentBoundary): string[] {
  return [
    ...NEVER,
    ...(boundary.mayWrite.length === 0 ? WRITE_TOOLS : []),
    ...(boundary.mayCurl ? [] : ["Bash"]),
  ];
}
