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
import { DESIGN_COMMAND, START_COMMAND } from "@aep/contracts/commands";
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
// Resolves TRUE (turn accepted) by default — `send` reports acceptance so the
// composer knows whether to clear (#428). A test that needs the refused path
// overrides it with `mockSend.mockResolvedValueOnce(false)`.
const mockSend = vi.fn<(instruction: string, files?: File[]) => Promise<boolean>>(
  async () => true,
);
const mockNewConversation = vi.fn();
// Messages the mocked hook serves — set per test (the rotation dialog's
// wording reads them); reset in each describe's beforeEach.
let mockMessages: unknown[] = [];
let mockConversationReady = true;
let mockHistoryReady = true;
vi.mock("../useAgentChat", () => ({
  useAgentChat: () => ({
    messages: mockMessages,
    isSending: false,
    activeTurnId: undefined,
    conversationReady: mockConversationReady,
    historyReady: mockHistoryReady,
    conversationError: false,
    send: mockSend,
    newConversation: mockNewConversation,
  }),
}));

// The panel navigates on a CLICK (the header's spec button, the questions
// pill) — which would crash outside a RouterProvider, so the hook is stubbed.
// It must not navigate on its own; see the questions describe below.
const mockPanelNavigate = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => mockPanelNavigate,
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

  // The waiting pulse is derived from the LOG, not this panel's composer
  // state (#666): an aimed send dispatches through its own hook, so
  // `isSending` here never flips for it — and its message sat in the feed for
  // the whole dispatch-plus-first-token window with nothing pulsing, reading
  // as dropped. The mocked hook's `isSending` is false, which IS the aimed
  // send's condition.
  it("pulses for an in-flight message this panel did not send", () => {
    mockMessages = [
      {
        id: "m1",
        role: "user",
        content: "make this shorter",
        status: "in_flight",
        anchor: { file: "specs/requirements/prd.md", nodes: [{ name: "n", kind: "paragraph" }] },
      },
    ];
    renderPanel();
    expect(screen.getByText("Working…")).toBeInTheDocument();
    mockMessages = [];
  });

  it("does not pulse over settled history", () => {
    mockMessages = [
      { id: "m1", role: "user", content: "hello", status: "completed" },
      { id: "m2", role: "assistant", turnId: "t1", content: "done" },
    ];
    renderPanel();
    expect(screen.queryByText("Working…")).not.toBeInTheDocument();
    mockMessages = [];
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

  it("sends the design command when nothing is in flight", () => {
    const onAutoGenerated = vi.fn();
    renderPanel({ autoGenerate: "design", onAutoGenerated });

    expect(mockSend).toHaveBeenCalledWith(DESIGN_COMMAND);
    expect(onAutoGenerated).toHaveBeenCalledTimes(1);
  });

  // The panel mounts fresh on the Generate CTA, so the thread id is usually
  // still resolving (#430): the signal must be HELD — neither sent nor
  // consumed — until it lands, or the click is swallowed silently (send
  // no-ops without an id, and a consumed param never fires again).
  it("holds the signal — unconsumed — until the thread id resolves", () => {
    mockConversationReady = false;
    const onAutoGenerated = vi.fn();
    renderPanel({ autoGenerate: "design", onAutoGenerated });

    expect(mockSend).not.toHaveBeenCalled();
    expect(onAutoGenerated).not.toHaveBeenCalled();
    mockConversationReady = true;
  });

  // The backstop on INJECTED commands, and why it must live in the panel: the
  // overview's spec card decides from the LOCAL chat log, which does not exist
  // until this panel has mounted and rehydrated. A teammate opening the
  // overview in a fresh browser sees an empty log and a card reading "nothing
  // has started", while the server thread holds someone else's unanswered
  // question. Sent, the `/start` reads to the start skill as the skip valve and
  // answers their questions with the agent's own defaults — silently.
  it("drops a GUARDED seed while a question is waiting", () => {
    addMessage(KEY, { role: "question", turnId: "t1", toolCallId: "tc1", questions: QUESTIONS });
    setPendingSeed(KEY, START_COMMAND, true);
    renderPanel();

    expect(mockSend).not.toHaveBeenCalled();
    // Consumed, not deferred: a command left in the slot would fire on a later
    // mount, once the question is answered and the guard opens — a send with no
    // click behind it, which nothing on screen would explain.
    expect(consumePendingSeed(KEY)).toBeNull();
  });

  it("drops a GUARDED seed while a turn is in flight", () => {
    addMessage(KEY, { role: "user", content: "/start", turnId: "t1", status: "in_flight" });
    setPendingSeed(KEY, START_COMMAND, true);
    renderPanel();

    expect(mockSend).not.toHaveBeenCalled();
  });

  // The backstop reads the local log, and until the server's history lands
  // that log is empty — so checking it any earlier asks a question whose answer
  // is always "nothing is happening", which is exactly the case an injected
  // `/start` destroys: a teammate's unanswered question, invisible in a fresh
  // browser. `conversationReady` only says the thread has an id.
  it("holds a GUARDED seed until the history has been read", () => {
    mockHistoryReady = false;
    setPendingSeed(KEY, START_COMMAND, true);
    renderPanel();

    expect(mockSend).not.toHaveBeenCalled();
    // HELD, not consumed — it must still fire once the history lands.
    expect(consumePendingSeed(KEY)?.message).toBe(START_COMMAND);
    mockHistoryReady = true;
  });

  // A seed carrying the user's OWN words is the only copy of what they said.
  it("never holds an unguarded seed on the history", () => {
    mockHistoryReady = false;
    setPendingSeed(KEY, "the submitted interview answers");
    renderPanel();

    expect(mockSend).toHaveBeenCalledWith("the submitted interview answers");
    mockHistoryReady = true;
  });

  it("sends a GUARDED seed when the exchange is closed", () => {
    setPendingSeed(KEY, START_COMMAND, true);
    renderPanel();

    expect(mockSend).toHaveBeenCalledWith(START_COMMAND);
  });

  // The guard is a property of the seed, not of the text. An UNGUARDED seed
  // carries the user's own words — submitted interview answers most of all —
  // and the form has already closed for the whole room by the time it is
  // written, so dropping one loses them with nothing on screen.
  it("always sends an unguarded seed, even mid-question", () => {
    addMessage(KEY, { role: "question", turnId: "t1", toolCallId: "tc1", questions: QUESTIONS });
    setPendingSeed(KEY, "the submitted interview answers");
    renderPanel();

    expect(mockSend).toHaveBeenCalledWith("the submitted interview answers");
  });

  it("holds a pending seed — unconsumed — until the thread id resolves", () => {
    mockConversationReady = false;
    setPendingSeed(KEY, "the submitted interview answers");
    renderPanel();

    expect(mockSend).not.toHaveBeenCalled();
    // Still in the store, ready for the effect re-run when the id lands.
    expect(consumePendingSeed(KEY)?.message).toBe("the submitted interview answers");
    mockConversationReady = true;
  });

  it("sends nothing while a question is waiting to be answered", () => {
    addMessage(KEY, { role: "question", turnId: "t1", toolCallId: "tc1", questions: QUESTIONS });
    renderPanel({ autoGenerate: "design", onAutoGenerated: vi.fn() });

    expect(mockSend).not.toHaveBeenCalled();
  });

  it("sends nothing while a turn is in flight", () => {
    addMessage(KEY, { role: "user", content: "/start", turnId: "t1", status: "in_flight" });
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
    renderPanel({ autoGenerate: "design", onAutoGenerated });

    expect(onAutoGenerated).toHaveBeenCalledTimes(1);
  });

  it("never fires late: answering after a suppressed mount sends nothing", () => {
    addMessage(KEY, { role: "question", turnId: "t1", toolCallId: "tc1", questions: QUESTIONS });
    renderPanel({ autoGenerate: "design", onAutoGenerated: vi.fn() });
    expect(mockSend).not.toHaveBeenCalled();

    act(() =>
      addMessage(KEY, { role: "user", content: "anyone", turnId: "t2", status: "completed" }),
    );

    expect(mockSend).not.toHaveBeenCalled();
  });
});

