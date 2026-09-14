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
 * THE CREW, as a scrolling terminal can carry it: a block of rows drawn under a
 * rule, redrawn in place while the step lines stream above it.
 *
 * The console's crew view and this one are the SAME model — `buildCrew` decides
 * who is in the crew, how each member stands, what its live sub-line says and
 * how long it has been quiet, and this module only chooses glyphs and columns.
 * Nothing here re-walks the events to work out the tree, the states, the ages or
 * the captions: two surfaces that each derive their own is exactly how they come
 * to disagree about one run, and disagreeing about a 40-minute run is worse than
 * showing nothing.
 *
 * What IS this module's own is presentation a terminal forces: one tone per row
 * (the pane colours whole lines, so the width arithmetic stays exact — an ANSI
 * escape counted as a character is a wrapped line, and a wrapped line is a
 * mangled transcript), a hard truncation at the terminal's width, and a height
 * cap so the block can never be taller than the screen it is pinned to.
 *
 * Deliberately NOT here: the timeline. A time axis has no honest rendering in a
 * terminal that scrolls — `Crew.window` and `CrewMember.spans` are read by the
 * console's lanes and by nothing in this package.
 */

import {
  crewStateLabel,
  crewTone,
  formatAgentStatus,
  formatDuration,
  isCrewSettled,
  planTone,
  type Crew,
  type CrewMember,
  type CrewPlanItem,
  type CrewState,
  type CrewTask,
  type LineTone,
  type RunEventView,
} from "@aep/progress-view";
import type { AgentTags } from "./agent-tags.js";

/** One drawn line: its text, and the semantic weight the pane colours it by. */
export interface BlockRow {
  text: string;
  tone: LineTone;
}

export interface CrewBlockOptions {
  /** The terminal's width. Every row is truncated to fit inside it. */
  columns: number;
  /** The most lines the block may occupy, so it cannot outgrow the screen. */
  maxRows: number;
  /** This run's short names for its agents — the SAME registry the feed tags with. */
  tag: AgentTags;
}

/**
 * The state glyph.
 *
 * Never the only signal: the state's own word sits on the row whenever the
 * runtime's caption does not already lead with it, so the block reads the same
 * to somebody whose terminal has no colour. The glyph is for scanning, the word
 * for knowing — the same split the console's crew rows make.
 */
const GLYPHS: Record<CrewState, string> = {
  working: "●",
  waiting: "◑",
  stalled: "◔",
  done: "✓",
  failed: "✗",
  cancelled: "⊘",
};

/** A backgrounded shell command's glyph, matching the console's task rows. */
const TASK_GLYPH = "⟳";

/**
 * An agent's plan entry, by where it stands. `deleted` never reaches here — the
 * model drops a removed entry, since it is not work any more.
 *
 * The glyphs are this surface's (a terminal has no checkbox); the WEIGHT is the
 * model's `planTone`, so a completed entry reads the same green here as it does
 * on the console.
 */
const PLAN_GLYPHS: Record<string, string> = {
  pending: "☐",
  in_progress: "▸",
  completed: "☑",
};

/** How long a member has been quiet, in the sketch's shorthand. */
const QUIET = "♥";

/** Two spaces, so the block sits in the same column as the streamed step lines. */
const MARGIN = "  ";

/**
 * Line breaks collapsed to spaces, indentation kept.
 *
 * A row is one physical line **by construction**, not by every caller
 * remembering: `openCrewPane` redraws by counting the rows it last drew, so a
 * row carrying a newline makes the block taller than the pane believes and the
 * redraw eats the transcript above it. It also breaks `lay`'s arithmetic, which
 * measures a row by `String.length`. A backgrounded shell label is what bites —
 * a heredoc or an `&&` chain arrives with its newlines intact.
 *
 * Not `oneLine`, which trims: here the leading MARGIN and the depth indent are
 * the tree, and trimming them flattens it.
 */
function flat(text: string): string {
  return text.replace(/[\r\n]+/g, " ");
}

/** Cut to width, saying so — a silently cut line reads as a line that ended. */
function trunc(raw: string, width: number): string {
  const text = flat(raw);
  if (width <= 0) return "";
  if (text.length <= width) return text;
  return `${text.slice(0, Math.max(0, width - 1))}…`;
}

/**
 * A row's two halves: what it is, and how it is going.
 *
 * The right half is what a reader scans down, so it keeps its column even when
 * the left half has to be cut for it. When even that will not fit, the right
 * half is dropped rather than pushed off the edge half-drawn.
 */
function lay(leftRaw: string, rightRaw: string, width: number): string {
  // Flattened before anything is measured — see `flat`.
  const left = flat(leftRaw);
  const right = flat(rightRaw);
  if (!right) return trunc(left, width);
  if (right.length + 4 > width) return trunc(left, width);
  const gap = width - left.length - right.length;
  if (gap >= 2) return `${left}${" ".repeat(gap)}${right}`;
  return `${trunc(left, width - right.length - 2)}  ${right}`;
}

/** Whitespace collapsed onto one line — a closing report runs to a paragraph. */
function oneLine(text: string): string {
  return text.replace(/\s+/g, " ").trim();
}

/**
 * The live half of a row: what the runtime says it is doing, prefixed by the
 * state's word when the sentence does not already open with it.
 *
 * "waiting · waiting on the model" says it twice, which is the check the console
 * makes for the same reason. `working` never gets a word: it is the default
 * every quiet row would carry, and a column of "working" tells nobody anything.
 */
