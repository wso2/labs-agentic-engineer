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
 * The thin Express SSE layer. One turn = one HTTP request:
 *
 *   GET  /healthz                  → 200 (unauthenticated liveness probe)
 *   POST /conversations/:id/turns  → SSE stream of RAW StreamPart frames + [DONE]
 *   GET  /conversations/:id        → rehydrate { status, messages, ... } (404 if unknown)
 *
 * Every conversation route is behind the M2M gate (`aud`-checked Bearer JWT).
 * The turn requires an `X-Anthropic-Key` header — the model is built PER TURN
 * from it (§12.3.1), so the service holds no key of its own. While a turn
 * streams, a `: keep-alive` comment is emitted every `keepAliveMs` so long
 * generations survive an idle ingress.
 *
 * The ONLY turn shape is the workspace shape (§12/D9): the body carries IDs +
 * shas, never file content. `snapshot-path.ts` fences + derives the snapshot
 * dirs (`X-Org-Id` is LOAD-BEARING — the conversation's org segment must equal
 * it, 403 otherwise) and `load-workspace.ts` reads the files + a lazy skill
 * source from the mount. Bodies that inline `files`/`skills` (the pre-§12
 * contract) are rejected with a clean 400.
 *
 * The route maps `runConversationTurn`'s `onEvent` straight to `data: <part>`
 * frames (no envelope). The stream starts LAZILY (on the first event), so a
 * pre-stream failure (400 missing key / invalid body / unknown snapshot, 403
 * org-fence, 409 concurrent turn) is still an HTTP status; once streaming,
 * every failure is an `error` frame then `[DONE]`.
 */

import express from "express";
import type { Express, Request, Response, NextFunction } from "express";
import type { LanguageModel } from "ai";
import {
  SSE_DONE,
  TOOLSETS,
  isToolset,
  isCollabConfig,
  type CollabConfig,
  type McpConfig,
  type StreamPart,
  type Toolset,
} from "@aep/agent-stream";
import type { ConversationStore } from "./store/conversation-store.js";
import { runConversationTurn, TurnGuard, ConcurrentTurnError } from "./conversation/run-conversation-turn.js";
import { joinRoom, type RoomPeer } from "./collab/room-peer.js";
import type { SkillSource } from "./agents/main/skill-source.js";
import { readSnapshot, loadSkillsFromSnapshot } from "./conversation/load-workspace.js";
import { resolveWorkspace, WorkspaceRefError } from "./shared/snapshot-path.js";
import { createAuthMiddleware, type AgentsAuthConfig } from "./shared/auth.js";
import { startKeepAlive } from "./shared/keepalive.js";
import { config } from "./shared/config.js";

export interface CreateAppDeps {
  store: ConversationStore;
  /** Build the model from the request's `X-Anthropic-Key` (§12.3.1). Injected so tests pass a mock. */
  buildModel: (apiKey: string) => LanguageModel;
  /**
   * The resolved model id `buildModel` instantiates — usage attribution on the
   * terminal manifest (#249). Optional so mock-model callers (tests, evals)
   * need not invent one; absent → the manifest usage carries `model: ""`.
   */
  modelId?: string;
  /** M2M gate config (always on): JWKS or shared secret. */
  auth: AgentsAuthConfig;
  /** SSE keep-alive cadence in ms (default `config.keepAliveMs`). */
  keepAliveMs?: number;
  /** The shared workspaces mount (default `config.workspaceMountRoot`); tests/evals point it at a fixture tree. */
  workspaceMountRoot?: string;
}

/** Runtime guard for an untrusted `mcp` value (the server's pre-stream 400 check). */
function isMcpConfig(v: unknown): v is McpConfig {
  if (typeof v !== "object" || v === null) return false;
  const c = v as Record<string, unknown>;
  return typeof c.url === "string" && c.url !== "" && typeof c.token === "string" && c.token !== "";
}

function startSSE(res: Response): void {
  res.status(200);
  res.setHeader("Content-Type", "text/event-stream");
  res.setHeader("Cache-Control", "no-cache, no-transform");
  res.setHeader("Connection", "keep-alive");
  res.setHeader("X-Accel-Buffering", "no"); // defeat proxy buffering of the stream
  res.flushHeaders();
}

