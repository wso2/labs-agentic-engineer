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

import { EMPTY_FEEDBACK_QUEUE, onRevision, prototypeHash, type FeedbackQueue } from "@wso2/prototype-kit/feedback";
import type { TurnOutcome } from "../../agent-chat/chatStore";
import type { PrototypeFeedback } from "../../agent-chat/turnScope";
import { restored, sent } from "./feedback";
import type { AppPrototype, PrototypeFiles } from "./prototypes";

// A prototype's review across a revision (#889), kept beside the review
// rather than in it, since the review closes and opens again while the
// agent works. Send keeps the review open:
//
//   sent      the batch is held here until its turn ends; the queue starts
//             again (comments written meanwhile are held there; Send waits);
//   revising  the review keeps showing the revision it had, never the files
//             the agent is still writing;
//   landed    the turn ended and the prototype is ready: the review swaps the
//             new revision in and says how many comments it addressed; the
//             comments held meanwhile go on to it (the next batch names it),
//             each still marked with the revision it was written on;
//   failed    the turn failed, or what it left is invalid: the last good
//             revision keeps showing and the batch goes back in front of the
//             queue, with the reason. A turn that failed after writing valid
//             files leaves them showing: the batch goes back onto them (each
//             comment still marked with the revision it was written on).
//
// A turn's outcome comes from the chat store, which reports it as the turn
// goes idle, before the prototype stops revising here. A turn whose stream
// was lost reports none; the prototype's status then says how it went.

/** What a revision's end leaves the review to say; null once it is seen. */
export type RevisionNotice =
  | { kind: "updated"; addressed: number }
  | { kind: "failed"; reason: string }
  | null;

export interface ReviewSession {
  queue: FeedbackQueue;
  /** The batch sent, until its turn ends, and the outcome the chat reported for it. */
  sent: { feedback: PrototypeFeedback; outcome: TurnOutcome | null } | null;
  notice: RevisionNotice;
  /** The revision the review shows: the last ready one, kept while the agent revises it and when its revision is invalid. */
  shown: PrototypeFiles | null;
  revising: boolean;
}

export const NEW_SESSION: ReviewSession = { queue: EMPTY_FEEDBACK_QUEUE, sent: null, notice: null, shown: null, revising: false };


/** The session once its batch went to the agent: the queue starts again (drafts kept), and the last notice is done. */
export function sendStarted(s: ReviewSession, feedback: PrototypeFeedback): ReviewSession {
  return { ...s, queue: sent(s.queue), sent: { feedback, outcome: null }, notice: null };
}

/** The chat reported how the running turn ended. */
export function turnEnded(s: ReviewSession, outcome: TurnOutcome): ReviewSession {
  return s.sent ? { ...s, sent: { ...s.sent, outcome } } : s;
}

/** The session following the prototype as the room and the chat now have it (the same session when nothing changed). */
export function follow(s: ReviewSession, prototype: AppPrototype): ReviewSession {
  const revising = prototype.status === "revising";
  if (revising) {
    // Seen first mid-turn, the files as they are then are the best there is.
    return s.revising ? s : { ...s, revising, shown: s.shown ?? prototype.files };
  }
  const shown = prototype.status === "ready" ? prototype.files : prototype.status === "invalid" ? s.shown : null;
  // A turn that ended unseen (nothing followed the prototype while it ran) still settles once its outcome is known.
  const unseenEnd = s.sent !== null && s.sent.outcome !== null;
  if (!s.revising && !unseenEnd) return shown === s.shown ? s : { ...s, shown };
  return settled({ ...s, revising, shown }, prototype);
}

/** The turn that revised the prototype is over: landed, or failed with the batch given back. */
function settled(s: ReviewSession, prototype: AppPrototype): ReviewSession {
  const failure =
    s.sent?.outcome === "failed"
      ? "The prototype wasn't updated"
      : prototype.status === "invalid"
        ? `The updated prototype can't be shown (${prototype.problem ?? "it is invalid"})`
        : null;
  const shownHash = s.shown ? prototypeHash(s.shown.manifestText, s.shown.source) : null;
  if (failure === null) {
    const queue = shownHash !== null ? onRevision(s.queue, shownHash) : s.queue;
    return { ...s, queue, sent: null, notice: { kind: "updated", addressed: s.sent?.feedback.requests.length ?? 0 } };
  }
  // A failed turn may have written valid files before it stopped: they are showing, so the batch goes back onto them.
  const partway = s.sent !== null && shownHash !== null && shownHash !== s.sent.feedback.prototypeHash;
  const given = s.sent ? restored(s.queue, s.sent.feedback) : s.queue;
  const queue = partway && shownHash !== null ? onRevision(given, shownHash) : given;
  const seeing = partway
    ? "The agent stopped partway, so you're seeing the changes it made before it stopped."
    : `${failure}, so you're still seeing the previous version.`;
  const back = s.sent ? " Your comments are back in the queue: Retry sends them again." : "";
  return { ...s, queue, sent: null, notice: { kind: "failed", reason: `${seeing}${back}` } };
}
