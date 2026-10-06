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
 * The pod as the tests drive it: the real pod listeners and `/v1` edge on
 * free ports, the Turn socket in a temp dir, a local IdP (`test-keys.ts`),
 * the in-process tools socket (`FakeToolsSocket`), a temp snapshot mount
 * with one project, and a model the test chooses. Nothing is mocked between
 * the HTTP request and the turn.
 */

import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { tmpdir } from "node:os";
import type { LanguageModel } from "ai";
import type { JWTVerifyGetKey } from "jose";
import { Agent, fetch as undiciFetch } from "undici";
import { localRoomJoiner } from "../../src/collab/local-room.js";
import { loadPodConfig } from "../../src/pod/config.js";
import { startPodListeners, type PodListeners, type PodLogLine } from "../../src/pod/listeners.js";
import { InMemoryConversationStore } from "../../src/store/memory-store.js";
import { ThreadBook } from "../../src/conversations/thread-book.js";
import { MarketplaceBook } from "../../src/conversations/marketplace-book.js";
import { FakeToolsSocket } from "../../src/tools-socket/fake.js";
import { UsageOutbox } from "../../src/usage/outbox.js";
import { TurnDesk, type TurnLogLine } from "../../src/turns/turn-desk.js";
import { finishedTurnSink, TurnStarter, type BuildModel, type JoinRoom } from "../../src/turns/start-turn.js";
import { anthropicConnection, type ModelConnection } from "../../src/shared/model.js";
import { testKeys } from "./test-keys.js";

export const ORG_ID = "ou-acme";
export const ORG_HANDLE = "acme";
export const PROJECT = "greeter";
export const HEAD = "a".repeat(40);
export const SKILLS = "b".repeat(40);

/** The connection a pod with a Default key renders. */
export const CONNECTION: ModelConnection = { ...anthropicConnection("sk-ant-test-0000000000", "claude-sonnet-5"), contextWindow: 200_000 };

export interface EdgeOptions {
  /** One model per turn, in order; or a builder that sees the connection. */
  models?: LanguageModel[];
  buildModel?: BuildModel;
  /** `null`: the org has no key. */
  connection?: ModelConnection | null;
  room?: JoinRoom;
  /** Where the Turn socket listens (a temp dir unless named). */
  turnSocket?: string;
  /** Join Rooms as the pod does (`localRoomJoiner`) on this Room socket. */
  roomSocket?: string;
  /** The project snapshot's files (path → content). */
  files?: Record<string, string>;
  /** Reference documents stored for the project (name → bytes). */
  references?: Record<string, string | Buffer>;
  /** The skills snapshot's files. */
  skillFiles?: Record<string, string>;
  keepAliveMs?: number;
  /** The starter's `headless` (no one answers questions in this run). */
  headless?: boolean;
  /** Replaces the local IdP's key set (an unreachable IdP). */
  jwks?: JWTVerifyGetKey;
}

export interface Edge {
  base: string;
  /** The Turn socket's path. */
  turnSocket: string;
  tools: FakeToolsSocket;
  outbox: UsageOutbox;
  /** The pod's listeners (a shutdown test closes them itself). */
  pod: PodListeners;
  desk: TurnDesk;
  threads: ThreadBook;
  store: InMemoryConversationStore;
  turns: TurnStarter;
  logs: PodLogLine[];
  /** The desk's and the starter's log lines. */
  turnLogs: TurnLogLine[];
  /** `AE_SNAPSHOTS_DIR` (a test can break a snapshot). */
  snapshotsDir: string;
  healthUrl: string;
  /** A user token of `ouHandle` (the pod's org unless named) with these claims. */
  token(claims?: { ouHandle?: string; sub?: string; name?: string; email?: string }): Promise<string>;
  /** A client_credentials token: the publisher or the AE-only client. */
  m2m(client: "publisher" | "ae-internal"): Promise<string>;
  /** A client_credentials token that names the user audience, with org claims. */
  m2mOnUserAudience(ouHandle: string, ouId: string): Promise<string>;
  close(): Promise<void>;
}

function write(root: string, files: Record<string, string | Buffer>): void {
  for (const [path, content] of Object.entries(files)) {
    mkdirSync(dirname(join(root, path)), { recursive: true });
    writeFileSync(join(root, path), content);
  }
}

