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
import type { FilePart, LanguageModel } from "ai";
import {
  SSE_DONE,
  isTurnSpec,
  isCollabConfig,
  isSurface,
  isTurnAim,
  isTurnAttachmentsOrAbsent,
  SURFACES,
  type CollabConfig,
  type McpConfig,
  type StreamPart,
  type Surface,
  type Toolset,
  type TurnAim,
  type TurnJournal,
  type TurnSpec,
} from "@aep/agent-stream";
import { composeInstruction, eagerSkillsFor, toolsetFor, wantsRegisterDraftTool } from "./prompts/turn.js";
import type { ConversationStore } from "./store/conversation-store.js";
import { runConversationTurn, TurnGuard, ConcurrentTurnError } from "./conversation/run-conversation-turn.js";
import { projectDisplayHistory } from "./conversation/display-history.js";
import { joinRoom, type RoomPeer } from "./collab/room-peer.js";
import type { SkillSource } from "./agents/main/skill-source.js";
import {
  readSnapshot,
  loadSkillsFromSnapshot,
  readReferenceAttachments,
  overlayReferenceTexts,
  toAttachmentParts,
} from "./conversation/load-workspace.js";
import { conversationOrgId, resolveWorkspace, WorkspaceRefError } from "./shared/snapshot-path.js";
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

