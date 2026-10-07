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

// The project's Room on the org's AE Studio (ae-collab): joined at the URL
// AE Studio's `ready` answer names, held through a later `provisioning`,
// kept past its token's `exp` by Hocuspocus token sync, renewed once per room
// when the Room drops the bearer, and the commit's warnings kept for the
// spec workspace.

import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from "vitest";
import type * as Y from "yjs";
import type { components } from "../../../generated/aep-api";

interface FakeConfig {
  url: string;
  name: string;
  document: Y.Doc;
  onSynced: () => void;
  onStatus: (data: { status: string }) => void;
  onAuthenticationFailed: (data: { reason: string }) => void;
  onClose?: (data: { event: { code: number; reason: string } }) => void;
}

interface FakeRoom extends FakeConfig {
  destroyed: boolean;
  sendToken: Mock<() => void>;
  sendStateless: Mock<(payload: string) => void>;
  emitStateless: (message: unknown) => void;
}

const instances: FakeRoom[] = [];

vi.mock("@hocuspocus/provider", () => {
  class FakeProvider {
    room: FakeRoom;
    private statelessHandlers = new Set<(data: { payload: string }) => void>();
    constructor(config: FakeConfig) {
      this.room = {
        ...config,
        destroyed: false,
        sendToken: vi.fn(),
        sendStateless: vi.fn(),
        emitStateless: (message) => this.statelessHandlers.forEach((h) => h({ payload: JSON.stringify(message) })),
      };
      instances.push(this.room);
    }
    on(event: string, handler: (data: { payload: string }) => void) {
      if (event === "stateless") this.statelessHandlers.add(handler);
    }
    attach() {}
    disconnect() {}
    destroy() {
      this.room.destroyed = true;
    }
    sendToken() {
      this.room.sendToken();
    }
    sendStateless(payload: string) {
      this.room.sendStateless(payload);
    }
  }
  return { HocuspocusProvider: FakeProvider };
});

let onTokenRefresh: ((token: string) => void) | null = null;
vi.mock("../../../auth/token", () => ({
  getAccessToken: vi.fn(async () => "token"),
  renewAccessToken: vi.fn(async () => "renewed"),
  redirectToSignIn: vi.fn(),
  subscribeAccessTokenRefresh: vi.fn((fn: (token: string) => void) => {
    onTokenRefresh = fn;
    return () => {
      onTokenRefresh = null;
    };
  }),
}));

type AeStudio = components["schemas"]["AeStudio"];
let answer: AeStudio | undefined;
vi.mock("../../ae-studio/api/queries", () => ({
  useAeStudio: () => ({ data: answer, isPending: answer === undefined }),
}));

const token = await import("../../../auth/token");
const { onPodOutage } = await import("../../../api/aeStudio");
const { useSpecRoom } = await import("./specRoom");

const READY: AeStudio = {
  state: "ready",
  urls: { designAgent: "http://ae-design-agent.mock", collab: "ws://ae-collab.mock", tools: "http://ae-studio-tools.mock" },
};

// Rooms are module-level and shared, so each test opens its own project's.
let project = 0;
function openRoom() {
  const name = `shop${++project}`;
  const hook = renderHook(() => useSpecRoom("acme", name, true));
  return { ...hook, name };
}
const latest = () => instances.at(-1)!;

beforeEach(() => {
  vi.useFakeTimers();
  instances.length = 0;
  answer = READY;
  vi.mocked(token.renewAccessToken).mockResolvedValue("renewed");
  vi.mocked(token.redirectToSignIn).mockClear();
  vi.mocked(token.renewAccessToken).mockClear();
});
afterEach(async () => {
  cleanup();
  await act(() => vi.advanceTimersByTimeAsync(10_000)); // let every room linger out and close
  vi.useRealTimers();
});