export async function startEdge(opts: EdgeOptions = {}): Promise<Edge> {
  const keys = await testKeys();
  const root = mkdtempSync(join(tmpdir(), "ae-edge-"));
  const snapshot = join(root, "projects", PROJECT, HEAD);
  mkdirSync(snapshot, { recursive: true });
  write(snapshot, opts.files ?? { "specs/requirements/prd.md": "# PRD\n" });
  const refs = Object.fromEntries(
    Object.entries(opts.references ?? {}).map(([name, bytes]) => [`specs/requirements/references/${name}`, bytes]),
  );
  write(snapshot, refs);
  mkdirSync(join(root, "skills", SKILLS), { recursive: true });
  write(join(root, "skills", SKILLS), opts.skillFiles ?? {});

  const tools = new FakeToolsSocket({
    projects: { [PROJECT]: { headSha: HEAD, skillsSha: SKILLS, references: Object.keys(opts.references ?? {}).sort() } },
    skillsSha: SKILLS,
  });
  const store = new InMemoryConversationStore();
  const threads = new ThreadBook({ store });
  const outbox = new UsageOutbox(tools, { log: () => {} });
  outbox.run();
  const turnLogs: TurnLogLine[] = [];
  const desk = new TurnDesk({ onFinished: finishedTurnSink(threads, outbox), log: (l) => turnLogs.push(l) });
  const models = [...(opts.models ?? [])];
  const turns = new TurnStarter({
    log: (l) => turnLogs.push(l),
    desk,
    threads,
    store,
    tools,
    snapshotsDir: root,
    connection: opts.connection === undefined ? CONNECTION : opts.connection,
    buildModel:
      opts.buildModel ??
      (() => {
        const model = models.shift();
        if (!model) throw new Error("test: no model left for this turn");
        return model;
      }),
    ...(opts.room ? { room: opts.room } : {}),
    ...(opts.roomSocket ? { room: localRoomJoiner({ socketPath: opts.roomSocket, orgHandle: ORG_HANDLE }) } : {}),
    surface: "console",
    orgId: ORG_ID,
    ...(opts.headless ? { headless: true } : {}),
  });
  const cfg = loadPodConfig({
    AE_ORG_ID: ORG_ID,
    AE_ORG_HANDLE: ORG_HANDLE,
    AE_IDP_ISSUER: keys.issuer,
    AE_IDP_JWKS_URL: "http://unused",
    AE_USER_AUDIENCES: "aep-console-client",
    AE_MCP_SOCKET: "/unused/mcp.sock",
    AE_TURN_SOCKET: opts.turnSocket ?? join(root, "turn.sock"),
    AE_ROOM_SOCKET: opts.roomSocket ?? "/unused/room.sock",
    AE_SNAPSHOTS_DIR: root,
    AE_LISTEN_PORT: "0",
    AE_HEALTH_PORT: "0",
  })!;
  const logs: PodLogLine[] = [];
  const pod = await startPodListeners(cfg, {
    jwks: opts.jwks ?? keys.jwks,
    log: (l) => logs.push(l),
    edge: {
      turns,
      desk,
      threads,
      marketplace: new MarketplaceBook(store, { busy: (conversationId) => desk.active({ kind: "marketplace", conversationId }) !== null }),
      ...(opts.keepAliveMs ? { keepAliveMs: opts.keepAliveMs } : {}),
    },
  });
  let closing: Promise<void> | undefined;
  return {
    base: pod.publicUrl,
    healthUrl: pod.healthUrl,
    turnSocket: cfg.turnSocket,
    pod,
    outbox,
    tools,
    desk,
    threads,
    store,
    turns,
    logs,
    turnLogs,
    snapshotsDir: root,
    token: (claims = {}) => {
      const ouHandle = claims.ouHandle ?? ORG_HANDLE;
      return keys.userWith({ ...claims, ouHandle, ouId: ouHandle === ORG_HANDLE ? ORG_ID : `ou-${ouHandle}` });
    },
    m2m: (client) => keys.m2m(client),
    m2mOnUserAudience: (ouHandle, ouId) => keys.m2mOnUserAudience(ouHandle, ouId),
    close() {
      closing ??= (async () => {
        await desk.abortAll("shutdown");
        await pod.close();
        rmSync(root, { recursive: true, force: true });
      })();
      return closing;
    },
  };
}

/** One SSE frame: its id, the raw data line, and the parsed part (`undefined` for `[DONE]`). */
export interface SseFrame {
  id: number;
  raw: string;
  data: { type: string } & Record<string, unknown>;
}

