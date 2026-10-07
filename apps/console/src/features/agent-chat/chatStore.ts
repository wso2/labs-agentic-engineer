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

import type { QuestionAnswer, StreamPart } from "@aep/agent-stream";
import { START_COMMAND } from "@aep/contracts/commands";
import type { ConversationMessage } from "./api/conversation";
import { ConversationRotatedError, TurnInProgressError, type TurnStatus } from "./api/turns";
import {
  answerableQuestionId,
  appendAgentText,
  askedScope,
  dropQuestion,
  dropTurnOutput,
  historyItems,
  setAnswers,
  upsertActivity,
  upsertQuestion,
  type ChatItem,
  type NoteAction,
} from "./chatLog";
import { foldTurn, type TurnSink, type TurnStreamApi } from "./foldTurn";
import { serializeQuestionAnswer } from "./questionCards";
import { scopeOfBody, turnBody, type TurnBody, type TurnScope } from "./turnScope";

// The chat store: one conversation per project, one turn at a time.
//
// Each project's chat is a small state machine. It loads (the thread, its
// history, then any turn already running, which it attaches to: that is how a
// reload lands back inside a running turn), then it is ready, and its turn is
// idle, being started, or running. A send is refused unless the turn is idle,
// so the composer waits while one runs; the server's 409 `turn_in_progress`
// is the same rule, and a send it refuses attaches to the turn that is
// running instead. Every turn carries the scope it was sent with.
//
// Module-level rather than in a component: the chat panel closes and opens
// again while a turn runs, and a page (a stub's Start interview) sends
// without the panel mounted. The turn's stream is folded here regardless.
// The transport and the reattach logic follow the old console's agent-chat
// (useAgentChat.ts, runTurn.ts); the store itself is new, built for one
// shell-level chat per project rather than the old console's panel per page.

/** The turn's phase; a turn being started or running carries its instruction when known ("/interview F2"). */
export type TurnPhase =
  | { phase: "idle" }
  | { phase: "starting"; instruction?: string }
  | { phase: "running"; turnId: string; instruction?: string };

export interface ProjectChat {
  /** `loading` while the thread and its history are read; `error` when they could not be. */
  status: "loading" | "ready" | "error";
  error: string | null;
  items: ChatItem[];
  turn: TurnPhase;
}

/** What the store asks of the server. */
export interface ChatApi extends TurnStreamApi {
  conversationId: (projectName: string) => Promise<string>;
  history: (projectName: string, conversationId: string) => Promise<ConversationMessage[]>;
  startTurn: (projectName: string, conversationId: string, body: TurnBody) => Promise<string>;
  activeTurn: (projectName: string) => Promise<TurnStatus | null>;
}

export type TurnOutcome = "completed" | "failed";

export interface ChatStoreOptions {
  api: ChatApi;
  /** A file write the agent made: the local spec doc applies it while the room is not wired. */
  onAgentWrite?: (projectName: string, part: StreamPart) => void;
  /** How long to wait before asking again whether someone else's turn is running. */
  pollDelay?: (chat: ProjectChat, pollsSoFar: number) => number;
  /**
   * Run before a turn is started: commit the room's pending edits, so the
   * commit the turn records as its base is what its agent reads. A failure
   * here does not stop the turn.
   */
  beforeTurn?: (projectName: string) => Promise<void>;
}

/** A turn to show and fold: the running one the server named, or one just started here. */
interface TurnToAttach {
  turnId: string;
  /** The message that started it, for a turn this browser did not send. */
  instruction?: string;
  author?: string;
}

/**
 * How long until the next look for a running turn. An empty chat is exactly
 * where a platform-started turn (the kickoff) is about to appear, and where a
 * slow poll is indistinguishable from a broken product, so it looks often for
 * a while; otherwise, now and then. As in the old console's useAgentChat.
 */
