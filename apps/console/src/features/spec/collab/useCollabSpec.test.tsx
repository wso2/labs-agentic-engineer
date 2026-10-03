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

import type { ReactNode } from "react";
import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from "vitest";
import * as Y from "yjs";
import { markdownToFragment } from "@aep/collab-doc";
import type { components } from "../../../generated/aep-api";
import {
  redirectToSignIn,
  renewAccessToken,
  subscribeAccessTokenRefresh,
} from "../../../auth/token";
import { aeStudioKeys } from "../../ae-studio/api/queries";
import { useCollabSpec } from "./useCollabSpec";

const PRD_PATH = "specs/requirements/prd.md";

interface FakeConfig {
  url: string;
  name: string;
  document: Y.Doc;
  onSynced: () => void;
  onStatus: (data: { status: string }) => void;
  onAuthenticationFailed: (data: { reason: string }) => void;
  onClose: (data: { event: { code: number; reason: string } }) => void;
}

/** A recorded room: the hook's callbacks plus what it did to the provider. */
interface FakeRoom extends FakeConfig {
  disconnectCalls: number;
  sendToken: Mock<() => void>;
  sendStateless: Mock<(payload: string) => void>;
  /** Delivers a stateless message from the server, as the provider would. */
  emitStateless: (message: unknown) => void;
  /** The provider's awareness map, by client id; a test seeds remote peers here. */
  awarenessStates: Map<number, unknown>;
  /** Fires the awareness "change" listeners, as a remote peer joining or leaving would. */
  emitAwarenessChange: () => void;
}

/** The awareness state a peer publishes: the `user` field the hook reads. */
const peerState = (name: string, kind: "user" | "agent") => ({ user: { name, kind } });

const instances: FakeRoom[] = [];
/** Remote peers already in the room's awareness when the NEXT provider is built. */
let peersAtJoin: [number, unknown][] = [];

vi.mock("@hocuspocus/provider", () => {
  class FakeProvider {
    document: Y.Doc;
    room: FakeRoom;
    awarenessStates = new Map<number, unknown>();
    private awarenessHandlers = new Set<() => void>();
    awareness = {
      on: (_event: string, handler: () => void) => this.awarenessHandlers.add(handler),
      off: (_event: string, handler: () => void) => this.awarenessHandlers.delete(handler),
      getStates: () => this.awarenessStates,
    };
    private statelessHandlers = new Set<(data: { payload: string }) => void>();
    constructor(config: FakeConfig) {
      this.document = config.document;
      peersAtJoin.forEach(([id, state]) => this.awarenessStates.set(id, state));
      this.room = {
        ...config,
        disconnectCalls: 0,
        sendToken: vi.fn(),
        sendStateless: vi.fn(),
        awarenessStates: this.awarenessStates,
        emitAwarenessChange: () => this.awarenessHandlers.forEach((h) => h()),
        emitStateless: (message) =>
          this.statelessHandlers.forEach((h) => h({ payload: JSON.stringify(message) })),
      };
      instances.push(this.room);
    }
    // The real provider's websocket re-arms its own reconnect on close; the
    // hook calls this to silence it before rebuilding.
    disconnect() {
      this.room.disconnectCalls += 1;
    }
    setAwarenessField() {}
    on(event: string, handler: (data: { payload: string }) => void) {
      if (event === "stateless") this.statelessHandlers.add(handler);
    }
    off(event: string, handler: (data: { payload: string }) => void) {
      if (event === "stateless") this.statelessHandlers.delete(handler);
    }
    attach() {}
    destroy() {}
    sendToken() {
      this.room.sendToken();
    }
    sendStateless(payload: string) {
      this.room.sendStateless(payload);
    }
  }
  return { HocuspocusProvider: FakeProvider };
});

vi.mock("../../../auth/token", () => ({
  getAccessToken: vi.fn(async () => "token"),
  renewAccessToken: vi.fn(async () => "token"),
  subscribeAccessTokenRefresh: vi.fn(() => () => {}),
  redirectToSignIn: vi.fn(),
}));

type AeStudio = components["schemas"]["AeStudio"];

const READY: AeStudio = {
  state: "ready",
  urls: {
    designAgent: "http://ae-design-agent.mock",
    collab: "ws://ae-collab.mock",
    tools: "http://ae-studio-tools.mock",
  },
};