// Rotation (#430 D4): a project-wide act behind a confirmation that NAMES the
// live state — never a gate, because rotation is also the escape hatch from an
// abandoned interview.
describe("AgentChatPanel — New conversation confirms before rotating", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    consumePendingSeed(KEY);
    replaceMessages(KEY, []);
    mockMessages = [];
  });

  it("opens the confirmation and rotates only on confirm", () => {
    renderPanel();
    fireEvent.click(screen.getByRole("button", { name: "New conversation" }));
    expect(screen.getByText("Start a new conversation?")).toBeInTheDocument();
    expect(
      screen.getByText(/fresh conversation for everyone on the project/),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Start new conversation" }));
    expect(mockNewConversation).toHaveBeenCalledTimes(1);
  });

  it("cancel closes without rotating", () => {
    renderPanel();
    fireEvent.click(screen.getByRole("button", { name: "New conversation" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(mockNewConversation).not.toHaveBeenCalled();
  });

  // The dialog names what is at stake: an unanswered question reads as an
  // abandonment warning, not a generic "are you sure?".
  it("names the unanswered questions when an interview is open", () => {
    mockMessages = [
      {
        id: "q1",
        role: "question",
        turnId: "t1",
        toolCallId: "tc1",
        questions: [{ question: "Who signs in?", options: [{ label: "Anyone" }] }],
      },
    ];
    renderPanel();
    fireEvent.click(screen.getByRole("button", { name: "New conversation" }));
    expect(screen.getByText(/an unanswered question/)).toBeInTheDocument();
    expect(screen.getByText(/abandons them for everyone/)).toBeInTheDocument();
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
    expect(mockSend).toHaveBeenCalledWith("/spec an expense tracker", []);
  });

  it("sends a bare /design verbatim for the server to expand", () => {
    typeAndSubmit("/design");
    expect(mockSend).toHaveBeenCalledWith("/design", []);
  });

  it("sends a plain chat message verbatim", () => {
    typeAndSubmit("please regenerate the design");
    expect(mockSend).toHaveBeenCalledWith("please regenerate the design", []);
  });

  // --- Chat attachments (#428) ---------------------------------------------

  /** A File of a given size without allocating the bytes. */
  function fileOf(name: string, size = 16): File {
    const file = new File(["x"], name);
    Object.defineProperty(file, "size", { value: size });
    return file;
  }

  function attach(names: string[]) {
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    fireEvent.change(input, { target: { files: names.map((n) => fileOf(n)) } });
  }

  it("sends the attached files alongside the typed message", () => {
    renderPanel();
    attach(["error.png", "rows.csv"]);
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "what is wrong?" } });
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter" });
    const [instruction, files] = mockSend.mock.calls[0] as [string, File[]];
    expect(instruction).toBe("what is wrong?");
    expect(files.map((f) => f.name)).toEqual(["error.png", "rows.csv"]);
  });

  it("carries attachments on a slash command too — a sketch can ride /design", () => {
    // The composer does not inspect what was typed (ADR-0019 decision 3).
    renderPanel();
    attach(["sketch.png"]);
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "/design" } });
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter" });
    const [instruction, files] = mockSend.mock.calls[0] as [string, File[]];
    expect(instruction).toBe("/design");
    expect(files.map((f) => f.name)).toEqual(["sketch.png"]);
  });

  it("clears text and cards once the turn is accepted", async () => {
    renderPanel();
    attach(["error.png"]);
    // An image attachment draws no name, so its identity in the DOM is the alt
    // text on the thumbnail.
    expect(screen.getByAltText("error.png")).toBeInTheDocument();
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "look" } });
    await act(async () => {
      fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter" });
    });
    expect(screen.getByRole("textbox")).toHaveValue("");
    expect(screen.queryByAltText("error.png")).not.toBeInTheDocument();
  });

  it("KEEPS text and cards when the send is refused", async () => {
    // The bytes exist nowhere but the browser (ADR-0019), so clearing on a
    // routine 409 would cost the user a re-pick of every file from disk.
    mockSend.mockResolvedValueOnce(false);
    renderPanel();
    attach(["error.png"]);
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "look" } });
    await act(async () => {
      fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter" });
    });
    expect(screen.getByRole("textbox")).toHaveValue("look");
    expect(screen.getByAltText("error.png")).toBeInTheDocument();
  });

  // `/start` is the ONE command the server expands: only it can append the idea
  // captured at project creation, which the browser never reads or parses. If
  // the composer expanded it here, the server would see prose instead of the
  // command and the idea would silently never arrive.
  it("sends /start UNEXPANDED so the server can attach the captured idea", () => {
    typeAndSubmit("/start");
    expect(mockSend).toHaveBeenCalledWith("/start", []);
  });

  it("sends /start with an inline idea unexpanded too", () => {
    typeAndSubmit("/start a rota planner for nurses");
    expect(mockSend).toHaveBeenCalledWith("/start a rota planner for nurses", []);
  });
});

