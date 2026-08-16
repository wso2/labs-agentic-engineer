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
 * The PATH the agent took, read off `claude.log`.
 *
 * This is the half of the score that the CLI and the skill are tuned against.
 * The outcome half (`build.ts`) is what keeps it honest: a run can reach a green
 * build by luck, and a run can take a clean path to the wrong answer, so neither
 * number means anything without the other.
 *
 * Every metric here is derived from the transcript rather than measured during
 * the run, which is what lets a recorded log from any past run — including the
 * playground's, which has the same shape — be re-scored by a newer extractor.
 * The tests do exactly that, against two real runs.
 */

import { PATH_METRICS } from "../config.js";

const { filters: FILTERS, noBodyChars: NO_BODY_CHARS, charsPerToken: CHARS_PER_TOKEN } = PATH_METRICS;
const VERBS = new Set<string>(PATH_METRICS.verbs);

/** One `bal library` invocation and what came back. */
export interface Lookup {
  /** The full shell command, as the agent wrote it. */
  command: string;
  /** `search` | `overview` | `ops` | `type` | `api`, or "" when the verb is unreadable. */
  verb: string;
  /** The `org/name` the verb was pointed at, or "" for `search` / a malformed call. */
  target: string;
  /**
   * Every verb in the command, because one Bash call can hold more than one
   * invocation — a measured run ran `type ballerina/log --help; overview
   * ballerina/log` as a single call, and reading only the first undercounted
   * `overview` by one. A `Lookup` stays ONE tool call (that is the unit of
   * latency and of a turn), while the verb census counts invocations.
   */
  verbs: string[];
  /** Wrapped in `head`/`grep`/`sed`/`tail`/`awk` — the habit that truncates navigation. */
  piped: boolean;
  /** The tool answered with a failure JSON on stderr (exit 1), not a document. */
  failed: boolean;
  /** The failure's `kind`, when it failed. */
  failureKind?: string;
  /** The `## Next` block survived into the result. */
  sawNext: boolean;
  /** Result size, the input the model actually paid for. */
  chars: number;
}

export interface PathMetrics {
  lookups: number;
  /** How many were wrapped in a filter. The skill and `--help` both ask for zero. */
  piped: number;
  /** Exit-2 answers: a call that bought nothing but a suggestion. */
  failed: number;
  /** Results that carried the `## Next` navigation block. */
  sawNext: number;
  /** Calls per verb, so "never reached for `ops`" is visible rather than inferred. */
  byVerb: Record<string, number>;
  /**
   * The longest run of consecutive lookups aimed at one package that bought
   * nothing — the `aws.auth` shape. Counts a call as zero-yield when it failed
   * OR came back with no usable body, because `api … | grep X` returning empty
   * is the same dead end as a failure and cost the same turn.
   */
  worstDetour: Detour;
  /** Total result bytes, and the model-input cost they stand for. */
  lookupChars: number;
  lookupTokens: number;
  /** Whole-session figures, straight off the SDK's own result message. */
  costUsd: number;
  durationMs: number;
  turns: number;
  /** Reasoning actually captured — 0 means the run predates ADR-0002 decision 16. */
  thinkingBlocks: number;
  subagentThinkingBlocks: number;
}

export interface Detour {
  /** The package the wasted calls were aimed at, "" when there was no detour. */
  target: string;
  /** How many consecutive zero-yield calls. */
  calls: number;
  /** What those calls cost in result bytes. */
  chars: number;
}

/**
 * Parse an NDJSON transcript.
 *
 * Tolerant of a truncated last line: a killed run leaves one, and refusing to
 * score the other 400 messages because of it would lose the run that most needs
 * reading.
 */
export function parseTranscript(ndjson: string): Record<string, unknown>[] {
  const out: Record<string, unknown>[] = [];
  for (const line of ndjson.split("\n")) {
    if (!line.trim()) continue;
    try {
      const parsed: unknown = JSON.parse(line);
      if (parsed && typeof parsed === "object") out.push(parsed as Record<string, unknown>);
    } catch {
      // Deliberately silent: see above.
    }
  }
  return out;
}

