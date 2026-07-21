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
 * Agent SSE event contract — the typed, documented surface over the main spec
 * agent's turn stream.
 *
 * The wire stays RAW `StreamPart` (the SDK's `TextStreamPart`, one frame per
 * part); this module does NOT add an envelope. It exists so the producer (the
 * Express SSE route in `server.ts`), the eval, and the playground share ONE
 * definition of: the emitted event catalog, the payloads carried inside the
 * frames (`OpResult`, the per-tool `*Input` shapes), the reviewable `Change`
 * projection, and the turn-request body (`TurnRequest`).
 *
 * Ownership: this module is the leaf source of truth for the wire, published by
 * the `@aep/agent-stream` package so the producer (the agents service SSE
 * route), the fold consumers (evals, playground, console), and the BFF share ONE
 * definition. The domain (`bundle.ts`) and the agents-service `tool.ts` import
 * these types and their Zod schemas carry a compile-time drift guard asserting
 * they stay assignable to the `*Input` types here — so there is no
 * hand-maintained parallel copy. This stream is NOT part of the generated
 * OpenAPI contracts in `packages/contracts` (raw AI SDK frames aren't
 * OpenAPI-representable); the package stays free of any AI-SDK dependency.
 */

// --- Result payloads (the `tool-result.output` value) -----------------------

/** The file-mutation operations the main agent performs. */
export type Op = "add" | "edit" | "remove";

/** Error codes the `FileBundle` returns to steer one-step self-correction. */
export type ErrCode =
  | "ALREADY_EXISTS"
  | "INVALID_PATH"
  | "NOT_FOUND"
  | "NOT_UNIQUE"
  | "NO_SUCH_FILE"
  | "EMPTY_OLD_STRING"
  | "INVALID_YAML"
  | "INVALID_JSON"
  | "SCHEMA_VIOLATION"
  | "INVALID_DSL"
  | "PROTECTED_PATH";

/** A candidate line echoed back for NOT_UNIQUE / NOT_FOUND re-anchoring. */
export interface MatchCandidate {
  line: number;
  text: string;
}

/**
 * A successful op. Carries NO file content — a successful result is just the
 * status; the model anchors later edits from its own tool-call args plus the
 * re-inlined CURRENT STATE, and consumers reconstruct file state by folding the
 * stream (see `Change` / `applyToolCall`).
 */
export interface OpOk {
  ok: true;
  path: string;
  op: Op;
  /** `applied` = state changed; `already-applied`/`noop` = idempotent no-change. */
  status: "applied" | "already-applied" | "noop";
}

/** A failed op. Keeps the self-correction payload (candidates / count). */
export interface OpErr {
  ok: false;
  path: string;
  op: Op;
  code: ErrCode;
  message: string;
  /** Populated for NOT_UNIQUE / NOT_FOUND to steer one-step re-anchoring. */
  candidates?: MatchCandidate[];
  count?: number;
}

export type OpResult = OpOk | OpErr;

// --- Per-tool input shapes (the `tool-call.input` value) --------------------
//
// These are the WIRE source of truth. The Zod `inputSchema`s in
// `@aep/agents` `tool.ts` carry a compile-time assert that `z.infer<schema>`
// stays equal to these — divergence fails that package's typecheck.

export interface AddFileInput {
  path: string;
  content: string;
}

export interface EditFileInput {
  path: string;
  oldString: string;
  newString: string;
}

export interface RemoveFileInput {
  path: string;
}

// --- Skills (progressive disclosure, ADR-0002) ------------------------------
//
// Skills are GUIDANCE, not code, and they never travel on the wire: the turn's
// `WorkspaceRef.skillsRef` names an immutable `_skills` snapshot on the shared
// mount, and the service reads the catalog (and, lazily, the bodies) from
// there. The system prompt shows only a name+description catalog; the agent
// pulls a body on demand via the `loadSkill` tool. The body enters context only
// when loaded, and then persists as a tool result in message history.

/**
 * The `loadSkill` tool input. WIRE source of truth; drift-guarded in `tool.ts`.
 * Takes ALL the skills a turn needs in one call — batching keeps skill loading
 * to a single agent step instead of one step per skill.
 */
export interface LoadSkillInput {
  names: string[];
}

/** One resolved skill body inside a `loadSkill` result. */
export interface LoadedSkill {
  name: string;
  content: string;
  /** Reference paths for `loadSkillReference` — the body says when each is worth reading. */
  references?: string[];
}

/**
 * The `loadSkill` tool result. `ok: false` still carries every skill that DID
 * resolve plus the missing names and the available catalog, so the model
 * self-corrects in one round-trip (cf. NOT_FOUND) by re-calling for the
 * corrected missing names only.
 */
export type LoadSkillResult =
  | { ok: true; skills: LoadedSkill[] }
  | { ok: false; error: string; skills: LoadedSkill[]; missing: string[]; available: string[] };

