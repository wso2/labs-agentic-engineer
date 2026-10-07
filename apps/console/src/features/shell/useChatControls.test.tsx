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

import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ProjectChat } from "../agent-chat/chatStore";
import type { ShellScope } from "./scope";

// What the shell keeps for the chat panel: the compose request, and whether
// the Issues chat, a branch stacked on the main chat, is started and shown.
// It starts on Start, on New Issue, on Create Issue and when a turn runs in
// it; leaving the Issues page puts it away again.

let issuesChat: Record<string, ProjectChat> = {};
const listeners = new Map<string, Set<() => void>>();
const watched: string[] = [];
const issuesStore = {
  get: (projectName: string): ProjectChat =>
    issuesChat[projectName] ?? { status: "ready", error: null, items: [], turn: { phase: "idle" } },
  subscribe: (projectName: string, fn: () => void) => {
    const set = listeners.get(projectName) ?? new Set();
    set.add(fn);
    listeners.set(projectName, set);
    return () => set.delete(fn);
  },
  watch: (projectName: string) => {
    watched.push(projectName);
    return () => {};
  },
};
vi.mock("../agent-chat/useProjectChat", () => ({
  chatStoreFor: () => issuesStore,
}));

const { useChatControls } = await import("./useChatControls");

const scope = (page: "overview" | "issues", card: "issue" | "questions" | null = null, projectName = "acme"): ShellScope => ({
  kind: "project",
  projectName,
  page,
  card,
  specFile: null,
});

function mount(first: ShellScope) {
  const openChat = vi.fn();
  const hook = renderHook(({ s }) => useChatControls(s, openChat), { initialProps: { s: first } });
  return { hook, openChat, go: (s: ShellScope) => hook.rerender({ s }) };
}

const NOT_STARTED = { started: false, minimised: false };
const OPEN = { started: true, minimised: false };

beforeEach(() => {
  issuesChat = {};
  listeners.clear();
  watched.length = 0;
});
afterEach(cleanup);

describe("useChatControls", () => {
  it("on the Issues page, the branch is not started until asked", () => {
    const { hook } = mount(scope("issues"));
    expect(hook.result.current.branch).toEqual(NOT_STARTED);
  });

  it("starts the branch, minimises it, and opens it again", () => {
    const { hook } = mount(scope("issues"));
    act(() => hook.result.current.controls.startBranch("issues"));
    expect(hook.result.current.branch).toEqual(OPEN);
    act(() => hook.result.current.minimiseBranch());
    expect(hook.result.current.branch).toEqual({ started: true, minimised: true });
    act(() => hook.result.current.controls.startBranch("issues"));
    expect(hook.result.current.branch).toEqual(OPEN);
  });

  it("Create Issue (compose on the Issues page) starts the branch and puts the words in its composer", () => {
    const { hook, openChat } = mount(scope("issues"));
    act(() => hook.result.current.controls.compose("/issue "));
    expect(hook.result.current.branch).toEqual(OPEN);
    expect(hook.result.current.composeRequest).toEqual({ text: "/issue ", view: "issues", projectName: "acme", nonce: 1 });
    expect(openChat).toHaveBeenCalled();
  });

  it("a compose for the Issues chat opens a minimised branch", () => {
    const { hook } = mount(scope("issues"));
    act(() => hook.result.current.controls.startBranch("issues"));
    act(() => hook.result.current.minimiseBranch());
    act(() => hook.result.current.controls.compose("/issue x", { view: "issues", projectName: "acme" }));
    expect(hook.result.current.branch).toEqual(OPEN);
  });

  it("a compose for the main chat starts no branch", () => {
    const { hook } = mount(scope("overview"));
    act(() => hook.result.current.controls.compose("Add approvals"));
    expect(hook.result.current.composeRequest).toMatchObject({ view: "main", projectName: "acme" });
    hook.rerender({ s: scope("issues") });
    expect(hook.result.current.branch).toEqual(NOT_STARTED);
  });

  it("New Issue starts the branch of the project it names, before the move to Issues has rendered", () => {
    const { hook, go } = mount(scope("overview"));
    act(() => hook.result.current.controls.startBranch("issues", "acme"));
    go(scope("issues"));
    expect(hook.result.current.branch).toEqual(OPEN);
  });

  it("a turn running in the Issues chat starts it", () => {
    const { hook } = mount(scope("issues"));
    expect(watched).toContain("acme");
    issuesChat.acme = { status: "ready", error: null, items: [], turn: { phase: "running", turnId: "t1" } };
    act(() => listeners.get("acme")?.forEach((fn) => fn()));
    expect(hook.result.current.branch).toEqual(OPEN);
  });

  it("a running turn does not reopen a branch the user minimised", () => {
    const { hook } = mount(scope("issues"));
    act(() => hook.result.current.controls.startBranch("issues"));
    act(() => hook.result.current.minimiseBranch());
    issuesChat.acme = { status: "ready", error: null, items: [], turn: { phase: "running", turnId: "t1" } };
    act(() => listeners.get("acme")?.forEach((fn) => fn()));
    expect(hook.result.current.branch).toEqual({ started: true, minimised: true });
  });

  it("arriving while a turn runs there shows the branch at once", () => {
    issuesChat.acme = { status: "ready", error: null, items: [], turn: { phase: "running", turnId: "t1" } };
    const { hook } = mount(scope("issues"));
    expect(hook.result.current.branch).toEqual(OPEN);
  });

  it("leaving the Issues page resets it; its Questions card and an issue's card do not", () => {
    const { hook, go } = mount(scope("issues"));
    act(() => hook.result.current.controls.startBranch("issues"));
    go(scope("issues", "questions"));
    expect(hook.result.current.branch).toEqual(OPEN);
    go(scope("issues", "issue"));
    expect(hook.result.current.branch).toEqual(OPEN);
    go(scope("issues"));
    expect(hook.result.current.branch).toEqual(OPEN);
    go(scope("overview"));
    go(scope("issues"));
    expect(hook.result.current.branch).toEqual(NOT_STARTED);
  });

  it("keeps each project's branch apart", () => {
    const { hook, go } = mount(scope("issues"));
    act(() => hook.result.current.controls.startBranch("issues"));
    go(scope("issues", null, "beta"));
    expect(hook.result.current.branch).toEqual(NOT_STARTED);
  });
});
