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

// The declared plan (#576, ADR-0025): what the turn said it is ABOUT to write,
// reconciled against what it actually writes. runTurn folds `declare_plan`
// tool-calls and file mutations into this store; the spec rail subscribes for
// its checklist and count, and SpecView for follow-the-write (ADR-0026).
// Lives next to chatStore so runTurn does not import the spec feature.
//
// Every status is DERIVED from the stream, never self-reported by the agent:
// `writing` when the file's mutation starts, `done` when it completes, `error`
// only for the entry being written when the turn died. A clean turn's plan
// dissolves — the files are simply there; a dead turn's wreckage (done ticks,
// the error, the remaining ghosts) persists until the next declaring turn
// replaces it, because the wreckage IS the case for updating the design.

import { DECLARE_PLAN_TOOL } from "@aep/agent-stream";

export type PlanEntryStatus = "planned" | "writing" | "done" | "error";

/**
 * The mutations that WRITE a document, and so move a plan entry along.
 * `removeFile` is excluded on purpose: a deletion is not a write, following it
 * would send the editor to a document about to vanish, and ticking its entry
 * green would record a deleted file as written.
 */
export const WRITE_TOOLS: ReadonlySet<string> = new Set(["addFile", "editFile"]);

/** The HITL tools whose call ends a turn waiting on the user (ADR-0012). */
const QUESTION_TOOLS: ReadonlySet<string> = new Set(["ask_question", "ask_questions"]);


export interface PlanEntry {
  /** Full repo-relative spec-bundle path — the reconciliation identity. */
  path: string;
  status: PlanEntryStatus;
}

export interface PlanSnapshot {
  /** The declaring turn. A different turn declaring replaces the snapshot. */
  turnId: string;
  /** The UNION of every declaration in the turn, first-seen order. */
  entries: PlanEntry[];
  /**
   * The path an agent write is currently streaming — tracked for EVERY file
   * mutation, declared or not, because follow-the-write (ADR-0026) steers by
   * it and a turn without a plan still writes.
   */
  writingPath: string | null;
  /** False once the turn reached a terminal. */
  turnActive: boolean;
  /**
   * The turn ended by ASKING, not by finishing: the agent put a question to
   * the user partway through the work and is waiting on the answer. The plan
   * survives that gap — the answer's turn carries the same work on — so a
   * pause is neither a completion nor a failure.
   */
  paused: boolean;
  /** The turn died leaving undone entries — the rail keeps the residue. */
  wreckage: boolean;
}

const plans = new Map<string, PlanSnapshot>();
const listeners = new Map<string, Set<() => void>>();

function notify(chatKey: string): void {
  for (const fn of listeners.get(chatKey) ?? []) fn();
}

// Every write REPLACES the snapshot (and its entries array) rather than
// mutating it: `useSyncExternalStore` compares snapshots by identity, so an
// in-place mutation renders only when something ELSE happens to re-render the
// subscriber — the exact staleness this store exists to prevent.
function commit(chatKey: string, next: PlanSnapshot): void {
  plans.set(chatKey, next);
  notify(chatKey);
}

function fresh(turnId: string): PlanSnapshot {
  return {
    turnId,
    entries: [],
    writingPath: null,
    turnActive: true,
    paused: false,
    wreckage: false,
  };
}

/**
 * The base the fold builds THIS turn's next snapshot from. Any event from a
 * turn other than the snapshot's starts fresh — a new turn's first plan or
 * first write is what wipes a previous turn's wreckage.
 */
function forTurn(chatKey: string, turnId: string): PlanSnapshot {
  const cur = plans.get(chatKey);
  if (cur && cur.turnId === turnId) return cur;
  // A PAUSED plan is adopted by the turn that resumes it, rather than thrown
  // away. A design run that asks a question mid-flight continues in the answer's
  // turn — same work, new turn id — and starting fresh there would drop the
  // plan and its progress at the exact moment the user came back to it.
  if (cur?.paused) {
    return { ...cur, turnId, turnActive: true, paused: false };
  }
  // Wreckage outlives the turn that follows it. Decision 6 says the residue
  // stands "until the next DECLARING turn replaces it" — so an ordinary turn
  // in between (a chat edit, a settle) carries it along rather than quietly
  // clearing the alarm by writing one unrelated file. Its writes still count:
  // a ghost this turn happens to write is settled like any other entry.
  if (cur?.wreckage) {
    return { ...cur, turnId, turnActive: true };
  }
  return fresh(turnId);
}

/**
 * Parse a `declare_plan` tool-call input (which may arrive as a JSON string —
 * same tolerance as the register-draft fold). Returns the ordered path list,
 * or null when the shape is not a plan at all.
 */
export function parseDeclarePlan(input: unknown): string[] | null {
  let value = input;
  if (typeof value === "string") {
    try {
      value = JSON.parse(value) as unknown;
    } catch {
      return null;
    }
  }
  if (typeof value !== "object" || value === null) return null;
  const paths = (value as { paths?: unknown }).paths;
  if (!Array.isArray(paths)) return null;
  return paths
    .filter((p): p is string => typeof p === "string")
    .map((p) => p.trim())
    .filter((p) => p.length > 0);
}

