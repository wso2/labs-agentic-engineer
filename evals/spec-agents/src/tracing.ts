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
 * Full agent-communication capture, three layers per run:
 *  - evalite traces (serve UI): one trace per turn;
 *  - `<name>.transcript.md`: the readable agent↔sim conversation;
 *  - `<name>.trace.json`: the raw, unabridged StreamPart stream.
 * Per-turn token usage rides the terminal manifest frame (#249) and is summed
 * into the run's cost report.
 */

import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { reportTrace, shouldReportTrace } from "evalite/traces";
import type { QuestionSessionInfo, StreamPart } from "@aep/agent-stream";
import type { SimAnswer } from "./sim-user.js";

export interface TurnUsage {
  inputTokens: number;
  outputTokens: number;
}

export interface TurnRecord {
  turn: number;
  section: string;
  instruction: string;
  /** Concatenated text-deltas — everything the agent said this turn. */
  agentText: string;
  toolCalls: Array<{ toolName: string; input: unknown }>;
  questions: string[];
  /**
   * The grilling-session checklist this turn's round carried (#486), when it was
   * a session round. Lifted out of the tool input because it is the one thing a
   * reviewer must be able to see at a glance: a deep-dive turn that asked
   * WITHOUT a checklist ran a one-form interview instead of a session.
   */
  session?: QuestionSessionInfo;
  answers: SimAnswer[];
  /** The raw, unabridged StreamPart stream for this turn. */
  parts: StreamPart[];
  usage?: TurnUsage;
  ms: number;
}

export function newTurnRecord(section: string, turn: number, instruction: string): TurnRecord {
  return { turn, section, instruction, agentText: "", toolCalls: [], questions: [], answers: [], parts: [], ms: 0 };
}

/** Fold one streamed part into the record (the driver's onPart hook). */
export function collectPart(rec: TurnRecord, part: StreamPart): void {
  rec.parts.push(part);
  const p = part as { type: string; text?: string; toolName?: string; input?: unknown; usage?: { inputTokens?: number; outputTokens?: number } };
  if (p.type === "text-delta" && p.text) rec.agentText += p.text;
  if (p.type === "tool-call") rec.toolCalls.push({ toolName: p.toolName ?? "?", input: p.input });
  if (p.type === "manifest" && p.usage) {
    rec.usage = { inputTokens: p.usage.inputTokens ?? 0, outputTokens: p.usage.outputTokens ?? 0 };
  }
}

export function sumUsage(records: TurnRecord[]): TurnUsage {
  return records.reduce(
    (acc, r) => ({
      inputTokens: acc.inputTokens + (r.usage?.inputTokens ?? 0),
      outputTokens: acc.outputTokens + (r.usage?.outputTokens ?? 0),
    }),
    { inputTokens: 0, outputTokens: 0 },
  );
}

/** `Voting & nominations: ✔ Eligibility · ▸ Quorum · ○ Nominee limits` */
export function renderSessionLine(session: QuestionSessionInfo): string {
  const marks: Record<string, string> = { done: "✔", now: "▸", todo: "○" };
  const areas = session.areas.map((a) => `${marks[a.state] ?? "○"} ${a.name}`).join(" · ");
  const title = session.title?.trim();
  return `session${title ? ` "${title}"` : ""}: ${areas || "(no areas)"}`;
}

/** One evalite trace per turn (visible under the case in the serve UI). */
export function reportTurnTrace(rec: TurnRecord, start: number): void {
  if (!shouldReportTrace()) return;
  reportTrace({
    input: rec.instruction,
    output: [
      rec.agentText.trim(),
      rec.toolCalls.length ? `tool calls: ${rec.toolCalls.map((t) => t.toolName).join(", ")}` : "",
      rec.session ? renderSessionLine(rec.session) : "",
      rec.questions.length ? `asked:\n${rec.questions.map((q) => `- ${q}`).join("\n")}` : "",
      rec.answers.length
        ? `sim answered:\n${rec.answers.map((a) => `- [${a.source}] ${a.selected.join(", ")}${a.freeText ? ` — ${a.freeText}` : ""}`).join("\n")}`
        : "",
    ]
      .filter(Boolean)
      .join("\n\n"),
    ...(rec.usage
      ? {
          usage: {
            inputTokens: rec.usage.inputTokens,
            outputTokens: rec.usage.outputTokens,
            totalTokens: rec.usage.inputTokens + rec.usage.outputTokens,
          },
        }
      : {}),
    start,
    end: start + rec.ms,
  });
}

export function renderTranscript(
  title: string,
  records: TurnRecord[],
  artifacts: Record<string, string>,
): string {
  const lines: string[] = [`# Transcript — ${title}`, ""];
  let section = "";
  for (const r of records) {
    if (r.section !== section) {
      section = r.section;
      lines.push(`# Section: ${section}`, "");
    }
    lines.push(`## Turn ${r.turn} (${(r.ms / 1000).toFixed(1)}s${r.usage ? `, ${r.usage.inputTokens}in/${r.usage.outputTokens}out tokens` : ""})`, "");
    lines.push("### User → agent", "", "```", r.instruction, "```", "");
    if (r.agentText.trim()) lines.push("### Agent said", "", r.agentText.trim(), "");
    if (r.session) lines.push("### Grilling session round", "", renderSessionLine(r.session), "");
    if (r.toolCalls.length) {
      lines.push("### Tool calls", "");
      for (const t of r.toolCalls) lines.push(`- \`${t.toolName}\` ${JSON.stringify(t.input)?.slice(0, 400) ?? ""}`);
      lines.push("");
    }
    if (r.answers.length) {
      lines.push("### Sim user answered", "");
      for (const a of r.answers) {
        const vol = a.volunteered.length ? ` (volunteered: ${a.volunteered.join(" | ")})` : "";
        lines.push(`- **${a.question}** → [${a.source}] ${a.selected.join(", ")}${a.freeText ? ` — ${a.freeText}` : ""}${vol}`);
      }
      lines.push("");
    }
  }
  for (const [path, content] of Object.entries(artifacts)) {
    lines.push(`# Artifact: ${path}`, "", content || "(empty)", "");
  }
  return lines.join("\n");
}

/** Write the readable transcript + raw trace next to the run's project dir. */
export function writeRunArtifacts(
  home: string,
  name: string,
  records: TurnRecord[],
  artifacts: Record<string, string>,
): { transcriptPath: string; tracePath: string } {
  mkdirSync(home, { recursive: true });
  const transcriptPath = join(home, `${name}.transcript.md`);
  const tracePath = join(home, `${name}.trace.json`);
  writeFileSync(transcriptPath, renderTranscript(name, records, artifacts));
  writeFileSync(tracePath, JSON.stringify(records, null, 2));
  return { transcriptPath, tracePath };
}