// The Room's URL comes from AE Studio's answer; each test sets the answer.
let studio: { data: AeStudio | undefined; isPending: boolean } = { data: READY, isPending: false };
vi.mock("../../ae-studio/api/queries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../ae-studio/api/queries")>()),
  useAeStudio: () => studio,
}));

const USER = { name: "Ada", email: "ada@example.com" };

/** The project the hook is rendered for; a test switches rooms by changing it and rerendering. */
let project = "proj1";

let queryClient: QueryClient;

/** Renders the hook against AE Studio's `answer`; "pending" is the first read in flight. */
function renderCollab(answer: AeStudio | "pending" = READY) {
  studio = answer === "pending" ? { data: undefined, isPending: true } : { data: answer, isPending: false };
  queryClient = new QueryClient();
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  return renderHook(() => useCollabSpec(project, USER, "acme"), { wrapper });
}

/** Moves fake time on, running whatever the hook armed. */
async function elapse(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

/** The token-refresh callback the hook subscribed with, called as OIDC would. */
function emitTokenRefresh(token: string) {
  const calls = vi.mocked(subscribeAccessTokenRefresh).mock.calls;
  const cb = calls[calls.length - 1]?.[0];
  if (!cb) throw new Error("the hook never subscribed to token refresh");
  cb(token);
}

beforeEach(() => {
  instances.length = 0;
  peersAtJoin = [];
  project = "proj1";
  vi.clearAllMocks();
  vi.mocked(renewAccessToken).mockResolvedValue("token");
});

// 10 §6: the Room lives in the org's pod, at the URL AE Studio hands out, and
// the session's token follows the user through the provider's own token sync.
describe("useCollabSpec — the Room on AE Studio's ae-collab", () => {
  it("connects to urls.collab/v1/rooms, sends the renewed token, and shows flush-warnings", () => {
    const { result } = renderCollab();
    expect(instances).toHaveLength(1);
    const room = instances[0]!;
    expect(room.url).toBe("ws://ae-collab.mock/v1/rooms");

    emitTokenRefresh("t2");
    expect(room.sendToken).toHaveBeenCalledTimes(1);
    expect(room.sendStateless).not.toHaveBeenCalledWith(
      expect.stringContaining('"type":"token"'),
    );

    act(() =>
      room.emitStateless({
        type: "flush-warnings",
        warnings: [{ path: "specs/a.md", message: "soft" }],
      }),
    );
    expect(result.current.flushWarnings).toEqual([{ path: "specs/a.md", message: "soft" }]);
    // Each save's set replaces the last; an empty one clears the Alert.
    act(() => room.emitStateless({ type: "flush-warnings", warnings: [] }));
    expect(result.current.flushWarnings).toEqual([]);
  });

  it("each save's warnings replace the last, and dismissing clears them", () => {
    const { result } = renderCollab();
    const room = instances[0]!;
    act(() =>
      room.emitStateless({
        type: "flush-warnings",
        warnings: [{ path: "specs/a.md", message: "one" }, { path: "specs/b.md", message: "two" }],
      }),
    );
    act(() =>
      room.emitStateless({
        type: "flush-warnings",
        warnings: [{ path: "specs/c.md", message: "three" }],
      }),
    );
    expect(result.current.flushWarnings).toEqual([{ path: "specs/c.md", message: "three" }]);
    act(() => result.current.dismissFlushWarnings());
    expect(result.current.flushWarnings).toEqual([]);
  });

  it("keeps only well-formed warnings from a message", () => {
    const { result } = renderCollab();
    act(() =>
      instances[0]!.emitStateless({
        type: "flush-warnings",
        warnings: [{ path: "specs/a.md", message: "ok" }, { path: 3 }, "junk", null],
      }),
    );
    expect(result.current.flushWarnings).toEqual([{ path: "specs/a.md", message: "ok" }]);
  });

  it("puts no token in the Room URL", () => {
    renderCollab();
    expect(instances[0]!.url).not.toContain("token");
  });

  it.each([
    ["the first AE Studio read is in flight", "pending" as const, "connecting"],
    ["AE Studio is provisioning", { state: "provisioning" } as AeStudio, "offline"],
    ["AE Studio failed", { state: "failed" } as AeStudio, "offline"],
  ])("builds no provider while %s", (_, answer, status) => {
    const { result } = renderCollab(answer);
    expect(instances).toHaveLength(0);
    expect(result.current.status).toBe(status);
    // The local doc still exists, so the view works solo.
    expect(result.current.doc).not.toBeNull();
  });

  it.each(["failed", "absent"] as const)(
    "joins once AE Studio turns ready, and leaves when it turns %s",
    (state) => {
      const { rerender, result } = renderCollab({ state: "provisioning" });
      expect(instances).toHaveLength(0);
      studio = { data: READY, isPending: false };
      rerender();
      expect(instances).toHaveLength(1);
      act(() => instances[0]!.onSynced());
      expect(result.current.status).toBe("connected");
      studio = { data: { state }, isPending: false };
      rerender();
      expect(result.current.status).toBe("offline");
      expect(result.current.provider).toBeNull();
      expect(instances).toHaveLength(1);
    },
  );

  it("answers no token-please: the server never asks, and nothing is sent", async () => {
    renderCollab();
    act(() => instances[0]!.emitStateless({ type: "token-please", id: "r1" }));
    await act(async () => {});
    expect(renewAccessToken).not.toHaveBeenCalled();
    expect(instances[0]!.sendStateless).not.toHaveBeenCalled();
  });
});

// A refused or expired bearer is handled ONCE: renew the session and rejoin;
// if the session cannot be renewed, the user signs in again. A refusal right
// after a renewal is a verdict, and the room stays offline. The rejoin is a
// rebuild like any other, so it waits on the backoff ladder.
describe("useCollabSpec — an auth failure renews once and rebuilds", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => vi.useRealTimers());

  it("rebuilds once when the renewal returns a token, and does not redirect", async () => {
    renderCollab();
    act(() => instances[0]!.onAuthenticationFailed({ reason: "permission-denied" }));
    await act(async () => {});

    expect(renewAccessToken).toHaveBeenCalledTimes(1);
    expect(instances).toHaveLength(1); // armed on the ladder, not yet rebuilt
    await elapse(1_000);
    expect(instances).toHaveLength(2);
    expect(redirectToSignIn).not.toHaveBeenCalled();
  });

  // A Room that refuses each pushed token right after the rejoin syncs earns
  // a renewal every round; without the ladder that is a renew + room load +
  // git seed as fast as the round trip.
  it("backs off the rebuild after each renewal while losses keep coming", async () => {
    renderCollab();
    act(() => instances[0]!.onSynced());
    act(() => instances[0]!.onClose({ event: { code: 1000, reason: "token-expired" } }));
    await act(async () => {});
    await elapse(1_000);
    expect(instances).toHaveLength(2);

    // Synced again, so this loss renews too, but no healthy session came between.
    act(() => instances[1]!.onSynced());
    act(() => instances[1]!.onClose({ event: { code: 1000, reason: "token-expired" } }));
    await act(async () => {});
    expect(renewAccessToken).toHaveBeenCalledTimes(2);
    await elapse(1_000);
    expect(instances).toHaveLength(2); // 2 s, not 1 s
    await elapse(1_000);
    expect(instances).toHaveLength(3);
  });

  it("does not rebuild after unmount while the post-renewal rebuild is armed", async () => {
    const { unmount } = renderCollab();
    act(() => instances[0]!.onAuthenticationFailed({ reason: "permission-denied" }));
    await act(async () => {});
    unmount();
    await elapse(5_000);
    expect(instances).toHaveLength(1);
  });

  it("redirects to sign-in once when the renewal fails, and rebuilds nothing", async () => {
    vi.mocked(renewAccessToken).mockResolvedValue(null);
    const { result } = renderCollab();
    act(() => instances[0]!.onAuthenticationFailed({ reason: "permission-denied" }));
    await act(async () => {});

    expect(redirectToSignIn).toHaveBeenCalledTimes(1);
    expect(instances).toHaveLength(1);
    expect(result.current.status).toBe("offline");
  });

  it("does not renew again on a second failure before the rejoin synced", async () => {
    const { result } = renderCollab();
    act(() => instances[0]!.onAuthenticationFailed({ reason: "permission-denied" }));
    await act(async () => {});
    await elapse(1_000);
    expect(instances).toHaveLength(2);

    act(() => instances[1]!.onAuthenticationFailed({ reason: "permission-denied" }));
    await elapse(30_000);
    expect(renewAccessToken).toHaveBeenCalledTimes(1);
    expect(redirectToSignIn).not.toHaveBeenCalled();
    expect(instances).toHaveLength(2);
    expect(result.current.status).toBe("offline");
  });

  it("handles a rejection and its close as one failure", async () => {
    renderCollab();
    act(() => {
      instances[0]!.onAuthenticationFailed({ reason: "permission-denied" });
      instances[0]!.onClose({ event: { code: 1000, reason: "permission-denied" } });
    });
    await act(async () => {});
    await elapse(1_000);
    expect(renewAccessToken).toHaveBeenCalledTimes(1);
    expect(instances).toHaveLength(2);
  });

  // ae-collab closes a connection whose token ran out (`token-expired`) or
  // whose pushed token it refused (`permission-denied`) with a Close message,
  // not an auth failure. The socket stays open, so nothing else would rejoin.
  it.each(["token-expired", "permission-denied"])(
    "a %s close renews and rebuilds",
    async (reason) => {
      renderCollab();
      act(() => instances[0]!.onSynced());
      act(() => instances[0]!.onClose({ event: { code: 1000, reason } }));
      // The synced doc is never rejoined, so its provider is silenced first.
      expect(instances[0]!.disconnectCalls).toBe(1);
      await act(async () => {});
      await elapse(1_000);
      expect(renewAccessToken).toHaveBeenCalledTimes(1);
      expect(instances).toHaveLength(2);
    },
  );

  it("a plain socket close is a drop, not an auth failure", async () => {
    renderCollab();
    act(() => instances[0]!.onClose({ event: { code: 1006, reason: "" } }));
    await act(async () => {});
    expect(renewAccessToken).not.toHaveBeenCalled();
  });

  it("a session that synced again earns the next failure a renewal", async () => {
    renderCollab();
    act(() => instances[0]!.onClose({ event: { code: 1000, reason: "token-expired" } }));
    await act(async () => {});
    await elapse(1_000);
    expect(instances).toHaveLength(2);
    act(() => instances[1]!.onSynced());
    act(() => instances[1]!.onClose({ event: { code: 1000, reason: "token-expired" } }));
    await act(async () => {});
    await elapse(2_000);
    expect(renewAccessToken).toHaveBeenCalledTimes(2);
    expect(instances).toHaveLength(3);
  });

  // The budget guards one room against a verdict a new token cannot change.
  // Another project is another room, with its own verdict to reach.
  it("gives a room switched to its own renewal, after the last room spent one", async () => {
    const { rerender } = renderCollab();
    act(() => instances[0]!.onAuthenticationFailed({ reason: "permission-denied" }));
    await act(async () => {});
    await elapse(1_000);
    expect(instances).toHaveLength(2);
    // Refused again before syncing: proj1's renewal is spent, it stays offline.
    act(() => instances[1]!.onAuthenticationFailed({ reason: "permission-denied" }));
    await act(async () => {});
    expect(renewAccessToken).toHaveBeenCalledTimes(1);

    project = "proj2";
    rerender();
    expect(instances).toHaveLength(3);
    expect(instances[2]!.name).toBe("spec-acme-proj2");
    act(() => instances[2]!.onAuthenticationFailed({ reason: "permission-denied" }));
    await act(async () => {});
    expect(renewAccessToken).toHaveBeenCalledTimes(2);
    await elapse(30_000);
    expect(instances).toHaveLength(4);
    expect(instances[3]!.name).toBe("spec-acme-proj2");
    expect(redirectToSignIn).not.toHaveBeenCalled();
  });

  it("gives a room switched back to its own renewal again, as a fresh join", async () => {
    const { rerender } = renderCollab();
    act(() => instances[0]!.onAuthenticationFailed({ reason: "permission-denied" }));
    await act(async () => {});
    await elapse(1_000);
    act(() => instances[1]!.onAuthenticationFailed({ reason: "permission-denied" }));
    await act(async () => {});
    expect(renewAccessToken).toHaveBeenCalledTimes(1);

    // Away to proj2 and back, with no loss in proj2.
    project = "proj2";
    rerender();
    project = "proj1";
    rerender();
    expect(instances).toHaveLength(4);
    act(() => instances[3]!.onAuthenticationFailed({ reason: "permission-denied" }));
    await act(async () => {});
    expect(renewAccessToken).toHaveBeenCalledTimes(2);
  });

  it("does nothing after unmount when the renewal lands late", async () => {
    let resolveRenew: (token: string | null) => void = () => {};
    vi.mocked(renewAccessToken).mockReturnValue(
      new Promise((resolve) => {
        resolveRenew = resolve;
      }),
    );
    const { unmount } = renderCollab();
    act(() => instances[0]!.onAuthenticationFailed({ reason: "permission-denied" }));
    unmount();
    await act(async () => resolveRenew(null));
    expect(redirectToSignIn).not.toHaveBeenCalled();
    expect(instances).toHaveLength(1);
  });
});

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