export function readPathMetrics(ndjson: string): PathMetrics {
  const messages = parseTranscript(ndjson);
  const results = toolResults(messages);
  const lookups = readLookups(messages, results);
  const thinking = countThinking(messages);
  const session = readSessionResult(messages);

  const byVerb: Record<string, number> = {};
  for (const l of lookups) {
    for (const verb of l.verbs.length > 0 ? l.verbs : [l.verb || "?"]) {
      byVerb[verb] = (byVerb[verb] ?? 0) + 1;
    }
  }

  const chars = lookups.reduce((sum, l) => sum + l.chars, 0);
  return {
    lookups: lookups.length,
    piped: lookups.filter((l) => l.piped).length,
    failed: lookups.filter((l) => l.failed).length,
    sawNext: lookups.filter((l) => l.sawNext).length,
    byVerb,
    worstDetour: worstDetour(lookups),
    lookupChars: chars,
    // The same 4-chars-per-token rule the analysis used. Approximate on purpose:
    // it is compared against itself across runs, never billed.
    lookupTokens: Math.round(chars / CHARS_PER_TOKEN),
    ...session,
    ...thinking,
  };
}

/** Every `bal library` call in the transcript, in the order it was made. */
export function readLookups(messages: Record<string, unknown>[], results: Map<string, string>): Lookup[] {
  const out: Lookup[] = [];
  for (const message of messages) {
    if (message.type !== "assistant") continue;
    for (const block of contentBlocks(message)) {
      if (block.type !== "tool_use" || block.name !== "Bash") continue;
      const input = block.input as Record<string, unknown> | undefined;
      const command = typeof input?.command === "string" ? input.command : "";
      if (!/\bbal library\b/.test(command)) continue;
      const body = results.get(typeof block.id === "string" ? block.id : "") ?? "";
      const failure = readFailure(body);
      out.push({
        command,
        ...readInvocation(command),
        verbs: readVerbs(command),
        piped: FILTERS.test(command),
        failed: failure !== undefined,
        ...(failure !== undefined ? { failureKind: failure } : {}),
        sawNext: body.includes("## Next"),
        chars: body.length,
      });
    }
  }
  return out;
}

/**
 * The verb and package a command was aimed at.
 *
 * Read off the token AFTER `library` rather than by matching each verb name, so
 * a future verb is reported as itself instead of silently counted as "?". A
 * `--help` probe lands here as its own verb and that is correct — it is a call
 * the run spent, and `type --help` was one of them.
 */
export function readInvocation(command: string): { verb: string; target: string } {
  const match = /\bbal library\s+([^\s|;&]+)(?:\s+([^\s|;&-][^\s|;&]*))?/.exec(command);
  if (!match) return { verb: "", target: "" };
  const verb = match[1] ?? "";
  if (!VERBS.has(verb)) return { verb: verb.replace(/^--?/, "") || "", target: "" };
  // `search` takes keywords, never a coordinate — reporting the first keyword as
  // a package would invent detours between unrelated searches.
  if (verb === "search") return { verb, target: "" };
  return { verb, target: match[2] ?? "" };
}

/**
 * Every verb in a command, in order — one entry per `bal library` invocation.
 *
 * `--help` and any future verb come out as themselves rather than as "?", so a
 * verb this build does not know about is still counted under its own name.
 */
export function readVerbs(command: string): string[] {
  const out: string[] = [];
  for (const match of command.matchAll(/\bbal library\s+([^\s|;&]+)/g)) {
    const token = match[1] ?? "";
    out.push(VERBS.has(token) ? token : token.replace(/^--?/, ""));
  }
  return out.filter((v) => v !== "");
}

