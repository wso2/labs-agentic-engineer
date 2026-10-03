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
 * The turn start path (07 §1, §5): one place that turns a request into a
 * running turn, for the `/v1` edge and (Task 3.13) the Turn socket. Before
 * a turn is accepted it checks, in order: the pod is not shutting down, the
 * org has a model key, the instruction is usable, the project resolves (the
 * tools socket lookup, which also writes the snapshots), the snapshots and
 * attachments can be read, no turn runs on the scope, and the thread admits
 * the send (P-7: lookup → admit → desk.start, so a refused send takes no
 * lock). Every refusal is a `TurnStartError` the edge maps to a status.
 *
 * The accepted turn runs detached from the request, inside the TurnDesk: its
 * `run` joins the Room for a file-writing project turn, loads the MCP tools
 * when the turn's gates say so, and calls `runConversationTurn` with the
 * desk's abort signal. A failure the agent can name (`turn-error.ts`) ends
 * the turn `agent-error` with its code; anything else that throws ends it
 * `internal` (the desk's rule).
 */

import { randomUUID } from "node:crypto";
import type { FilePart, LanguageModel } from "ai";
import type { StreamPart, Surface, TurnAim, TurnAttachment, TurnSpec } from "@aep/agent-stream";
import type { RoomPeer } from "../collab/room-peer.js";
import type { SkillSource } from "../agents/main/skill-source.js";
import { AttachmentRefusedError, fitAttachments, fitReferences, type UnreadableReference } from "../conversation/attachments.js";
import {
  loadSkillsFromSnapshot,
  overlayReferenceTexts,
  readReferenceAttachments,
  readSnapshot,
  toAttachmentParts,
} from "../conversation/load-workspace.js";
import { runConversationTurn } from "../conversation/run-conversation-turn.js";
import { stepContextOf } from "../conversation/step-context.js";
import { codedErrorFrame, turnErrorFrame } from "../conversation/turn-error.js";
import type { ThreadBook } from "../conversations/thread-book.js";
import { composeInstruction, eagerSkillsFor, toolsetFor, wantsRegisterDraftTool } from "../prompts/turn.js";
import { connectionHost, type ModelConnection } from "../shared/model.js";
import { projectSnapshotDir, skillsSnapshotDir } from "../shared/snapshot-path.js";
import type { ConversationStore } from "../store/conversation-store.js";
import { ToolsSocketError, type ProjectSnapshot, type ToolsSocket } from "../tools-socket/client.js";
import { startTurnSummary, turnSpecFor } from "./start-spec.js";
import { TurnDesk, TurnInProgressError, type Scope, type TurnMeta, type TurnOutcome, type TurnRun } from "./turn-desk.js";
import { catalogTurn, designOrRoomTurn } from "./turn-spec.js";

/** The longest instruction a turn takes (aep-api's `createTurnMaxInstructionBytes`). */
export const MAX_INSTRUCTION_BYTES = 64 << 10;

/** A project name: a DNS label, as the platform names projects. */
const PROJECT_RE = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/;

/** Whether `name` can name a project at all (anything else is `project_unknown`). */
export function isProjectName(name: string): boolean {
  return PROJECT_RE.test(name);
}

/** Who a turn is credited to: the verified user (07 §1 "Credit"). */
export interface Credit {
  /** The JWT `sub`. */
  userId: string;
  name: string;
  email: string;
}

/** What a browser sent for one turn. */
export interface TurnInput {
  /** Verbatim; `/<skill>` commands are parsed here (`start-spec.ts`). */
  instruction: string;
  target?: string;
  aim?: TurnAim;
  /** Chat attachments, bytes base64 (`turn-input.ts` caps them). */
  attachments: TurnAttachment[];
}

/** What a turn's model is built for, beyond its connection. */
export interface TurnModelContext {
  /** The organization the turn runs for, named on its provider log lines. */
  orgId?: string;
  /** The conversation the turn runs in (a provider may route by session). */
  conversationId: string;
  /** Told when a model call waits out a short 429 (a `provider-wait` frame). */
  onProviderWait?: (host: string) => void;
}

export type BuildModel = (conn: ModelConnection, ctx: TurnModelContext) => LanguageModel;

/** Join the project's Room for one turn, as the credited user. */
export type JoinRoom = (project: string, credit: Credit) => Promise<RoomPeer>;

export interface TurnStarterDeps {
  desk: TurnDesk;
  threads: ThreadBook;
  store: ConversationStore;
  tools: ToolsSocket;
  /** `AE_SNAPSHOTS_DIR`: where the lookup's snapshots are read from. */
  snapshotsDir: string;
  /** The org's model connection (`connectionFromEnv`); `null` = no key (`no_default_key`). */
  connection: ModelConnection | null;
  buildModel: BuildModel;
  /**
   * Joins the Room for a file-writing project turn. Absent, a project turn
   * writes to a throwaway bundle over the snapshot (nothing persists).
   * The pod's adapter over `tools.roomToken` is Task 3.13's.
   */
  room?: JoinRoom;
  /** Who reads the turns' prose (`console` in the pod; absent in a local run). */
  surface?: Surface;
  /** The pod's org, named on provider log lines. */
  orgId?: string;
}

/** Why a turn was not started. `code` is the problem (or TurnConflict) code. */
export class TurnStartError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    /** With `turn_in_progress`: the running turn. */
    readonly activeTurnId?: string,
  ) {
    super(message);
    this.name = "TurnStartError";
  }
}