// A refusal after the renewal was spent is the one drop a rebuild must not
// answer: the fresh provider would present the same token and be refused
// again. The server closes the socket right after refusing, so the refusal and
// the close arrive in either order; both must end offline and stay there.
describe("useCollabSpec — a refusal after the renewal is terminal", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => vi.useRealTimers());

  /** Spend the renewal: refuse the first room, let it renew and rejoin. */
  async function spendRenewal() {
    act(() => instances[0]!.onAuthenticationFailed({ reason: "permission-denied" }));
    await act(async () => {});
    await elapse(1_000);
    expect(instances).toHaveLength(2);
  }

  it("cancels a rebuild the preceding drop already queued", async () => {
    const { result } = renderCollab();
    await spendRenewal();
    act(() => instances[1]!.onSynced());
    // Synced again: this failure may renew once more, so spend it too.
    act(() => instances[1]!.onAuthenticationFailed({ reason: "permission-denied" }));
    await act(async () => {});
    await elapse(2_000);
    expect(instances).toHaveLength(3);
    act(() => instances[2]!.onStatus({ status: "disconnected" }));
    act(() => instances[2]!.onAuthenticationFailed({ reason: "permission-denied" }));

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000);
    });

    expect(instances).toHaveLength(3);
    expect(result.current.status).toBe("offline");
  });

  it("blocks a rebuild the following drop would have queued", async () => {
    const { result } = renderCollab();
    await spendRenewal();
    act(() => instances[1]!.onAuthenticationFailed({ reason: "permission-denied" }));
    act(() => instances[1]!.onStatus({ status: "disconnected" }));

    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000);
    });

    expect(instances).toHaveLength(2);
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

  it("re-reads AE Studio, so a pod that is restarting shows as the banner", () => {
    renderCollab();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    act(() =>
      instances[0]!.onAuthenticationFailed({ reason: "upstream-unavailable" }),
    );
    expect(invalidate).toHaveBeenCalledWith({ queryKey: aeStudioKeys.all });
  });

  it("never renews or redirects: an outage is not about the bearer", async () => {
    renderCollab();
    act(() =>
      instances[0]!.onAuthenticationFailed({ reason: "upstream-unavailable" }),
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000);
    });
    expect(renewAccessToken).not.toHaveBeenCalled();
    expect(redirectToSignIn).not.toHaveBeenCalled();
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

