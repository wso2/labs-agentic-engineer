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
 * The Turn socket (`packages/contracts/sockets/ae-studio/turn/`):
 * ae-studio-tools starts the turns aep-api asks for, a kickoff (`start`) or a
 * Plan, on the Unix socket in `AE_TURN_SOCKET`. The mount is the gate, so no
 * request carries a token.
 *
 * `POST /turns` starts the turn through the same start path as a browser turn
 * (`TurnStarter.startServerTurn`), or reattaches to the turn its `turnId`
 * names, then answers NDJSON: the handler is one more subscriber on the
 * turn's replay buffer. It projects the ok results of `planTask` and
 * `updateTask` to `task-op` lines (`taskOpOf`, aep-api's plan tap filter
 * moved into the pod), sends a `keep-alive` line every 15 s, and ends with
 * one `result` line. A caller that goes away only detaches: the turn runs on,
 * and a retry with the same `turnId` replays it from the start.
 */

import express, { type ErrorRequestHandler, type Express, type Response } from "express";
import { isTurnSpec, PLAN_TASK, UPDATE_TASK } from "@aep/agent-stream";
import type { components } from "../generated/turn-socket.js";
import { ReplayTruncatedError, type ReplayFrame, type ReplayPart, type TurnEndPart } from "../turns/replay-buffer.js";
import type { TurnDesk, TurnStatus } from "../turns/turn-desk.js";
import { TurnStartError, type ServerTurnRequest, type TurnStarter } from "../turns/start-turn.js";
import { notFound, sendProblem, sendTurnStartError } from "./http.js";

type Schemas = components["schemas"];
export type TaskOpFrame = Schemas["TaskOpFrame"];
export type KeepAliveFrame = Schemas["KeepAliveFrame"];
export type ResultFrame = Schemas["ResultFrame"];
/** One line of the turn stream. */
export type TurnSocketFrame = TaskOpFrame | KeepAliveFrame | ResultFrame;

/** The keep-alive cadence of the contract. */
const KEEP_ALIVE_MS = 15_000;
/** A body carries the plan's scope and the existing-Task renders, never files. */
const MAX_BODY_BYTES = 4 << 20;
/** How often a follower checks again on a turn whose replay overflowed. */
const TRUNCATED_RECHECK_MS = 1_000;
/** The result message of a turn the pod's shutdown ended. */
export const SHUTDOWN_MESSAGE = "the studio is shutting down";

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const REQUEST_KEYS = new Set(["turnId", "project", "kind", "credit", "at", "scope", "taskContext", "text"]);
/** A plan turn's pin: a resolved commit, never a ref (the studio resolves `tags/<version>`). */
const SHA_RE = /^[0-9a-f]{40}$/;
const CREDIT_KEYS = ["userId", "name", "email"] as const;

