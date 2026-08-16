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
 * The DEVELOPER view of a coding run — what took the time, what exactly failed,
 * and what the model was reasoning about when it went there.
 *
 * None of this belongs in the progress feed. That feed answers "is it alive and
 * is anything broken" for someone watching their project get built; full tool
 * output and chains of reasoning would bury the two lines that matter. So the
 * detail lives where it already is and is READ on demand, rather than being
 * summarised into the feed or copied into a third file.
 *
 * Two artifacts per run, and this module joins them:
 *   .logs/claude.log — the raw SDK transcript. Every message: thinking blocks,
 *     whole tool inputs, whole outputs, the subagents' closing reports. Nothing
 *     is missing from it, which is why there is no derived index — a cache of an
 *     analysis can drift from the truth, and this one would.
 *   progress.ndjson — the runner's own feed, which is where the TIMINGS are: the
 *     SDK does not stamp its messages, so per-call durations exist only because
 *     the runner measured them between the call and its outcome.
 */

import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { formatDuration, SLOW_CALL_MS } from "@aep/progress-view";

const RUNS_DIR = join(".aep-playground", "runs");
const MAX_INPUT_CHARS = 160;
const MAX_OUTPUT_CHARS = 400;

export type LogView = "steps" | "slow" | "thinking";

/** One tool call, joined across the transcript and the runner's measurements. */
interface Step {
  id: string;
  tool: string;
  /** The fan-out call that owns it, "" for the main agent's own work. */
  owner: string;
  input: string;
  ok?: boolean;
  output?: string;
  durationMs?: number;
}

interface Thought {
  owner: string;
  text: string;
}

function readJsonl(path: string): Record<string, unknown>[] {
  if (!existsSync(path)) return [];
  const out: Record<string, unknown>[] = [];
  for (const line of readFileSync(path, "utf8").split("\n")) {
    if (line.trim() === "") continue;
    try {
      const parsed: unknown = JSON.parse(line);
      if (parsed && typeof parsed === "object") out.push(parsed as Record<string, unknown>);
    } catch {
      // A non-JSON line in a transcript is the runner's own stdout leaking in.
      // Skipping it is right here: this reader is about the SDK's messages.
    }
  }
  return out;
}

function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}

function blocks(m: Record<string, unknown>): Record<string, unknown>[] {
  const inner = m.message;
  if (!inner || typeof inner !== "object") return [];
  const content = (inner as Record<string, unknown>).content;
  if (!Array.isArray(content)) return [];
  return content.filter((b): b is Record<string, unknown> => !!b && typeof b === "object");
}

// The human-readable field a tool input leads with, when it has one. Without
// this every row is a JSON dump — and for a fan-out call that dump is 160
// characters of the subagent's prompt, which tells a reader nothing about the
// step. Same fields the feed reads, `command` included: this view is about what
// ran, so a shell command is the point rather than something to rewrite.
function leadField(v: unknown): string | undefined {
  if (!v || typeof v !== "object") return undefined;
  const o = v as Record<string, unknown>;
  for (const key of ["command", "file_path", "path", "pattern", "glob", "url", "skill", "description"]) {
    if (typeof o[key] === "string" && o[key]) return o[key] as string;
  }
  return undefined;
}

function compact(v: unknown, limit: number): string {
  let text: string;
  if (typeof v === "string") text = v;
  else if (Array.isArray(v)) text = v.map((b) => (b && typeof b === "object" ? str((b as Record<string, unknown>).text) : String(b))).join(" ");
  else {
    try {
      text = JSON.stringify(v) ?? "";
    } catch {
      text = "";
    }
  }
  const flat = text.replace(/\s+/g, " ").trim();
  return flat.length <= limit ? flat : `${flat.slice(0, limit - 1)}…`;
}

/**
 * The newest run under a project, or a specific one by directory name. Newest by
 * NAME, which is sound because the archive names every run by its ISO timestamp.
 */
export function resolveRunDir(projectDir: string, run?: string): string | undefined {
  const runs = join(projectDir, RUNS_DIR);
  if (!existsSync(runs)) return undefined;
  if (run) {
    const explicit = join(runs, run);
    return existsSync(explicit) ? explicit : undefined;
  }
  const dirs = readdirSync(runs, { withFileTypes: true })
    .filter((d) => d.isDirectory())
    .map((d) => d.name)
    .sort();
  const latest = dirs[dirs.length - 1];
  return latest ? join(runs, latest) : undefined;
}

interface RunLog {
  steps: Step[];
  thoughts: Thought[];
}

