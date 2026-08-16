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

// Collab question cards spike (2026-07-23): the shared `questions` map on the
// project's Yjs room — how agent questions surface on the MAIN spec panel for
// every participant to see and co-author. Written by useRoomQuestion (which
// mirrors this client's chat log into the doc) and by the form's answer/skip
// actions; observed by the form for live updates.

import type { Doc, Map as YMap } from "yjs";
import type { AskQuestionInput, QuestionAnswer } from "@aep/agent-stream";

const QUESTIONS_MAP = "questions" as const;
const ANSWERS_MAP = "questionAnswers" as const;

/** One collaboratively-answered question card, stored as a plain value in the
 *  Yjs `questions` map (keyed by toolCallId). Last-write-wins per entry — a
 *  re-mirror of the same question is the same value, so nothing is at stake.
 *  The co-edited `answers` are NOT part of it; see `answersMap`. */
export interface RoomQuestion {
  toolCallId: string;
  questions: AskQuestionInput[];
  /**
   * Legacy (#430 D5 removed ownership): pre-#430 entries carry the id of the
   * user whose client mirrored them first. Never written any more and never
   * read — any project member submits or skips; kept in the type so old room
   * entries still parse.
   */
  ownerId?: string;
  /**
   * The shared draft answer, co-edited by the room; null until first touched.
   * RESOLVED at read time from the separate answers map — the value stored
   * under this field is legacy only (rooms written before the split), which
   * `mirrorQuestion` migrates on first touch and never writes again.
   */
  answers: QuestionAnswer[] | null;
  /** Set once the asker submits or skips — the form closes for the whole room. */
  submitted?: boolean;
  /**
   * True while the batch is still streaming (#270 latency): `questions` is the
   * prefix parsed so far and grows on re-mirror; the form renders and takes
   * selections but submit stays gated until the final mirror clears this.
   */
  streaming?: boolean;
  /**
   * Epoch ms of the first mirror. Gates ORPHAN closing (an entry with no
   * backing log message): another tab or device of the same user legitimately
   * lacks the asker's log, so an orphan may only close once it is old enough
   * that nobody is coming back for it. Absent on pre-stamp entries — treated
   * as ancient (closable), which is exactly the legacy-zombie case.
   */
  askedAt?: number;
}

// --- Shared `questions` map helpers ----------------------------------------

/** What is actually stored under a toolCallId — the MIRROR of the question. */
type StoredQuestion = Omit<RoomQuestion, "answers"> & {
  /** Legacy inline answers; read (and migrated) but never written. */
  answers?: QuestionAnswer[] | null;
};

function questionsMap(doc: Doc): YMap<StoredQuestion> {
  return doc.getMap<StoredQuestion>(QUESTIONS_MAP);
}

/**
 * The co-edited answers, in their OWN map keyed `<toolCallId>#<index>` — never
 * inside the mirrored question value (#485 live-testing round 3).
 *
 * A reload creates a fresh local Y.Doc and mirrors the question into it from
 * the chat log — which is available instantly (localStorage cache, then the
 * thread bootstrap) — while the provider is still handshaking. When the room's
 * persisted state finally syncs in, that write and the PRE-RELOAD session's
 * write target the same key concurrently, and Y.Map resolves such a tie by
 * clientID (random per doc). Half the time the server's copy wins and takes
 * everything in it with it — including answers the user had already given, so
 * the form could never satisfy its submit gate. Splitting them out removes the
 * tie: the incoming copy has nothing under an answer key, so the answer
 * survives. Per-INDEX keys go one further — two people answering different
 * questions at once no longer overwrite each other either.
 */
function answersMap(doc: Doc): YMap<QuestionAnswer> {
  return doc.getMap<QuestionAnswer>(ANSWERS_MAP);
}

function answerKey(toolCallId: string, index: number): string {
  return `${toolCallId}#${index}`;
}

/** The answers stored for a card, aligned to `count`; null when untouched. */
function readAnswers(doc: Doc, toolCallId: string, count: number): QuestionAnswer[] | null {
  const map = answersMap(doc);
  let touched = false;
  const out: QuestionAnswer[] = [];
  for (let i = 0; i < count; i++) {
    const stored = map.get(answerKey(toolCallId, i));
    if (stored) touched = true;
    out.push(stored ?? { selected: [] });
  }
  return touched ? out : null;
}

/** The stored mirror plus its resolved answers — what every reader sees. */
function resolve(doc: Doc, stored: StoredQuestion): RoomQuestion {
  const { answers: legacy, ...rest } = stored;
  return {
    ...rest,
    answers: readAnswers(doc, stored.toolCallId, stored.questions.length) ?? legacy ?? null,
  };
}

/**
 * Mirror a parsed question into the room's shared map (idempotent by
 * toolCallId — a re-fold overwrites the same key). No ownership claim (#430
 * D5): the chat thread is project-scoped, so every member's log carries the
 * question and any member may submit — there is no submit right to protect.
 */