describe("the Room's address", () => {
  it("is the ready answer's collab URL, for the project's room", () => {
    const { result, name } = openRoom();
    expect(instances).toHaveLength(1);
    expect(latest()).toMatchObject({ url: "ws://ae-collab.mock/v1/rooms", name: `spec-acme-${name}` });
    expect(result.current.status).toBe("connecting");
  });

  it("is not joined before AE Studio is ready, and is joined once it is", () => {
    answer = { state: "provisioning" };
    const { rerender, result } = openRoom();
    expect(instances).toHaveLength(0);
    answer = READY;
    rerender();
    expect(instances).toHaveLength(1);
    expect(result.current.status).toBe("connecting");
  });

  it("is held through a later provisioning, and left when AE Studio fails", () => {
    const { rerender, result } = openRoom();
    act(() => latest().onSynced());
    answer = { state: "provisioning" };
    rerender();
    expect(instances).toHaveLength(1);
    expect(latest().destroyed).toBe(false);
    expect(result.current.status).toBe("connected");
    answer = { state: "failed" };
    rerender();
    expect(latest().destroyed).toBe(true);
    expect(result.current.status).toBe("offline");
    expect(result.current.doc).toBeNull();
  });
});

describe("the Room's bearer", () => {
  it("follows every OIDC renewal through Hocuspocus token sync", () => {
    openRoom();
    act(() => latest().onSynced());
    act(() => onTokenRefresh?.("next"));
    expect(latest().sendToken).toHaveBeenCalledTimes(1);
    expect(latest().sendStateless).not.toHaveBeenCalled();
  });

  it.each(["token-expired", "permission-denied"])(
    "a close for %s renews the session once and rejoins; a second loss before the rejoin syncs stays offline",
    async (reason) => {
      const { result } = openRoom();
      act(() => latest().onSynced());
      await act(async () => latest().onClose?.({ event: { code: 4401, reason } }));
      expect(result.current.status).toBe("offline");
      expect(token.renewAccessToken).toHaveBeenCalledTimes(1);
      await act(() => vi.advanceTimersByTimeAsync(1_000));
      expect(instances).toHaveLength(2);
      await act(async () => latest().onClose?.({ event: { code: 4401, reason } }));
      await act(() => vi.advanceTimersByTimeAsync(60_000));
      expect(instances).toHaveLength(2);
      expect(token.renewAccessToken).toHaveBeenCalledTimes(1);
      expect(result.current.status).toBe("offline");
    },
  );

  it("a refused join renews once too, and sends the user to sign in when the session cannot be renewed", async () => {
    vi.mocked(token.renewAccessToken).mockResolvedValue(null);
    openRoom();
    await act(async () => latest().onAuthenticationFailed({ reason: "permission-denied" }));
    expect(token.renewAccessToken).toHaveBeenCalledTimes(1);
    expect(token.redirectToSignIn).toHaveBeenCalledTimes(1);
  });

  it("an upstream outage is retried, and re-reads AE Studio", async () => {
    const outages = vi.fn();
    const off = onPodOutage(outages);
    openRoom();
    act(() => latest().onAuthenticationFailed({ reason: "upstream-unavailable" }));
    off();
    expect(outages).toHaveBeenCalledTimes(1);
    expect(token.renewAccessToken).not.toHaveBeenCalled();
    await act(() => vi.advanceTimersByTimeAsync(1_000));
    expect(instances).toHaveLength(2);
  });
});

describe("a commit of the Room", () => {
  it("waits up to 50 s for the Room's answer", async () => {
    const { result } = openRoom();
    act(() => latest().onSynced());
    let settled: unknown = "pending";
    void result.current.flush().then(
      () => (settled = "done"),
      (e: unknown) => (settled = e),
    );
    await act(() => vi.advanceTimersByTimeAsync(45_000));
    expect(settled).toBe("pending");
    await act(() => vi.advanceTimersByTimeAsync(5_000));
    expect(settled).toBeInstanceOf(Error);
  });

  it("keeps the warnings of the last commit, replaced by the next, until dismissed", () => {
    const { result } = openRoom();
    act(() => latest().onSynced());
    act(() =>
      latest().emitStateless({ type: "flush-warnings", warnings: [{ path: "specs/a.md", message: "front matter dropped" }, { bad: 1 }] }),
    );
    expect(result.current.flushWarnings).toEqual([{ path: "specs/a.md", message: "front matter dropped" }]);
    act(() => latest().emitStateless({ type: "flush-warnings", warnings: [] }));
    expect(result.current.flushWarnings).toEqual([]);
    act(() => latest().emitStateless({ type: "flush-warnings", warnings: [{ path: "specs/b.md", message: "x" }] }));
    act(() => result.current.dismissFlushWarnings());
    expect(result.current.flushWarnings).toEqual([]);
  });
});
