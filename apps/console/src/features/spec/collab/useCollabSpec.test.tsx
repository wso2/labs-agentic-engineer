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
import * as Y from "yjs";
import { markdownToFragment } from "@aep/collab-doc";
import { useCollabSpec } from "./useCollabSpec";

const PRD_PATH = "specs/requirements/prd.md";

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
describe("useCollabSpec — a REJECTION is terminal, an outage is not", () => {
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

// ADR-0020: the console reads the room, agents author it. `getXmlFragment`
// creates a fragment for any path it is asked for, so a read used to make its
// own answer true — which is what let an unseeded room paint a blank document
// over a PRD that exists in git, and what conjured phantom files into the rail.
describe("useCollabSpec — reading a document never creates one", () => {
  beforeEach(() => {
    instances.length = 0;
  });

  it("returns null for a path the room does not hold, and leaves the doc alone", () => {
    const { result } = renderCollab();
    act(() => instances[0]!.onSynced());
    const doc = instances[0]!.document;

    expect(result.current.getFileFragment(PRD_PATH)).toBeNull();
    expect(doc.share.has(PRD_PATH)).toBe(false);
    // The file list unions this with git; a path invented by a read would show
    // up in the rail as a real document that opens onto nothing.
    expect(result.current.docPaths).not.toContain(PRD_PATH);
  });

  it("returns the fragment once the room really holds the path", () => {
    const { result } = renderCollab();
    act(() => instances[0]!.onSynced());
    const doc = instances[0]!.document;
    // How an agent's file arrives: over sync, with its share entry present.
    act(() => {
      doc.getXmlFragment(PRD_PATH).insert(0, [new Y.XmlText("hello")]);
    });

    expect(result.current.getFileFragment(PRD_PATH)).not.toBeNull();
    expect(result.current.docPaths).toContain(PRD_PATH);
  });

  // An EMPTY document still opens for editing. `share.has` can only be trusted
  // as "the room holds this file" because an empty markdown document seeds as
  // one empty paragraph (`markdownToFragment`) rather than zero blocks — a
  // fragment with no children generates no update, so its key would never
  // replicate and the file would read as absent, leaving it permanently
  // read-only. Emptying a document is a supported action, so this is reachable
  // by clearing one and reopening it.
  it("opens an empty document the room holds", () => {
    const { result } = renderCollab();
    act(() => instances[0]!.onSynced());
    const doc = instances[0]!.document;
    // How an emptied file arrives over sync: present, with no text in it.
    act(() => {
      markdownToFragment("", doc.getXmlFragment(PRD_PATH));
    });
    expect(doc.getXmlFragment(PRD_PATH).length).toBe(1);

    expect(result.current.getFileFragment(PRD_PATH)).not.toBeNull();
    expect(result.current.docPaths).toContain(PRD_PATH);
  });
});

// #586. The collab server reaches its oracle, and reads the spec bundle, over
// the same `aep-api` that restarts on every deploy — and BOTH of those failures
// arrive here, because Hocuspocus runs the load hook inside the same try/catch
// as authentication and answers either with a permission-denied frame. Latching
// on those left the spec view offline until the page was reloaded, and the
// frame leaves the socket OPEN, so nothing below this retries on its own.
describe("useCollabSpec — an unreachable upstream retries instead of latching", () => {
  beforeEach(() => {
    instances.length = 0;
    vi.useFakeTimers();
  });
  afterEach(() => vi.useRealTimers());

  it("rebuilds after a refusal that never synced", async () => {
    const { result } = renderCollab();
    act(() =>
      instances[0]!.onAuthenticationFailed({ reason: "upstream-unavailable" }),
    );
    expect(result.current.status).toBe("offline");

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000);
    });
    expect(instances).toHaveLength(2);
  });

  it("backs off rather than retrying once a second while it keeps failing", async () => {
    // The room never syncs here, so the "has this session held long enough to
    // be healthy?" reset has no sync to measure from — it must not read a
    // never-synced room as healthy and flatten the ladder.
    renderCollab();
    const refuse = async (waitMs: number) => {
      const room = instances[instances.length - 1]!;
      act(() => room.onAuthenticationFailed({ reason: "upstream-unavailable" }));
      await act(async () => {
        await vi.advanceTimersByTimeAsync(waitMs);
      });
    };
    await refuse(1_000);
    expect(instances).toHaveLength(2);
    // Second failure is due at 2s, not 1s.
    await refuse(1_000);
    expect(instances).toHaveLength(2);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000);
    });
    expect(instances).toHaveLength(3);
  });

  // The same flattening, reached from the other side: a session that DID hold
  // long enough to count as healthy earns the next attempt a fast retry — but
  // only that one. The timestamp belongs to a provider being thrown away, so
  // leaving it set makes every refused replacement look like it just came off a
  // healthy session, and the ladder never climbs.
  it("spends a healthy session's fast-retry credit only once", async () => {
    renderCollab();
    act(() => instances[0]!.onSynced());
    // Hold the room well past the reset window, then lose the upstream.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(60_000);
    });

    const refuse = async (waitMs: number) => {
      const room = instances[instances.length - 1]!;
      act(() => room.onAuthenticationFailed({ reason: "upstream-unavailable" }));
      await act(async () => {
        await vi.advanceTimersByTimeAsync(waitMs);
      });
    };

    // The credit: this one retries fast.
    await refuse(1_000);
    expect(instances).toHaveLength(2);
    // The next refusal must climb the ladder, not reset it. Nothing has synced
    // since, so 1s is not enough.
    await refuse(1_000);
    expect(instances).toHaveLength(2);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000);
    });
    expect(instances).toHaveLength(3);
  });

  it("still latches when the bearer itself was refused", async () => {
    const { result } = renderCollab();
    act(() => instances[0]!.onAuthenticationFailed({ reason: "Forbidden" }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000);
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