/** Runtime guard for an untrusted `journal` value (#463). */
function isJournal(v: unknown): v is TurnJournal {
  if (typeof v !== "object" || v === null) return false;
  const j = v as Record<string, unknown>;
  if (typeof j.text !== "string" || j.text.trim() === "") return false;
  // Attachment NAMES (#428) — never bytes. Checked for type when present, so a
  // malformed list is a clean 400 rather than a blank chip in someone's thread.
  if (
    j.attachments !== undefined &&
    !(Array.isArray(j.attachments) && j.attachments.every((n) => typeof n === "string"))
  ) {
    return false;
  }
  if (j.author === undefined) return true;
  if (typeof j.author !== "object" || j.author === null) return false;
  const a = j.author as Record<string, unknown>;
  return typeof a.id === "string" && a.id !== "" && typeof a.displayName === "string" && a.displayName !== "";
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
  // A workspace turn body used to be a few hundred bytes of IDs + shas. Chat
  // attachments (#428) now ride it INLINE as base64, so the ceiling has to admit
  // them — and the number is derived, not chosen: the per-turn attachment budget
  // is 20 MiB ENCODED (MAX_REFERENCE_ATTACHMENT_ENCODED_BYTES), so 24 MiB leaves
  // 4 MiB for the prompt, the refs and the JSON framing around them.
  //
  // Raising an authenticated internal endpoint's ceiling ~94x is not free, which
  // is why it is pinned to that budget: any future request to widen it further
  // has to widen the budget first.
  const jsonParser = express.json({ limit: "24mb" });
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
      turn?: unknown;
      target?: unknown;
      previousTurnFailed?: unknown;
      headless?: unknown;
      instruction?: unknown;
      workspace?: unknown;
      files?: unknown;
      filesChangedExternally?: unknown;
      skills?: unknown;
      toolset?: unknown;
      mcp?: unknown;
      journal?: unknown;
      aim?: unknown;
      attachments?: unknown;
      collab?: unknown;
      webSearch?: unknown;
      surface?: unknown;
      eagerSkills?: unknown;
    };

    // The retired pre-composition contract — reject it loudly, exactly as the
    // pre-§12 inline `files`/`skills` shape is rejected below. A stale caller
    // must never run a turn whose wording this service did not compose.
    if (body.instruction !== undefined) {
      res.status(400).json({ error: "instruction is no longer accepted — send a turn spec" });
      return;
    }
    if (body.toolset !== undefined) {
      res.status(400).json({ error: "toolset is no longer accepted — it is derived from turn.kind" });
      return;
    }
    if (body.eagerSkills !== undefined) {
      res.status(400).json({ error: "eagerSkills is no longer accepted — they are derived from the turn" });
      return;
    }

    // Pre-stream validation → HTTP status (no SSE headers sent yet).
    //
    // The caller states what the turn is FOR; this service composes the wording
    // (see prompts/turn.ts).
    if (body.turn === undefined) {
      res.status(400).json({ error: "turn is required" });
      return;
    }
    if (!isTurnSpec(body.turn)) {
      res.status(400).json({ error: "turn must be a valid turn spec (kind: chat | flow | start | plan)" });
      return;
    }
    const turn: TurnSpec = body.turn;
    if (body.target !== undefined && typeof body.target !== "string") {
      res.status(400).json({ error: "target must be a string" });
      return;
    }

    // The caller's surface (#580): who is reading this turn's prose. The one
    // turn property that cannot be derived — it is who is asking, not what is
    // being asked for — so an unknown value is a 400 rather than a silent
    // fallback that would narrate to the wrong audience.
    let surface: Surface | undefined;
    if (body.surface !== undefined) {
      if (!isSurface(body.surface)) {
        res.status(400).json({ error: `surface must be one of: ${SURFACES.join(", ")}` });
        return;
      }
      surface = body.surface;
    }
    // aim (#666): what the user pointed at, and what for. Parsed BEFORE the
    // instruction because it leads the wording, and reused for the journal
    // below — one value, so the tag the transcript renders can never disagree
    // with the selection the model was told about.
    let aim: TurnAim | undefined;
    if (body.aim !== undefined) {
      if (!isTurnAim(body.aim)) {
        res.status(400).json({
          error:
            "aim must be { anchor: { file, nodes: [{ name, kind, context? }] }, intent: change|discuss }",
        });
        return;
      }
      aim = body.aim;
    }
    const instruction = composeInstruction(turn, {
      target: typeof body.target === "string" ? body.target : undefined,
      previousTurnFailed: body.previousTurnFailed === true,
      headless: body.headless === true,
      ...(aim ? { aim } : {}),
    });

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
    // Reference PDFs (#384): a `start` turn's TurnSpec.references may name
    // `.pdf` documents. They are never in `files` — the snapshot filter admits
    // no `.pdf`, whatever the bytes look like — so no document can ride one turn
    // as both prompt text and a file part. Attached separately as native file
    // parts (see run-conversation-turn.ts / run-turn.ts); every other kind, and
    // a start turn with no PDF references, leaves this empty.
    let referenceAttachments: FilePart[] = [];
    let turnId: string;
    let projectId: string | undefined;
    try {
      const ws = resolveWorkspace({
        conversationIdParam: id,
        workspace: body.workspace,
        orgIdClaim: orgId,
        ...(deps.workspaceMountRoot ? { mountRoot: deps.workspaceMountRoot } : {}),
      });
      projectId = ws.projectId;
      files = readSnapshot(ws.snapshotDir);
      skillSource = loadSkillsFromSnapshot(ws.skillsSnapshotDir);
      if (turn.kind === "start" || turn.kind === "flow") {
        // Flows generate artifacts that must be grounded in the attachments
        // (a sketch is the wireframe brief); run-conversation-turn dedupes
        // against history, so re-naming a document never re-stores it.
        referenceAttachments = readReferenceAttachments(ws.snapshotDir, turn.references);
      }
      turnId = ws.turnId;
    } catch (err) {
      if (err instanceof WorkspaceRefError) {
        res.status(err.status).json({ error: err.message });
        return;
      }
      // A stat-checked snapshot failing to read = an infrastructure fault.
      res.status(500).json({ error: err instanceof Error ? err.message : "workspace read failed" });
      return;
    }

    // toolset: which domain tools to register (§9.3). DERIVED from the turn —
    // planning registers the task tools and no file tools, everything else
    // mutates the bundle.
    const toolset: Toolset = toolsetFor(turn);

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

    // Chat attachments (#428): bytes INLINE on the body, because nothing stores
    // them (console ADR-0019). Converted to native file parts and appended to the
    // reference parts — the per-turn encoded budget is SHARED between the two
    // channels rather than doubled, since the ceiling belongs to the model
    // request, not to either channel.
    if (!isTurnAttachmentsOrAbsent(body.attachments)) {
      res.status(400).json({
        error: "attachments must be an array of { name: string, mediaType: string, data: string }",
      });
      return;
    }
    // Kept SEPARATE from referenceAttachments, not merged: chat attachments must
    // not be deduped against history (see run-conversation-turn). The encoded
    // budget is still shared — that ceiling belongs to the model request, not to
    // either channel — which is why the reference parts' cost is passed in.
    let chatAttachments: FilePart[] = [];
    if (body.attachments && body.attachments.length > 0) {
      const spent = referenceAttachments.reduce(
        (n, part) => n + (typeof part.data === "string" ? part.data.length : 0),
        0,
      );
      chatAttachments = toAttachmentParts(body.attachments, spent);
    }

    // journal (#463): the turn's display record — raw client-sent text + acting
    // user. Optional (older callers/evals journal nothing); malformed → clean
    // 400. Rebuilt field-by-field: the body object is untrusted, and a spread
    // would persist whatever extra keys a caller smuggled beside `text`.
    let journal: TurnJournal | undefined;
    if (body.journal !== undefined) {
      if (!isJournal(body.journal)) {
        res.status(400).json({ error: "journal must be { text: string, author?: { id, displayName } }" });
        return;
      }
      journal = {
        text: body.journal.text,
        ...(body.journal.author
          ? { author: { id: body.journal.author.id, displayName: body.journal.author.displayName } }
          : {}),
        // NAMES OF WHAT SURVIVED, not what the caller sent. The two differ
        // whenever reference parts exhaust the shared encoded budget and a chat
        // attachment is skipped: persisting the caller's list would put a chip
        // on a file the model never received — the exact confusion the chips
        // exist to prevent, inverted. Derived from the parts for the same reason
        // the prompt's file list is.
        ...(chatAttachments.length > 0
          ? { attachments: chatAttachments.flatMap((p) => (p.filename ? [p.filename] : [])) }
          : {}),
        // From the SAME value the instruction was composed from, never a
        // second field the caller sends alongside: a journal that disagreed
        // with the prompt would render a tag for a selection the agent was
        // never pointed at.
        ...(aim ? { anchor: aim.anchor } : {}),
      };
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

    // eagerSkills (#335 latency): skill bodies inlined into this turn's prompt
    // up front, skipping the loadSkill round-trip. DERIVED from the turn —
    // which guidance a flow needs is a property of the flow, not of the call,
    // so a console CTA, a typed command and a playground run cannot diverge.
    const derivedEager = eagerSkillsFor(turn);
    const eagerSkills = derivedEager.length > 0 ? derivedEager : undefined;

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
        // The room excludes reference documents by design — text references
        // ride in from the turn's snapshot instead, which is their authority
        // (aep-api overlays the off-git store into it).
        files = overlayReferenceTexts(roomPeer.files(), files);
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
        instruction,
        files,
        filesChangedExternally: body.filesChangedExternally === true,
        skillSource,
        ...(referenceAttachments.length ? { referenceAttachments } : {}),
        ...(chatAttachments.length ? { chatAttachments } : {}),
        ...(toolset ? { toolset } : {}),
        ...(wantsRegisterDraftTool(turn, projectId) ? { registerDraft: true } : {}),
        ...(mcp ? { mcp } : {}),
        ...(journal ? { journal: { ...journal, turnId } } : {}),
        ...(eagerSkills ? { eagerSkills } : {}),
        webSearch: body.webSearch === true,
        ...(surface ? { surface } : {}),
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
    // The same cross-tenant fence as the turn POST (§12): the id's org segment
    // must equal the caller's X-Org-Id claim. The M2M token is shared, so
    // without this any holder could read another org's thread — which now
    // carries per-turn author identities (#463).
    const id = req.params.id as string;
    try {
      const claim = req.header("x-org-id");
      if (!claim || conversationOrgId(id) !== claim) {
        res.status(403).json({ error: "conversation org does not match the caller's organization" });
        return;
      }
    } catch (err) {
      if (err instanceof WorkspaceRefError) {
        res.status(err.status).json({ error: err.message });
        return;
      }
      throw err;
    }
    const conv = await deps.store.get(id);
    if (!conv) {
      res.status(404).json({ error: "conversation not found" });
      return;
    }
    // Display projection (#463): user rows carry the journal's raw client-sent
    // text + author instead of the composed model prompt; assistant/tool rows
    // pass through raw — the UI projects their text, question tool-calls, and
    // Changes client-side (§9).
    res.json({
      status: conv.status,
      messages: projectDisplayHistory(conv),
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
