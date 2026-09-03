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

import { hasToolCall, isStepCount, type FilePart, type LanguageModel, type ModelMessage, type ToolSet } from "ai";
import {
  FileBundle,
  ASK_QUESTION_TOOL,
  ASK_QUESTIONS_TOOL,
  type McpConfig,
  type StreamPart,
  type Surface,
  type Toolset,
} from "@aep/agent-stream";
import { DocFileBundle } from "../collab/doc-bundle.js";
import { StreamingDocWriter } from "../collab/streaming-add.js";
import type { RoomPeer } from "../collab/room-peer.js";
import { runTurn } from "../agents/main/run-turn.js";
import { buildFileToolSet, buildRegisterDraftTools } from "../agents/main/tools/files.js";
import { tapWrites, type WriteLedger } from "../agents/main/tools/write-ledger.js";
import { buildTaskPlanTools } from "../agents/main/tools/task-plan.js";
import { TaskPlan } from "../agents/main/task-plan-accumulator.js";
import { buildInstructions, buildTaskPlanInstructions, buildPrompt, buildEagerSkillsBlock } from "../agents/main/prompt.js";
import type { SkillSource } from "../agents/main/skill-source.js";
import { buildManifestPart, toTurnUsage } from "./manifest.js";
import { attachmentsNote } from "../prompts/turn.js";
import { config } from "../shared/config.js";
import {
  isAnthropicModel,
  modelCacheBreakpoint,
  modelProviderOptions,
  webSearchTool,
} from "../shared/model.js";
import { loadMcpTools } from "../shared/mcp-client.js";
import { turnTelemetry } from "../shared/telemetry.js";
import type { Conversation, ConversationStore, TurnJournalEntry } from "../store/conversation-store.js";

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
  "NOTE: files were changed outside your last proposals — the \"Existing files:\" " +
  "below are authoritative; ignore any earlier value you proposed.\n\n";

function freshConversation(id: string): Conversation {
  const now = new Date(); // store re-stamps on save; this is the lazy-create placeholder
  return { id, messages: [], turns: [], status: "active", createdAt: now, updatedAt: now };
}

/**
 * True when the turn ended on a HITL question tool-call (`ask_question` or
 * `ask_questions`, console ADR-0012 / #270). Scans only the messages appended
 * THIS turn; the paired `hasToolCall` stop conditions guarantee such a call is
 * the last step, so a match means the turn is awaiting the user's answer.
 */
