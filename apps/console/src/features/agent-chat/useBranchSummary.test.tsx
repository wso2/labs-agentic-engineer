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

// @vitest-environment jsdom

import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { StreamPart } from "@aep/agent-stream";
import type { ShellScope } from "../shell/scope";
import { createChatStore, type ChatApi } from "./chatStore";

// When the user leaves the Issues page after talking there, the main chat sums
// the visit up: "From Issues · N messages · <the agent's last line>" with a
// Reopen. A second visit rewords that card instead of stacking another.

const PROJECT = "acme";

function sse(parts: StreamPart[]): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder();
  return new ReadableStream<Uint8Array>({
    start(c) {
      for (const p of parts) c.enqueue(encoder.encode(`data: ${JSON.stringify(p)}\n\n`));
      c.enqueue(encoder.encode("data: [DONE]\n\n"));
      c.close();
    },
  });
}

function fakeStore(history: unknown[]) {
  const streams = new Map<string, ReadableStream<Uint8Array>>();
  let next = 0;
  const api = {
    conversationId: async () => "conv-1",
    history: async () => history,
    activeTurn: async () => null,
    startTurn: async () => `t${++next}`,
    turn: async () => null,
    openStream: async (_p: string, turnId: string) => streams.get(turnId) ?? sse([{ type: "turn-committed" }]),
  } as unknown as ChatApi;
  const store = createChatStore({ api, pollDelay: () => 60_000 });
  /** The user says something and the agent answers it. */
  const talk = async (said: string, reply: string) => {
    streams.set(`t${next + 1}`, sse([{ type: "text-delta", delta: reply }, { type: "turn-committed" }]));
    await store.send(PROJECT, said, { kind: "product" });
    await vi.waitFor(() => expect(store.get(PROJECT).turn).toEqual({ phase: "idle" }));
  };
  return { store, talk };
}

let main: ReturnType<typeof fakeStore>;
let issues: ReturnType<typeof fakeStore>;
vi.mock("./useProjectChat", () => ({
  get chatStore() {
    return main.store;
  },
  chatStoreFor: (view: string) => (view === "issues" ? issues.store : main.store),
}));

const { useBranchSummary } = await import("./useBranchSummary");

const onIssues: ShellScope = { kind: "project", projectName: PROJECT, page: "issues", card: null, specFile: null };
const onOverview: ShellScope = { kind: "project", projectName: PROJECT, page: "overview", card: null, specFile: null };
const onQuestions: ShellScope = { kind: "project", projectName: PROJECT, page: "issues", card: "questions", specFile: null };
const onIssueCard: ShellScope = { kind: "project", projectName: PROJECT, page: "issues", card: "issue", specFile: null };

const notes = (store: ReturnType<typeof createChatStore>) => store.get(PROJECT).items.filter((i) => i.kind === "note");

beforeEach(async () => {
  main = fakeStore([]);
  issues = fakeStore([
    { role: "user", content: "An older report" },
    { role: "assistant", content: [{ type: "text", text: "Older reply." }] },
  ]);
  await main.store.open(PROJECT);
  await issues.store.open(PROJECT);
});
afterEach(cleanup);

/** Mount on a scope, and move to the next ones. */
function visit(...scopes: ShellScope[]) {
  const hook = renderHook(({ scope }) => useBranchSummary(scope), { initialProps: { scope: scopes[0]! } });
  return {
    go: (scope: ShellScope) => hook.rerender({ scope }),
    unmount: hook.unmount,
  };
}

describe("useBranchSummary", () => {
  it("posts one From Issues note when the user leaves Issues after talking there", async () => {
    const view = visit(onIssues);
    await issues.talk("Save does nothing", "Drafted it.\nShall I file it?");
    view.go(onOverview);
    expect(notes(main.store)).toEqual([
      {
        kind: "note",
        id: expect.any(String),
        text: "From Issues · 2 messages · Drafted it. Shall I file it?",
        actions: [{ kind: "open-issues", label: "Reopen" }],
      },
    ]);
  });

  it("says nothing when nothing was said during the visit, and nothing on a reload", async () => {
    const view = visit(onIssues);
    view.go(onOverview);
    expect(notes(main.store)).toEqual([]);
    cleanup();
    visit(onOverview);
    expect(notes(main.store)).toEqual([]);
  });

  it("stays in Issues for its Questions card, and leaves it for an issue's card, which is the main chat's", async () => {
    const view = visit(onIssues);
    await issues.talk("One", "Two.");
    view.go(onQuestions);
    expect(notes(main.store)).toEqual([]);
    view.go(onIssueCard);
    expect(notes(main.store)).toHaveLength(1);
  });

  it("cuts a long last line to 140 characters", async () => {
    const view = visit(onIssues);
    await issues.talk("Go", "x".repeat(300));
    view.go(onOverview);
    const note = notes(main.store)[0];
    expect(note?.kind === "note" && note.text).toBe(`From Issues · 2 messages · ${"x".repeat(139)}…`);
  });

  it("rewords the previous note on the next visit, adding the messages, instead of stacking another", async () => {
    const view = visit(onIssues);
    await issues.talk("First", "Reply one.");
    view.go(onOverview);
    view.go(onIssues);
    await issues.talk("Second", "Reply two.");
    view.go(onOverview);
    expect(notes(main.store)).toEqual([
      expect.objectContaining({ text: "From Issues · 4 messages · Reply two.", actions: [{ kind: "open-issues", label: "Reopen" }] }),
    ]);
  });

  it("posts a new note when something else came after the last one", async () => {
    const view = visit(onIssues);
    await issues.talk("First", "Reply one.");
    view.go(onOverview);
    await main.talk("Meanwhile", "Sure.");
    view.go(onIssues);
    await issues.talk("Second", "Reply two.");
    view.go(onOverview);
    expect(notes(main.store).map((n) => n.kind === "note" && n.text)).toEqual([
      "From Issues · 2 messages · Reply one.",
      "From Issues · 2 messages · Reply two.",
    ]);
  });
});