export function foreignTurnPollDelay(chat: ProjectChat, pollsSoFar: number): number {
  return chat.items.length === 0 && pollsSoFar < 8 ? 2_000 : 12_000;
}

const INITIAL: ProjectChat = { status: "loading", error: null, items: [], turn: { phase: "idle" } };

interface Entry {
  state: ProjectChat;
  listeners: Set<() => void>;
  conversationId: string | null;
  loading: Promise<void> | null;
  /** A message to send once the conversation turns out empty (the held kickoff). */
  seed: string | null;
  /** File writes already applied, by turn and tool call: a replay must not apply one twice. */
  applied: Set<string>;
  watchers: number;
  pollTimer: ReturnType<typeof setTimeout> | null;
  polls: number;
  /**
   * The turns this browser started: the messages it sent, and the kickoff of
   * a project it just created (`claimKickoff`). Only their questions are
   * announced (`onQuestionsAsked`): a teammate's turn never takes over
   * this user's screen.
   */
  ownTurns: Set<string>;
  /** This browser created the project, so the kickoff, once seen, is its own. */
  kickoffClaimed: boolean;
  /** Question items already announced: a replay of the turn announces none again. */
  announced: Set<string>;
}

export function createChatStore(options: ChatStoreOptions) {
  const { api, onAgentWrite, beforeTurn } = options;
  const pollDelay = options.pollDelay ?? foreignTurnPollDelay;
  const entries = new Map<string, Entry>();
  const turnEndListeners = new Set<(projectName: string, outcome: TurnOutcome) => void>();
  const askedListeners = new Set<(projectName: string, itemId: string) => void>();
  let localIds = 0;
  const localId = (prefix: string) => `${prefix}${++localIds}`;

  function entry(projectName: string): Entry {
    let e = entries.get(projectName);
    if (!e) {
      e = {
        state: INITIAL,
        listeners: new Set(),
        conversationId: null,
        loading: null,
        seed: null,
        applied: new Set(),
        watchers: 0,
        pollTimer: null,
        polls: 0,
        ownTurns: new Set(),
        kickoffClaimed: false,
        announced: new Set(),
      };
      entries.set(projectName, e);
    }
    return e;
  }

  function update(projectName: string, change: (state: ProjectChat) => Partial<ProjectChat>): void {
    const e = entry(projectName);
    e.state = { ...e.state, ...change(e.state) };
    for (const fn of e.listeners) fn();
  }

  const setItems = (projectName: string, change: (items: ChatItem[]) => ChatItem[]) =>
    update(projectName, (s) => ({ items: change(s.items) }));

  /** Tell the listeners about each question an own turn asked, the first time it is seen. */
  function announceQuestions(projectName: string, turnId: string): void {
    const e = entry(projectName);
    if (!e.ownTurns.has(turnId)) return;
    for (const item of e.state.items) {
      if (item.kind !== "question" || item.turnId !== turnId || item.answers || e.announced.has(item.id)) continue;
      e.announced.add(item.id);
      for (const fn of askedListeners) fn(projectName, item.id);
    }
  }

  /** Count a turn as this browser's own, announcing any question it already asked. */
  function own(projectName: string, turnId: string): void {
    entry(projectName).ownTurns.add(turnId);
    announceQuestions(projectName, turnId);
  }

  /** The kickoff running now, when this browser claimed it: it becomes an own turn. */
  function takeClaimedKickoff(projectName: string): void {
    const e = entry(projectName);
    const { turn } = e.state;
    if (!e.kickoffClaimed || turn.phase !== "running" || !turn.instruction?.startsWith(START_COMMAND)) return;
    e.kickoffClaimed = false;
    own(projectName, turn.turnId);
  }

  function sinkFor(projectName: string, turnId: string, onEnded: (outcome: TurnOutcome) => void): TurnSink {
    const e = entry(projectName);
    return {
      text: (delta) => setItems(projectName, (items) => appendAgentText(items, turnId, delta)),
      activity: (activity) => setItems(projectName, (items) => upsertActivity(items, turnId, activity)),
      question: (question) => {
        setItems(projectName, (items) => upsertQuestion(items, turnId, question));
        announceQuestions(projectName, turnId);
      },
      withdrawQuestion: (toolCallId) => setItems(projectName, (items) => dropQuestion(items, turnId, toolCallId)),
      wrote: (part) => {
        const key = `${turnId}:${part.toolCallId ?? ""}`;
        if (e.applied.has(key)) return;
        e.applied.add(key);
        onAgentWrite?.(projectName, part);
      },
      error: (text) => setItems(projectName, (items) => [...items, { kind: "error", id: localId("e"), text }]),
      ended: onEnded,
    };
  }

  /** Show a running turn and fold its stream, from its start, to its end. */
  async function attach(projectName: string, turn: TurnToAttach): Promise<void> {
    // Idle (a turn found running) or starting (one just sent from here).
    if (entry(projectName).state.turn.phase === "running") return;
    const { turnId } = turn;
    update(projectName, (s) => {
      // A replay from the start re-adds the turn's output, so what an earlier
      // attach folded goes first.
      let items = dropTurnOutput(s.items, turnId);
      if (turn.instruction && !items.some((i) => i.kind === "user" && i.turnId === turnId)) {
        items = [
          ...items,
          {
            kind: "user",
            id: localId("u"),
            text: turn.instruction,
            state: "sent",
            turnId,
            ...(turn.author ? { author: turn.author } : {}),
          },
        ];
      }
      return {
        items,
        turn: { phase: "running", turnId, ...(turn.instruction ? { instruction: turn.instruction } : {}) },
      };
    });
    takeClaimedKickoff(projectName);
    let outcome: TurnOutcome | null = null;
    try {
      await foldTurn({
        api,
        projectName,
        turnId,
        signal: new AbortController().signal,
        sink: sinkFor(projectName, turnId, (o) => (outcome = o)),
      });
    } catch {
      setItems(projectName, (items) => [
        ...items,
        { kind: "error", id: localId("e"), text: "Lost the agent's stream. It picks up again when the chat reopens." },
      ]);
    }
    update(projectName, () => ({ turn: { phase: "idle" } }));
    if (outcome) for (const fn of turnEndListeners) fn(projectName, outcome);
    trySeed(projectName);
  }

  async function readHistory(projectName: string): Promise<void> {
    const e = entry(projectName);
    e.conversationId ??= await api.conversationId(projectName);
    const history = await api.history(projectName, e.conversationId);
    update(projectName, () => ({ items: historyItems(history) }));
  }

  /** The running turn, when it is this conversation's and this chat is not folding one already. */
  async function runningTurn(projectName: string): Promise<TurnStatus | null> {
    const active = await api.activeTurn(projectName);
    const e = entry(projectName);
    if (!active || active.status !== "running" || e.state.turn.phase !== "idle") return null;
    if (active.conversationId !== e.conversationId) {
      // A teammate started a new thread: follow it next time round.
      e.conversationId = null;
      return null;
    }
    return active;
  }

  function attachStatus(projectName: string, active: TurnStatus): void {
    void attach(projectName, {
      turnId: active.turnId,
      ...(active.instruction ? { instruction: active.instruction } : {}),
      ...(active.authorDisplayName ? { author: active.authorDisplayName } : {}),
    });
  }

  /** Read the thread, its history and any running turn, once. */
  function open(projectName: string): Promise<void> {
    const e = entry(projectName);
    if (e.state.status === "ready") return Promise.resolve();
    e.loading ??= (async () => {
      update(projectName, () => ({ status: "loading", error: null }));
      try {
        await readHistory(projectName);
        const active = await runningTurn(projectName);
        if (active) attachStatus(projectName, active);
        update(projectName, () => ({ status: "ready" }));
        trySeed(projectName);
      } catch (err) {
        update(projectName, () => ({
          status: "error",
          error: err instanceof Error ? err.message : "Couldn't load the conversation",
        }));
      } finally {
        e.loading = null;
      }
    })();
    return e.loading;
  }

  function schedulePoll(projectName: string): void {
    const e = entry(projectName);
    if (e.watchers === 0 || e.pollTimer) return;
    e.pollTimer = setTimeout(() => {
      e.pollTimer = null;
      void (async () => {
        try {
          if (e.state.status === "ready" && e.state.turn.phase === "idle") {
            // A thread left behind (a teammate started a new one): follow it.
            if (!e.conversationId) await readHistory(projectName);
            const active = await runningTurn(projectName);
            if (active) {
              // Someone else's turn (a teammate's, or the platform's kickoff):
              // what finished meanwhile first, then the turn itself.
              await readHistory(projectName);
              attachStatus(projectName, active);
            }
          }
        } catch {
          // A failed look is retried on the next one.
        } finally {
          e.polls += 1;
          schedulePoll(projectName);
        }
      })();
    }, pollDelay(e.state, e.polls));
  }

  /** Send the held message once the conversation is known to be empty; drop it once it is known not to be. */
  function trySeed(projectName: string): void {
    const e = entry(projectName);
    if (!e.seed || e.state.status !== "ready" || e.state.turn.phase !== "idle") return;
    const seed = e.seed;
    e.seed = null;
    if (e.state.items.length === 0) void send(projectName, seed, { kind: "product" });
  }

  /**
   * Start a turn in the project's current thread. A thread this chat lost
   * track of (a teammate started a new one) is resolved again first. When the
   * server says the thread was replaced (409 `conversation_rotated`: a
   * teammate's new thread, or the server's own once the context filled), the
   * chat follows the new thread, its history with the message being sent
   * kept after it, and the turn starts there: the message is not lost.
   */
  async function startInCurrentThread(projectName: string, rowId: string, body: TurnBody): Promise<string> {
    const e = entry(projectName);
    e.conversationId ??= await api.conversationId(projectName);
    try {
      return await api.startTurn(projectName, e.conversationId, body);
    } catch (err) {
      if (!(err instanceof ConversationRotatedError)) throw err;
      e.conversationId = await api.conversationId(projectName);
      const history = historyItems(await api.history(projectName, e.conversationId));
      update(projectName, (s) => ({ items: [...history, ...s.items.filter((i) => i.id === rowId)] }));
      return api.startTurn(projectName, e.conversationId, body);
    }
  }

  /**
   * Send a message as the next turn, with its scope. Resolves true once the
   * server accepted the turn (its stream folds in the background), false when
   * it was not sent: the chat is not ready, a turn is running, or the server
   * refused it (the chat then says why).
   */
  async function send(projectName: string, text: string, scope: TurnScope): Promise<boolean> {
    const e = entry(projectName);
    const instruction = text.trim();
    if (!instruction || e.state.status !== "ready" || e.state.turn.phase !== "idle") return false;
    const rowId = localId("u");
    const body = turnBody(instruction, scope);
    update(projectName, (s) => ({
      turn: { phase: "starting", instruction },
      items: [
        ...s.items,
        {
          kind: "user",
          id: rowId,
          text: instruction,
          state: "sending",
          scope: scopeOfBody(body),
          ...(scope.kind === "prototype" && scope.feedback ? { prototypeFeedback: scope.feedback } : {}),
        },
      ],
    }));
    let turnId: string;
    try {
      await beforeTurn?.(projectName).catch(() => undefined);
      turnId = await startInCurrentThread(projectName, rowId, body);
    } catch (err) {
      update(projectName, (s) => ({
        turn: { phase: "idle" },
        items: [
          ...s.items.map((i) => (i.id === rowId && i.kind === "user" ? { ...i, state: "failed" as const } : i)),
          { kind: "error", id: localId("e"), text: err instanceof Error ? err.message : "Couldn't reach the agent." },
        ],
      }));
      if (err instanceof TurnInProgressError && err.activeTurnId) {
        void attach(projectName, { turnId: err.activeTurnId });
      }
      return false;
    }
    setItems(projectName, (items) =>
      items.map((i) => (i.id === rowId && i.kind === "user" ? { ...i, state: "sent" as const, turnId } : i)),
    );
    own(projectName, turnId);
    void attach(projectName, { turnId, instruction });
    return true;
  }

  return {
    get: (projectName: string): ProjectChat => entry(projectName).state,

    subscribe(projectName: string, fn: () => void): () => void {
      const e = entry(projectName);
      e.listeners.add(fn);
      return () => e.listeners.delete(fn);
    },

    open,

    /** Keep a project's chat current while something shows it: load it, and look for others' turns. */
    watch(projectName: string): () => void {
      const e = entry(projectName);
      e.watchers += 1;
      void open(projectName);
      schedulePoll(projectName);
      return () => {
        e.watchers -= 1;
        if (e.watchers === 0 && e.pollTimer) {
          clearTimeout(e.pollTimer);
          e.pollTimer = null;
        }
      };
    },

    /** Read the conversation again after it failed to load. */
    retry(projectName: string): void {
      entry(projectName).conversationId = null;
      void open(projectName);
    },

    send,

    /** Post a line in the agent's voice about something started outside the chat: "v1 is building". */
    post(projectName: string, text: string, actions: NoteAction[] = []): void {
      setItems(projectName, (items) => [
        ...items,
        { kind: "note", id: localId("n"), text, ...(actions.length > 0 ? { actions } : {}) },
      ]);
    },

    /**
     * Answer the questions the conversation waits on: the item keeps the
     * answers and reads as answered, and they go to the agent as the next
     * turn, in the scope of the turn that asked them (`askedScope`), wherever
     * the user answered from. A send that fails leaves them answerable again.
     */
    async answer(projectName: string, itemId: string, answers: QuestionAnswer[]): Promise<boolean> {
      const { items } = entry(projectName).state;
      const card = items.find((i) => i.id === itemId);
      if (card?.kind !== "question" || answerableQuestionId(items) !== itemId) return false;
      const scope = askedScope(items, itemId);
      setItems(projectName, (items) => setAnswers(items, itemId, answers));
      const sent = await send(projectName, serializeQuestionAnswer(card.questions, answers), scope);
      if (!sent) setItems(projectName, (items) => setAnswers(items, itemId, null));
      return sent;
    },

    /**
     * Hold a message until the project's conversation is known, then send it
     * only if the conversation is empty: the kickoff the platform held for
     * documents that never came. A conversation that already started (the
     * kickoff ran after all) drops it.
     */
    seed(projectName: string, instruction: string): void {
      entry(projectName).seed = instruction;
      void open(projectName).then(() => trySeed(projectName));
    },

    /**
     * This browser just created the project: the platform's kickoff, running
     * now or once it is seen, is this browser's own turn, so its questions are
     * announced as if the user had sent it.
     */
    claimKickoff(projectName: string): void {
      entry(projectName).kickoffClaimed = true;
      takeClaimedKickoff(projectName);
    },

    /**
     * Be told when a turn this browser started asks questions, once per
     * question item, as the first of them lands; returns the unsubscribe.
     */
    onQuestionsAsked(fn: (projectName: string, itemId: string) => void): () => void {
      askedListeners.add(fn);
      return () => askedListeners.delete(fn);
    },

    /** Be told when any project's turn ends; returns the unsubscribe. */
    onTurnEnd(fn: (projectName: string, outcome: TurnOutcome) => void): () => void {
      turnEndListeners.add(fn);
      return () => turnEndListeners.delete(fn);
    },
  };
}