// The peer list is UI state over the provider's awareness. It must not outlive
// the provider: a rebuild across an agent's departure would otherwise keep a
// ghost "agent" peer (and Build gated) until reload.
describe("useCollabSpec — peers follow the live provider", () => {
  beforeEach(() => {
    instances.length = 0;
    vi.useFakeTimers();
  });
  afterEach(() => vi.useRealTimers());

  it("clears an agent peer when the room is rebuilt because the Room URL went away", () => {
    const { result, rerender } = renderCollab();
    act(() => {
      instances[0]!.awarenessStates.set(900, peerState("Spec Agent", "agent"));
      instances[0]!.emitAwarenessChange();
    });
    expect(result.current.peers.map((p) => p.kind)).toEqual(["agent"]);

    // AE Studio fails (no Room URL), then comes back; the agent has left.
    studio = { data: { state: "failed" }, isPending: false };
    rerender();
    studio = { data: READY, isPending: false };
    rerender();

    expect(instances).toHaveLength(2);
    expect(result.current.peers).toEqual([]);
  });

  it("clears an agent peer when the room is rebuilt after a post-sync drop", async () => {
    const { result } = renderCollab();
    act(() => instances[0]!.onSynced());
    act(() => {
      instances[0]!.awarenessStates.set(900, peerState("Spec Agent", "agent"));
      instances[0]!.emitAwarenessChange();
    });
    expect(result.current.peers).toHaveLength(1);

    act(() => instances[0]!.onStatus({ status: "disconnected" }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000);
    });

    expect(instances).toHaveLength(2);
    expect(result.current.peers).toEqual([]);
  });

  it("clears an agent peer when the room is rebuilt after a re-sign-in renewal", async () => {
    const { result } = renderCollab();
    act(() => {
      instances[0]!.awarenessStates.set(900, peerState("Spec Agent", "agent"));
      instances[0]!.emitAwarenessChange();
    });
    expect(result.current.peers).toHaveLength(1);

    act(() => instances[0]!.onAuthenticationFailed({ reason: "permission-denied" }));
    await act(async () => {});
    await elapse(1_000);

    expect(instances).toHaveLength(2);
    expect(result.current.peers).toEqual([]);
  });

  it("shows a peer already in a new provider's awareness at join, without a change", () => {
    peersAtJoin = [[900, peerState("Grace", "user")]];
    const { result } = renderCollab();

    expect(result.current.peers).toEqual([
      expect.objectContaining({ clientId: 900, name: "Grace", kind: "user" }),
    ]);
  });
});

