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
 * The TurnDesk: every design-agent turn in the pod runs through it. It is the
 * ONLY turn lock (nothing outside the pod guards a turn): one running turn per
 * scope, a project for browser, kickoff and Plan turns alike, a conversation
 * for marketplace turns. A busy scope throws `TurnInProgressError` naming the
 * running turn (the edges answer `409 turn_in_progress`).
 *
 * A turn runs detached from whoever started it. Its frames go to a
 * `ReplayBuffer` that watchers attach to; the desk ends the stream itself
 * (`turn-completed` / `turn-failed`, then `[DONE]`) and frees the lock on
 * every way a turn can end: the run resolves, the run throws or rejects (at
 * any point, before its first frame too), the 30-minute cap fires, or
 * `abortAll` runs at shutdown. The cap and shutdown end the turn at once and
 * abort the run's signal; whatever the run does afterwards is ignored.
 *
 * Each finished turn hands its whole record (the §7 `TurnRecord`) to
 * `onFinished` exactly once. Retention: a buffer stays attachable
 * `REPLAY_RETENTION_MS` after the end; a `TurnStatus` is kept for the last
 * `STATUS_KEEP_PER_SCOPE` finished turns of its scope or `STATUS_KEEP_MS`
 * after it finished, whichever is shorter; the last terminal facts per scope
 * (`lastTerminal`) are kept for the pod's life.
 */

import { randomUUID } from "node:crypto";
import type { StreamPart, TurnUsage } from "@aep/agent-stream";
import type { components as ApiComponents } from "../generated/api.js";
import type { TurnRecord } from "../tools-socket/client.js";
import {
  REPLAY_RETENTION_MS,
  ReplayBuffer,
  type ReplayFrame,
  type TurnEndPart,
  type TurnFailReason,
} from "./replay-buffer.js";

export type TurnStatus = ApiComponents["schemas"]["TurnStatus"];

/** The 30-minute turn cap. */
export const TURN_CAP_MS = 30 * 60_000;
/** Finished `TurnStatus` records kept per scope. */
const STATUS_KEEP_PER_SCOPE = 20;
/** How long a finished `TurnStatus` is kept. */
const STATUS_KEEP_MS = 60 * 60_000;
/** How long `abortAll` waits for aborted runs to settle. */
const ABORT_GRACE_MS = 2_000;

/** What a turn holds the lock on. */
export type Scope = { kind: "project"; project: string } | { kind: "marketplace"; conversationId: string };

/** What the starter knows about a turn before it runs. */
export interface TurnMeta {
  conversationId: string;
  kind: TurnStatus["kind"];
  /** The flow the turn runs (`""` for a plain chat turn). */
  flow: string;
  /** The display line of the message that started the turn. */
  instruction: string;
  /** Who started the turn; absent when no human can be named. */
  author?: TurnRecord["author"];
  /** The resolved model id (the ledger's pricing key, with `modelHost`). */
  model: string;
  modelHost: string;
  /**
   * The snapshot shas the starter resolved before the run, when it knows them:
   * a turn that ends before its run reports any (a throw, the cap, shutdown)
   * still records what it was started against.
   */
  baseRef?: string;
  skillsRef?: string;
}

/** How a run ended, as the run reports it. */
export interface TurnOutcome {
  status: "completed" | "failed";
  /** On failure; `agent-error` when absent. */
  reason?: TurnFailReason;
  code?: string;
  message?: string;
  /** With code `provider_limit`: the model host whose limit stopped the turn. */
  host?: string;
  /** With code `provider_limit`: when the provider said its limit resets (RFC 3339). */
  resetAt?: string;
  /** The turn's token usage, failed turns included where the SDK reported it. `model` is not read: the record takes `TurnMeta.model`. */
  usage?: TurnUsage;
  contextTokens?: number;
  /** The repo snapshot sha the turn read. */
  baseRef: string;
  /** The Org skills snapshot sha the turn read. */
  skillsRef: string;
}

/** One turn's body: emit parts, honour the signal (the cap and shutdown abort it). */
export interface TurnRun {
  (emit: (part: StreamPart) => void, signal: AbortSignal): Promise<TurnOutcome>;
}

/** The scope already runs a turn. */
export class TurnInProgressError extends Error {
  constructor(readonly activeTurnId: string) {
    super(`turn ${activeTurnId} is in progress`);
    this.name = "TurnInProgressError";
  }
}

type Ending = Omit<TurnOutcome, "status"> & { status: "completed" | "failed" };

interface Turn {
  scopeKey: string;
  meta: TurnMeta;
  status: TurnStatus;
  controller: AbortController;
  capTimer: ReturnType<typeof setTimeout>;
  finishedAtMs?: number;
  /** Settles (never rejects) once the run has. */
  settled: Promise<void>;
}

interface BufferEntry {
  scopeKey: string;
  buffer: ReplayBuffer;
}

