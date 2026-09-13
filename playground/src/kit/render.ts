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
 * Render the agent's raw `StreamPart` frames to stdout as they arrive (the chat
 * is append-only, so there is no frame to re-draw — plain writes suffice), plus
 * the per-file change summary printed after the turn writes to disk.
 */

import { stdout } from "node:process";
import type { StreamPart } from "@aep/agent-stream";
import type { FileChange } from "./project-fs.js";

// Color only on a real terminal — otherwise raw escapes litter piped/redirected output.
const color = (code: string) => (s: string): string => (stdout.isTTY ? `\x1b[${code}m${s}\x1b[0m` : s);
const dim = color("2");
const green = color("32");
const red = color("31");

/** The path/name a tool acted on (file tools carry `path`; loadSkill carries `names`). */
function inputLabel(input: unknown): string {
  const v = (input ?? {}) as Record<string, unknown>;
  if (typeof v.path === "string") return v.path;
  if (Array.isArray(v.names)) return v.names.filter((n) => typeof n === "string").join(", ");
  if (Array.isArray(v.paths)) return v.paths.filter((p) => typeof p === "string").join(", "); // declare_plan
  if (typeof v.name === "string") return v.name;
  if (typeof v.question === "string") return v.question; // ask_question
  if (Array.isArray(v.questions)) return `${v.questions.length} question(s)`; // ask_questions
  return "";
}

/** The status the HITL question tools resolve with (services/agents tools/files.ts). */
const AWAITING_USER_RESPONSE = "awaiting_user_response";

/**
 * The status the fire-and-forget tools resolve with. `declare_plan` carries no
 * `ok` — it is not a write op, so it has no `OpResult` shape — and without this
 * the error fall-through below painted every resolved plan declaration as a red
 * `✗ error`.
 */
const OK = "ok";

interface ResultShape {
  ok?: boolean;
  op?: string;
  status?: string;
  code?: string;
  message?: string;
}

/** One streamed frame → one line of terminal output (silent for ignored types). */
export function renderPart(part: StreamPart): void {
  switch (part.type) {
    case "text-delta":
      if (part.text) stdout.write(part.text);
      break;
    case "tool-call":
      stdout.write(`\n${dim("→")} ${part.toolName ?? "?"} ${dim(inputLabel(part.input))}\n`);
      break;
    case "tool-result": {
      const r = part.output as ResultShape | string | undefined;
      // MCP discovery tools return bare strings, not the file tools'
      // {ok, op, status} shape — render them as the successes they are
      // (a thrown MCP failure arrives as a tool-error part, not here).
      if (typeof r === "string") {
        const preview = r.replace(/\s+/g, " ").trim();
        stdout.write(`  ${green("✓")} ${dim(preview.length > 80 ? `${preview.slice(0, 80)}…` : preview)}\n`);
      } else if (r?.status === AWAITING_USER_RESPONSE) {
        // The HITL question tools (ask_question / ask_questions) resolve to a
        // PLACEHOLDER carrying no `ok` — the turn ends here and the answer
        // arrives as the next message. Without this branch the fall-through
        // below rendered every question card as a red "✗ error", which is what
        // a `/start` interview shows on its very first turn.
        stdout.write(`  ${dim("…")} awaiting your answer\n`);
      } else if (r?.ok) stdout.write(`  ${green("✓")} ${r.op ?? "loaded"} ${dim(r.status ?? "")}\n`);
      // A fire-and-forget tool resolves with `status: "ok"` and no `ok` flag.
      else if (r?.status === OK) stdout.write(`  ${green("✓")} ${r.op ?? OK}\n`);
      else if (r) stdout.write(`  ${red("✗")} ${r.code ?? "error"}${r.message ? `: ${r.message}` : ""}\n`);
      break;
    }
    case "tool-error":
      stdout.write(`  ${red("✗")} tool error: ${String(part.error)}\n`);
      break;
    case "error":
      stdout.write(`\n${red("[error]")} ${String(part.error)}\n`);
      break;
    default:
      break; // tool-input-*, finish, start, … — not shown
  }
}

const SIGIL: Record<FileChange["kind"], string> = { add: green("+"), edit: "✎", remove: red("−") };

/** Print what landed on disk (or would, under --dry-run) after a turn. */
export function renderSummary(changes: FileChange[], dryRun: boolean): void {
  if (changes.length === 0) {
    stdout.write(dim("  (no file changes)\n"));
    return;
  }
  stdout.write(dim(dryRun ? "  would change:\n" : "  wrote:\n"));
  for (const c of changes) stdout.write(`  ${SIGIL[c.kind]} ${c.path}\n`);
}