export interface TurnSocketDeps {
  turns: TurnStarter;
  desk: TurnDesk;
  /** The keep-alive cadence (default 15 s). */
  keepAliveMs?: number;
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/**
 * The Task operation one stream part carries, or `null`: a `tool-result` of
 * `planTask` / `updateTask` whose output is ok, with op `plan` / `update`
 * (port of aep-api's `plan_tap.go` `consume` filter). An `ok: false` result
 * is the agent correcting itself and never leaves the pod.
 */
export function taskOpOf(part: unknown): Pick<TaskOpFrame, "op" | "output"> | null {
  if (!isRecord(part) || part.type !== "tool-result") return null;
  if (part.toolName !== PLAN_TASK && part.toolName !== UPDATE_TASK) return null;
  const output = part.output;
  if (!isRecord(output) || output.ok !== true) return null;
  if (output.op !== "plan" && output.op !== "update") return null;
  return { op: output.op, output };
}

/** One NDJSON line: the frame, keys in the contract's order, then a newline. */
export function frameLine(frame: TurnSocketFrame): string {
  return `${JSON.stringify(frame)}\n`;
}

function result(status: ResultFrame["status"], code?: string, message?: string): ResultFrame {
  return { type: "result", status, ...(code !== undefined ? { code } : {}), ...(message !== undefined ? { message } : {}) };
}

/** How a failed turn ended, as its terminal part or its status records it. */
interface Failure {
  reason: string;
  code?: string;
  message?: string;
  resetAt?: string;
}

/**
 * A failed turn's result: its own code, else the reason it failed for
 * (`shutdown`, `stream-died`, ...). A `provider_limit` carries the reset time
 * the provider stated, so aep-api waits until then before its next try.
 */
function failedResult({ reason, code, message, resetAt }: Failure): ResultFrame {
  const frame = result("failed", code ?? reason, message ?? (reason === "shutdown" ? SHUTDOWN_MESSAGE : undefined));
  return code === "provider_limit" && resetAt !== undefined ? { ...frame, resetAt } : frame;
}

/** The result line of a turn's terminal part. */
export function resultOf(end: TurnEndPart): ResultFrame {
  return end.type === "turn-completed" ? result("completed") : failedResult(end);
}

/** The result line of a finished turn whose frames are gone (past the replay retention). */
function resultOfStatus(status: TurnStatus): ResultFrame {
  if (status.status === "completed") return result("completed");
  return failedResult({ ...status, reason: status.reason ?? "agent-error" });
}

function isTurnEnd(part: ReplayPart): part is TurnEndPart {
  return isRecord(part) && (part.type === "turn-completed" || part.type === "turn-failed");
}

/** The request as the start path takes it, or why it is not one. */
export function parseTurnRequest(body: unknown): ServerTurnRequest | string {
  if (!isRecord(body)) return "the body must be a JSON object";
  const unknown = Object.keys(body).find((k) => !REQUEST_KEYS.has(k));
  if (unknown !== undefined) return `unknown field ${unknown}`;
  const { turnId, project, kind, credit, at, scope, taskContext, text } = body;
  if (typeof turnId !== "string" || !UUID_RE.test(turnId)) return "turnId must be a uuid";
  if (typeof project !== "string" || project === "") return "project is required";
  if (kind !== "start" && kind !== "plan") return "kind must be start or plan";
  if (!isRecord(credit) || Object.keys(credit).some((k) => !(CREDIT_KEYS as readonly string[]).includes(k))) {
    return "credit must be {userId, name, email}";
  }
  if (!CREDIT_KEYS.every((k) => typeof credit[k] === "string")) return "credit must be {userId, name, email}";
  if (text !== undefined && typeof text !== "string") return "text must be a string";
  if (at !== undefined) {
    if (typeof at !== "string" || !SHA_RE.test(at)) return "at must be a 40-hex commit sha";
    if (kind !== "plan") return "at is only for a plan turn";
  }
  if (!isTurnSpec({ kind: "plan", scope, taskContext })) return "scope or taskContext is malformed";
  return {
    turnId: turnId.toLowerCase(),
    project,
    kind,
    credit: { userId: credit.userId as string, name: credit.name as string, email: credit.email as string },
    ...(at !== undefined ? { at } : {}),
    ...(scope !== undefined ? { scope: scope as NonNullable<ServerTurnRequest["scope"]> } : {}),
    ...(taskContext !== undefined ? { taskContext: taskContext as NonNullable<ServerTurnRequest["taskContext"]> } : {}),
    ...(text !== undefined ? { text } : {}),
  };
}

/**
 * Follow turn `turnId` to its end: every Task operation to `onOp`, then its
 * result. A reader the buffer dropped for falling behind re-attaches where
 * it stopped; a turn whose frames are gone answers from its status.
 * `null` when the caller left first.
 */
async function follow(
  desk: TurnDesk,
  turnId: string,
  onOp: (op: Pick<TaskOpFrame, "op" | "output">) => void,
  caller: { gone: boolean; onGone(detach: () => void): void },
): Promise<ResultFrame | null> {
  let from = 0;
  for (;;) {
    let frames: AsyncIterable<ReplayFrame> | null;
    try {
      frames = desk.attach(turnId, from);
    } catch (err) {
      if (!(err instanceof ReplayTruncatedError)) throw err;
      // An overflowed turn can be attached again once it has ended.
      await new Promise((r) => setTimeout(r, TRUNCATED_RECHECK_MS));
      if (caller.gone) return null;
      continue;
    }
    if (!frames) {
      const status = desk.status(turnId);
      return status && status.status !== "running" ? resultOfStatus(status) : result("failed", "stream-died");
    }
    const reader = frames[Symbol.asyncIterator]();
    caller.onGone(() => void reader.return?.());
    for (;;) {
      const next = await reader.next();
      if (caller.gone) return null;
      if (next.done) break;
      const { id, part } = next.value;
      from = id + 1;
      if (isTurnEnd(part)) return resultOf(part);
      const op = taskOpOf(part);
      if (op) onOp(op);
    }
  }
}

async function streamTurn(res: Response, desk: TurnDesk, turnId: string, keepAliveMs: number): Promise<void> {
  res.writeHead(200, { "content-type": "application/x-ndjson", "cache-control": "no-cache" });
  res.flushHeaders();
  const send = (frame: TurnSocketFrame): void => {
    if (!res.writableEnded && !res.destroyed) res.write(frameLine(frame));
  };
  let detach: (() => void) | undefined;
  const caller = {
    gone: false,
    onGone(d: () => void) {
      detach = d;
    },
  };
  // The caller leaving only stops this reader; the turn belongs to the desk.
  const onClose = () => {
    caller.gone = true;
    detach?.();
  };
  res.on("close", onClose);
  const keepAlive = setInterval(() => send({ type: "keep-alive" }), keepAliveMs);
  keepAlive.unref();
  try {
    const end = await follow(desk, turnId, (op) => send({ type: "task-op", ...op }), caller);
    if (end) send(end);
  } finally {
    clearInterval(keepAlive);
    res.off("close", onClose);
    res.end();
  }
}

/** A body the JSON parser refused: too large, or not JSON. */
function bodyError(): ErrorRequestHandler {
  return (err: { status?: number; type?: string }, _req, res, next) => {
    if (err.type === "entity.too.large") return sendProblem(res, 413, "payload_too_large", "the body exceeds the size limit");
    if (err.status === 400) return sendProblem(res, 400, "invalid_turn", "the body is not JSON");
    next(err);
  };
}

/** Never express's default page: no stack, no message. */
const internalError: ErrorRequestHandler = (_err, _req, res, next) => {
  if (res.headersSent) return next(_err);
  sendProblem(res, 500, "internal", "");
};

/** The Turn socket's app: `POST /turns`, nothing else. */
export function createTurnSocketApp(deps: TurnSocketDeps): Express {
  const keepAliveMs = deps.keepAliveMs ?? KEEP_ALIVE_MS;
  const app = express();
  app.disable("x-powered-by");
  app.post("/turns", express.json({ limit: MAX_BODY_BYTES }), async (req, res) => {
    const request = parseTurnRequest(req.body);
    if (typeof request === "string") return sendProblem(res, 400, "invalid_turn", request);
    let started: { turnId: string };
    try {
      started = await deps.turns.startServerTurn(request);
    } catch (err) {
      if (!(err instanceof TurnStartError)) throw err;
      return sendTurnStartError(res, err);
    }
    await streamTurn(res, deps.desk, started.turnId, keepAliveMs);
  });
  app.use((_req, res) => notFound(res, "not_found"));
  app.use(bodyError());
  app.use(internalError);
  return app;
}