export class TurnDesk {
  private readonly now: () => number;
  private readonly capMs: number;
  private readonly onFinished: (rec: TurnRecord) => void;
  /** Running and retained turns by id. */
  private readonly turns = new Map<string, Turn>();
  /** The running turn per scope: the lock. */
  private readonly running = new Map<string, Turn>();
  /** Retained finished turn ids per scope, oldest first. */
  private readonly finished = new Map<string, string[]>();
  /** Attachable buffers by turn id (retention independent of the status). */
  private readonly buffers = new Map<string, BufferEntry>();
  private readonly last = new Map<string, { status: "completed" | "failed"; baseRef: string }>();

  constructor(opts: { now?: () => number; capMs?: number; onFinished: (rec: TurnRecord) => void }) {
    this.now = opts.now ?? Date.now;
    this.capMs = opts.capMs ?? TURN_CAP_MS;
    this.onFinished = opts.onFinished;
  }

  /**
   * Start a turn, or reattach: a `turnId` the desk still knows (running, or
   * finished and retained) in the same scope returns `reattached: true` and
   * starts nothing. Without a `turnId` the desk makes a uuid. Throws
   * `TurnInProgressError` when the scope runs another turn.
   */
  start(scope: Scope, meta: TurnMeta, run: TurnRun, turnId?: string): { turnId: string; reattached: boolean } {
    this.prune();
    const key = scopeKey(scope);
    if (turnId !== undefined) {
      const knownScope = this.turns.get(turnId)?.scopeKey ?? this.buffers.get(turnId)?.scopeKey;
      if (knownScope !== undefined) {
        if (knownScope !== key) throw new Error(`turn ${turnId} belongs to another scope`);
        return { turnId, reattached: true };
      }
    }
    const busy = this.running.get(key);
    if (busy) throw new TurnInProgressError(busy.status.turnId);

    const id = turnId ?? randomUUID();
    const buffer = new ReplayBuffer();
    const controller = new AbortController();
    const turn: Turn = {
      scopeKey: key,
      meta,
      status: {
        turnId: id,
        ...(scope.kind === "project" ? { project: scope.project } : {}),
        conversationId: meta.conversationId,
        kind: meta.kind,
        flow: meta.flow,
        status: "running",
        instruction: meta.instruction,
        authorId: meta.author?.id ?? "",
        authorDisplayName: meta.author?.name ?? "",
        createdAt: new Date(this.now()).toISOString(),
      },
      controller,
      capTimer: setTimeout(() => {
        this.finish(turn, { status: "failed", reason: "stream-died", message: "the turn ran past the 30-minute cap", ...refsOf(meta) });
        controller.abort();
      }, this.capMs),
      settled: Promise.resolve(),
    };
    this.turns.set(id, turn);
    this.running.set(key, turn);
    this.buffers.set(id, { scopeKey: key, buffer });

    // Start the run now (its abort listeners are in place before start
    // returns); a runner that throws synchronously ends like one that rejects.
    let outcome: Promise<TurnOutcome>;
    try {
      outcome = Promise.resolve(run((part) => buffer.append(part), controller.signal));
    } catch (err) {
      outcome = Promise.reject(err);
    }
    turn.settled = outcome.then(
        (outcome) => this.finish(turn, outcome),
        (err: unknown) =>
          this.finish(turn, { status: "failed", reason: "internal", message: err instanceof Error ? err.message : String(err), ...refsOf(meta) }),
      );
    return { turnId: id, reattached: false };
  }

  /** The turn's frames from id `from`, then its live frames; `null` when not buffered (unknown or past retention). Throws `ReplayTruncatedError` while an overflowed turn runs. */
  attach(turnId: string, from: number): AsyncIterable<ReplayFrame> | null {
    return this.buffers.get(turnId)?.buffer.attach(from) ?? null;
  }

  /** The scope's running turn, or `null`. */
  active(scope: Scope): TurnStatus | null {
    this.prune();
    const turn = this.running.get(scopeKey(scope));
    return turn ? { ...turn.status } : null;
  }

  /** A running or retained turn's status, or `null`. */
  status(turnId: string): TurnStatus | null {
    this.prune();
    const turn = this.turns.get(turnId);
    return turn ? { ...turn.status } : null;
  }

  /** The scope's last finished turn: `previousTurnFailed` and `filesChangedExternally` are read from it. */
  lastTerminal(scope: Scope): { status: "completed" | "failed"; baseRef: string } | null {
    const facts = this.last.get(scopeKey(scope));
    return facts ? { ...facts } : null;
  }

