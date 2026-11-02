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

import {
  checkDesignReview,
  DESIGN_REVIEW_PATH,
  type DesignReview,
  type ReviewAnchor,
  type ReviewAuthor,
  type ReviewComment,
  type ReviewedArtifact,
} from "@aep/agent-stream";
import type { BuildHold } from "../../builds/model/picker";

// The design review as `specs/design/review.json` holds it (the contract is
// @aep/agent-stream's DesignReview): per artifact path, the user's comments
// and the hash the artifact had when they ticked it Checked. The Design card
// reads it live from the room and writes it back whole; everything it does to
// the file is a pure function here, so each is one write of the whole file.
//
// A comment WAITS while it is a draft or reopened: Address comments sends
// every waiting one (`sent`), and the agent marks each `addressed` with a
// reply. Only drafts are edited or deleted; only an addressed one reopens.
//
// A sent comment the agent found ambiguous carries its `question` and stays
// `sent`: it waits on the user, not on the agent. The user's `answer` reopens
// it, so it goes with the next Address comments and the agent acts on it with
// the answer (a `reopened` comment is the one state the batch already takes).

export const EMPTY_REVIEW: DesignReview = Object.freeze({ version: 1, artifacts: [] }) as DesignReview;

/** The review the file holds, or why it cannot be read (then nothing is written over it). */
export type ReadReview = { review: DesignReview; problem: null } | { review: DesignReview; problem: string };

/** The last text read, and what it read as: the card and every write read the same text again and again. */
let lastRead: { text: string; read: ReadReview } | null = null;

/** Read review.json's text; a missing file is an empty review, an invalid one says why. */
export function readReview(text: string | undefined): ReadReview {
  if (text === undefined || text.trim() === "") return { review: EMPTY_REVIEW, problem: null };
  if (lastRead?.text === text) return lastRead.read;
  const problem = checkDesignReview(DESIGN_REVIEW_PATH, text);
  const read: ReadReview = problem ? { review: EMPTY_REVIEW, problem: problem.message } : { review: JSON.parse(text) as DesignReview, problem: null };
  lastRead = { text, read };
  return read;
}

/** The file's text for a review, as it is written: two-space JSON and a final newline. */
export function reviewText(review: DesignReview): string {
  return `${JSON.stringify(review, null, 2)}\n`;
}

/** Whether a comment waits for the next Address comments. */
/**
 * A comment about one line of a file (a technical decision's Change…) opens
 * with that line as a markdown quote, then what the user wrote. The quote is
 * the comment's subject: it is shown apart, and kept as written when the
 * comment is edited.
 */
export function quotedComment(quote: string, words: string): string {
  return `${quote
    .split("\n")
    .map((l) => `> ${l}`)
    .join("\n")}\n\n${words}`;
}

/** A comment's quoted line and what was written under it; the quote is null when it quotes nothing. */
export function splitQuote(text: string): { quote: string | null; words: string } {
  const m = /^((?:> .*\n)+)\n([\s\S]*)$/.exec(text);
  if (!m) return { quote: null, words: text };
  const quote = m[1]!
    .trimEnd()
    .split("\n")
    .map((l) => l.slice(2))
    .join("\n");
  return { quote, words: m[2]! };
}

export function isWaiting(comment: Pick<ReviewComment, "status">): boolean {
  return comment.status === "draft" || comment.status === "reopened";
}

export function commentsOn(review: DesignReview, path: string): ReviewComment[] {
  return review.artifacts.find((a) => a.path === path)?.comments ?? [];
}

/** Every comment waiting to be sent, across the review. */
export function waitingComments(review: DesignReview): ReviewComment[] {
  return review.artifacts.flatMap((a) => (a.comments ?? []).filter(isWaiting));
}

/** Whether `path` is ticked Checked for the artifact as it is now (`hash`): a changed artifact is not. */
export function isChecked(review: DesignReview, path: string, hash: string | undefined): boolean {
  const checked = review.artifacts.find((a) => a.path === path)?.checkedHash;
  return checked !== undefined && hash !== undefined && checked === hash;
}

/** Ticked once, and the artifact changed since. */
export function changedSinceChecked(review: DesignReview, path: string, hash: string | undefined): boolean {
  const checked = review.artifacts.find((a) => a.path === path)?.checkedHash;
  return checked !== undefined && hash !== undefined && checked !== hash;
}