/**
 * Fold one declaration: union-append, restated paths ignored, no removal —
 * the only rule robust to an agent restating its whole plan. Returns how many
 * entries were genuinely new (the chat activity row says "N artifacts").
 */
export function planDeclared(chatKey: string, turnId: string, paths: string[]): number {
  const cur = plans.get(chatKey);
  // The declaring turn is the one that replaces a previous run's residue: this
  // run's plan is what it just declared, not the last run's leftovers.
  const plan =
    cur && cur.turnId !== turnId && cur.wreckage ? fresh(turnId) : forTurn(chatKey, turnId);
  const known = new Set(plan.entries.map((e) => e.path));
  const added: PlanEntry[] = [];
  for (const path of paths) {
    if (known.has(path)) continue;
    known.add(path);
    added.push({ path, status: "planned" });
  }
  if (added.length > 0) {
    commit(chatKey, { ...plan, entries: [...plan.entries, ...added] });
  } else if (plans.get(chatKey) !== plan) {
    commit(chatKey, plan); // a new turn restated an old plan — still replaces it
  }
  return added.length;
}

/** A file mutation's input stream opened and resolved its path. */
export function planFileWriting(chatKey: string, turnId: string, path: string): void {
  const plan = forTurn(chatKey, turnId);
  commit(chatKey, {
    ...plan,
    writingPath: path,
    entries: plan.entries.map((e) =>
      e.path === path && e.status !== "done" ? { ...e, status: "writing" } : e,
    ),
  });
}

/**
 * That mutation's input stream closed — the body is complete, so nothing more
 * is being typed. The entry stays `writing`: whether the write was ACCEPTED is
 * a separate fact that arrives with the tool-result, and the chat card learns
 * the same lesson one line over — painting success here would tick an entry
 * the write-gates may still reject.
 */
export function planFileStreamed(chatKey: string, turnId: string, path: string): void {
  const plan = plans.get(chatKey);
  if (!plan || plan.turnId !== turnId) return;
  if (plan.writingPath !== path) return;
  commit(chatKey, { ...plan, writingPath: null });
}

/** The bundle ruled on the write: `done`, or `error` when it was rejected. */
export function planFileSettled(
  chatKey: string,
  turnId: string,
  path: string,
  ok: boolean,
): void {
  const plan = plans.get(chatKey);
  if (!plan || plan.turnId !== turnId) return;
  const entries = plan.entries.map((e) =>
    e.path === path ? { ...e, status: ok ? ("done" as const) : ("error" as const) } : e,
  );
  // A retained wreck that has since been written out in full is no longer a
  // wreck — the alarm goes with the last outstanding entry.
  if (plan.wreckage && entries.every((e) => e.status === "done")) {
    plans.delete(chatKey);
    notify(chatKey);
    return;
  }
  commit(chatKey, {
    ...plan,
    writingPath: plan.writingPath === path ? null : plan.writingPath,
    entries,
  });
}

/**
 * The turn reached a terminal. Clean → the plan dissolves (the files are
 * simply there). Failed → the entry being written becomes the error, the rest
 * stay ghosts, and the residue persists until the next declaring turn.
 */
export function planTurnEnded(
  chatKey: string,
  turnId: string,
  status: "completed" | "failed",
  awaitingAnswer = false,
): void {
  const plan = plans.get(chatKey);
  if (!plan || plan.turnId !== turnId) return;
  if (status === "completed") {
    // Ended by asking, with work still outstanding: hold the plan for the turn
    // that answers. Not wreckage — nothing failed — so no alarm is raised.
    if (awaitingAnswer && plan.entries.some((e) => e.status !== "done")) {
      commit(chatKey, { ...plan, turnActive: false, paused: true, writingPath: null });
      return;
    }
    plans.delete(chatKey);
    notify(chatKey);
    return;
  }
  const entries = plan.entries.map((e) =>
    e.status === "writing" ? { ...e, status: "error" as const } : e,
  );
  const wreckage = entries.some((e) => e.status !== "done");
  if (!wreckage) {
    plans.delete(chatKey);
    notify(chatKey);
    return;
  }
  commit(chatKey, {
    ...plan,
    entries,
    writingPath: null,
    turnActive: false,
    paused: false,
    wreckage,
  });
}

export function peekPlan(chatKey: string): PlanSnapshot | null {
  return plans.get(chatKey) ?? null;
}

export function subscribePlan(chatKey: string, fn: () => void): () => void {
  const set = listeners.get(chatKey) ?? new Set();
  set.add(fn);
  listeners.set(chatKey, set);
  return () => set.delete(fn);
}

/** Drop any plan for this key — tests drain the module-scoped map. */
export function clearPlan(chatKey: string): void {
  if (!plans.has(chatKey)) return;
  plans.delete(chatKey);
  notify(chatKey);
}

// --- Rehydrate (decision 6: reconstructed the way question cards are) --------