// #562 decision 6: "Nothing ever navigates the user automatically — not on
// question arrival, not on turn completion. Blocking questions do not earn the
// right to move the viewport."
//
// This replaced an effect that called `openSpec()` the moment an unanswered
// question appeared. It predates the kickoff firing at project creation, when
// reaching a question meant having asked for one; now every project's first
// minute produces one, so it threw every new user off the page they had just
// landed on — before they had read a word of what the agent was doing.
describe("AgentChatPanel — a question does not move the user", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    consumePendingSeed(KEY);
    replaceMessages(KEY, []);
  });

  const QUESTION: AskQuestionInput[] = [
    { question: "Who signs in?", options: [{ label: "Anyone" }] },
  ];

  it("stays put when an unanswered question arrives", () => {
    addMessage(KEY, { role: "question", turnId: "t1", toolCallId: "tc1", questions: QUESTION });
    renderPanel();

    expect(mockPanelNavigate).not.toHaveBeenCalled();
  });

  it("stays put when a second question arrives later", () => {
    renderPanel();
    act(() =>
      addMessage(KEY, { role: "question", turnId: "t1", toolCallId: "tc1", questions: QUESTION }),
    );

    expect(mockPanelNavigate).not.toHaveBeenCalled();
  });

  // The pill is what takes them there, and it is the whole point of not
  // moving them: they arrive because they chose to.
  it("navigates when the questions pill is clicked", () => {
    addMessage(KEY, { role: "question", turnId: "t1", toolCallId: "tc1", questions: QUESTION });
    renderPanel();

    fireEvent.click(screen.getByTestId("questions-pointer"));

    expect(mockPanelNavigate).toHaveBeenCalledWith(
      expect.objectContaining({ to: "/projects/$projectName/spec" }),
    );
  });

  it("does not navigate when specWorkspace is off — register chat has no spec", () => {
    addMessage(KEY, { role: "question", turnId: "t1", toolCallId: "tc1", questions: QUESTION });
    renderPanel({ specWorkspace: false });

    expect(screen.queryByTestId("questions-pointer")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Open spec workspace" })).not.toBeInTheDocument();
    expect(mockPanelNavigate).not.toHaveBeenCalled();
  });
});