// A GET /ae-studio that answers `provisioning` while the pod keeps running (a
// drift converge) says nothing about the Room. Tearing the provider down on it
// lost unsynced edits and rejoined from a reseed for no reason; a pod that
// really restarts drops the socket, and the drop paths above answer that.
describe("useCollabSpec — a provisioning blip keeps a healthy Room", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => vi.useRealTimers());

  it("keeps the provider, the doc and its edits through ready → provisioning → ready", async () => {
    const { result, rerender } = renderCollab();
    act(() => instances[0]!.onSynced());
    const provider = result.current.provider;
    const doc = instances[0]!.document;
    act(() => {
      doc.getMap<Y.Text>("files").set("specs/notes.txt", new Y.Text("unsaved edit"));
    });

    studio = { data: { state: "provisioning" }, isPending: false };
    rerender();
    expect(result.current.status).toBe("connected");
    // The console polls a provisioning answer every 2 s.
    await elapse(4_000);
    studio = { data: READY, isPending: false };
    rerender();
    await elapse(4_000);

    expect(instances).toHaveLength(1);
    expect(instances[0]!.disconnectCalls).toBe(0);
    expect(result.current.provider).toBe(provider);
    expect(result.current.doc).toBe(doc);
    expect(result.current.getFileText("specs/notes.txt")?.toString()).toBe("unsaved edit");
  });

  it("rebuilds when AE Studio comes back ready at another Room URL", () => {
    const { rerender } = renderCollab();
    act(() => instances[0]!.onSynced());
    studio = { data: { state: "provisioning" }, isPending: false };
    rerender();
    studio = {
      data: { ...READY, urls: { ...READY.urls!, collab: "ws://ae-collab-2.mock" } },
      isPending: false,
    };
    rerender();

    expect(instances).toHaveLength(2);
    expect(instances[1]!.url).toBe("ws://ae-collab-2.mock/v1/rooms");
  });

  it("leaves when a blip turns into a failure", () => {
    const { result, rerender } = renderCollab();
    act(() => instances[0]!.onSynced());
    studio = { data: { state: "provisioning" }, isPending: false };
    rerender();
    studio = { data: { state: "failed" }, isPending: false };
    rerender();

    expect(result.current.status).toBe("offline");
    expect(result.current.provider).toBeNull();
  });
});