/** Read a whole SSE response into frames (comments dropped). */
export async function readSse(res: Response): Promise<{ frames: SseFrame[]; text: string }> {
  const text = await res.text();
  const frames: SseFrame[] = [];
  for (const block of text.split("\n\n")) {
    const id = /^id: (\d+)$/m.exec(block)?.[1];
    const raw = /^data: (.*)$/m.exec(block)?.[1];
    if (raw === undefined) continue;
    frames.push({ id: Number(id), raw, data: raw === "[DONE]" ? (undefined as never) : JSON.parse(raw) });
  }
  return { frames, text };
}

/** `fetch` against the edge with a bearer and an optional JSON body. */
export function call(edge: Edge, path: string, token: string, init: { method?: string; json?: unknown; headers?: Record<string, string>; body?: FormData | string } = {}): Promise<Response> {
  const headers: Record<string, string> = { authorization: `Bearer ${token}`, ...init.headers };
  if (init.json !== undefined) headers["content-type"] = "application/json";
  return fetch(`${edge.base}${path}`, {
    method: init.method ?? (init.json !== undefined || init.body !== undefined ? "POST" : "GET"),
    headers,
    ...(init.json !== undefined ? { body: JSON.stringify(init.json) } : init.body !== undefined ? { body: init.body } : {}),
  });
}

/** The project's current conversation id, then a turn POSTed into it. */
export async function startTurn(edge: Edge, token: string, json: unknown = { instruction: "hi" }, project = PROJECT): Promise<Response> {
  const conv = (await (await call(edge, `/v1/projects/${project}/conversations/current`, token)).json()) as { conversationId?: string };
  return call(edge, `/v1/projects/${project}/conversations/${conv.conversationId ?? "00000000-0000-4000-8000-000000000000"}/turns`, token, { json });
}

/** Stream a turn to its end from `from`. */
export async function streamOf(edge: Edge, token: string, turnId: string, from = 0, project = PROJECT): Promise<SseFrame[]> {
  const res = await call(edge, `/v1/projects/${project}/turns/${turnId}/stream?from=${from}`, token);
  if (res.status !== 200) throw new Error(`stream: ${res.status}`);
  return (await readSse(res)).frames;
}

/** A Turn socket answer: its status, and its body read line by line. */
export interface SocketAnswer {
  status: number;
  contentType: string | null;
  /** The next NDJSON line, parsed; `undefined` at the end. */
  next(): Promise<Record<string, unknown> | undefined>;
  /** Every remaining line, raw. */
  rest(): Promise<string[]>;
  /** The whole body as JSON (a refusal). */
  json(): Promise<Record<string, unknown>>;
}

/** POST a body to the Turn socket at `path`. */
export async function postTurnSocket(path: string, body: unknown): Promise<SocketAnswer> {
  const dispatcher = new Agent({ connect: { socketPath: path } });
  const res = await undiciFetch("http://turn.sock/turns", {
    method: "POST",
    dispatcher,
    headers: { "content-type": "application/json" },
    body: typeof body === "string" ? body : JSON.stringify(body),
  });
  const reader = res.body?.getReader();
  const decoder = new TextDecoder();
  let buffered = "";
  let done = false;
  const nextRaw = async (): Promise<string | undefined> => {
    for (;;) {
      const nl = buffered.indexOf("\n");
      if (nl >= 0) {
        const line = buffered.slice(0, nl);
        buffered = buffered.slice(nl + 1);
        return line;
      }
      if (done || !reader) return undefined;
      const chunk = await reader.read();
      if (chunk.done) done = true;
      else buffered += decoder.decode(chunk.value, { stream: true });
    }
  };
  return {
    status: res.status,
    contentType: res.headers.get("content-type"),
    async next() {
      const line = await nextRaw();
      return line === undefined ? undefined : (JSON.parse(line) as Record<string, unknown>);
    },
    async rest() {
      const lines: string[] = [];
      for (let line = await nextRaw(); line !== undefined; line = await nextRaw()) lines.push(line);
      return lines;
    },
    async json() {
      const lines: string[] = [];
      for (let line = await nextRaw(); line !== undefined; line = await nextRaw()) lines.push(line);
      return JSON.parse(lines.join("\n") + buffered) as Record<string, unknown>;
    },
  };
}