/** An entry with nothing in it is dropped, so the file holds only what someone did. */
function tidy(entry: ReviewedArtifact): ReviewedArtifact | null {
  const comments = entry.comments?.length ? entry.comments : undefined;
  if (entry.checkedHash === undefined && comments === undefined) return null;
  return { path: entry.path, ...(entry.checkedHash !== undefined ? { checkedHash: entry.checkedHash } : {}), ...(comments ? { comments } : {}) };
}

/** The review with `path`'s entry changed by `fn` (made when missing). */
function onArtifact(review: DesignReview, path: string, fn: (entry: ReviewedArtifact) => ReviewedArtifact): DesignReview {
  const found = review.artifacts.some((a) => a.path === path);
  const entries = found ? review.artifacts : [...review.artifacts, { path }];
  const artifacts = entries.flatMap((a) => (a.path === path ? (tidy(fn(a)) ?? []) : [a]));
  return { version: 1, artifacts };
}

/** The review with every comment passed through `fn` (one returning null deletes it). */
function onComments(review: DesignReview, fn: (comment: ReviewComment) => ReviewComment | null): DesignReview {
  const artifacts = review.artifacts.flatMap((a) => {
    if (!a.comments) return [a];
    const comments = a.comments.flatMap((c) => fn(c) ?? []);
    return tidy({ ...a, comments }) ?? [];
  });
  return { version: 1, artifacts };
}

/** Tick `path` Checked at its current hash, or untick it (null). */
export function setChecked(review: DesignReview, path: string, hash: string | null): DesignReview {
  return onArtifact(review, path, (entry) => ({
    path: entry.path,
    ...(hash !== null ? { checkedHash: hash } : {}),
    ...(entry.comments ? { comments: entry.comments } : {}),
  }));
}

export interface NewDraft {
  id: string;
  author: ReviewAuthor;
  text: string;
  createdAt: string;
  /** Prototype comments only: where on the prototype it was made. */
  anchor?: ReviewAnchor;
}

/** A new draft on `path`, after its other comments. */
export function addDraft(review: DesignReview, path: string, draft: NewDraft): DesignReview {
  const comment: ReviewComment = {
    id: draft.id,
    author: draft.author,
    text: draft.text,
    createdAt: draft.createdAt,
    status: "draft",
    ...(draft.anchor ? { anchor: draft.anchor } : {}),
  };
  return onArtifact(review, path, (entry) => ({ ...entry, comments: [...(entry.comments ?? []), comment] }));
}

/** A draft's new words (and, for a prototype comment, where it is); anything but a draft is left as it is. */
export function editDraft(review: DesignReview, id: string, change: { text?: string; anchor?: ReviewAnchor }): DesignReview {
  return onComments(review, (c) => (c.id === id && c.status === "draft" ? { ...c, ...change } : c));
}

/** The review without draft `id`; a sent comment is never deleted. */
export function deleteDraft(review: DesignReview, id: string): DesignReview {
  return onComments(review, (c) => (c.id === id && c.status === "draft" ? null : c));
}

/** An addressed comment back in the next batch. */
export function reopen(review: DesignReview, id: string): DesignReview {
  return onComments(review, (c) => (c.id === id && c.status === "addressed" ? { ...c, status: "reopened" } : c));
}

/** What a send changed: each comment sent, and the status it had, to put back if the turn does not start. */
export interface Sent {
  review: DesignReview;
  sent: { id: string; was: ReviewComment["status"] }[];
}

/** Whether the agent asked about a comment and waits for the user's answer. */
export function isAsking(comment: Pick<ReviewComment, "status" | "question" | "answer">): boolean {
  return comment.status === "sent" && comment.question !== undefined && comment.answer === undefined;
}

/** A comment the agent asked about, with the artifact it is on. */
export interface CommentQuestion {
  path: string;
  comment: ReviewComment & { question: string };
}

/** Every comment the agent asked about and the user has not answered, in the file's order. */
export function commentQuestions(review: DesignReview): CommentQuestion[] {
  return review.artifacts.flatMap((a) =>
    (a.comments ?? []).flatMap((c) => (isAsking(c) && c.question !== undefined ? [{ path: a.path, comment: { ...c, question: c.question } }] : [])),
  );
}