  /** End every running turn with `turn-failed {reason}` and abort its run; waits briefly for the runs to settle. */
  async abortAll(reason: "shutdown"): Promise<void> {
    const turns = [...this.running.values()];
    for (const turn of turns) {
      this.finish(turn, { status: "failed", reason, ...refsOf(turn.meta) });
      turn.controller.abort();
    }
    let grace: ReturnType<typeof setTimeout> | undefined;
    await Promise.race([
      Promise.all(turns.map((t) => t.settled)),
      new Promise<void>((resolve) => (grace = setTimeout(resolve, ABORT_GRACE_MS))),
    ]);
    clearTimeout(grace);
  }

  /** End the turn once: status, lock, stream end, buffer retention, record. */
  private finish(turn: Turn, ending: Ending): void {
    if (turn.finishedAtMs !== undefined) return;
    const finishedAtMs = this.now();
    turn.finishedAtMs = finishedAtMs;
    clearTimeout(turn.capTimer);

    const failed = ending.status === "failed";
    const reason: TurnFailReason | undefined = failed ? (ending.reason ?? "agent-error") : undefined;
    const detail = failed ? failureDetail(ending) : {};
    turn.status = {
      ...turn.status,
      status: ending.status,
      ...(reason ? { reason } : {}),
      ...detail,
      finishedAt: new Date(finishedAtMs).toISOString(),
    };

    const { scopeKey: key } = turn;
    if (this.running.get(key) === turn) this.running.delete(key);
    this.last.set(key, { status: ending.status, baseRef: ending.baseRef });
    this.finished.set(key, [...(this.finished.get(key) ?? []), turn.status.turnId]);
    this.prune();

    const end: TurnEndPart = reason ? { type: "turn-failed", reason, ...detail } : { type: "turn-completed" };
    // The buffer lives only in `buffers`, so a retained status keeps no frames.
    const id = turn.status.turnId;
    this.buffers.get(id)?.buffer.end(end);
    const expire = setTimeout(() => {
      this.buffers.get(id)?.buffer.dispose();
      this.buffers.delete(id);
    }, REPLAY_RETENTION_MS);
    expire.unref?.();

    try {
      this.onFinished(this.record(turn, ending, reason));
    } catch {
      // The record's consumer (the usage outbox) owns its failures; a throw
      // here must not leave the desk half-updated or reject the run chain.
    }
  }

  private record(turn: Turn, ending: Ending, reason: TurnFailReason | undefined): TurnRecord {
    const { meta, status } = turn;
    const usage = ending.usage;
    return {
      turnId: status.turnId,
      ...(status.project !== undefined ? { project: status.project } : {}),
      conversationId: meta.conversationId,
      kind: meta.kind,
      flow: meta.flow,
      status: ending.status,
      ...(reason ? { reason } : {}),
      ...(reason && ending.code !== undefined ? { code: ending.code } : {}),
      baseRef: ending.baseRef,
      skillsRef: ending.skillsRef,
      startedAt: status.createdAt,
      finishedAt: status.finishedAt ?? new Date(this.now()).toISOString(),
      ...(meta.author ? { author: { id: meta.author.id, name: meta.author.name } } : {}),
      model: meta.model,
      modelHost: meta.modelHost,
      inputTokens: usage?.inputTokens ?? 0,
      outputTokens: usage?.outputTokens ?? 0,
      cacheReadTokens: usage?.cacheReadTokens ?? 0,
      cacheCreationTokens: usage?.cacheCreationTokens ?? 0,
      ...(ending.contextTokens !== undefined ? { contextTokens: ending.contextTokens } : {}),
    };
  }

  /** Drop finished statuses past the per-scope count or the age limit. Running turns are never dropped. */
  private prune(): void {
    const now = this.now();
    for (const [key, ids] of this.finished) {
      const keep = ids.filter((id, i) => {
        const at = this.turns.get(id)?.finishedAtMs;
        return at !== undefined && i >= ids.length - STATUS_KEEP_PER_SCOPE && now - at < STATUS_KEEP_MS;
      });
      for (const id of ids) if (!keep.includes(id)) this.turns.delete(id);
      if (keep.length === 0) this.finished.delete(key);
      else this.finished.set(key, keep);
    }
  }
}

/** The refs of a turn that ended before its run reported any: the starter's, else none. */
function refsOf(meta: TurnMeta): { baseRef: string; skillsRef: string } {
  return { baseRef: meta.baseRef ?? "", skillsRef: meta.skillsRef ?? "" };
}

function scopeKey(scope: Scope): string {
  return scope.kind === "project" ? `project:${scope.project}` : `marketplace:${scope.conversationId}`;
}

/** The optional failure fields, without undefined keys. */
function failureDetail(ending: Ending): Pick<TurnStatus, "code" | "message" | "host" | "resetAt"> {
  return {
    ...(ending.code !== undefined ? { code: ending.code } : {}),
    ...(ending.message !== undefined ? { message: ending.message } : {}),
    ...(ending.host !== undefined ? { host: ending.host } : {}),
    ...(ending.resetAt !== undefined ? { resetAt: ending.resetAt } : {}),
  };
}