function liveCaption(state: CrewState, caption: string): string {
  const word = crewStateLabel(state);
  if (state === "working" || caption.startsWith(word)) return caption;
  return caption ? `${word} · ${caption}` : word;
}

/** How long a member has been going, and how long since it last spoke. */
function liveTiming<E>(member: CrewMember<E>): string {
  const parts: string[] = [];
  if (member.elapsedMs !== undefined) parts.push(formatDuration(member.elapsedMs));
  if (member.silentForMs !== undefined) parts.push(`${QUIET} ${formatDuration(member.silentForMs)}`);
  return parts.join(" · ");
}

function memberRow<E extends RunEventView>(
  member: CrewMember<E>,
  opts: CrewBlockOptions,
  width: number,
): BlockRow {
  const settled = isCrewSettled(member.state);
  const tag = opts.tag(member.id);
  const indent = "  ".repeat(member.depth);
  const name = `${GLYPHS[member.state]} ${tag ? `${tag} ` : ""}${member.agent.label}`;
  // A settled member's sub-line is its closing REPORT — the only copy there will
  // ever be, since a spawned agent's transcript dies with the container. A live
  // one's is whatever it last said about itself.
  const caption = oneLine(settled ? member.caption : liveCaption(member.state, member.caption));
  // `background` is printed only when the runtime said TRUE: the platform pushes
  // fan-out into the background deliberately, and a reader wondering why a
  // parent kept working while a child ran is owed the reason on the row.
  const flag = member.background ? " (background)" : "";
  const left = `${MARGIN}${indent}${name}${flag}${caption ? `  ${caption}` : ""}`;
  // The runtime's OWN totals once it settled — status, duration, tool count,
  // line counts, tokens — through the same formatter the console's section
  // headers use, so the wording cannot fork. While it runs, the two clocks.
  const right = settled ? formatAgentStatus(member.agent) : liveTiming(member);
  return { text: lay(left, right, width), tone: crewTone(member.state) };
}

/**
 * A backgrounded command, under the agent that started it.
 *
 * The duration is arithmetic on the model's own two timestamps rather than a
 * fact of its own: `CrewTask` reports when a command started and when it
 * settled, and how long that is cannot be wrong.
 */
function taskRow(task: CrewTask, depth: number, now: number, width: number): BlockRow {
  const indent = "  ".repeat(depth + 1);
  const elapsed =
    task.startedAtMs === undefined
      ? ""
      : formatDuration(Math.max(0, (task.settledAtMs ?? now) - task.startedAtMs));
  const right = [task.status, elapsed].filter(Boolean).join(" ");
  return {
    text: lay(`${MARGIN}${indent}${TASK_GLYPH} ${task.label}`, right, width),
    tone: task.status === "failed" ? "error" : "muted",
  };
}

/** One entry of an agent's plan, under the agent whose plan it is. */
function planRow(item: CrewPlanItem, depth: number, width: number): BlockRow {
  const indent = "  ".repeat(depth + 1);
  const glyph = PLAN_GLYPHS[item.status] ?? "☐";
  const title = item.title || item.id;
  return {
    text: trunc(`${MARGIN}${indent}${glyph} ${title}`, width),
    tone: planTone(item.status),
  };
}

/** The rule, carrying the run-level facts so it costs no extra line. */
function ruleRow<E>(crew: Crew<E>, width: number): BlockRow {
  const parts = [`${String(crew.agents)} agent${crew.agents === 1 ? "" : "s"}`, `${String(crew.running)} running`];
  if (crew.silentForMs !== undefined) parts.push(`${QUIET} ${formatDuration(crew.silentForMs)}`);
  if (crew.outcome) parts.push(crew.outcome);
  const head = `${MARGIN}── crew · ${parts.join(" · ")} `;
  return { text: trunc(head.padEnd(width, "─"), width), tone: "muted" };
}

/**
 * The crew block: a rule, then one row per member in the model's own depth-first
 * order, each carrying its backgrounded commands and its plan entries.
 *
 * `members` is already flattened depth-first from the lead and every row draws
 * its own declared `depth`, so the nesting is decided in one place instead of by
 * a recursion here that could file a grandchild as a sibling.
 */
export function renderCrewBlock<E extends RunEventView>(
  crew: Crew<E>,
  now: number,
  opts: CrewBlockOptions,
): BlockRow[] {
  // One column short of the terminal's width. A row that reaches the last column
  // wraps, and a wrapped row makes the block one line taller than the pane
  // believes it is — which is how a redraw eats the transcript above it.
  const width = Math.max(20, opts.columns - 1);
  const rows: BlockRow[] = [ruleRow(crew, width)];
  for (const member of crew.members) {
    rows.push(memberRow(member, opts, width));
    for (const task of member.tasks) rows.push(taskRow(task, member.depth, now, width));
    // Already placed on its owner by the model — including the fallback for an
    // entry naming an agent no `agent_started` ever declared, which falls to the
    // lead rather than vanishing.
    for (const item of member.plan) rows.push(planRow(item, member.depth, width));
  }
  if (rows.length <= opts.maxRows) return rows;
  // Too tall for the screen it is pinned to. The tail is dropped and SAID to be
  // dropped: a block silently cut off is a block a reader trusts to be complete.
  const kept = rows.slice(0, Math.max(1, opts.maxRows - 1));
  const hidden = rows.length - kept.length;
  kept.push({ text: trunc(`${MARGIN}… ${String(hidden)} more`, width), tone: "muted" });
  return kept;
}
