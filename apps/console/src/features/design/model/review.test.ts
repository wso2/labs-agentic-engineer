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

import { describe, expect, it } from "vitest";
import { checkDesignReview, DESIGN_REVIEW_PATH } from "@aep/agent-stream";
import {
  addDraft,
  answerComment,
  changedSinceChecked,
  commentQuestions,
  commentsOn,
  deleteDraft,
  editDraft,
  EMPTY_REVIEW,
  isAsking,
  quotedComment,
  splitQuote,
  isChecked,
  overviewNote,
  primaryAction,
  readReview,
  reopen,
  reviewText,
  sendWaiting,
  setChecked,
  unsend,
  waitingComments,
} from "./review";

const DATA = "specs/design/domain-model.md";
const PROTO = "specs/design/components/expense-web/prototype.json";
const H1 = "a".repeat(64);
const H2 = "b".repeat(64);
const priya = { id: "u-priya", name: "Priya" };
const draft = (id: string, text = "use cost centre, not department") => ({ id, author: priya, text, createdAt: "2026-10-10T09:00:00Z" });

/** Every review these functions make is one the save gate takes. */
function valid(review: Parameters<typeof reviewText>[0]) {
  expect(checkDesignReview(DESIGN_REVIEW_PATH, reviewText(review))).toBeNull();
  return review;
}

describe("reading review.json", () => {
  it("reads a missing or empty file as an empty review", () => {
    expect(readReview(undefined)).toEqual({ review: EMPTY_REVIEW, problem: null });
    expect(readReview("  \n")).toEqual({ review: EMPTY_REVIEW, problem: null });
  });

  it("reads a valid file back as written", () => {
    const review = addDraft(EMPTY_REVIEW, DATA, draft("c1"));
    expect(readReview(reviewText(review))).toEqual({ review, problem: null });
  });

  it("says why an invalid file cannot be read, so nothing is written over it", () => {
    expect(readReview("{").problem).toMatch(/not valid JSON/);
    expect(readReview(JSON.stringify({ version: 2, artifacts: [] })).problem).toMatch(/violates/);
  });

  it("writes two-space JSON with a final newline", () => {
    expect(reviewText(EMPTY_REVIEW)).toBe('{\n  "version": 1,\n  "artifacts": []\n}\n');
  });
});

describe("Checked", () => {
  it("ticks an artifact at its current hash, and shows it ticked only while the artifact still hashes to it", () => {
    const review = valid(setChecked(EMPTY_REVIEW, DATA, H1));
    expect(isChecked(review, DATA, H1)).toBe(true);
    expect(isChecked(review, DATA, H2)).toBe(false);
    expect(changedSinceChecked(review, DATA, H2)).toBe(true);
    expect(changedSinceChecked(review, DATA, H1)).toBe(false);
    expect(isChecked(review, DATA, undefined)).toBe(false);
  });

  it("unticks, dropping an entry left with nothing in it", () => {
    expect(setChecked(setChecked(EMPTY_REVIEW, DATA, H1), DATA, null)).toEqual(EMPTY_REVIEW);
  });

  it("keeps an artifact's comments when it is ticked and unticked", () => {
    const commented = addDraft(EMPTY_REVIEW, DATA, draft("c1"));
    const ticked = valid(setChecked(commented, DATA, H1));
    expect(commentsOn(ticked, DATA)).toHaveLength(1);
    expect(setChecked(ticked, DATA, null)).toEqual(commented);
  });
});