/**
 * The user's answer to the agent's question on comment `id`: the comment is
 * reopened, so it goes with the next Address comments. An answer not yet sent
 * can be changed the same way; anything else is left as it is.
 */
export function answerComment(review: DesignReview, id: string, answer: string): DesignReview {
  const words = answer.trim();
  if (!words) return review;
  return onComments(review, (c) => {
    if (c.id !== id || c.question === undefined) return c;
    const open = isAsking(c) || (c.status === "reopened" && c.answer !== undefined);
    return open ? { ...c, answer: words, status: "reopened" } : c;
  });
}

/** Every waiting comment marked sent at `at` (ISO 8601). */
export function sendWaiting(review: DesignReview, at: string): Sent {
  const sent: Sent["sent"] = [];
  const next = onComments(review, (c) => {
    if (!isWaiting(c)) return c;
    sent.push({ id: c.id, was: c.status });
    return { ...c, status: "sent", sentAt: at };
  });
  return { review: next, sent };
}

/** The comments a send marked, back as they were (the turn never started). */
export function unsend(review: DesignReview, sent: Sent["sent"]): DesignReview {
  return onComments(review, (c) => {
    const was = sent.find((s) => s.id === c.id);
    if (!was || c.status !== "sent") return c;
    const back: ReviewComment = { ...c, status: was.was };
    delete back.sentAt;
    return back;
  });
}

/** The one primary action on the Design card, first match wins. */
export type Primary =
  | { kind: "answer"; count: number }
  | { kind: "address"; count: number }
  | { kind: "design" }
  | { kind: "build" }
  | { kind: "open"; key: string }
  | null;

/**
 * The card's one primary: Answer the agent's questions while any waits on
 * the user, then Address comments while any comment waits, then Design while
 * the spec has features the design has not caught up with (an answered
 * feature question puts its feature there), then Build once every checkable
 * file is checked, otherwise Open on the first unchecked row (`rows`, in the
 * Overview's order). Null while nothing is listed.
 */
export function primaryAction(input: {
  questions: number;
  waiting: number;
  /** The spec has features to design or update. */
  toDesign: boolean;
  rows: readonly { key: string; checked: boolean }[];
}): Primary {
  if (input.questions > 0) return { kind: "answer", count: input.questions };
  if (input.waiting > 0) return { kind: "address", count: input.waiting };
  if (input.toDesign) return { kind: "design" };
  if (input.rows.length === 0) return null;
  const first = input.rows.find((r) => !r.checked);
  return first ? { kind: "open", key: first.key } : { kind: "build" };
}

/** The Overview's note beside Address comments and Build: a sentence, or the running build to link to. */
export type OverviewNotePart = { kind: "text"; text: string } | { kind: "building"; version: string };

/**
 * What the note says, in order: what the primary asks (comments to send, the
 * design to update), then why Build is held, else where Build stands against
 * the review. A Build held says why, never "Everything is checked." alone; a
 * Build still loading says nothing of it.
 */
export function overviewNote(input: { waiting: number; primary: Primary; hold: BuildHold | null }): OverviewNotePart[] {
  const { waiting, primary, hold } = input;
  const text = (t: string): OverviewNotePart => ({ kind: "text", text: t });
  const parts: OverviewNotePart[] = [];
  if (waiting > 0) parts.push(text("Comments go to the agent only from here, all together."));
  else if (primary?.kind === "design") parts.push(text("Update the design first: the spec has changed since."));
  if (hold?.kind === "running") return [...parts, { kind: "building", version: hold.version }];
  if (hold?.kind === "agent-working") return [...parts, text("Wait for the agent to finish.")];
  if (hold?.kind === "held") {
    const why = hold.features.map((f) => `${f.name}: ${f.reasons.join(", ")}`).join("; ");
    return [...parts, text(`Not ready to build yet — ${why}.`)];
  }
  if (hold?.kind === "nothing-new") return [...parts, text("Nothing new to build: what is designed is built and unchanged.")];
  if (parts.length > 0 || hold?.kind === "loading") return parts;
  return [text(primary?.kind === "build" ? "Everything is checked." : "Build once everything is checked; until then it builds with what is unchecked.")];
}
