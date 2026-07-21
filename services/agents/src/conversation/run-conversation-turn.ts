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
 * The single per-turn orchestration the SSE route calls: load-or-lazy-create →
 * mark active → fresh throwaway WorkingBundle from the passed snapshot →
 * `runTurn` (prepending a one-line CURRENT-STATE-authoritative note ONLY when
 * the FE flagged an external edit) → set status → persist the whole aggregate
 * → emit the terminal manifest part (D14; success only — never after a throw).
 *
 * Files NEVER touch the store; the passed `files` snapshot is both the inlined
 * CURRENT STATE and (when diverged) the basis of the note. History is
 * append-only — no turn rewrites a prior message, so the prompt-cache prefix is
 * preserved across turns.
 */

import { isStepCount, type LanguageModel, type ModelMessage, type ToolSet } from "ai";
import { FileBundle, type McpConfig, type StreamPart, type Toolset } from "@aep/agent-stream";
import { DocFileBundle } from "../collab/doc-bundle.js";
import { StreamingDocWriter } from "../collab/streaming-add.js";
import type { RoomPeer } from "../collab/room-peer.js";
import { runTurn } from "../agents/main/run-turn.js";
import { buildFileTools, ASK_QUESTION } from "../agents/main/tools/files.js";
import { buildTaskPlanTools } from "../agents/main/tools/task-plan.js";
import { TaskPlan } from "../agents/main/task-plan-accumulator.js";
import { buildInstructions, buildTaskPlanInstructions, buildPrompt } from "../agents/main/prompt.js";
import type { SkillSource } from "../agents/main/skill-source.js";
import { buildManifestPart, toTurnUsage } from "./manifest.js";
import { config } from "../shared/config.js";
import { modelProviderOptions } from "../shared/model.js";
import { loadMcpTools } from "../shared/mcp-client.js";
import type { Conversation, ConversationStore } from "../store/conversation-store.js";

/** Thrown when a second turn starts for an id whose turn is still in flight (→ HTTP 409). */
export class ConcurrentTurnError extends Error {
  readonly code = "CONCURRENT_TURN";
  constructor(public readonly conversationId: string) {
    super(`a turn is already in progress for conversation ${conversationId}`);
    this.name = "ConcurrentTurnError";
  }
}

/**
 * Per-id in-flight guard — serializes turns for one conversation. This (not a
 * status read) is what makes 409 real and testable: status is only persisted at
 * turn END and `get()` returns a clone, so a mid-turn second POST would never
 * observe `status === 'active'`. One guard per app (composition root).
 */
export class TurnGuard {
  private readonly inflight = new Set<string>();
  acquire(id: string): void {
    if (this.inflight.has(id)) throw new ConcurrentTurnError(id);
    this.inflight.add(id);
  }
  release(id: string): void {
    this.inflight.delete(id);
  }
}

const DIVERGENCE_NOTE =
  "NOTE: files were changed outside your last proposals — the CURRENT STATE " +
  "below is authoritative; ignore any earlier value you proposed.\n\n";

function freshConversation(id: string): Conversation {
  const now = new Date(); // store re-stamps on save; this is the lazy-create placeholder
  return { id, messages: [], status: "active", createdAt: now, updatedAt: now };
}

/**
 * True when the turn ended on an `ask_question` tool-call (HITL). Scans only the
 * messages appended THIS turn. Dormant while `ask_question` is disabled (§5).
 */
function endedAwaitingHuman(appended: ModelMessage[]): boolean {
  for (const m of appended) {
    if (m.role !== "assistant" || !Array.isArray(m.content)) continue;
    for (const part of m.content) {
      if (part.type === "tool-call" && part.toolName === ASK_QUESTION) return true;
    }
  }
  return false;
}