describe("comments", () => {
  it("adds drafts after an artifact's other comments", () => {
    const review = valid(addDraft(addDraft(EMPTY_REVIEW, DATA, draft("c1")), DATA, draft("c2", "two")));
    expect(commentsOn(review, DATA).map((c) => [c.id, c.status])).toEqual([
      ["c1", "draft"],
      ["c2", "draft"],
    ]);
  });

  it("keeps a prototype comment's place on the prototype", () => {
    const anchor = { screenId: "pending", roleId: "manager", stateId: "default", elementIds: ["row-1"] };
    const review = valid(addDraft(EMPTY_REVIEW, PROTO, { ...draft("c1"), anchor }));
    expect(commentsOn(review, PROTO)[0]?.anchor).toEqual(anchor);
  });

  it("edits and deletes drafts only", () => {
    const sent = sendWaiting(addDraft(EMPTY_REVIEW, DATA, draft("c1")), "2026-10-10T10:00:00Z").review;
    const both = addDraft(sent, DATA, draft("c2"));
    const edited = editDraft(editDraft(both, "c1", { text: "changed" }), "c2", { text: "changed" });
    expect(commentsOn(edited, DATA).map((c) => c.text)).toEqual(["use cost centre, not department", "changed"]);
    const deleted = deleteDraft(deleteDraft(both, "c1"), "c2");
    expect(commentsOn(deleted, DATA).map((c) => c.id)).toEqual(["c1"]);
  });

  it("drops an artifact's entry once its last draft is deleted", () => {
    expect(deleteDraft(addDraft(EMPTY_REVIEW, DATA, draft("c1")), "c1")).toEqual(EMPTY_REVIEW);
  });

  it("sends every draft and reopened comment, stamped, in one review", () => {
    let review = addDraft(addDraft(EMPTY_REVIEW, DATA, draft("c1")), PROTO, draft("c2"));
    review = {
      version: 1,
      artifacts: [
        ...review.artifacts,
        {
          path: "specs/design/decisions.md",
          comments: [{ ...draft("c3"), status: "addressed", reply: "done", revision: 2 }],
        },
      ],
    };
    review = reopen(review, "c3");
    expect(waitingComments(review).map((c) => c.id)).toEqual(["c1", "c2", "c3"]);
    const { review: after, sent } = sendWaiting(review, "2026-10-10T10:00:00Z");
    valid(after);
    expect(waitingComments(after)).toEqual([]);
    expect(sent).toEqual([
      { id: "c1", was: "draft" },
      { id: "c2", was: "draft" },
      { id: "c3", was: "reopened" },
    ]);
    expect(commentsOn(after, DATA)[0]).toMatchObject({ status: "sent", sentAt: "2026-10-10T10:00:00Z" });
    expect(unsend(after, sent)).toEqual(review);
  });

  it("reopens only an addressed comment", () => {
    const review = addDraft(EMPTY_REVIEW, DATA, draft("c1"));
    expect(reopen(review, "c1")).toEqual(review);
  });
});

describe("the agent's questions on comments", () => {
  const sent = () => sendWaiting(addDraft(EMPTY_REVIEW, DATA, draft("c1", "the total")), "2026-10-10T10:00:00Z").review;
  const ask = (review: ReturnType<typeof sent>) =>
    ({ version: 1, artifacts: review.artifacts.map((a) => ({ ...a, comments: a.comments?.map((c) => ({ ...c, question: "Before or after tax?" })) })) }) as typeof review;

  it("lists a sent comment the agent asked about, until it is answered", () => {
    const asked = valid(ask(sent()));
    expect(commentQuestions(asked)).toEqual([{ path: DATA, comment: expect.objectContaining({ id: "c1", question: "Before or after tax?" }) }]);
    expect(commentQuestions(sent())).toEqual([]);
    expect(isAsking({ status: "sent", question: "Which?" })).toBe(true);
    expect(isAsking({ status: "addressed", question: "Which?", answer: "This." })).toBe(false);
  });

  it("reopens the comment with the answer, so the next Address comments sends it", () => {
    const answered = valid(answerComment(ask(sent()), "c1", "  After tax.  "));
    expect(commentsOn(answered, DATA)[0]).toMatchObject({ status: "reopened", question: "Before or after tax?", answer: "After tax." });
    expect(commentQuestions(answered)).toEqual([]);
    expect(waitingComments(answered).map((c) => c.id)).toEqual(["c1"]);
    expect(commentsOn(sendWaiting(answered, "2026-10-10T11:00:00Z").review, DATA)[0]).toMatchObject({ status: "sent", answer: "After tax." });
  });

  it("changes an answer not yet sent, and takes no blank answer or one to a comment never asked about", () => {
    const answered = answerComment(ask(sent()), "c1", "After tax.");
    expect(commentsOn(answerComment(answered, "c1", "Before tax."), DATA)[0]?.answer).toBe("Before tax.");
    expect(answerComment(ask(sent()), "c1", "   ")).toEqual(ask(sent()));
    expect(commentsOn(answerComment(sent(), "c1", "After tax."), DATA)[0]).not.toHaveProperty("answer");
  });
});

