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

// A doc must never outlive the connection that filled it. The collab server
// unloads a room on last-leave and reseeds a NEW Y.Doc from git HEAD on the
// next join; markdown files are top-level Y.XmlFragments with no key to
// converge on, so a reconnect carrying this doc's already-seeded fragments
// merges them with the fresh seed's independent items and the file comes back
// doubled. These tests pin the rejoin-from-scratch behavior that prevents it.

import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type * as Y from "yjs";
import { useCollabSpec } from "./useCollabSpec";

interface FakeConfig {
  document: Y.Doc;
  onSynced: () => void;
  onStatus: (data: { status: string }) => void;
  onAuthenticationFailed: (data: { reason: string }) => void;
}

/** A recorded room: the hook's callbacks plus what it did to the provider. */
interface FakeRoom extends FakeConfig {
  disconnectCalls: number;
}

const instances: FakeRoom[] = [];

vi.mock("@hocuspocus/provider", () => {
  class FakeProvider {
    document: Y.Doc;
    room: FakeRoom;
    awareness = { on: () => {}, off: () => {}, getStates: () => new Map() };
    constructor(config: FakeConfig) {
      this.document = config.document;
      this.room = { ...config, disconnectCalls: 0 };
      instances.push(this.room);
    }
    // The real provider's websocket re-arms its own reconnect on close; the
    // hook calls this to silence it before rebuilding.
    disconnect() {
      this.room.disconnectCalls += 1;
    }
    setAwarenessField() {}
    on() {}
    off() {}
    attach() {}
    destroy() {}
    sendStateless() {}
  }
  return { HocuspocusProvider: FakeProvider };
});

vi.mock("../../../auth/token", () => ({
  getAccessToken: vi.fn(async () => "token"),
  renewAccessToken: vi.fn(async () => "token"),
  subscribeAccessTokenRefresh: vi.fn(() => () => {}),
}));

const USER = { name: "Ada", email: "ada@example.com" };

function renderCollab() {
  return renderHook(() => useCollabSpec("proj1", USER, "acme"));
}

describe("useCollabSpec — rejoin from scratch after a post-sync drop", () => {
  beforeEach(() => {
    instances.length = 0;
    vi.useFakeTimers();
  });
  afterEach(() => vi.useRealTimers());

  it("drops the synced doc and joins with a fresh one", async () => {
    renderCollab();
    expect(instances).toHaveLength(1);
    const firstDoc = instances[0]!.document;

    // Server state has landed — this doc now carries seeded fragments.
    act(() => instances[0]!.onSynced());
    // Websocket drops. The server unloads the room and will reseed from HEAD.
    act(() => instances[0]!.onStatus({ status: "disconnected" }));

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000);
    });

    expect(instances).toHaveLength(2);
    expect(instances[1]!.document).not.toBe(firstDoc);
  });

  it("keeps the doc when the drop happens before the first sync", async () => {
    renderCollab();
    // Never synced: the doc is still empty, so it can carry nothing back into
    // a reseeded room. The provider's own retry handles this case.
    act(() => instances[0]!.onStatus({ status: "disconnected" }));

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });

    expect(instances).toHaveLength(1);
  });

  it("rebuilds once per drop, not once per disconnect event", async () => {
    renderCollab();
    act(() => instances[0]!.onSynced());
    act(() => {
      instances[0]!.onStatus({ status: "disconnected" });
      instances[0]!.onStatus({ status: "disconnected" });
      instances[0]!.onStatus({ status: "disconnected" });
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000);
    });

    expect(instances).toHaveLength(2);
  });

  it("does not resurrect the room after unmount", async () => {
    const { unmount } = renderCollab();
    act(() => instances[0]!.onSynced());
    act(() => instances[0]!.onStatus({ status: "disconnected" }));
    unmount();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });

    expect(instances).toHaveLength(1);
  });

  // The old provider's websocket re-arms a reconnect from its own `onClose`,
  // on the same 1s delay as the rebuild. If that socket reopens before React
  // commits the teardown it carries the seeded fragments back into a reseeded
  // room — the doubling this all exists to prevent. So the drop must silence
  // the old provider immediately, not merely outrace it.
  it("silences the old provider before the rebuild is even due", () => {
    renderCollab();
    act(() => instances[0]!.onSynced());
    act(() => instances[0]!.onStatus({ status: "disconnected" }));

    expect(instances[0]!.disconnectCalls).toBe(1);
    expect(instances).toHaveLength(1); // still only armed, not yet rebuilt
  });
});

// A rejected bearer is the one drop a rebuild must not answer: the fresh
// provider would present the same token and be rejected again. The server
// closes the socket right after rejecting, so the rejection and the close
// arrive in either order — both must end offline and stay there.
describe("useCollabSpec — an authentication failure is terminal", () => {
  beforeEach(() => {
    instances.length = 0;
    vi.useFakeTimers();
  });
  afterEach(() => vi.useRealTimers());

  it("cancels a rebuild the preceding drop already queued", async () => {
    const { result } = renderCollab();
    act(() => instances[0]!.onSynced());
    act(() => instances[0]!.onStatus({ status: "disconnected" }));
    act(() => instances[0]!.onAuthenticationFailed({ reason: "expired" }));

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000);
    });

    expect(instances).toHaveLength(1);
    expect(result.current.status).toBe("offline");
  });

  it("blocks a rebuild the following drop would have queued", async () => {
    const { result } = renderCollab();
    act(() => instances[0]!.onSynced());
    act(() => instances[0]!.onAuthenticationFailed({ reason: "expired" }));
    act(() => instances[0]!.onStatus({ status: "disconnected" }));

    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000);
    });

    expect(instances).toHaveLength(1);
    expect(result.current.status).toBe("offline");
  });
});

// A rebuild costs the server a room load and a git seed. A pod that
// accepts-then-drops must not be held at one of those per second, per tab.
describe("useCollabSpec — rebuilds back off while drops keep coming", () => {
  beforeEach(() => {
    instances.length = 0;
    vi.useFakeTimers();
  });
  afterEach(() => vi.useRealTimers());

  /** Sync the newest room, then drop it. */
  const syncThenDrop = () => {
    const room = instances[instances.length - 1]!;
    act(() => room.onSynced());
    act(() => room.onStatus({ status: "disconnected" }));
  };

  it("doubles the delay when a session drops again right after syncing", async () => {
    renderCollab();
    syncThenDrop();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000);
    });
    expect(instances).toHaveLength(2);

    // Second drop with no healthy session in between: 2s, not 1s.
    syncThenDrop();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000);
    });
    expect(instances).toHaveLength(2);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000);
    });
    expect(instances).toHaveLength(3);
  });

  it("starts over after a session that held", async () => {
    renderCollab();
    syncThenDrop();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000);
    });
    expect(instances).toHaveLength(2);

    // This one stays synced past the reset window, so it was healthy: the
    // next drop is back to the base delay instead of the doubled one.
    act(() => instances[1]!.onSynced());
    await act(async () => {
      await vi.advanceTimersByTimeAsync(31_000);
    });
    act(() => instances[1]!.onStatus({ status: "disconnected" }));

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000);
    });
    expect(instances).toHaveLength(3);
  });
});