/** The files, skills and documents one turn reads, prepared before it is accepted. */
interface TurnMaterial {
  files: Record<string, string>;
  skillSource: SkillSource;
  references: FilePart[];
  unreadableReferences: UnreadableReference[];
  chatAttachments: FilePart[];
  baseRef: string;
  skillsRef: string;
}

/** Everything a run needs, settled before the turn is accepted. */
interface Launch {
  scope: Scope;
  conversationId: string;
  input: TurnInput;
  credit: Credit;
  spec: TurnSpec;
  flow: string;
  material: TurnMaterial;
  conn: ModelConnection;
  /** The project turn joins the Room (`undefined` for marketplace turns). */
  roomProject?: string;
  route: "project" | "marketplace";
}

export class TurnStarter {
  private refusing = false;

  constructor(private readonly deps: TurnStarterDeps) {}

  /**
   * Refuse every later start with `503 shutting_down` (07 §10). Flipped at
   * SIGTERM before `desk.abortAll` (Task 3.13), since the desk itself does not
   * stop a later start.
   */
  refuse(): void {
    this.refusing = true;
  }

  /** Start a browser turn in the project's current thread; resolves to its turn id. */
  async startProjectTurn(req: { project: string; conversationId: string; input: TurnInput; credit: Credit }): Promise<string> {
    const conn = this.admissible(req.input);
    const lookup = await this.lookup(req.project);
    const { spec, flow } = turnSpecFor(req.input.instruction, lookup);
    const material = await this.material(req.input, spec, () => ({
      snapshotDir: projectSnapshotDir(this.deps.snapshotsDir, req.project, lookup.headSha),
      skillsDir: skillsSnapshotDir(this.deps.snapshotsDir, lookup.skillsSha),
      baseRef: lookup.headSha,
      skillsRef: lookup.skillsSha,
    }), conn);
    const scope: Scope = { kind: "project", project: req.project };
    this.refuseBusy(scope);
    // A full or demoted thread is refused before anything holds the lock.
    if ((await this.deps.threads.admit(req.project, req.conversationId, conn.contextWindow)) === "rotated") {
      throw new TurnStartError(409, "conversation_rotated", "the conversation is no longer the project's current thread");
    }
    return this.launch({
      scope,
      conversationId: req.conversationId,
      input: req.input,
      credit: req.credit,
      spec,
      flow,
      material,
      conn,
      ...(this.deps.room ? { roomProject: req.project } : {}),
      route: "project",
    });
  }

  /** Start a turn in a marketplace conversation (no project, no Room); the caller checked ownership. */
  async startMarketplaceTurn(req: { conversationId: string; input: TurnInput; credit: Credit }): Promise<string> {
    const conn = this.admissible(req.input);
    const { skillsSha } = await this.toolsCall(() => this.deps.tools.skills());
    const { spec, flow } = turnSpecFor(req.input.instruction, { references: [] });
    // The marketplace reads only the Org skills snapshot: it is both the
    // turn's files and its skills (07 §6).
    const material = await this.material(req.input, spec, () => {
      const dir = skillsSnapshotDir(this.deps.snapshotsDir, skillsSha);
      return { snapshotDir: dir, skillsDir: dir, baseRef: skillsSha, skillsRef: skillsSha };
    }, conn);
    const scope: Scope = { kind: "marketplace", conversationId: req.conversationId };
    this.refuseBusy(scope);
    return this.launch({
      scope,
      conversationId: req.conversationId,
      input: req.input,
      credit: req.credit,
      spec,
      flow,
      material,
      conn,
      route: "marketplace",
    });
  }