// A forced flush is answered by the pod's Files socket within its own budget
// (40 s), and ae-collab gives up on the socket at 45 s. A console that stopped
// waiting earlier reported a slow, successful save as a failure and blocked
// Build on it.
describe("useCollabSpec — a flush outwaits the Room's own deadlines", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => vi.useRealTimers());

  it("is still waiting at 45 s and times out at 50 s", async () => {
    const { result } = renderCollab();
    act(() => instances[0]!.onSynced());
    let outcome: "pending" | "resolved" | Error = "pending";
    void result.current.flush().then(
      () => (outcome = "resolved"),
      (e: Error) => (outcome = e),
    );
    expect(instances[0]!.sendStateless).toHaveBeenCalledWith(
      expect.stringContaining('"type":"flush"'),
    );

    await elapse(45_000);
    expect(outcome).toBe("pending");
    await elapse(5_000);
    expect(outcome).toEqual(new Error("Timed out waiting for the workspace to commit."));
  });

  it("resolves on the Room's late ack inside the window", async () => {
    const { result } = renderCollab();
    act(() => instances[0]!.onSynced());
    let outcome: "pending" | "resolved" | Error = "pending";
    void result.current.flush().then(
      () => (outcome = "resolved"),
      (e: Error) => (outcome = e),
    );
    const sent = JSON.parse(String(instances[0]!.sendStateless.mock.calls[0]![0])) as { id: string };

    await elapse(44_000);
    act(() => instances[0]!.emitStateless({ type: "flushed", id: sent.id }));
    await elapse(0);
    expect(outcome).toBe("resolved");
  });
});