export interface RunConversationTurnInput {
  id: string;
  instruction: string;
  files: Record<string, string>;
  /** Optional FE flag (default false): prepend the CURRENT-STATE-authoritative note (§10). */
  filesChangedExternally?: boolean;
  /**
   * The turn's skill supply (§12, ADR-0002): a lazy, disk-backed source over the
   * turn's `_skills` snapshot. Its name+description catalog lands at the end of
   * the system prompt; the agent pulls a body via `loadSkill`. Omitted/empty →
   * no catalog, no `loadSkill`.
   */
  skillSource?: SkillSource;
  /**
   * Which tool set to register (tasks-github-native §9.3). Default/absent →
   * `files` (today's file-mutation tools, byte-identical). `task-plan` →
   * `planTask`/`updateTask` over a read-only snapshot, no file tools.
   */
  toolset?: Toolset;
  /**
   * Caller-supplied MCP discovery endpoint for this turn (dependency-management
   * migration Phase 5). Present → `tools/list` is fetched (best-effort) and
   * merged into the tool set as dynamic tools, under a shadow-guard so a
   * discovered tool can never shadow a built-in one. Omitted → no fetch, no
   * merge (byte-identical to today).
   */
  mcp?: McpConfig;
  /**
   * Live collab-room peer for a room-scoped turn (#86 phase 4). Present (and
   * toolset `files`) → the bundle mirrors every applied op onto the room's
   * Y.Doc as it happens; `input.files` is the doc snapshot the SERVER read
   * from the synced replica. The server owns the peer's lifecycle (join
   * before, leave after); this function only writes through it.
   */
  collabPeer?: RoomPeer;
  /** Injected at the composition root (createModel is called ONCE there, not per turn). */
  model: LanguageModel;
  /**
   * The resolved model id `model` was built with (`resolveModelId`, threaded
   * from the composition root alongside the model — #249). Attributes the
   * turn's token usage on the terminal manifest; absent (mock-model tests,
   * evals) → the manifest usage carries `model: ""`.
   */
  modelId?: string;
  store: ConversationStore;
  guard: TurnGuard;
  onEvent: (p: StreamPart) => void;
  abortSignal?: AbortSignal;
}

