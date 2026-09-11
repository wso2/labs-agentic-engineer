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
 * Authored files land in the PROJECT, and nowhere else.
 *
 * The `aep` skill already says so ("everything you produce goes inside it"), and
 * a run still built an entire component in the wrong directory. Measured: a
 * subagent wrote 18 source files into `/workspace/run/todo-webapp/`, ran
 * `npm install` and `vite build` there, and finished green — 210s of work
 * discarded wholesale, 89MB orphaned in the run archive, and the component
 * rebuilt from scratch afterwards in the right place. A second subagent in the
 * same run absorbed the other's issue for the same reason.
 *
 * The confusion is structural, not careless. In local mode the skills plugin is
 * materialised at `<runDir>/.aep/skills-plugin`, a SIBLING of the project mount,
 * so the only absolute paths the agent is ever handed point at `/workspace/run`
 * — starting with the skill's own "Base directory for this skill" line. The
 * deny-list then explicitly licenses reading a skill's `references/`, i.e.
 * reading outside the cwd. From there "the run's directory is the project root"
 * is a reasonable inference, and it is wrong.
 *
 * So this is the guarantee behind the prose. It gates the tools that AUTHOR files (`Write`/`Edit`/
 * `NotebookEdit`) and denies a path outside the workspace with a message naming
 * the root to use instead, which turns a silent 210s detour into one corrected
 * call.
 *
 * What it deliberately does NOT gate:
 *
 *   - **Reads.** The agent must read its own skills, and their `references/`
 *     files live outside the project by construction. Gating reads would break
 *     the skill system to prevent a writing mistake.
 *   - **Bash.** A toolchain writes wherever it writes: `bal build` populates
 *     `~/.ballerina`, `npm` populates `~/.npm`, both create caches and lockfiles
 *     the run depends on. Gating Bash by path is neither feasible (a command line
 *     is not a path) nor desirable (it would break every build). The real
 *     containment boundary for a coding run is the disposable pod, not this hook;
 *     what this catches is the specific, observed, expensive mistake of AUTHORING
 *     a component in the wrong tree.
 *
 * Path containment is lexical, after `path.resolve`. A symlink out of the
 * workspace would defeat it. That is acceptable: this is a wrong-directory guard,
 * not a sandbox, and the pod is the sandbox.
 */

import os from "node:os";
import path from "node:path";
import type { HookCallback, PreToolUseHookInput } from "@anthropic-ai/claude-agent-sdk";

/** The tools that author a file at a path the model chose. */
const WRITE_TOOLS = new Set(["Write", "Edit", "NotebookEdit"]);

/** Where those tools name their target. `NotebookEdit` uses its own key. */
const PATH_KEYS = ["file_path", "notebook_path"] as const;

/**
 * Trees an authored file may legitimately land in besides the project: the temp
 * directory, and any dot-directory directly under `$HOME`.
 *
 * The second is one rule rather than a list of caches because every toolchain
 * puts its cache in a hidden home directory — `.ballerina`, `.npm`, `.m2`,
 * `.gradle`, `.cargo` — and this file has no business tracking which stacks the
 * image ships. An earlier version named three of them, which meant the guard
 * silently contradicted the next stack skill added. It stays narrow where it
 * matters: a sibling project or a materialised skills directory is not hidden,
 * so the one expensive mistake this guard exists for is still caught.
 */
export function allowsWriteOutsideProject(target: string): boolean {
  if (isInside(os.tmpdir(), target)) return true;

  const home = path.resolve(os.homedir());
  const rel = path.relative(home, target);
  if (rel === "" || rel.startsWith("..") || path.isAbsolute(rel)) return false;
  return rel.split(path.sep)[0].startsWith(".");
}

/** True when `target` is `root` itself or sits underneath it. */
function isInside(root: string, target: string): boolean {
  const rel = path.relative(root, target);
  return rel === "" || (!rel.startsWith("..") && !path.isAbsolute(rel));
}

/**
 * The decision, separated from the hook plumbing so it can be tested against
 * plain objects. Returns the denial reason, or undefined to allow.
 *
 * A call naming no path at all is allowed: this guard's job is wrong-directory
 * writes, and inventing a failure for a malformed call would only mask whatever
 * the SDK reports about it.
 *
 * `isAllowed` is the seam: pass `() => false` to exercise the core denial without
 * the machine's real home and temp directories deciding the outcome.
 */
export function workspaceWriteDenial(
  toolName: string,
  toolInput: unknown,
  workspaceRoot: string,
  isAllowed: (target: string) => boolean = allowsWriteOutsideProject,
): string | undefined {
  if (!WRITE_TOOLS.has(toolName)) return undefined;
  if (!toolInput || typeof toolInput !== "object") return undefined;

  const input = toolInput as Record<string, unknown>;
  const raw = PATH_KEYS.map((k) => input[k]).find((v) => typeof v === "string" && v !== "");
  if (typeof raw !== "string") return undefined;

  // Relative paths resolve against the workspace, which is the session's cwd —
  // so a relative path is in-project by construction and never the mistake.
  const target = path.resolve(workspaceRoot, raw);
  const root = path.resolve(workspaceRoot);
  if (isInside(root, target)) return undefined;
  if (isAllowed(target)) return undefined;

  return (
    `Refusing to write outside the project. ${toolName} named ${target}, but the project root for this run ` +
    `is ${root} and every file you author belongs under it. Paths you may READ outside the project (your ` +
    `skills, their references/, the toolchain's caches) are not places you may write. Re-issue this call with ` +
    `a path under ${root} — a relative path is resolved against it.`
  );
}

/**
 * PreToolUse hook denying an authored file outside the project.
 *
 * `onDeny` is called with the reason so the run says on its own feed that it
 * blocked something. A denial the reader cannot see is a mystery the next time
 * this comes up, and the whole reason this hook exists is that the failure it
 * catches was invisible for a whole run.
 *
 * `isAllowed` is the platform's rule, passed in rather than reached for, because
 * WHERE an authored file may land is a `RuntimePolicy` clause and this is only
 * the mechanism that enforces it. It defaults to that same rule so a direct
 * caller — a test, a future entrypoint — cannot accidentally get a laxer one.
 */
export function createWorkspaceWriteGuard(
  workspaceRoot: string,
  onDeny?: (reason: string) => void,
  isAllowed: (target: string) => boolean = allowsWriteOutsideProject,
): HookCallback {
  const announced = new Set<string>();

  return async (input) => {
    const hookInput = input as PreToolUseHookInput;
    if (hookInput?.hook_event_name !== "PreToolUse") return {};

    const reason = workspaceWriteDenial(hookInput.tool_name, hookInput.tool_input, workspaceRoot, isAllowed);
    if (!reason) return {};

    // Registered under one matcher per tool name, so the same call can reach this
    // more than once; the announcement is deduped so one denial is reported once.
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