  /** The checks that need nothing but the request: shutdown, key, instruction. */
  private admissible(input: TurnInput): ModelConnection {
    if (this.refusing) throw new TurnStartError(503, "shutting_down", "the design agent is shutting down");
    const conn = this.deps.connection;
    if (!conn) throw new TurnStartError(409, "no_default_key", "the organization has no default model key");
    if (input.instruction.trim() === "") throw new TurnStartError(400, "invalid_turn", "instruction is required");
    if (Buffer.byteLength(input.instruction) > MAX_INSTRUCTION_BYTES) {
      throw new TurnStartError(413, "payload_too_large", "instruction exceeds the size limit");
    }
    return conn;
  }

  private async lookup(project: string): Promise<ProjectSnapshot> {
    if (!isProjectName(project)) throw new TurnStartError(404, "project_unknown", "no such project");
    const found = await this.toolsCall(() => this.deps.tools.lookup(project));
    if (!found) throw new TurnStartError(404, "project_unknown", "no such project");
    return found;
  }

  /** A tools socket call; a failure is the sidecar's, not the request's. */
  private async toolsCall<T>(call: () => Promise<T>): Promise<T> {
    try {
      return await call();
    } catch (err) {
      if (err instanceof ToolsSocketError) {
        throw new TurnStartError(503, "tools_unavailable", "the studio's tools are not answering");
      }
      throw err;
    }
  }

  private refuseBusy(scope: Scope): void {
    const active = this.deps.desk.active(scope);
    if (active) throw new TurnStartError(409, "turn_in_progress", "a turn is already running", active.turnId);
  }

  /**
   * Read the snapshots and fit the documents to the connection. A chat
   * attachment the model cannot take is the user's to fix (400); a snapshot
   * that will not read after a good lookup is an infrastructure fault.
   */
  private async material(
    input: TurnInput,
    spec: TurnSpec,
    dirs: () => { snapshotDir: string; skillsDir: string; baseRef: string; skillsRef: string },
    conn: ModelConnection,
  ): Promise<TurnMaterial> {
    let read: { files: Record<string, string>; skillSource: SkillSource; references: FilePart[]; baseRef: string; skillsRef: string };
    try {
      const d = dirs();
      read = {
        files: readSnapshot(d.snapshotDir),
        skillSource: loadSkillsFromSnapshot(d.skillsDir),
        // Flows ground their artifacts in the reference documents; the
        // history dedupe keeps a re-named document from being stored twice.
        references: spec.kind === "start" || spec.kind === "flow" ? readReferenceAttachments(d.snapshotDir, spec.references) : [],
        baseRef: d.baseRef,
        skillsRef: d.skillsRef,
      };
    } catch (err) {
      throw new TurnStartError(500, "internal", err instanceof Error ? err.message : "snapshot read failed");
    }
    const references = await fitReferences(read.references, conn.capabilities);
    // The encoded budget is shared with the references (the ceiling belongs
    // to the model request), so their cost is passed in.
    const spent = references.parts.reduce((n, part) => n + (typeof part.data === "string" ? part.data.length : 0), 0);
    let chatAttachments: FilePart[];
    try {
      chatAttachments = await fitAttachments(toAttachmentParts(input.attachments, spent), conn.capabilities);
    } catch (err) {
      if (err instanceof AttachmentRefusedError) throw new TurnStartError(400, "attachment_rejected", err.message);
      throw err;
    }
    return {
      files: read.files,
      skillSource: read.skillSource,
      references: references.parts,
      unreadableReferences: references.unreadable,
      chatAttachments,
      baseRef: read.baseRef,
      skillsRef: read.skillsRef,
    };
  }

  /** Hand the turn to the desk; resolves to its id. */
  private launch(l: Launch): string {
    const { desk } = this.deps;
    const turnId = randomUUID();
    const last = desk.lastTerminal(l.scope);
    const summary = startTurnSummary(l.input.instruction, l.spec);
    const meta: TurnMeta = {
      conversationId: l.conversationId,
      kind: "browser",
      flow: l.flow,
      instruction: summary,
      author: { id: l.credit.userId, name: l.credit.name },
      model: l.conn.model,
      modelHost: connectionHost(l.conn),
      baseRef: l.material.baseRef,
      skillsRef: l.material.skillsRef,
    };
    const instruction = composeInstruction(l.spec, {
      target: l.input.target,
      // D20 from the pod's last terminal facts: the last turn failed, or it
      // ran on another snapshot than this one.
      previousTurnFailed: last?.status === "failed",
      ...(l.input.aim ? { aim: l.input.aim } : {}),
    });
    const filesChangedExternally = last !== null && last.baseRef !== l.material.baseRef;
    try {
      desk.start(l.scope, meta, this.run(l, { turnId, summary, instruction, filesChangedExternally }), turnId);
    } catch (err) {
      if (err instanceof TurnInProgressError) {
        throw new TurnStartError(409, "turn_in_progress", "a turn is already running", err.activeTurnId);
      }
      throw err;
    }
    return turnId;
  }