function endedAwaitingHuman(appended: ModelMessage[]): boolean {
  for (const m of appended) {
    if (m.role !== "assistant" || !Array.isArray(m.content)) continue;
    for (const part of m.content) {
      if (
        part.type === "tool-call" &&
        (part.toolName === ASK_QUESTION_TOOL || part.toolName === ASK_QUESTIONS_TOOL)
      ) {
        return true;
      }
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
   * Native file parts (currently: `.pdf` reference documents, #384) attached to
   * THIS turn's user message alongside `instruction` — resolved by the caller
   * from `TurnSpec.references` against the snapshot dir (`load-workspace.ts`'s
   * `readReferenceAttachments`), since this function only sees `files`/
   * `instruction`, never the raw `TurnSpec` or a filesystem path. Absent/empty
   * → the message stays a plain string, byte-identical to a turn without it.
   */
  referenceAttachments?: FilePart[];
  /**
   * Native file parts for the files the user attached to THIS message (#428).
   *
   * Separate from `referenceAttachments` — NOT merely for wording, but because
   * they must NOT be deduped against history. That dedupe exists for
   * references, which a flow re-lists automatically with unchanged content, so
   * re-sending them is pure waste. A chat attachment is the opposite: a
   * deliberate per-message act. Someone who revises a PDF, keeps its name and
   * re-attaches it means the new bytes — filtering those out would silently
   * serve the model the stale copy from history.
   */
  chatAttachments?: FilePart[];
  /**
   * Skill names to inline into THIS turn's prompt up front (#335 latency):
   * bodies resolve through `skillSource` and ride the user prompt — never the
   * system prompt, whose cacheable prefix must stay byte-stable across turns.
   * Unknown names are skipped. The catalog + lazy `loadSkill` are unaffected.
   */
  eagerSkills?: string[];
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
  /**
   * Marketplace register flow: merge draftExternalResource onto the files set.
   * Omitted → byte-identical files tools (spec/project turns).
   */
  registerDraft?: boolean;
  /**
   * Attach Anthropic's provider-executed `web_search` tool for this turn
   * (external-dependency-discovery #252). The caller (BFF) sets this true
   * under the same condition as `mcp`. Registered ONLY when true AND the
   * turn's `model` is actually Anthropic (`isAnthropicModel`) — Anthropic-only,
   * injecting it against another provider would error. Omitted/false, or a
   * non-Anthropic model, → the tool map is byte-identical to a turn without it.
   */
  webSearch?: boolean;
  /**
   * Where the person reading this turn's prose is sitting (#580). Present → the
   * surface's narration skill is inlined into the SYSTEM prompt as standing
   * policy, outranking any narration a loaded flow skill defines for itself.
   * Absent (a local playground run) → no policy block, and the prompt is
   * byte-identical to today.
   */
  surface?: Surface;
  /** Injected at the composition root (createModel is called ONCE there, not per turn). */
  model: LanguageModel;
  /**
   * The resolved model id `model` was built with (`resolveModelId`, threaded
   * from the composition root alongside the model — #249). Attributes the
   * turn's token usage on the terminal manifest; absent (mock-model tests,
   * evals) → the manifest usage carries `model: ""`.
   */
  modelId?: string;
  /**
   * The turn's journal entry (#463): the raw client-sent instruction + acting
   * user, appended to `conv.turns` alongside the transcript in the same save —
   * the display source the get-conversation read serves for user rows. Absent
   * (older callers, evals) → no entry; the read falls back to the raw message.
   */
  journal?: Omit<TurnJournalEntry, "messageIndex" | "createdAt">;
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
    //    supplied, ADR-0002). The `files` set also carries the HITL question
    //    tools (ask_question / ask_questions); `task-plan` does not. The
    //    FileBundle is held by name: it is the source of the terminal manifest (D14).
    const toolset: Toolset = input.toolset ?? "files";
    const skills = input.skillSource;
    let bundle: FileBundle | undefined;
    let tools: ToolSet;
    let instructions: string;
    // The turn's write ledger (files turns only) — see write-ledger.ts: it is
    // what lets each file write settle at its OWN tool-input-end instead of at
    // the step's tail, which for a batched step is minutes later.
    let writes: WriteLedger | undefined;
    if (toolset === "task-plan") {
      // Read-only context: `files` mutates nothing; the accumulator validates
      // planTask/updateTask against it (known components + existing Tasks).
      tools = buildTaskPlanTools(new TaskPlan(input.files), skills);
      instructions = buildTaskPlanInstructions(skills, input.surface);
    } else {
      bundle = input.collabPeer
        ? new DocFileBundle(input.collabPeer, input.files)
        : new FileBundle(input.files);
      const fileToolSet = buildFileToolSet(bundle, skills);
      tools = fileToolSet.tools;
      writes = fileToolSet.writes;
      if (input.registerDraft) {
        tools = { ...tools, ...buildRegisterDraftTools() };
      }
      instructions = buildInstructions(skills, input.surface);
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

    // 3b'. Web search (external-dependency-discovery #252): Anthropic's
    //      provider-executed `web_search` tool, gated on the caller-supplied
    //      `webSearch` flag (the BFF sets it true under the same design-
    //      generate/collab condition as `mcp`) AND the turn's model actually
    //      being Anthropic (`isAnthropicModel`) — the tool is Anthropic-only,
    //      so injecting it against another provider would error; a mismatch
    //      degrades silently to no tool. Absent flag, or a non-Anthropic
    //      model, leaves `tools` untouched (byte-identical to today). Spread
    //      LAST — same shadow-guard as the MCP merge above — so a discovered
    //      MCP tool can never shadow it either.
    if (input.webSearch && isAnthropicModel(input.model)) {
      tools = { ...tools, web_search: webSearchTool() };
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
    // 3d. Per-call verdicts (write-ledger.ts): a file write is applied and
    //     reported at its own tool-input-end, so a step that batches five
    //     addFiles settles each one as it lands instead of flushing all five
    //     verdicts after the last body. Only the WIRE is re-projected — the doc
    //     writer keeps observing the SDK's own frames, so its optimistic preview
    //     is still finalized (or rolled back) by the authoritative execute().
    const forward = writes ? tapWrites(writes, input.onEvent) : input.onEvent;
    const onEvent = docWriter
      ? (p: StreamPart) => {
          docWriter!.observe(p);
          forward(p);
        }
      : forward;

    // 4. one generic turn. The instructions append the skill catalog at the END
    //    of the system prompt; buildPrompt inlines CURRENT STATE; prepend a one-line
    //    divergence note ONLY when the FE flagged an external edit (append-only).
    const note = input.filesChangedExternally ? DIVERGENCE_NOTE : "";
    // Eager skills (#335): resolve the requested bodies and inline them ahead
    // of the instruction — the model applies them in its FIRST step instead of
    // spending a whole model call on loadSkill. Unknown names skip silently
    // (the snapshot is the authority on what exists).
    const eagerBlock = buildEagerSkillsBlock(skills, input.eagerSkills, input.surface);
    const cacheBreakpoint = modelCacheBreakpoint();
    // Stamps this turn's steps with the conversation they belong to, so two
    // projects generating at once are attributable in the trace UI.
    const telemetry = turnTelemetry(conv.id);
    // REFERENCE attachments dedupe against history by filename: a flow naming
    // the same document a kickoff already attached must not re-enter it — one
    // copy of a 5MB PDF per conversation, not one per flow invocation. The model
    // reads the history copy either way, because a reference's content is
    // whatever the store holds and re-listing it says nothing new.
    //
    // Chat attachments (#428) are deliberately EXEMPT — see `chatAttachments`.
    const attachedAlready = new Set(
      conv.messages.flatMap((m) =>
        Array.isArray(m.content)
          ? m.content.flatMap((part) => {
              const f = part as { type?: string; filename?: string };
              return f.type === "file" && f.filename ? [f.filename] : [];
            })
          : [],
      ),
    );
    const freshReferences = (input.referenceAttachments ?? []).filter(
      (part) => !part.filename || !attachedAlready.has(part.filename),
    );
    // Attachments last so a re-attached file is the LATER copy in the message,
    // which is the one the model reads as current.
    const freshAttachments = [...freshReferences, ...(input.chatAttachments ?? [])];
    const startLen = conv.messages.length;
    const res = await runTurn({
      model: input.model,
      instructions,
      prompt:
        note +
        attachmentsNote((input.chatAttachments ?? []).flatMap((p) => (p.filename ? [p.filename] : []))) +
        eagerBlock +
        buildPrompt(input.files, input.instruction),
      messages: conv.messages, // appended in place by runTurn
      ...(freshAttachments.length ? { fileParts: freshAttachments } : {}),
      tools,
      // End the turn at a HITL question call (the question tools live on the
      // `files` set only, so these never fire on a task-plan turn).
      stopWhen: [
        isStepCount(config.maxSteps),
        hasToolCall(ASK_QUESTION_TOOL),
        hasToolCall(ASK_QUESTIONS_TOOL),
      ],
      maxOutputTokens: config.maxOutputTokens,
      providerOptions: modelProviderOptions(),
      // History is append-only (see the module doc above), so the prefix this
      // marks is byte-identical on the next step and the next turn — which is
      // exactly what makes it cacheable. Omitted entirely when caching is off,
      // so the request is byte-identical to before this existed.
      ...(cacheBreakpoint ? { cacheBreakpoint } : {}),
      ...(telemetry ? { telemetry } : {}),
      onEvent,
      ...(input.abortSignal ? { abortSignal: input.abortSignal } : {}),
    });

    // 5. set status: awaiting-human when the turn ended on a HITL question call.
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

    // 7. persist the whole aggregate (history is append-only). The journal
    //    entry (#463) commits in the same save as the transcript it describes,
    //    stamped with the INDEX of the user message this turn appended
    //    (startLen — runTurn appends the prompt first): the display read pairs
    //    entry↔message by that stated fact, so an un-journaled turn anywhere
    //    in the history can never shift another turn's pairing.
    if (input.journal) {
      conv.turns = [...(conv.turns ?? []), { ...input.journal, messageIndex: startLen, createdAt: new Date() }];
    }
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