/** The `loadSkillReference` tool input. WIRE source of truth; drift-guarded in `tool.ts`. */
export interface LoadSkillReferenceInput {
  name: string;
  path: string;
}

/**
 * The `loadSkillReference` tool result. A miss lists what IS addressable so the
 * model self-corrects in one round-trip: the skills carrying references (name
 * miss) or that skill's reference paths (path miss).
 */
export type LoadSkillReferenceResult =
  | { ok: true; name: string; path: string; content: string }
  | { ok: false; name: string; path: string; error: string; available: string[] };

// --- MCP discovery (caller-supplied, dependency-management migration Phase 5) -
//
// The org's dependency-discovery MCP server (aep-api
// `internal/feature/dependencies/mcp_server.go`, mounted at `POST
// /internal/v1/mcp`) lists read-only tools (list_external_resources,
// get_external_resource_schema, list_org_endpoints,
// list_platform_resource_types) so the main agent proposes `dependencies`
// entries that reuse resources/endpoints already registered in the org instead
// of inventing new names/shapes. Mirrors `WorkspaceRef`: the CALLER (the BFF)
// resolves the endpoint and mints a short-lived, org-bound bearer token, and
// pushes both in the turn payload; the service never reads either from its own
// env. Omitted → no `tools/list` fetch and no discovery tools registered
// (byte-identical to a turn without `mcp`).

/** Caller-supplied MCP discovery endpoint for this turn. */
export interface McpConfig {
  /** The MCP JSON-RPC endpoint (aep-api's `/internal/v1/mcp`, org-bound). */
  url: string;
  /**
   * Bearer token for that endpoint. Short-lived (minted per call, ~5 min TTL on
   * the aep-api side, `AudienceMCP`/`aep-api-mcp`) — a turn that outlives it sees
   * the discovery tools 401 partway through; `loadMcpTools` degrades that to "no
   * tools" up front, but a mid-turn `tools/call` 401 surfaces as a failed tool
   * call (best-effort, not retried).
   */
  token: string;
}

/**
 * Caller-supplied collab-room reference for a room-scoped turn (#86 phase 4).
 * Mirrors `McpConfig`: the BFF resolves the room and forwards the caller's
 * bearer; the service never reads either from its own env (the ws URL alone
 * comes from service config — the BFF doesn't know the agents-side route).
 * Present → the agents service joins the room as a live Yjs peer, reads the
 * file bundle FROM the doc, and applies file ops to it; nothing is committed
 * to git (persistence is the #86 phase-3 committer). Omitted → the
 * committed-truth snapshot turn, byte-identical to today.
 */
export interface CollabConfig {
  /** The room id (`spec-<org>-<project>`), resolved by the BFF. */
  roomId: string;
  /**
   * The caller's bearer, forwarded request-scoped (#86 decision 7): the
   * collab server's BFF oracle validates it exactly like a browser join.
   */
  token: string;
}

/** Runtime guard for an untrusted `collab` value (the server's pre-stream 400 check). */
export function isCollabConfig(v: unknown): v is CollabConfig {
  if (typeof v !== "object" || v === null) return false;
  const c = v as Record<string, unknown>;
  return typeof c.roomId === "string" && c.roomId !== "" && typeof c.token === "string" && c.token !== "";
}

// --- The reviewable change (§7) ---------------------------------------------

/**
 * A reviewable projection of one `tool-result` part: the op intent plus its
 * result. A browser folds these into a live diff; the eval reconstructs files
 * via `applyToolCall` instead. Pure field projection — see `toChange` in
 * `@aep/agents` `change.ts`.
 */
export interface Change {
  toolCallId: string;
  toolName: string;
  op: Op;
  path: string;
  /** editFile payload. */
  oldString?: string;
  newString?: string;
  /** addFile payload. */
  content?: string;
  result: OpResult;
}

// --- The turn request (the `POST /conversations/:id/turns` body) -------------

/**
 * The shared-volume workspace reference (shared-volume-clone-architecture §12,
 * D9): IDs + shas only — **no filesystem path ever crosses the boundary**. The
 * agents service derives
 * `$WORKSPACE_MOUNT_ROOT/repos/<org>/<proj>/<repoSlug>/snapshots/<ref>/` (and
 * the `_skills` analog) itself from these fields, so a hostile payload has no
 * path input to traverse: the tenancy fence is structural, not validated-in.
 */
export interface WorkspaceRef {
  /**
   * The namespaced conversation id, `org_<orgId>--proj_<projectId>--<useCase>--<uuid>`.
   * Must equal the URL `:id`; supplies the org/proj path segments and the org
   * value asserted against the caller's `X-Org-Id` claim (the IDOR fence).
   */
  conversationId: string;
  /** Per-dispatch uuid (turn attribution/tracing; never used in path derivation). */
  turnId: string;
  /** The repo directory segment (`git_repositories.repo_slug`; validated slug format). */
  repoSlug: string;
  /** 40-hex committed base sha — the turn reads `snapshots/<ref>/`. */
  ref: string;
  /** 40-hex `_skills` head sha — skills load from `_skills/org-skills/snapshots/<skillsRef>/`. */
  skillsRef: string;
}