export async function runConversationTurn(input: RunConversationTurnInput): Promise<Conversation> {
  input.guard.acquire(input.id); // throws ConcurrentTurnError on a concurrent turn → 409
  // Live-preview writer (flag-gated, room-scoped files turns only): mirrors each
  // addFile body into the doc AS IT STREAMS. Hoisted so the finally can drain +
  // roll back a severed/rejected preview on EVERY exit path (before peer.leave).
  let docWriter: StreamingDocWriter | undefined;
  try {
    // 1. load or lazily create
    const conv = (await input.store.get(input.id)) ?? freshConversation(input.id);

    // 2. mark active (in memory; not saved mid-turn — the guard handles concurrency)
    conv.status = "active";

    // 3. select the tool set from `toolset` (default `files`). Both build a
    //    throwaway per-turn accumulator from the passed snapshot; the skill
    //    catalog + `loadSkill` are registered identically (only when skills were
    //    supplied, ADR-0002); ask_question stays DISABLED. The FileBundle is
    //    held by name: it is the source of the terminal manifest (D14).
    const toolset: Toolset = input.toolset ?? "files";
    const skills = input.skillSource;
    let bundle: FileBundle | undefined;
    let tools: ToolSet;
    let instructions: string;
    if (toolset === "task-plan") {
      // Read-only context: `files` mutates nothing; the accumulator validates
      // planTask/updateTask against it (known components + existing Tasks).
      tools = buildTaskPlanTools(new TaskPlan(input.files), skills);
      instructions = buildTaskPlanInstructions(skills);
    } else {
      bundle = input.collabPeer
        ? new DocFileBundle(input.collabPeer, input.files)
        : new FileBundle(input.files);
      tools = buildFileTools(bundle, skills);
      instructions = buildInstructions(skills);
    }

    // 3b. MCP discovery (dependency-management migration Phase 5): best-effort —
    //     a caller-supplied `mcp` merges the org's dependency-discovery tools
    //     (list_external_resources, etc.) into the tool set for this turn.
    //     `loadMcpTools` never throws (server down/401/malformed → `{}`, logged),
    //     so a turn with `mcp` never fails ON ITS ACCOUNT. Omitted `mcp`, or a
    //     failed/empty load, means `tools` IS the base set (no wrapping object)
    //     — byte-identical to an mcp-free turn. `baseTools` (the `tools` set
    //     already built above) spreads LAST — the shadow-guard — so a
    //     discovered tool can never shadow a core file-mutation/task-plan/
    //     loadSkill tool of the same name.
    if (input.mcp) {
      const mcpTools = await loadMcpTools(input.mcp);
      if (Object.keys(mcpTools).length > 0) {
        tools = { ...mcpTools, ...tools };
      }
    }

    // 3c. Live doc streaming: a room-scoped `files` turn has a bundle + peer to
    //     preview into, so mirror each applied addFile body onto the doc as it
    //     streams. Wrap onEvent so the writer observes every part; SSE forwarding
    //     is unaffected (observe only enqueues). The bundle's execute() stays the
    //     authority (validation + D14 manifest); the writer is an optimistic
    //     preview that reconciles to the same content.
    if (input.collabPeer && bundle) {
      docWriter = new StreamingDocWriter(input.collabPeer, bundle);
    }
    const onEvent = docWriter
      ? (p: StreamPart) => {
          docWriter!.observe(p);
          input.onEvent(p);
        }
      : input.onEvent;

    // 4. one generic turn. The instructions append the skill catalog at the END
    //    of the system prompt; buildPrompt inlines CURRENT STATE; prepend a one-line
    //    divergence note ONLY when the FE flagged an external edit (append-only).
    const note = input.filesChangedExternally ? DIVERGENCE_NOTE : "";
    const startLen = conv.messages.length;
    const res = await runTurn({
      model: input.model,
      instructions,
      prompt: note + buildPrompt(input.files, input.instruction),
      messages: conv.messages, // appended in place by runTurn
      tools,
      stopWhen: [isStepCount(config.maxSteps) /*, hasToolCall("ask_question") */],
      maxOutputTokens: config.maxOutputTokens,
      providerOptions: modelProviderOptions(),
      onEvent,
      ...(input.abortSignal ? { abortSignal: input.abortSignal } : {}),
    });

    // 5. set status (awaiting-human dormant while ask_question disabled)
    conv.status = endedAwaitingHuman(conv.messages.slice(startLen)) ? "awaiting-human" : "done";

    // 6. per-turn spend (#249): project the whole-turn usage onto the pinned
    //    wire shape; it rides the terminal manifest below so the aep-api fold
    //    captures it alongside the file shas.
    const usage = toTurnUsage(res.usage, input.modelId ?? "");
    if (config.logLevel === "debug") {
      process.stderr.write(
        `[turn ${conv.id}] finishReason=${res.finishReason} tokens in/out=` +
          `${res.usage.inputTokens ?? "?"}/${res.usage.outputTokens ?? "?"}\n`,
      );
    }

    // 7. persist the whole aggregate (history is append-only)
    await input.store.save(conv);

    // 8. terminal manifest (D14) — emitted LAST, only on full success (any
    //    throw above skips it, so a severed/failed stream carries no manifest
    //    and the aep-api fold refuses to commit). Mutated-paths-only from the
    //    turn's bundle; empty for chat-only and task-plan turns. Carries the
    //    turn's token usage (#249) — failed turns report none (v1).
    input.onEvent(buildManifestPart(bundle, usage));
    return conv;
  } finally {
    // Drain the live-preview writer and undo any addFile body we streamed but that
    // never finalized (a severed or rejected op). Runs before the server detaches
    // the peer (server.ts finally); on a clean turn every preview is already
    // finalized by its tool-result, so this drops nothing.
    if (docWriter) {
      await docWriter.drain();
      docWriter.rollbackDangling();
    }
    input.guard.release(input.id);
  }
}
