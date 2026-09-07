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

import { afterEach, describe, expect, it } from "vitest";
import {
  clearPlan,
  parseDeclarePlan,
  peekPlan,
  planDeclared,
  planFileSettled,
  planFileStreamed,
  planFileWriting,
  planTurnEnded,
  rehydratePlanFromHistory,
} from "./planStore";

const KEY = "test-key";

/** The two halves of a successful write, as the fold delivers them. */
function settle(key: string, turnId: string, path: string): void {
  planFileStreamed(key, turnId, path);
  planFileSettled(key, turnId, path, true);
}
const CELL = "specs/design/design.cell";
const OVERVIEW = "specs/design/domain-model.md";
const PORTAL = "specs/design/components/portal/design.json";

afterEach(() => clearPlan(KEY));

describe("planDeclared — union, no removal", () => {
  it("appends in first-seen order and dedupes restated paths", () => {
    expect(planDeclared(KEY, "t1", [CELL, OVERVIEW])).toBe(2);
    // The second wave restates OVERVIEW — the union must ignore it, so a
    // full-plan restatement can never shrink or double the count.
    expect(planDeclared(KEY, "t1", [OVERVIEW, PORTAL])).toBe(1);
    expect(peekPlan(KEY)?.entries.map((e) => e.path)).toEqual([CELL, OVERVIEW, PORTAL]);
  });

  it("a new turn's declaration replaces the previous turn's plan", () => {
    planDeclared(KEY, "t1", [CELL]);
    planTurnEnded(KEY, "t1", "failed"); // leaves wreckage
    planDeclared(KEY, "t2", [OVERVIEW]);
    const plan = peekPlan(KEY);
    expect(plan?.turnId).toBe("t2");
    expect(plan?.wreckage).toBe(false);
    expect(plan?.entries.map((e) => e.path)).toEqual([OVERVIEW]);
  });
});

describe("a question mid-flight pauses the plan rather than ending it", () => {
  it("holds the plan for the answering turn, and that turn adopts it", () => {
    planDeclared(KEY, "t1", [CELL, OVERVIEW]);
    planFileWriting(KEY, "t1", CELL);
    settle(KEY, "t1", CELL);
    // The agent asks; the turn ends without finishing the work.
    planTurnEnded(KEY, "t1", "completed", true);
    const paused = peekPlan(KEY);
    expect(paused?.paused).toBe(true);
    expect(paused?.wreckage).toBe(false);
    expect(paused?.entries).toHaveLength(2);

    // The answer's turn carries the same work on — same entries, new turn.
    planFileWriting(KEY, "t2", OVERVIEW);
    const resumed = peekPlan(KEY);
    expect(resumed?.turnId).toBe("t2");
    expect(resumed?.entries.map((e) => e.status)).toEqual(["done", "writing"]);
    settle(KEY, "t2", OVERVIEW);
    planTurnEnded(KEY, "t2", "completed");
    expect(peekPlan(KEY)).toBe(null);
  });

  it("a turn that asks with nothing outstanding still dissolves", () => {
    planDeclared(KEY, "t1", [CELL]);
    planFileWriting(KEY, "t1", CELL);
    settle(KEY, "t1", CELL);
    planTurnEnded(KEY, "t1", "completed", true);
    expect(peekPlan(KEY)).toBe(null);
  });
});