interface HistoryPart {
  type?: string;
  toolName?: string;
  input?: unknown;
  /** `tool-result` payload: `{ type: "json", value: OpResult }`. */
  output?: unknown;
}

/**
 * Rebuild the residue from the replayed transcript, so wreckage survives a
 * reload for as long as the conversation log does — the same durability the
 * question cards have.
 *
 * Accumulated PER TURN, across every assistant message in it. The AI SDK
 * appends one assistant message per STEP (`run-turn.ts` pushes the whole of
 * `result.responseMessages`), so a turn's `declare_plan` lands in the step that
 * made it and the writes it predicted land in later ones — reading a single
 * message would find a declaration with no writes beside it and rehydrate a
 * clean turn as permanent wreckage. A user message opens the next turn, which
 * is what bounds the accumulation.
 *
 * The last turn that declared anything is the record. If its plan completed,
 * nothing is projected — the plan dissolved — so only a turn that died short
 * leaves a snapshot here. Skipped entirely while a live fold owns the store:
 * history REPLACES the log around the very moments a turn is running, and the
 * stream is the fresher truth.
 */
export function rehydratePlanFromHistory(
  chatKey: string,
  history: Array<{ role: string; content: unknown }>,
): void {
  const current = plans.get(chatKey);
  if (current?.turnActive) return;

  interface TurnAcc {
    entries: PlanEntry[];
    known: Set<string>;
    declared: boolean;
    /** The work put a question to the user and has not been answered since. */
    awaitingAnswer: boolean;
  }
  const newTurn = (): TurnAcc => ({
    entries: [],
    known: new Set(),
    declared: false,
    awaitingAnswer: false,
  });

  // One accumulator per turn; a user message opens the next one.
  const turns: TurnAcc[] = [newTurn()];
  for (const message of history) {
    if (message.role === "user") {
      // A user message while a QUESTION IS OUTSTANDING is the other half of
      // that question, and the writing carries on in the turn it opens — so it
      // continues the work rather than starting new work.
      //
      // Decided on the STATE, never on what the message says. Reading the
      // answer markers off the text looked equivalent and was not: ADR-0012
      // makes free text in the composer an equally valid answer, so a user who
      // types "just use Stripe" instead of clicking an option carries no
      // marker — and the plan was severed exactly as it was before. This is
      // also the rule the live fold already follows, where a paused plan is
      // adopted by whatever turn comes next.
      const open = turns[turns.length - 1]!;
      if (open.awaitingAnswer) {
        open.awaitingAnswer = false;
        continue;
      }
      turns.push(newTurn());
      continue;
    }
    if (message.role !== "assistant" && message.role !== "tool") continue;
    if (!Array.isArray(message.content)) continue;
    const turn = turns[turns.length - 1]!;
    for (const raw of message.content) {
      if (typeof raw !== "object" || raw === null) continue;
      const part = raw as HistoryPart;
      // The VERDICT settles an entry, never the call. A write that the bundle
      // rejected still leaves a `tool-call` in the transcript, so settling on
      // the call alone rehydrated a refused write as a green tick — the same
      // trap the live fold has, one surface over. `removeFile` is absent from
      // WRITE_TOOLS on purpose: a deletion is not a write.
      if (part.type === "tool-result" && WRITE_TOOLS.has(part.toolName ?? "")) {
        const value = (part.output as { value?: unknown } | undefined)?.value as
          | { ok?: unknown; path?: unknown }
          | undefined;
        const path = value?.path;
        const entry =
          typeof path === "string" ? turn.entries.find((e) => e.path === path) : undefined;
        if (entry) entry.status = value?.ok === false ? "error" : "done";
        continue;
      }
      if (part.type !== "tool-call") continue;
      if (part.toolName === DECLARE_PLAN_TOOL) {
        turn.declared = true;
        for (const path of parseDeclarePlan(part.input) ?? []) {
          if (turn.known.has(path)) continue;
          turn.known.add(path);
          turn.entries.push({ path, status: "planned" });
        }
      } else if (QUESTION_TOOLS.has(part.toolName ?? "")) {
        turn.awaitingAnswer = true;
      } else if (WRITE_TOOLS.has(part.toolName ?? "")) {
        const path = (part.input as { path?: unknown } | null | undefined)?.path;
        const entry =
          typeof path === "string"
            ? turn.entries.find((e) => e.path === path)
            : undefined;
        if (entry) entry.status = "done";
      }
    }
  }

  const record = turns.filter((t) => t.declared).at(-1);
  const unfinished = record !== undefined && record.entries.some((e) => e.status !== "done");
  // Outstanding work with a question still open is PAUSED, not wrecked: the
  // agent is waiting on the user, and reloading the page must not turn that
  // into "the design run didn\'t finish".
  const wreckage = unfinished && !(record?.awaitingAnswer ?? false);
  if (!unfinished) {
    if (current) {
      plans.delete(chatKey);
      notify(chatKey);
    }
    return;
  }
  plans.set(chatKey, {
    turnId: "history",
    entries: record!.entries,
    writingPath: null,
    turnActive: false,
    paused: !wreckage,
    wreckage,
  });
  notify(chatKey);
}