/**
 * The turn-request body (D9/§12): the body carries a `WorkspaceRef` and the
 * service reads the file snapshot AND the skills from the shared read-only
 * mount — no file content or skill bodies ever cross the wire.
 *
 * `filesChangedExternally` flags an out-of-band edit so the server prepends a
 * CURRENT-STATE-authoritative note. The producer (server) validates an
 * untrusted body against this shape; the eval client (and the BFF) construct
 * it — one definition, no drift.
 */
export interface TurnRequest {
  instruction: string;
  /** Where to read files + skills from the shared mount (IDs + shas only). */
  workspace: WorkspaceRef;
  filesChangedExternally?: boolean;
  /**
   * Which domain tool set to register (tasks-github-native §9.3). `files`
   * (default, and identical to an absent value) registers the file-mutation
   * tools — nothing changes for the generation flows. `task-plan` registers the
   * `planTask`/`updateTask` tools instead (no file tools); `files` then carries
   * READ-ONLY context (spec/design bundle + existing-Task renderings), nothing
   * mutates it. See `contracts/task-tools.ts`.
   */
  toolset?: Toolset;
  /**
   * Caller-supplied MCP discovery endpoint for this turn (dependency-management
   * migration Phase 5). Present → the turn loop fetches `tools/list` from it
   * (best-effort) and registers each as a dynamic tool, merged under a
   * shadow-guard so a discovered tool can never shadow a built-in one. Omitted →
   * no fetch, no discovery tools (byte-identical to an mcp-free turn).
   */
  mcp?: McpConfig;
  /**
   * Room-scoped turn (#86 phase 4): join this collab room as a live Yjs peer,
   * read files from the doc, apply ops to the doc, commit nothing. Omitted →
   * the committed-truth snapshot turn (byte-identical to today).
   */
  collab?: CollabConfig;
}

/** The registrable tool sets a turn may request (`TurnRequest.toolset`). */
export const TOOLSETS = ["files", "task-plan"] as const;

export type Toolset = (typeof TOOLSETS)[number];

/** Runtime guard for an untrusted `toolset` value (the server's pre-stream 400 check). */
export function isToolset(v: unknown): v is Toolset {
  return (TOOLSETS as readonly unknown[]).includes(v);
}

// --- The terminal manifest (shared-volume-clone-architecture D14) ------------

/**
 * Per-turn token usage carried on the terminal manifest (#249). Field names are
 * the pinned cross-runtime wire shape — the coding-runner progress `result`
 * event carries the identical object and the aep-api parses both against one
 * definition, so they must not drift. Tokens only, no cost figure: USD is
 * derived server-side from tokens + model (console ADR-0011).
 */
export interface TurnUsage {
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheCreationTokens: number;
  /** The resolved model id the turn ran on (the server-side pricing key). */
  model: string;
}

/**
 * The terminal manifest frame — ALWAYS emitted (possibly empty) after a turn
 * completes successfully, before `[DONE]`; a severed/failed stream carries NO
 * manifest, which is exactly what lets the consumer (the aep-api fold) treat
 * its absence as "do not commit". Covers ONLY the paths mutated THIS turn:
 * `files` maps each still-present touched path to the sha256 (lowercase hex,
 * over the UTF-8 bytes) of its final content; `deleted` lists the touched
 * paths no longer present. A chat-only or task-plan turn emits
 * `{files: {}, deleted: []}`. Structurally a `StreamPart` (open type), named
 * here so both fold sides hash-check against ONE definition.
 */
export interface ManifestPart {
  type: "manifest";
  /** path → sha256 hex of the final content, for every path mutated this turn and still present. */
  files: Record<string, string>;
  /** Paths mutated this turn that are no longer present at turn end. */
  deleted: string[];
  /**
   * The turn's token spend (#249). Present on every manifest the agents
   * service emits today; optional so older producers/recorded streams stay
   * valid. Manifest-only ⇒ a failed/severed turn carries no usage (v1).
   */
  usage?: TurnUsage;
}

// --- The emitted event catalog ----------------------------------------------

/**
 * The documented subset of `StreamPart` `type`s the SSE route emits, one SSE
 * frame each. The wire carries the raw part verbatim; this catalog names what a
 * consumer can expect to see. `manifest` (D14) is terminal: at most one, after
 * the loop's last `finish` and before `[DONE]`, only on a successful turn.
 */
export const AGENT_SSE_EVENT_TYPES = [
  "text-delta",
  "tool-input-start",
  "tool-input-delta",
  "tool-call",
  "tool-result",
  "tool-error",
  "error",
  "finish",
  "manifest",
] as const;

export type AgentSseEventType = (typeof AGENT_SSE_EVENT_TYPES)[number];

/** The terminal sentinel sent after the last frame: `data: [DONE]`. */
export const SSE_DONE = "[DONE]" as const;
