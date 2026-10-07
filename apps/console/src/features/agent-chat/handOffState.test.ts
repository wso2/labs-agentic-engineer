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

import { afterEach, describe, expect, it, vi } from "vitest";
import type { ProjectChat } from "./chatStore";
import { continueInIssues, getHandOff, setHandOff } from "./handOffState";

// What became of a hand-off the main chat announced, kept per browser, and
// the move New Issue makes: the request goes to the Issues chat once it has
// loaded, or into its composer when that chat cannot take it now.

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("a hand-off's state", () => {
  it("is pending until the user chooses, then what they chose, per project and call", () => {
    expect(getHandOff("acme", "c1")).toBe("pending");
    setHandOff("acme", "c1", "continued");
    setHandOff("acme", "c2", "stayed");
    expect(getHandOff("acme", "c1")).toBe("continued");
    expect(getHandOff("acme", "c2")).toBe("stayed");
    expect(getHandOff("other", "c1")).toBe("pending");
    expect(localStorage.getItem("aep:handoff:acme:c1")).toBe("continued");
  });

  it("reads anything else kept there as pending", () => {
    localStorage.setItem("aep:handoff:acme:c1", "maybe");
    expect(getHandOff("acme", "c1")).toBe("pending");
  });

  it("is pending, and the choice is not kept, where the browser keeps nothing", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    expect(() => setHandOff("acme", "c1", "stayed")).not.toThrow();
    expect(getHandOff("acme", "c1")).toBe("pending");
  });
});

/** An Issues chat that loads when opened, into `loaded`. */
function issuesChat(loaded: Partial<ProjectChat>, sendResult = true) {
  let state: ProjectChat = { status: "loading", error: null, items: [], turn: { phase: "idle" } };
  return {
    get: vi.fn(() => state),
    open: vi.fn(async () => {
      await Promise.resolve();
      state = { ...state, status: "ready", ...loaded };
    }),
    send: vi.fn(async () => sendResult),
  };
}

describe("continuing in Issues", () => {
  it("sends the request as the Issues chat's next message once it has loaded", async () => {
    const store = issuesChat({});
    const compose = vi.fn();
    expect(await continueInIssues("acme", "Save does nothing", { store, compose })).toBe("sent");
    expect(store.open).toHaveBeenCalledWith("acme");
    expect(store.send).toHaveBeenCalledWith("acme", "Save does nothing", { kind: "product" });
    expect(compose).not.toHaveBeenCalled();
  });

  it("puts the request in the Issues composer while a turn runs there, and sends nothing", async () => {
    const store = issuesChat({ turn: { phase: "running", turnId: "t9" } });
    const compose = vi.fn();
    expect(await continueInIssues("acme", "Save does nothing", { store, compose })).toBe("composed");
    expect(store.send).not.toHaveBeenCalled();
    expect(compose).toHaveBeenCalledWith("Save does nothing");
  });

  it("puts the request in the composer when the Issues chat could not load", async () => {
    const store = issuesChat({ status: "error", error: "Couldn't load the conversation" });
    const compose = vi.fn();
    expect(await continueInIssues("acme", "Save does nothing", { store, compose })).toBe("composed");
    expect(store.send).not.toHaveBeenCalled();
    expect(compose).toHaveBeenCalledWith("Save does nothing");
  });

  it("puts the request in the composer when the send was refused, so it is not lost", async () => {
    const store = issuesChat({}, false);
    const compose = vi.fn();
    expect(await continueInIssues("acme", "Save does nothing", { store, compose })).toBe("composed");
    expect(compose).toHaveBeenCalledWith("Save does nothing");
  });
});
