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

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ComponentProps } from "react";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AskQuestionInput } from "@aep/agent-stream";
import { START_COMMAND } from "@aep/contracts/commands";
import { AgentChatPanel } from "./AgentChatPanel";
import {
  addMessage,
  chatKeyFor,
  consumePendingSeed,
  replaceMessages,
  setPendingSeed,
} from "../chatStore";

const ORG = "acme";
const PROJECT = "proj1";
const KEY = chatKeyFor(ORG, PROJECT);

// --- useAgentChat: replaced wholesale — this file only exercises the
// pendingSeed consume-once + turn-end fallback wiring this task added, not
// the panel's own send/stream-fold behavior (untested before this task and
// out of scope here). ------------------------------------------------------
const mockSend = vi.fn();
vi.mock("../useAgentChat", () => ({
  useAgentChat: () => ({
    messages: [],
    isSending: false,
    activeTurnId: undefined,
    send: mockSend,
    newConversation: vi.fn(),
  }),
}));

// The merged multi-user panel stamps outgoing messages with the signed-in
// author (via useCurrentAuthor -> useSession), which throws outside an
// AuthGuard — this test renders the panel bare, so stub the session.
vi.mock("../../../auth/SessionContext", () => ({
  useSession: () => ({
    user: { name: "Test User", email: "test@example.com" },
    orgHandle: "acme",
    signOut: vi.fn(),
  }),
}));

// The panel's stick-to-bottom scroll behavior is browser-only (drives a
// ResizeObserver, absent in jsdom) and orthogonal to the pendingSeed wiring
// under test — stub it out with inert refs.
vi.mock("use-stick-to-bottom", () => ({
  useStickToBottom: () => ({
    scrollRef: { current: null },
    contentRef: { current: null },
  }),
}));

// --- Turn-end fallback (#252 Task 5): its own behavior is covered by
// useTurnEndDependencyRefresh.test.tsx — here it's a stub so this test can
// assert SpecView-style wiring (right chatKey/projectName) without needing a
// QueryClientProvider.
const mockUseTurnEndDependencyRefresh = vi.fn();
vi.mock("../useTurnEndDependencyRefresh", () => ({
  useTurnEndDependencyRefresh: (...args: unknown[]) =>
    mockUseTurnEndDependencyRefresh(...args),
}));

type PanelProps = ComponentProps<typeof AgentChatPanel>;

function panelProps(overrides: Partial<PanelProps> = {}): PanelProps {
  return { org: ORG, projectName: PROJECT, onClose: () => {}, ...overrides };
}

// The panel reads the spec file list (flow stepper, #372) through
// react-query, so tests provide the same QueryClientProvider the app root
// does — with retries off and no network (queries just stay pending).
const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, enabled: false } } });

function withProviders(node: React.ReactElement) {
  return <QueryClientProvider client={queryClient}>{node}</QueryClientProvider>;
}

function renderPanel(overrides: Partial<PanelProps> = {}) {
  return render(withProviders(<AgentChatPanel {...panelProps(overrides)} />));
}

describe("AgentChatPanel — pendingSeed + turn-end wiring (#252 Task 5)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    consumePendingSeed(KEY); // drain any leftover seed between tests
  });

  it("auto-sends a seed that was already pending before mount, exactly once", () => {
    setPendingSeed(KEY, "resolve dependency A");
    renderPanel();
    expect(mockSend).toHaveBeenCalledWith("resolve dependency A");
    expect(mockSend).toHaveBeenCalledTimes(1);
  });

  it("does not send anything when no seed is pending", () => {
    renderPanel();
    expect(mockSend).not.toHaveBeenCalled();
  });

  it("auto-sends a NEW seed set after mount (panel already open)", () => {
    renderPanel();
    expect(mockSend).not.toHaveBeenCalled();

    act(() => setPendingSeed(KEY, "resolve dependency B"));
    expect(mockSend).toHaveBeenCalledWith("resolve dependency B");
    expect(mockSend).toHaveBeenCalledTimes(1);
  });

  it("consumes the seed from the store (so a second mount never resends it)", () => {
    setPendingSeed(KEY, "resolve dependency C");
    const { unmount } = renderPanel();
    expect(mockSend).toHaveBeenCalledTimes(1);
    unmount();

    mockSend.mockClear();
    renderPanel();
    expect(mockSend).not.toHaveBeenCalled();
  });

  it("wires the universal turn-end freshness fallback with this project's chat key", () => {
    renderPanel();
    expect(mockUseTurnEndDependencyRefresh).toHaveBeenCalledWith(KEY, PROJECT);
  });
});