  private run(
    l: Launch,
    t: { turnId: string; summary: string; instruction: string; filesChangedExternally: boolean },
  ): TurnRun {
    const { deps } = this;
    const host = connectionHost(l.conn);
    const roomScoped = l.roomProject !== undefined;
    const gates = { flow: l.flow, roomScoped };
    const toolset = toolsetFor(l.spec);
    const eager = eagerSkillsFor(l.spec);
    const m = l.material;
    return async (emit, signal): Promise<TurnOutcome> => {
      let contextTokens: number | undefined;
      const onEvent = (part: StreamPart): void => {
        contextTokens = stepContextOf(part) ?? contextTokens;
        // A failure the agent can name streams as its coded frame, never the provider's raw error.
        emit(part.type === "error" ? (codedErrorFrame(part.error, host) ?? part) : part);
      };
      let peer: RoomPeer | undefined;
      try {
        let files = m.files;
        if (l.roomProject !== undefined && deps.room) {
          peer = await deps.room(l.roomProject, l.credit);
          // The Room holds no reference documents; their text rides in from
          // the snapshot, which is their authority.
          files = overlayReferenceTexts(peer.files(), m.files);
        }
        const model = deps.buildModel(l.conn, {
          ...(deps.orgId ? { orgId: deps.orgId } : {}),
          conversationId: l.conversationId,
          onProviderWait: (wait) => emit({ type: "provider-wait", host: wait } as StreamPart),
        });
        const refs = { baseRef: m.baseRef, skillsRef: m.skillsRef };
        try {
          const res = await runConversationTurn({
            id: l.conversationId,
            instruction: t.instruction,
            files,
            filesChangedExternally: t.filesChangedExternally,
            skillSource: m.skillSource,
            ...(m.references.length ? { referenceAttachments: m.references } : {}),
            ...(m.unreadableReferences.length ? { unreadableReferences: m.unreadableReferences } : {}),
            ...(m.chatAttachments.length ? { chatAttachments: m.chatAttachments } : {}),
            toolset,
            ...(wantsRegisterDraftTool(l.spec, l.route) ? { registerDraft: true } : {}),
            ...(catalogTurn(gates) ? { mcp: deps.tools } : {}),
            webSearch: designOrRoomTurn(gates),
            ...(eager.length ? { eagerSkills: eager } : {}),
            ...(deps.surface ? { surface: deps.surface } : {}),
            ...(peer ? { collabPeer: peer } : {}),
            journal: {
              turnId: t.turnId,
              text: t.summary,
              author: { id: l.credit.userId, displayName: l.credit.name },
              // The names that reached the model, not the ones that were sent.
              ...(m.chatAttachments.length ? { attachments: m.chatAttachments.flatMap((p) => (p.filename ? [p.filename] : [])) } : {}),
              ...(l.input.aim ? { anchor: l.input.aim.anchor } : {}),
            },
            model,
            connection: l.conn,
            store: deps.store,
            onEvent,
            abortSignal: signal,
          });
          return { status: "completed", usage: res.usage, ...(contextTokens !== undefined ? { contextTokens } : {}), ...refs };
        } catch (err) {
          if (signal.aborted) throw err; // the desk already ended the turn (cap or shutdown)
          const frame = turnErrorFrame(err, host);
          const coded = "code" in frame ? frame : undefined;
          return {
            status: "failed",
            reason: "agent-error",
            message: frame.error,
            ...(coded ? { code: coded.code } : {}),
            ...(coded && "host" in coded && coded.host ? { host: coded.host } : {}),
            ...(coded && "resetAt" in coded && coded.resetAt ? { resetAt: coded.resetAt } : {}),
            ...refs,
          };
        }
      } finally {
        // The agent never lingers in the Room past its turn (presence honesty).
        peer?.leave();
      }
    };
  }
}

/**
 * The desk's `onFinished`: the record goes to the usage outbox, and a project
 * turn's closing context size goes to its thread for auto-rotation.
 */
export function finishedTurnSink(
  threads: Pick<ThreadBook, "noteContextTokens">,
  outbox: { push(r: Parameters<TurnRecordSink>[0]): void },
): TurnRecordSink {
  return (record) => {
    outbox.push(record);
    if (record.project !== undefined && record.contextTokens !== undefined) {
      threads.noteContextTokens(record.project, record.conversationId, record.contextTokens);
    }
  };
}

type TurnRecordSink = ConstructorParameters<typeof TurnDesk>[0]["onFinished"];
