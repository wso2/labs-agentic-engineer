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
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ShellScope } from "./scope";

// What the shell keeps for the chat panel: the compose request. It names the
// chat it is for: the one the caller names, else the one in focus.

const { useChatControls } = await import("./useChatControls");

const scope = (
  page: "overview" | "issues",
  card: "issue" | "questions" | null = null,
  projectName = "acme",
  issueNumber: number | null = card === "issue" ? 7 : null,
): ShellScope => ({
  kind: "project",
  projectName,
  page,
  card,
  specFile: null,
  issueNumber,
});

function mount(first: ShellScope) {
  const openChat = vi.fn();
  const hook = renderHook(({ s }) => useChatControls(s, openChat), { initialProps: { s: first } });
  return { hook, openChat, go: (s: ShellScope) => hook.rerender({ s }) };
}

afterEach(cleanup);

describe("useChatControls", () => {
  it("Create Issue (compose on the Issues page) puts the words in the Issues chat's composer, and opens the chat", () => {
    const { hook, openChat } = mount(scope("issues"));
    act(() => hook.result.current.controls.compose("/issue "));
    expect(hook.result.current.composeRequest).toEqual({ text: "/issue ", view: "issues", projectName: "acme", nonce: 1 });
    expect(openChat).toHaveBeenCalled();
  });

  it("on the main chat, a compose is for the main chat", () => {
    const { hook } = mount(scope("overview"));
    act(() => hook.result.current.controls.compose("Add approvals"));
    expect(hook.result.current.composeRequest).toMatchObject({ view: "main", projectName: "acme" });
  });

  it("on an issue's card, a compose is for that issue's chat", () => {
    const { hook } = mount(scope("issues", "issue"));
    act(() => hook.result.current.controls.compose("Close it"));
    expect(hook.result.current.composeRequest).toMatchObject({ view: "issue", projectName: "acme", issueNumber: 7 });
  });

  it("a caller moving the user names the chat, before the move has rendered", () => {
    const { hook, go } = mount(scope("overview"));
    act(() => hook.result.current.controls.compose("/issue x", { view: "issues", projectName: "acme" }));
    go(scope("issues"));
    expect(hook.result.current.composeRequest).toMatchObject({ view: "issues", projectName: "acme", text: "/issue x" });
  });

  it("is cleared by its own nonce only", () => {
    const { hook } = mount(scope("overview"));
    act(() => hook.result.current.controls.compose("one"));
    act(() => hook.result.current.controls.compose("two"));
    act(() => hook.result.current.clearComposeRequest(1));
    expect(hook.result.current.composeRequest?.text).toBe("two");
    act(() => hook.result.current.clearComposeRequest(2));
    expect(hook.result.current.composeRequest).toBeNull();
  });
});