export function mirrorQuestion(
  doc: Doc,
  entry: { toolCallId: string; questions: AskQuestionInput[]; streaming?: boolean },
): void {
  const map = questionsMap(doc);
  const existing = map.get(entry.toolCallId);
  // A room written before the answers split carries its draft inline. Move it
  // across on first touch so an interview open across the upgrade keeps what
  // the user already selected.
  if (existing?.answers) migrateLegacyAnswers(doc, entry.toolCallId, existing.answers);
  const next: StoredQuestion = {
    toolCallId: entry.toolCallId,
    questions: entry.questions,
    // A re-mirror flips streaming off by OMITTING it — never resurrect the
    // gate from a stale entry, and never leak `streaming: false` into the doc.
    ...(entry.streaming ? { streaming: true } : {}),
    ...(existing?.submitted ? { submitted: true } : {}),
    askedAt: existing?.askedAt ?? Date.now(),
  };
  // The log re-mirrors on every change; an identical write would still churn
  // the doc and wake every observer in the room for nothing.
  if (existing && !existing.answers && sameStoredQuestion(existing, next)) return;
  map.set(entry.toolCallId, next);
}

function sameStoredQuestion(a: StoredQuestion, b: StoredQuestion): boolean {
  return (
    a.submitted === b.submitted &&
    a.streaming === b.streaming &&
    a.askedAt === b.askedAt &&
    JSON.stringify(a.questions) === JSON.stringify(b.questions)
  );
}

function migrateLegacyAnswers(
  doc: Doc,
  toolCallId: string,
  legacy: QuestionAnswer[],
): void {
  const map = answersMap(doc);
  legacy.forEach((answer, i) => {
    if (!map.has(answerKey(toolCallId, i))) map.set(answerKey(toolCallId, i), answer);
  });
}

/**
 * Apply an answer edit against the LIVE entry rather than a React-render
 * snapshot. Load-bearing while a batch streams and in a shared room: two
 * edits landing between renders (fast successive clicks, or a teammate's
 * concurrent selection) would otherwise each compute from the same stale
 * answers and the later write would clobber the earlier one. `update` also
 * receives the live `questions`, so an edit is always aligned to the current
 * question count.
 *
 * Only the slots whose value actually CHANGED are written, so an edit to one
 * question never republishes (and so never clobbers) another's.
 */
export function updateRoomAnswer(
  doc: Doc,
  toolCallId: string,
  update: (live: RoomQuestion) => QuestionAnswer[],
): void {
  const stored = questionsMap(doc).get(toolCallId);
  if (!stored) return;
  const live = resolve(doc, stored);
  const next = update(live);
  const map = answersMap(doc);
  next.forEach((answer, i) => {
    const key = answerKey(toolCallId, i);
    if (JSON.stringify(map.get(key)) === JSON.stringify(answer)) return;
    map.set(key, answer);
  });
}

/**
 * Close a question for the WHOLE room — used both when the asker submits the
 * answers and when they skip the questions. The form disappears for everyone
 * and the spec body returns to the files.
 */
export function closeRoomQuestion(doc: Doc, toolCallId: string): void {
  const map = questionsMap(doc);
  const existing = map.get(toolCallId);
  if (!existing) return;
  // The answer slots stay beside their closed question: they are what was
  // submitted, they are bounded by the interview's own question count, and
  // deleting them would race a teammate's client still rendering the form.
  map.set(toolCallId, { ...existing, submitted: true });
}

/** How long an unbacked ("orphan") entry may live before it is closable. */
export const ORPHAN_QUESTION_TTL_MS = 24 * 60 * 60 * 1000;

/**
 * Close (for the whole room) entries the chat log no longer backs. Two
 * classes, deliberately different rules:
 *
 * - SUPERSEDED (the log HAS the message, but a later delivered user message —
 *   e.g. a composer reply, an equally valid answer path per ADR-0012 — made it
 *   unanswerable): close immediately. The log in hand is proof — and since the
 *   thread is project-scoped (#430), every member's log converges on the same
 *   proof, so any client may close on it.
 * - ORPHAN (no log message at all): close only past ORPHAN_QUESTION_TTL_MS
 *   (unstamped legacy entries count as ancient). A client that has not
 *   rehydrated yet legitimately lacks the message — an aggressive orphan rule
 *   would close LIVE questions out from under fresher clients. The TTL keeps a
 *   persistent collab room from resurrecting zombie forms forever without
 *   racing living sessions.
 */
export function closeStaleRoomQuestions(
  doc: Doc,
  supersededToolCallIds: ReadonlySet<string>,
  knownToolCallIds: ReadonlySet<string>,
  now: number = Date.now(),
): void {
  for (const entry of readRoomQuestions(doc)) {
    if (entry.submitted) continue;
    const superseded = supersededToolCallIds.has(entry.toolCallId);
    const orphanExpired =
      !knownToolCallIds.has(entry.toolCallId) &&
      (entry.askedAt === undefined || now - entry.askedAt > ORPHAN_QUESTION_TTL_MS);
    if (superseded || orphanExpired) closeRoomQuestion(doc, entry.toolCallId);
  }
}

/** Snapshot the room's questions in insertion order, answers resolved. */
export function readRoomQuestions(doc: Doc): RoomQuestion[] {
  return [...questionsMap(doc).values()].map((stored) => resolve(doc, stored));
}

/** Observe the room's questions AND their answers; returns an unsubscribe. */
export function observeRoomQuestions(doc: Doc, fn: () => void): () => void {
  const questions = questionsMap(doc);
  const answers = answersMap(doc);
  questions.observe(fn);
  answers.observe(fn);
  return () => {
    questions.unobserve(fn);
    answers.unobserve(fn);
  };
}