// The one remaining generation CTA (#159 design). The requirements half is
// gone: the platform fires `/start` at project creation (#562), so no signal is
// handed across a navigation for it any more — the spec card's start CTA seeds
// the composer where the user already is.
describe("AgentChatPanel — the design CTA", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    consumePendingSeed(KEY);
    // The log is a module-level store: an unanswered question left by another
    // block reads as an open exchange here and suppresses the send.
    replaceMessages(KEY, []);
  });

  it("auto-sends /design verbatim for the design signal", () => {
    // Flow commands go verbatim (#373): the SERVER expands the token and
    // decides the flow's eager skills, so the CTA equals a typed /design.
    renderPanel({ autoGenerate: "design" });
    expect(mockSend).toHaveBeenCalledWith("/design");
  });

  it("fires the signal exactly once", () => {
    const { rerender } = renderPanel({ autoGenerate: "design" });
    rerender(withProviders(<AgentChatPanel {...panelProps({ autoGenerate: "design" })} />));
    expect(mockSend).toHaveBeenCalledTimes(1);
  });
});

// The scoped launchers are gone from the composer (#579): each one changes a
// specific place in the PRD and is now offered there, as a code lens on the
// section it changes. The gate they carried — inert while a question form is
// live, since firing one is not an answer and would supersede everyone's form —
// moved with them (`SpecMdEditor`'s `lenses` binding, `SpecView`'s
// `lensBusyReason`).
describe("AgentChatPanel — the composer offers no flow launchers", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    consumePendingSeed(KEY);
    replaceMessages(KEY, []);
    mockMessages = [];
    mockConversationReady = true;
  });

  it("has no Actions menu", () => {
    renderPanel();
    expect(screen.queryByRole("button", { name: /Actions/ })).toBeNull();
  });
});
