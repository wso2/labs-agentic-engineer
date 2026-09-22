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

// The Annotate batch (#817): one Send all is ONE `/prototype` turn carrying the
// whole queue as a typed field; how that turn ends decides the queue's fate.

import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { chatKeyFor, getMessages, notifyTurnEnd, replaceMessages } from "../../agent-chat/chatStore";
import { prototypeKeys } from "../api/keys";
import type { PrototypeAnnotation } from "../model/annotations";
import { usePrototypeFeedback } from "./usePrototypeFeedback";

const ORG = "acme";
const PROJECT = "lunch";
const KEY = chatKeyFor(ORG, PROJECT);

vi.mock("../../../auth/SessionContext", () => ({ useSession: () => ({ orgHandle: ORG }) }));
vi.mock("../../agent-chat/currentUser", () => ({ useCurrentAuthor: () => ({ id: "u-1", displayName: "Ann" }) }));

const mockFetchCurrent = vi.fn();
vi.mock("../../agent-chat/api/conversations", async (importOriginal) => {
  const real = await importOriginal<typeof import("../../agent-chat/api/conversations")>();
  return { ...real, fetchCurrentConversationId: (...a: unknown[]) => mockFetchCurrent(...a) };
});
const mockStartTurn = vi.fn();
vi.mock("../../agent-chat/api/turns", async (importOriginal) => {
  const real = await importOriginal<typeof import("../../agent-chat/api/turns")>();
  return { ...real, startCollabTurn: (...a: unknown[]) => mockStartTurn(...a) };
});
// The turn's fold, ended by the test through the log's turn-end bus, exactly
// as the real fold announces a terminal frame.
let endTurn: (status: "completed" | "failed" | null) => void = () => {};
vi.mock("../../agent-chat/runTurn", () => ({
  attachAndFoldTurn: (chatKey: string) =>
    new Promise<void>((resolve) => {
      endTurn = (status) => {
        if (status) notifyTurnEnd(chatKey, status);
        resolve();
      };
    }),
}));

const request = (text: string, componentIds: string[] = []): Omit<PrototypeAnnotation, "id"> => ({
  prototypeSchemaVersion: 1,
  screenId: "screen.queue",
  flowId: null,
  stateId: "state.default",
  componentIds,
  request: text,
});

function mount() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const invalidate = vi.spyOn(queryClient, "invalidateQueries");
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  const view = renderHook(() => usePrototypeFeedback(PROJECT, "portal"), { wrapper });
  return { ...view, invalidate };
}

async function queuedTwo() {
  const view = mount();
  await waitFor(() => expect(view.result.current.ready).toBe(true));
  act(() => {
    view.result.current.add(request("Rename to Download", ["btn.export"]));
    view.result.current.add(request("Too busy"));
  });
  return view;
}

const prototypeInvalidations = (invalidate: { mock: { calls: unknown[][] } }) =>
  invalidate.mock.calls.filter(
    ([filters]: unknown[]) => JSON.stringify((filters as { queryKey?: unknown }).queryKey) === JSON.stringify(prototypeKeys.file(PROJECT, "portal")),
  );

beforeEach(() => {
  vi.clearAllMocks();
  replaceMessages(KEY, []);
  mockFetchCurrent.mockResolvedValue("conv-1");
  mockStartTurn.mockResolvedValue("turn-1");
});

describe("usePrototypeFeedback", () => {
  it("queues and removes requests, each with its own ID", async () => {
    const { result } = await queuedTwo();
    const [first, second] = result.current.annotations;
    expect(first!.id).not.toBe(second!.id);
    act(() => result.current.remove(first!.id));
    expect(result.current.annotations.map((a) => a.request)).toEqual(["Too busy"]);
  });

  it("sends the whole batch in ONE request, the instruction exactly /prototype", async () => {
    const { result } = await queuedTwo();
    let sent: Promise<boolean> = Promise.resolve(false);
    act(() => {
      sent = result.current.sendAll();
    });
    await waitFor(() => expect(mockStartTurn).toHaveBeenCalledTimes(1));
    const [project, conversation, instruction, files, collab, aiming, feedback] = mockStartTurn.mock.calls[0]!;
    expect([project, conversation, instruction, files, collab, aiming]).toEqual([PROJECT, "conv-1", "/prototype", [], false, undefined]);
    expect(feedback).toEqual({
      prototypePath: "specs/design/components/portal/prototype.json",
      annotations: result.current.annotations,
    });
    // Recorded in the project's chat like any other send.
    expect(getMessages(KEY).find((m) => m.role === "user")).toMatchObject({ content: "/prototype", turnId: "turn-1" });
    act(() => endTurn("completed"));
    await act(async () => expect(await sent).toBe(true));
  });

  it("refreshes the prototype exactly once after the turn completes, and clears the queue", async () => {
    const { result, invalidate } = await queuedTwo();
    let sent: Promise<boolean> = Promise.resolve(false);
    act(() => {
      sent = result.current.sendAll();
    });
    await waitFor(() => expect(result.current.sending).toBe(true));
    await waitFor(() => expect(mockStartTurn).toHaveBeenCalled());
    // Nothing is refreshed while the turn runs.
    expect(prototypeInvalidations(invalidate)).toHaveLength(0);
    act(() => endTurn("completed"));
    await act(async () => void (await sent));
    expect(prototypeInvalidations(invalidate)).toHaveLength(1);
    expect(result.current.annotations).toEqual([]);
    expect(result.current.sending).toBe(false);
    expect(result.current.error).toBeNull();
  });

  it.each([
    ["fails", "failed" as const],
    ["ends unseen", null],
  ])("keeps the queue and refreshes nothing when the turn %s", async (_, status) => {
    const { result, invalidate } = await queuedTwo();
    let sent: Promise<boolean> = Promise.resolve(true);
    act(() => {
      sent = result.current.sendAll();
    });
    await waitFor(() => expect(mockStartTurn).toHaveBeenCalled());
    act(() => endTurn(status));
    await act(async () => expect(await sent).toBe(false));
    expect(prototypeInvalidations(invalidate)).toHaveLength(0);
    expect(result.current.annotations).toHaveLength(2);
    expect(result.current.error).toMatch(/still queued/);
  });

  it("keeps the queue when the send is refused before a turn opens", async () => {
    mockStartTurn.mockRejectedValue(new Error("An agent turn is already running for this project — wait for it to finish."));
    const { result, invalidate } = await queuedTwo();
    await act(async () => expect(await result.current.sendAll()).toBe(false));
    expect(result.current.annotations).toHaveLength(2);
    expect(result.current.error).toMatch(/already running/);
    expect(prototypeInvalidations(invalidate)).toHaveLength(0);
  });

  it("sends nothing with an empty queue", async () => {
    const view = mount();
    await waitFor(() => expect(view.result.current.ready).toBe(true));
    await act(async () => expect(await view.result.current.sendAll()).toBe(false));
    expect(mockStartTurn).not.toHaveBeenCalled();
  });
});