describe("derived lifecycle", () => {
  it("planned → writing → done follows the mutation stream", () => {
    planDeclared(KEY, "t1", [CELL, OVERVIEW]);
    planFileWriting(KEY, "t1", CELL);
    expect(peekPlan(KEY)?.entries[0]?.status).toBe("writing");
    expect(peekPlan(KEY)?.writingPath).toBe(CELL);
    settle(KEY, "t1", CELL);
    expect(peekPlan(KEY)?.entries[0]?.status).toBe("done");
    expect(peekPlan(KEY)?.writingPath).toBe(null);
  });

  it("tracks the writing path for an UNdeclared file too — follow-the-write steers by it", () => {
    planFileWriting(KEY, "t1", CELL);
    expect(peekPlan(KEY)?.writingPath).toBe(CELL);
    expect(peekPlan(KEY)?.entries).toEqual([]);
  });

  it("a clean turn's plan dissolves — the files are simply there", () => {
    planDeclared(KEY, "t1", [CELL]);
    planFileWriting(KEY, "t1", CELL);
    settle(KEY, "t1", CELL);
    planTurnEnded(KEY, "t1", "completed");
    expect(peekPlan(KEY)).toBe(null);
  });

  it("a dead turn leaves wreckage: writing → error, planned stays a ghost", () => {
    planDeclared(KEY, "t1", [CELL, OVERVIEW, PORTAL]);
    planFileWriting(KEY, "t1", CELL);
    settle(KEY, "t1", CELL);
    planFileWriting(KEY, "t1", OVERVIEW);
    planTurnEnded(KEY, "t1", "failed");
    const plan = peekPlan(KEY);
    expect(plan?.wreckage).toBe(true);
    expect(plan?.turnActive).toBe(false);
    expect(plan?.entries.map((e) => e.status)).toEqual(["done", "error", "planned"]);
  });

  it("a failed turn whose plan all landed leaves nothing — no residue without loss", () => {
    planDeclared(KEY, "t1", [CELL]);
    planFileWriting(KEY, "t1", CELL);
    settle(KEY, "t1", CELL);
    planTurnEnded(KEY, "t1", "failed");
    expect(peekPlan(KEY)).toBe(null);
  });

  it("ignores a terminal for a turn the snapshot no longer belongs to", () => {
    planDeclared(KEY, "t2", [CELL]);
    planTurnEnded(KEY, "t1", "failed");
    expect(peekPlan(KEY)?.turnId).toBe("t2");
    expect(peekPlan(KEY)?.turnActive).toBe(true);
  });
});

describe("parseDeclarePlan", () => {
  it("accepts an object or its JSON string, drops junk entries", () => {
    expect(parseDeclarePlan({ paths: [CELL, "", 42, ` ${OVERVIEW} `] })).toEqual([
      CELL,
      OVERVIEW,
    ]);
    expect(parseDeclarePlan(JSON.stringify({ paths: [CELL] }))).toEqual([CELL]);
  });

  it("rejects shapes that are not a plan", () => {
    expect(parseDeclarePlan(null)).toBe(null);
    expect(parseDeclarePlan("not json")).toBe(null);
    expect(parseDeclarePlan({ files: [CELL] })).toBe(null);
  });
});