export function createApp(deps: CreateAppDeps): Express {
  const app = express();
  const guard = new TurnGuard(); // one in-flight guard per app (serializes turns per id)
  // A workspace turn body is a few hundred bytes of IDs + shas; 256kb is generous headroom.
  const jsonParser = express.json({ limit: "256kb" });
  const requireAuth = createAuthMiddleware(deps.auth);
  const keepAliveMs = deps.keepAliveMs ?? config.keepAliveMs;

  // Unauthenticated liveness probe (compose/ingress health checks).
  app.get("/healthz", (_req: Request, res: Response) => {
    res.status(200).json({ status: "ok" });
  });

  // Auth runs BEFORE body parsing so an unauthenticated caller can't spend the
  // parser on a large body.
  app.post("/conversations/:id/turns", requireAuth, jsonParser, async (req: Request, res: Response) => {
    const id = req.params.id as string;

    // Per-request Anthropic key (§12.3.1): required, model built per turn.
    const apiKey = req.header("x-anthropic-key");
    if (!apiKey || apiKey.trim() === "") {
      res.status(400).json({ error: "X-Anthropic-Key header is required" });
      return;
    }
    // Org id: LOAD-BEARING — the §12 fence asserts the conversation's org
    // segment equals it (403 otherwise).
    const orgId = req.header("x-org-id");
    if (config.logLevel === "debug" && orgId) {
      process.stderr.write(`[turn ${id}] org=${orgId}\n`);
    }

    const body = (req.body ?? {}) as {
      instruction?: unknown;
      workspace?: unknown;
      files?: unknown;
      filesChangedExternally?: unknown;
      skills?: unknown;
      toolset?: unknown;
      mcp?: unknown;
      collab?: unknown;
    };

    // Pre-stream validation → HTTP status (no SSE headers sent yet).
    if (typeof body.instruction !== "string" || body.instruction.trim() === "") {
      res.status(400).json({ error: "instruction is required" });
      return;
    }

    // The pre-§12 inline contract is GONE — reject it loudly so a stale caller
    // cannot silently run a turn against the wrong file/skill supply.
    if (body.files !== undefined) {
      res.status(400).json({ error: "files is no longer accepted — send a workspace reference (§12)" });
      return;
    }
    if (body.skills !== undefined) {
      res.status(400).json({ error: "skills is no longer accepted — skills load from the workspace's skills snapshot" });
      return;
    }
    if (body.workspace === undefined) {
      res.status(400).json({ error: "workspace is required" });
      return;
    }

    // Workspace resolution (§12/D9): derive + fence the snapshot dirs, then
    // read the files and the lazy skill source from the mount.
    let files: Record<string, string>;
    let skillSource: SkillSource;
    try {
      const ws = resolveWorkspace({
        conversationIdParam: id,
        workspace: body.workspace,
        orgIdClaim: orgId,
        ...(deps.workspaceMountRoot ? { mountRoot: deps.workspaceMountRoot } : {}),
      });
      files = readSnapshot(ws.snapshotDir);
      skillSource = loadSkillsFromSnapshot(ws.skillsSnapshotDir);
    } catch (err) {
      if (err instanceof WorkspaceRefError) {
        res.status(err.status).json({ error: err.message });
        return;
      }
      // A stat-checked snapshot failing to read = an infrastructure fault.
      res.status(500).json({ error: err instanceof Error ? err.message : "workspace read failed" });
      return;
    }

    // toolset (optional): which domain tools to register (§9.3). Absent → "files"
    // (byte-identical to today). Reject an unknown value pre-stream as a clean 400.
    let toolset: Toolset | undefined;
    if (body.toolset !== undefined) {
      if (!isToolset(body.toolset)) {
        res.status(400).json({ error: `toolset must be ${TOOLSETS.map((t) => `"${t}"`).join(" or ")}` });
        return;
      }
      toolset = body.toolset;
    }

    // mcp (optional, dependency-management migration Phase 5): the BFF-minted
    // discovery endpoint + short-lived bearer for this turn. Absent → no MCP
    // discovery (byte-identical to today). Present but malformed → a clean 400
    // rather than a confusing best-effort empty-tools no-op mid-stream.
    let mcp: McpConfig | undefined;
    if (body.mcp !== undefined) {
      if (!isMcpConfig(body.mcp)) {
        res.status(400).json({ error: "mcp must be { url: string, token: string }" });
        return;
      }
      mcp = body.mcp;
    }

    // collab (optional, #86 phase 4): a room-scoped turn. The room replaces
    // the snapshot as the FILE source (skills + the org fence still come from
    // the workspace); ops apply live to the doc; nothing commits. Reject a
    // malformed block, an unsupported toolset combination, or a deployment
    // with no collab server, pre-stream.
    let collab: CollabConfig | undefined;
    if (body.collab !== undefined) {
      if (!isCollabConfig(body.collab)) {
        res.status(400).json({ error: "collab must be { roomId: string, token: string }" });
        return;
      }
      if (toolset !== undefined && toolset !== "files") {
        res.status(400).json({ error: "collab turns support only the files toolset" });
        return;
      }
      if (!config.collabWsUrl) {
        res.status(503).json({ error: "collab is not configured on this deployment (AGENT_COLLAB_WS_URL)" });
        return;
      }
      collab = body.collab;
    }

    // Build the per-turn model from the request key (fail as a pre-stream 500).
    let model: LanguageModel;
    try {
      model = deps.buildModel(apiKey);
    } catch (err) {
      res.status(500).json({ error: err instanceof Error ? err.message : "model init failed" });
      return;
    }

    // Abort the turn if the client disconnects mid-stream; stop keep-alives too.
    // A holder (not a bare `let`) so the assignment inside `send` is visible to
    // every read site regardless of control-flow narrowing.
    const ac = new AbortController();
    let started = false;
    // Set when the client disconnects. Every terminal write is guarded on it (plus
    // `res.writableEnded`) so a client that leaves in the completion window never
    // triggers a write to a torn-down socket.
    let clientGone = false;
    const keepAlive: { stop: (() => void) | null } = { stop: null };
    res.on("close", () => {
      clientGone = true;
      ac.abort();
      keepAlive.stop?.();
    });
    // The socket is writable only while the client is attached and the response
    // has not been finished/closed.
    const canWrite = (): boolean => !clientGone && !res.writableEnded && !res.closed;

    // Lazy stream start: headers (and the keep-alive timer) go out on the FIRST
    // event. A failure BEFORE any event (e.g. a concurrent turn) is still a clean
    // HTTP status.
    const send = (part: StreamPart): void => {
      if (clientGone) return; // client left mid-turn — drop frames it can't receive
      if (!started) {
        startSSE(res);
        started = true;
        keepAlive.stop = startKeepAlive((frame) => res.write(frame), keepAliveMs);
      }
      res.write(`data: ${JSON.stringify(part)}\n\n`);
    };

    // Room-scoped turn (#86 phase 4): join as a live peer BEFORE streaming —
    // an oracle denial or sync timeout is a clean pre-stream status. The
    // synced doc replaces the snapshot as the turn's file source.
    let roomPeer: RoomPeer | undefined;
    if (collab) {
      try {
        roomPeer = await joinRoom({
          url: config.collabWsUrl!,
          roomId: collab.roomId,
          token: collab.token,
        });
        files = roomPeer.files();
      } catch (err) {
        res.status(502).json({
          error: err instanceof Error ? err.message : "collab room join failed",
        });
        return;
      }
    }

    try {
      await runConversationTurn({
        id,
        instruction: body.instruction,
        files,
        filesChangedExternally: body.filesChangedExternally === true,
        skillSource,
        ...(toolset ? { toolset } : {}),
        ...(mcp ? { mcp } : {}),
        ...(roomPeer ? { collabPeer: roomPeer } : {}),
        model,
        ...(deps.modelId ? { modelId: deps.modelId } : {}),
        store: deps.store,
        guard,
        onEvent: send,
        abortSignal: ac.signal,
      });
      keepAlive.stop?.();
      if (!canWrite()) return; // client vanished in the completion window — nothing to flush
      if (!started) startSSE(res); // no events emitted — still open + terminate the stream
      res.write(`data: ${SSE_DONE}\n\n`);
      res.end();
    } catch (err) {
      keepAlive.stop?.();
      if (!canWrite()) return; // client already gone — do not write to a torn-down socket
      if (!started) {
        // Pre-stream failure → HTTP status + JSON.
        if (err instanceof ConcurrentTurnError) {
          res.status(409).json({ error: err.message });
        } else {
          res.status(500).json({ error: err instanceof Error ? err.message : "internal error" });
        }
      } else {
        // Already streaming → cannot change status; emit an error frame then [DONE].
        const message = err instanceof Error ? err.message : String(err);
        res.write(`data: ${JSON.stringify({ type: "error", error: message })}\n\n`);
        res.write(`data: ${SSE_DONE}\n\n`);
        res.end();
      }
    } finally {
      // The agent peer never lingers in the room past its turn (presence
      // honesty); runs on every exit path, including client-gone returns.
      roomPeer?.leave();
    }
  });

  app.get("/conversations/:id", requireAuth, async (req: Request, res: Response) => {
    const conv = await deps.store.get(req.params.id as string);
    if (!conv) {
      res.status(404).json({ error: "conversation not found" });
      return;
    }
    // Raw ModelMessage[] — the UI projects tool parts to Changes client-side (§9).
    res.json({
      status: conv.status,
      messages: conv.messages,
      createdAt: conv.createdAt,
      updatedAt: conv.updatedAt,
    });
  });

  // Body-parser errors (invalid JSON, malformed payloads) → a clean 400.
  app.use((err: unknown, _req: Request, res: Response, next: NextFunction) => {
    if (res.headersSent) {
      next(err); // mid-stream — let the default handler tear down the connection
      return;
    }
    res.status(400).json({ error: "invalid request body" });
  });

  return app;
}