/** The `kind` of a failure JSON, or undefined when the body is a document. */
function readFailure(body: string): string | undefined {
  const trimmed = body.trim();
  if (!trimmed.startsWith("{")) return undefined;
  try {
    const parsed: unknown = JSON.parse(trimmed);
    if (parsed && typeof parsed === "object") {
      const kind = (parsed as Record<string, unknown>).kind;
      if (typeof kind === "string") return kind;
    }
  } catch {
    return undefined;
  }
  return undefined;
}

/**
 * The longest streak of consecutive zero-yield lookups against one package.
 *
 * Consecutive in CALL order, not in package order: the measured detours
 * interleave — `type aws.auth`, `type aws.s3 --deps`, `type aws --deps` are
 * three different targets answering one question — so the streak is broken by a
 * call that YIELDED, not by a call aimed elsewhere. Attribution goes to the
 * target that appears most in the streak, which is the package the agent was
 * circling even when it spelled it three ways.
 */
export function worstDetour(lookups: Lookup[]): Detour {
  let best: Detour = { target: "", calls: 0, chars: 0 };
  let run: Lookup[] = [];
  const settle = (): void => {
    if (run.length > best.calls) {
      best = {
        target: dominantTarget(run),
        calls: run.length,
        chars: run.reduce((sum, l) => sum + l.chars, 0),
      };
    }
    run = [];
  };
  for (const lookup of lookups) {
    if (lookup.failed || lookup.chars <= NO_BODY_CHARS) run.push(lookup);
    else settle();
  }
  settle();
  return best;
}

function dominantTarget(run: Lookup[]): string {
  const counts = new Map<string, number>();
  for (const l of run) {
    if (l.target) counts.set(l.target, (counts.get(l.target) ?? 0) + 1);
  }
  let winner = "";
  let most = 0;
  for (const [target, n] of counts) {
    if (n > most) {
      winner = target;
      most = n;
    }
  }
  return winner;
}

function countThinking(messages: Record<string, unknown>[]): {
  thinkingBlocks: number;
  subagentThinkingBlocks: number;
} {
  let total = 0;
  let sub = 0;
  for (const message of messages) {
    if (message.type !== "assistant") continue;
    for (const block of contentBlocks(message)) {
      // An EMPTY thinking block is not captured reasoning — it is the signature
      // of a run with no `thinking` display, and counting it would report the
      // capture as working when it is not.
      if (block.type !== "thinking" || !block.thinking) continue;
      total += 1;
      if (message.parent_tool_use_id) sub += 1;
    }
  }
  return { thinkingBlocks: total, subagentThinkingBlocks: sub };
}

function readSessionResult(messages: Record<string, unknown>[]): {
  costUsd: number;
  durationMs: number;
  turns: number;
} {
  const result = messages.find((m) => m.type === "result");
  return {
    costUsd: numberOf(result?.total_cost_usd),
    durationMs: numberOf(result?.duration_ms),
    turns: numberOf(result?.num_turns),
  };
}

/** tool_use_id → the result text the model saw. */
export function toolResults(messages: Record<string, unknown>[]): Map<string, string> {
  const out = new Map<string, string>();
  for (const message of messages) {
    if (message.type !== "user") continue;
    for (const block of contentBlocks(message)) {
      if (block.type !== "tool_result") continue;
      const id = typeof block.tool_use_id === "string" ? block.tool_use_id : "";
      if (id) out.set(id, resultText(block.content));
    }
  }
  return out;
}

/** A result is a string, or the array of blocks the SDK splits it into. */
function resultText(content: unknown): string {
  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return "";
  return content
    .map((b) => (b && typeof b === "object" ? String((b as Record<string, unknown>).text ?? "") : ""))
    .join("");
}

function contentBlocks(message: Record<string, unknown>): Record<string, unknown>[] {
  const inner = message.message;
  if (!inner || typeof inner !== "object") return [];
  const content = (inner as Record<string, unknown>).content;
  if (!Array.isArray(content)) return [];
  return content.filter((b): b is Record<string, unknown> => !!b && typeof b === "object");
}

function numberOf(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}