// The injected generate (#150 spec / #159 design). The overview's spec stage
// already declines to attach `?generate` while the agent is mid-exchange, but
// the signal also arrives from a pasted URL — this is where "may I send this"
// is decided. A `/start` landing on an unanswered question form reads to the
// start skill as the user's own skip valve, so the interview is silently
// replaced by the agent's recommended answers; nothing errors, and the loss is
// only visible as `*assumed*` tags in the PRD.
describe("AgentChatPanel — the injected generate is gated on an open exchange", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    consumePendingSeed(KEY);
    replaceMessages(KEY, []);
  });

  const QUESTIONS: AskQuestionInput[] = [
    { question: "Who signs in?", options: [{ label: "Anyone" }] },
  ];

  it("sends the requirements command when nothing is in flight", () => {
    const onAutoGenerated = vi.fn();
    renderPanel({ autoGenerate: "requirements", onAutoGenerated });

    expect(mockSend).toHaveBeenCalledWith(START_COMMAND);
    expect(onAutoGenerated).toHaveBeenCalledTimes(1);
  });

  it("sends nothing while a question is waiting to be answered", () => {
    addMessage(KEY, { role: "question", turnId: "t1", toolCallId: "tc1", questions: QUESTIONS });
    renderPanel({ autoGenerate: "requirements", onAutoGenerated: vi.fn() });

    expect(mockSend).not.toHaveBeenCalled();
  });

  it("sends nothing while a turn is in flight", () => {
    addMessage(KEY, { role: "user", content: "/start", turnId: "t1", status: "in_flight" });
    renderPanel({ autoGenerate: "requirements", onAutoGenerated: vi.fn() });

    expect(mockSend).not.toHaveBeenCalled();
  });

  it("gates the design signal on the same condition", () => {
    addMessage(KEY, { role: "question", turnId: "t1", toolCallId: "tc1", questions: QUESTIONS });
    renderPanel({ autoGenerate: "design", onAutoGenerated: vi.fn() });

    expect(mockSend).not.toHaveBeenCalled();
  });

  // A suppressed signal is CONSUMED, not deferred: `onAutoGenerated` strips the
  // param exactly as a sent one does. Left in the URL it would fire on a later
  // mount — once the question is answered and the guard opens — and a send with
  // no click behind it is worse than the bug being fixed.
  it("still consumes the signal when it suppresses the send", () => {
    addMessage(KEY, { role: "question", turnId: "t1", toolCallId: "tc1", questions: QUESTIONS });
    const onAutoGenerated = vi.fn();
    renderPanel({ autoGenerate: "requirements", onAutoGenerated });

    expect(onAutoGenerated).toHaveBeenCalledTimes(1);
  });

  it("never fires late: answering after a suppressed mount sends nothing", () => {
    addMessage(KEY, { role: "question", turnId: "t1", toolCallId: "tc1", questions: QUESTIONS });
    renderPanel({ autoGenerate: "requirements", onAutoGenerated: vi.fn() });
    expect(mockSend).not.toHaveBeenCalled();

    act(() =>
      addMessage(KEY, { role: "user", content: "anyone", turnId: "t2", status: "completed" }),
    );

    expect(mockSend).not.toHaveBeenCalled();
  });
});

// The `/<skill>` composer shortcut: a leading /token is expanded to a
// "load the skill and follow it" turn before send; plain chat is verbatim.
describe("AgentChatPanel — /<skill> composer shortcut", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    consumePendingSeed(KEY);
  });

  function typeAndSubmit(text: string) {
    renderPanel();
    fireEvent.change(screen.getByRole("textbox"), { target: { value: text } });
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter" });
  }

  // #373: the SERVER expands every /<skill> token now — the composer sends
  // commands verbatim, so a typed command and a CTA are byte-identical turns.
  it("sends /spec with follow-up text verbatim for the server to expand", () => {
    typeAndSubmit("/spec an expense tracker");
    expect(mockSend).toHaveBeenCalledWith("/spec an expense tracker");
  });

  it("sends a bare /design verbatim for the server to expand", () => {
    typeAndSubmit("/design");
    expect(mockSend).toHaveBeenCalledWith("/design");
  });

  it("sends a plain chat message verbatim", () => {
    typeAndSubmit("please regenerate the design");
    expect(mockSend).toHaveBeenCalledWith("please regenerate the design");
  });

  // `/start` is the ONE command the server expands: only it can append the idea
  // captured at project creation, which the browser never reads or parses. If
  // the composer expanded it here, the server would see prose instead of the
  // command and the idea would silently never arrive.
  it("sends /start UNEXPANDED so the server can attach the captured idea", () => {
    typeAndSubmit("/start");
    expect(mockSend).toHaveBeenCalledWith("/start");
  });

  it("sends /start with an inline idea unexpanded too", () => {
    typeAndSubmit("/start a rota planner for nurses");
    expect(mockSend).toHaveBeenCalledWith("/start a rota planner for nurses");
  });
});

// The generation CTAs (#150 spec / #159 design). Requirements go through
// `/start` — the console composes nothing and reads no local copy of the idea,
// so a different browser, device or teammate kicks off identically.
describe("AgentChatPanel — generation CTAs", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    consumePendingSeed(KEY);
  });

  it("auto-sends /start verbatim for the requirements signal", () => {
    renderPanel({ autoGenerate: "requirements" });
    // Flow commands go verbatim (#373): the SERVER expands the token and
    // decides the flow's eager skills, so the CTA equals a typed /start.
    expect(mockSend).toHaveBeenCalledWith("/start");
  });

  it("auto-sends /design verbatim for the design signal", () => {
    renderPanel({ autoGenerate: "design" });
    expect(mockSend).toHaveBeenCalledWith("/design");
  });

  it("fires the signal exactly once", () => {
    const { rerender } = renderPanel({ autoGenerate: "requirements" });
    rerender(withProviders(<AgentChatPanel {...panelProps({ autoGenerate: "requirements" })} />));
    expect(mockSend).toHaveBeenCalledTimes(1);
  });
});