/**
 * Join one run's transcript with its progress feed.
 *
 * Owner labels come from the fan-out calls' own descriptions, so a step reads as
 * "todo-api: bal build" rather than as an anonymous id. `parent_tool_use_id` is
 * the link the SDK sets on forwarded messages; current builds also report the
 * same work as task_* messages, but those carry no tool INPUT — which is exactly
 * what a developer view needs — so this reader follows the forwarded channel.
 */
export function readRunLog(runDir: string): RunLog {
  const messages = readJsonl(join(runDir, ".logs", "claude.log"));
  const progress = readJsonl(join(runDir, "progress.ndjson"));

  const durations = new Map<string, number>();
  for (const e of progress) {
    const id = str(e.toolUseId);
    if (id && typeof e.durationMs === "number") durations.set(id, e.durationMs);
  }

  const labels = new Map<string, string>();
  const byId = new Map<string, Step>();
  const steps: Step[] = [];
  const thoughts: Thought[] = [];

  for (const m of messages) {
    const owner = labels.get(str(m.parent_tool_use_id)) ?? (str(m.parent_tool_use_id) ? "subagent" : "");
    if (m.type === "assistant") {
      for (const b of blocks(m)) {
        if (b.type === "thinking") {
          const text = str(b.thinking) || str(b.text);
          if (text) thoughts.push({ owner, text });
          continue;
        }
        if (b.type !== "tool_use") continue;
        const id = str(b.id);
        const tool = str(b.name);
        const input = b.input;
        if (input && typeof input === "object") {
          const desc = str((input as Record<string, unknown>).description);
          // A fan-out call names every step forwarded from inside it.
          if (desc && (tool === "Agent" || tool === "Task")) labels.set(id, desc);
        }
        const measured = durations.get(id);
        const step: Step = {
          id,
          tool,
          owner,
          input: compact(leadField(input) ?? input, MAX_INPUT_CHARS),
          ...(measured === undefined ? {} : { durationMs: measured }),
        };
        steps.push(step);
        if (id) byId.set(id, step);
      }
      continue;
    }
    if (m.type === "user") {
      for (const b of blocks(m)) {
        if (b.type !== "tool_result") continue;
        const step = byId.get(str(b.tool_use_id));
        if (!step) continue;
        step.ok = b.is_error !== true;
        // The whole output, unabridged, is the point of this view — but a 5000
        // line build log still has to fit a terminal, so it is capped here and
        // the file itself remains the place to read the rest.
        step.output = compact(b.content, MAX_OUTPUT_CHARS);
      }
    }
  }
  return { steps, thoughts };
}

// The duration LEADS the row, in its own fixed column. This view exists to answer
// "what took the time", and a number that trails 160 characters of tool input
// cannot be scanned — which is what a first cut of this did.
const TIME_COLUMN = 8;

function stepLine(s: Step): string {
  const took = s.durationMs === undefined ? "" : formatDuration(s.durationMs);
  const who = s.owner ? `[${s.owner}] ` : "";
  const verdict = s.ok === undefined ? " …in flight" : s.ok ? "" : " ✗";
  return `${took.padStart(TIME_COLUMN)}  ${who}${s.tool} ${s.input}${verdict}`;
}

/** Render one view of a run. Returns the lines to print. */
export function renderLogView(runDir: string, view: LogView): string[] {
  const { steps, thoughts } = readRunLog(runDir);
  const out: string[] = [];

  if (view === "thinking") {
    if (thoughts.length === 0) {
      // An empty list used to be NORMAL and is now a symptom, so it names the
      // cause rather than reassuring. Both halves of the reasoning pair live in
      // `debugQueryOptions` (ADR-0002 decision 16): without a `thinking`
      // display the blocks arrive signed and empty, and without
      // `forwardSubagentText` the subagents — which do most of the work —
      // forward no blocks at all. A run predating that change has neither.
      out.push("  (no thinking blocks — the run captured none; see ADR-0002 decision 16)");
      return out;
    }
    for (const t of thoughts) {
      out.push(`  ${t.owner ? `[${t.owner}] ` : ""}────`);
      for (const line of t.text.split("\n")) out.push(`    ${line}`);
    }
    return out;
  }

  if (view === "slow") {
    const slow = steps
      .filter((s) => (s.durationMs ?? 0) >= SLOW_CALL_MS)
      .sort((a, b) => (b.durationMs ?? 0) - (a.durationMs ?? 0));
    if (slow.length === 0) {
      out.push(`  (nothing took ${formatDuration(SLOW_CALL_MS)} or more)`);
      return out;
    }
    for (const s of slow) out.push(`  ${stepLine(s)}`);
    return out;
  }

  for (const s of steps) {
    out.push(`  ${stepLine(s)}`);
    // The failure's own text, which the feed deliberately reduced to one line.
    if (s.ok === false && s.output) out.push(`      ${s.output}`);
  }
  return out;
}