describe("rehydratePlanFromHistory", () => {
  const declareCall = (paths: string[]) => ({
    type: "tool-call",
    toolName: "declare_plan",
    input: { paths },
  });
  const addCall = (path: string) => ({
    type: "tool-call",
    toolName: "addFile",
    input: { path, content: "…" },
  });

  it("rebuilds wreckage from the last declaring turn's record", () => {
    rehydratePlanFromHistory(KEY, [
      { role: "user", content: "/design" },
      { role: "assistant", content: [declareCall([CELL, OVERVIEW]), addCall(CELL)] },
    ]);
    const plan = peekPlan(KEY);
    expect(plan?.wreckage).toBe(true);
    expect(plan?.entries.map((e) => e.status)).toEqual(["done", "planned"]);
  });

  // The shape a REAL transcript has: the AI SDK appends one assistant message
  // per step, so the declaration and the writes it predicted are never in the
  // same message. Read one message at a time, a clean turn rehydrates as
  // permanent wreckage over files that exist.
  it("accumulates across the steps of one turn, not within one message", () => {
    rehydratePlanFromHistory(KEY, [
      { role: "user", content: "/design" },
      { role: "assistant", content: [declareCall([CELL, OVERVIEW])] },
      { role: "assistant", content: [addCall(CELL)] },
      { role: "assistant", content: [addCall(OVERVIEW)] },
    ]);
    expect(peekPlan(KEY)).toBe(null);
  });

  // Both waves belong to one turn, so both count — reading only the message
  // that held the LAST declaration would lose wave one's paths entirely.
  it("unions every wave of a turn, not just the last message to declare", () => {
    rehydratePlanFromHistory(KEY, [
      { role: "user", content: "/design" },
      { role: "assistant", content: [declareCall([CELL])] },
      { role: "assistant", content: [addCall(CELL)] },
      { role: "assistant", content: [declareCall([OVERVIEW, PORTAL])] },
      { role: "assistant", content: [addCall(OVERVIEW)] },
    ]);
    const plan = peekPlan(KEY);
    expect(plan?.entries.map((e) => e.path)).toEqual([CELL, OVERVIEW, PORTAL]);
    expect(plan?.entries.map((e) => e.status)).toEqual(["done", "done", "planned"]);
  });

  // A turn boundary is a user message: an earlier turn's writes must not settle
  // a later turn's ghosts.
  it("does not let an earlier turn's writes settle the last turn's plan", () => {
    rehydratePlanFromHistory(KEY, [
      { role: "user", content: "/design" },
      { role: "assistant", content: [declareCall([CELL]), addCall(CELL)] },
      { role: "user", content: "/design again" },
      { role: "assistant", content: [declareCall([CELL, OVERVIEW])] },
    ]);
    const plan = peekPlan(KEY);
    expect(plan?.wreckage).toBe(true);
    expect(plan?.entries.map((e) => e.status)).toEqual(["planned", "planned"]);
  });

  // The reported defect (employees-submit-expense-wer): the agent asked a
  // question partway through the design, the user answered, and the writing
  // finished in the turn the answer opened. Treating that answer as a turn
  // boundary severed the declaration from the five files written after it and
  // left permanent ghosts over documents that exist.
  it("an answer continues the work — it does not start a new turn", () => {
    rehydratePlanFromHistory(KEY, [
      { role: "user", content: "/design" },
      { role: "assistant", content: [declareCall([CELL, OVERVIEW, PORTAL])] },
      { role: "assistant", content: [addCall(CELL)] },
      { role: "assistant", content: [{ type: "tool-call", toolName: "ask_question", input: {} }] },
      { role: "user", content: 'Answer to "Which provider?": Stripe' },
      { role: "assistant", content: [addCall(OVERVIEW)] },
      { role: "assistant", content: [addCall(PORTAL)] },
    ]);
    expect(peekPlan(KEY)).toBe(null);
  });

  // Reloading while the agent waits on an answer must not read as failure.
  it("outstanding work with a question still open is paused, not wreckage", () => {
    rehydratePlanFromHistory(KEY, [
      { role: "user", content: "/design" },
      { role: "assistant", content: [declareCall([CELL, OVERVIEW])] },
      { role: "assistant", content: [addCall(CELL)] },
      { role: "assistant", content: [{ type: "tool-call", toolName: "ask_question", input: {} }] },
    ]);
    const plan = peekPlan(KEY);
    expect(plan?.wreckage).toBe(false);
    expect(plan?.paused).toBe(true);
    expect(plan?.entries.map((e) => e.status)).toEqual(["done", "planned"]);
  });

  // Answering by TYPING is an equally valid path (ADR-0012), so the
  // continuation cannot be decided from the message text — this is the case
  // that killed the answer-marker version of this rule.
  it("a free-text answer continues the work just as a card answer does", () => {
    rehydratePlanFromHistory(KEY, [
      { role: "user", content: "/design" },
      { role: "assistant", content: [declareCall([CELL, OVERVIEW])] },
      { role: "assistant", content: [addCall(CELL)] },
      { role: "assistant", content: [{ type: "tool-call", toolName: "ask_question", input: {} }] },
      { role: "user", content: "just use Stripe" },
      { role: "assistant", content: [addCall(OVERVIEW)] },
    ]);
    expect(peekPlan(KEY)).toBe(null);
  });

  // However many rounds it takes — each question re-arms the pause, each reply
  // clears it.
  it("survives several question-and-answer rounds in one run", () => {
    const ask = { type: "tool-call", toolName: "ask_question", input: {} };
    rehydratePlanFromHistory(KEY, [
      { role: "user", content: "/design" },
      { role: "assistant", content: [declareCall([CELL, OVERVIEW, PORTAL])] },
      { role: "assistant", content: [addCall(CELL)] },
      { role: "assistant", content: [ask] },
      { role: "user", content: "postgres" },
      { role: "assistant", content: [addCall(OVERVIEW)] },
      { role: "assistant", content: [ask] },
      { role: "user", content: "one admin role" },
      { role: "assistant", content: [addCall(PORTAL)] },
    ]);
    expect(peekPlan(KEY)).toBe(null);
  });

  // A genuinely new instruction still starts new work.
  it("a fresh instruction with NO question outstanding starts a new turn", () => {
    rehydratePlanFromHistory(KEY, [
      { role: "user", content: "/design" },
      { role: "assistant", content: [declareCall([CELL, OVERVIEW])] },
      { role: "assistant", content: [addCall(CELL)] },
      { role: "user", content: "actually, add an audit log" },
      { role: "assistant", content: [addCall(OVERVIEW)] },
    ]);
    expect(peekPlan(KEY)?.wreckage).toBe(true);
  });

  // A deletion is not a write — the live fold ignores removeFile for the same
  // reason, and the two must agree on the same transcript.
  it("a removeFile never settles an entry", () => {
    rehydratePlanFromHistory(KEY, [
      { role: "user", content: "/design" },
      {
        role: "assistant",
        content: [
          declareCall([CELL]),
          { type: "tool-call", toolName: "removeFile", input: { path: CELL } },
        ],
      },
    ]);
    expect(peekPlan(KEY)?.entries[0]?.status).toBe("planned");
  });

  it("a completed plan projects to nothing — it dissolved", () => {
    planDeclared(KEY, "t1", [CELL]);
    planTurnEnded(KEY, "t1", "failed"); // stale residue a fresh read clears
    rehydratePlanFromHistory(KEY, [
      { role: "assistant", content: [declareCall([CELL]), addCall(CELL)] },
    ]);
    expect(peekPlan(KEY)).toBe(null);
  });

  it("never clobbers a live fold", () => {
    planDeclared(KEY, "t-live", [CELL, OVERVIEW]);
    rehydratePlanFromHistory(KEY, [
      { role: "assistant", content: [declareCall([PORTAL]), addCall(PORTAL)] },
    ]);
    expect(peekPlan(KEY)?.turnId).toBe("t-live");
  });
});