describe("the one primary", () => {
  const rows = [
    { key: "prototype-expense-web", checked: true },
    { key: "roles", checked: false },
    { key: "architecture", checked: false },
  ];
  const idle = { questions: 0, waiting: 0, toDesign: false };

  it("is Answer while the agent waits on the user, before anything else", () => {
    expect(primaryAction({ ...idle, questions: 1, waiting: 2, toDesign: true, rows })).toEqual({ kind: "answer", count: 1 });
  });

  it("is Address comments while any comment waits, even with files unchecked", () => {
    expect(primaryAction({ ...idle, waiting: 2, toDesign: true, rows })).toEqual({ kind: "address", count: 2 });
  });

  it("is Design while the spec is ahead of the design (an answered feature question)", () => {
    expect(primaryAction({ ...idle, toDesign: true, rows })).toEqual({ kind: "design" });
  });

  it("is Open on the first unchecked row otherwise", () => {
    expect(primaryAction({ ...idle, rows })).toEqual({ kind: "open", key: "roles" });
  });

  it("is Build once every checkable file is checked and nothing waits", () => {
    expect(primaryAction({ ...idle, rows: rows.map((r) => ({ ...r, checked: true })) })).toEqual({ kind: "build" });
  });

  it("is nothing while nothing is listed", () => {
    expect(primaryAction({ ...idle, rows: [] })).toBeNull();
  });
});

describe("the Overview's note beside Build", () => {
  const text = (t: string) => ({ kind: "text", text: t });

  it("says everything is checked only while Build can start", () => {
    expect(overviewNote({ waiting: 0, primary: { kind: "build" }, hold: null })).toEqual([text("Everything is checked.")]);
  });

  it("names the running build, with a link, in place of everything is checked", () => {
    expect(overviewNote({ waiting: 0, primary: { kind: "build" }, hold: { kind: "running", version: "v2" } })).toEqual([{ kind: "building", version: "v2" }]);
  });

  it("names each held feature with the picker's reasons", () => {
    const hold = { kind: "held", features: [{ name: "Payroll export", reasons: ["waiting on Xero", "blocked by a question"] }, { name: "Approvals", reasons: ["design out of date"] }] } as const;
    expect(overviewNote({ waiting: 0, primary: { kind: "open", key: "roles" }, hold })).toEqual([
      text("Not ready to build yet — Payroll export: waiting on Xero, blocked by a question; Approvals: design out of date."),
    ]);
  });

  it("says when there is nothing new to build", () => {
    expect(overviewNote({ waiting: 0, primary: { kind: "build" }, hold: { kind: "nothing-new" } })).toEqual([
      text("Nothing new to build: what is designed is built and unchanged."),
    ]);
  });

  it("keeps what the primary asks, then why Build is held", () => {
    expect(overviewNote({ waiting: 2, primary: { kind: "address", count: 2 }, hold: { kind: "running", version: "v2" } })).toEqual([
      text("Comments go to the agent only from here, all together."),
      { kind: "building", version: "v2" },
    ]);
    expect(overviewNote({ waiting: 0, primary: { kind: "design" }, hold: null })).toEqual([text("Update the design first: the spec has changed since.")]);
  });

  it("asks to wait while the agent works, whatever the review says", () => {
    expect(overviewNote({ waiting: 0, primary: { kind: "build" }, hold: { kind: "agent-working" } })).toEqual([text("Wait for the agent to finish.")]);
    expect(overviewNote({ waiting: 0, primary: { kind: "design" }, hold: { kind: "agent-working" } })).toEqual([
      text("Update the design first: the spec has changed since."),
      text("Wait for the agent to finish."),
    ]);
  });

  it("says nothing of Build while the build offer loads", () => {
    expect(overviewNote({ waiting: 0, primary: { kind: "build" }, hold: { kind: "loading" } })).toEqual([]);
  });
});

describe("a comment about one line", () => {
  it("opens with the line quoted, and reads back as the quote and the words", () => {
    const text = quotedComment("[data-model] Amounts are kept in cents. (F1)", "Keep them as decimals.");
    expect(text).toBe("> [data-model] Amounts are kept in cents. (F1)\n\nKeep them as decimals.");
    expect(splitQuote(text)).toEqual({ quote: "[data-model] Amounts are kept in cents. (F1)", words: "Keep them as decimals." });
  });

  it("reads a comment that quotes nothing as words alone", () => {
    expect(splitQuote("Why cents?")).toEqual({ quote: null, words: "Why cents?" });
    expect(splitQuote("> not a quote without the blank line\nWhy?")).toEqual({ quote: null, words: "> not a quote without the blank line\nWhy?" });
  });
});