// CodeRabbit findings on this PR.
describe("a write is settled by its VERDICT, not by its call", () => {
  it("stays writing until the result lands, then ticks", () => {
    planDeclared(KEY, "t1", [CELL]);
    planFileWriting(KEY, "t1", CELL);
    planFileStreamed(KEY, "t1", CELL);
    // Body complete, bundle has not ruled — claiming success here would tick a
    // write the gates may still reject.
    expect(peekPlan(KEY)?.entries[0]?.status).toBe("writing");
    expect(peekPlan(KEY)?.writingPath).toBe(null);
    planFileSettled(KEY, "t1", CELL, true);
    expect(peekPlan(KEY)?.entries[0]?.status).toBe("done");
  });

  it("a REJECTED write is an error, not a tick", () => {
    planDeclared(KEY, "t1", [CELL, OVERVIEW]);
    planFileWriting(KEY, "t1", CELL);
    planFileStreamed(KEY, "t1", CELL);
    planFileSettled(KEY, "t1", CELL, false);
    expect(peekPlan(KEY)?.entries[0]?.status).toBe("error");
  });
});

describe("wreckage stands until a DECLARING turn replaces it", () => {
  it("an ordinary turn in between carries it along", () => {
    planDeclared(KEY, "t1", [CELL, OVERVIEW]);
    planFileWriting(KEY, "t1", CELL);
    settle(KEY, "t1", CELL);
    planTurnEnded(KEY, "t1", "failed");
    expect(peekPlan(KEY)?.wreckage).toBe(true);

    // A later turn writes something unrelated — the alarm must not clear just
    // because another turn happened.
    planFileWriting(KEY, "t2", "specs/requirements/prd.md");
    const carried = peekPlan(KEY);
    expect(carried?.wreckage).toBe(true);
    expect(carried?.entries.map((e) => e.path)).toEqual([CELL, OVERVIEW]);
  });

  it("writing out the remaining entry clears it", () => {
    planDeclared(KEY, "t1", [CELL, OVERVIEW]);
    planFileWriting(KEY, "t1", CELL);
    settle(KEY, "t1", CELL);
    planTurnEnded(KEY, "t1", "failed");
    planFileWriting(KEY, "t2", OVERVIEW);
    settle(KEY, "t2", OVERVIEW);
    expect(peekPlan(KEY)).toBe(null);
  });

  it("a new declaration replaces it rather than merging into it", () => {
    planDeclared(KEY, "t1", [CELL, OVERVIEW]);
    planTurnEnded(KEY, "t1", "failed");
    planDeclared(KEY, "t2", [PORTAL]);
    const plan = peekPlan(KEY);
    expect(plan?.wreckage).toBe(false);
    expect(plan?.entries.map((e) => e.path)).toEqual([PORTAL]);
  });
});

describe("rehydrate settles from the verdict", () => {
  const declareCall = (paths: string[]) => ({
    type: "tool-call",
    toolName: "declare_plan",
    input: { paths },
  });
  const result = (path: string, ok: boolean) => ({
    type: "tool-result",
    toolName: "addFile",
    output: { type: "json", value: { ok, op: "add", path, status: ok ? "applied" : "rejected" } },
  });

  it("a rejected write rehydrates as an error, not a tick", () => {
    rehydratePlanFromHistory(KEY, [
      { role: "user", content: "/design" },
      { role: "assistant", content: [declareCall([CELL, OVERVIEW])] },
      { role: "tool", content: [result(CELL, true)] },
      { role: "tool", content: [result(OVERVIEW, false)] },
    ]);
    expect(peekPlan(KEY)?.entries.map((e) => e.status)).toEqual(["done", "error"]);
  });
});
