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

import { useEffect, useState } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { OxygenTheme, OxygenUIThemeProvider, useColorScheme } from "@wso2/oxygen-ui";
import { PROTOTYPE_START_TIMEOUT_MS } from "@wso2/prototype-kit/host";
import type { ProjectChat, TurnOutcome } from "../../agent-chat/chatStore";
import { SAMPLE_MANIFEST, SAMPLE_SOURCE } from "../../../mocks/fixtures/prototype";
import { appPrototypes, manifestPath, revisingIn, sourcePath, type AppPrototype } from "../model/prototypes";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MAX_FEEDBACK_REQUESTS, MAX_FEEDBACK_TEXT, prototypeHash } from "@wso2/prototype-kit/feedback";
import { designKey } from "../../design/api/designModel";
import { unreviewed } from "../model/reviewed";

// The Prototype tab and its full-screen review, driven as a reviewer drives
// them: the prototype runs in the kit's PrototypeFrame, whose messages are
// played here as the sandboxed frame would send them, and the turns go to the
// project's chat, which is stubbed at its store.

const C = "expense-web";
const files = { [manifestPath(C)]: SAMPLE_MANIFEST, [sourcePath(C)]: SAMPLE_SOURCE };

let prototypes: AppPrototype[] | undefined;
vi.mock("../usePrototypes", () => ({ usePrototypes: () => prototypes }));

let chat: ProjectChat;
const send = vi.fn<(...args: unknown[]) => Promise<boolean>>(async () => true);
const turnEnds = new Set<(projectName: string, outcome: TurnOutcome) => void>();
vi.mock("../../agent-chat/useProjectChat", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../agent-chat/useProjectChat")>()),
  useProjectChat: () => chat,
  chatStore: {
    send: (...args: unknown[]) => send(...args),
    onTurnEnd: (fn: (projectName: string, outcome: TurnOutcome) => void) => {
      turnEnds.add(fn);
      return () => turnEnds.delete(fn);
    },
  },
}));

// The review store outlives the tab for the page's life; each test starts on a fresh one, on the stubbed chat.
type ReviewStore = ReturnType<typeof import("../model/reviewStore").createReviewStore>;
const reviews = vi.hoisted(() => ({ current: null as null | ReviewStore }));
vi.mock("../model/reviewStore", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../model/reviewStore")>()),
  createReviewStore: () =>
    new Proxy({} as ReviewStore, {
      get: (_, key) => reviews.current![key as keyof ReviewStore],
    }),
}));
const { createReviewStore } = await vi.importActual<typeof import("../model/reviewStore")>("../model/reviewStore");

const openChat = vi.fn();
vi.mock("../../shell/chatPanel", () => ({ useChatPanel: () => ({ open: openChat }) }));

// The theme's 2 MB runtime is the kit's browser lane's to run; the frame here only needs one.
vi.mock("../useReviewAssets", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../useReviewAssets")>()),
  useFrameRuntime: () => ({ value: "/* frame runtime */", error: null }),
}));

// jsdom lays nothing out and has no ResizeObserver; the review's anchors only need it to exist.
vi.stubGlobal(
  "ResizeObserver",
  class {
    observe() {}
    disconnect() {}
  },
);

const { PrototypeWorkspace } = await import("./PrototypeWorkspace");

const idle: ProjectChat = { status: "ready", error: null, items: [], turn: { phase: "idle" } };
const busy: ProjectChat = { ...idle, turn: { phase: "running", turnId: "t1", instruction: "/design F1" } };

let queryClient = new QueryClient();
let invalidate = vi.spyOn(queryClient, "invalidateQueries");

/** The console switched to its dark scheme, as its user menu does. */
function DarkMode() {
  const { setMode } = useColorScheme();
  useEffect(() => setMode("dark"), [setMode]);
  return null;
}

function Harness({ initial, dark = false }: { initial?: string; dark?: boolean }) {
  const [review, setReview] = useState<string | undefined>(initial);
  return (
    <QueryClientProvider client={queryClient}>
      <OxygenUIThemeProvider theme={OxygenTheme}>
        {dark && <DarkMode />}
        <PrototypeWorkspace projectName="acme-expenses" review={review} onReview={(c) => setReview(c ?? undefined)} />
      </OxygenUIThemeProvider>
    </QueryClientProvider>
  );
}

function frame(): HTMLIFrameElement {
  return screen.getByTitle("Acme Expenses prototype app") as HTMLIFrameElement;
}

/** A message from the sandboxed frame, as its runtime posts it. */
function fromFrame(data: object) {
  act(() => {
    window.dispatchEvent(new MessageEvent("message", { data, source: frame().contentWindow }));
  });
}

/** What the host last told the frame to draw. */
function lastView(post: { mock: { calls: unknown[][] } }) {
  const messages = post.mock.calls.map((c) => c[0] as { type: string; view?: { mode: string; screenId: string; selectedKeys: string[]; pins: object; drafts?: string[]; screenPins?: object[] } });
  return messages.filter((m) => m.view).at(-1)!.view!;
}

/** Render again after the room's files or the chat changed (`prototypes`, `chat`), as their stores would. */
let rerender: () => void = () => {};

async function openReview() {
  const rendered = render(<Harness initial={C} />);
  rerender = () => rendered.rerender(<Harness initial={C} />);
  const dialog = await screen.findByRole("dialog");
  await waitFor(() => expect(frame()).toBeInTheDocument());
  const post = vi.spyOn(frame().contentWindow!, "postMessage");
  fromFrame({ type: "proto:ready" });
  await waitFor(() => expect(post).toHaveBeenCalledWith(expect.objectContaining({ type: "proto:load" }), "*"));
  return { dialog, post };
}

function annotate(dialog: HTMLElement) {
  fireEvent.click(within(dialog).getByRole("button", { name: "Comment" }));
}

/** Where an element is drawn in the frame's viewport, as the frame reports it (jsdom lays nothing out). */
const BOX = { x: 40, y: 120, width: 160, height: 36 };

/** A click on an element in the prototype, as the frame reports it: where it is, and whether it held Shift. */
function clickElement(elementKey: string, { shift = false, box = BOX } = {}) {
  fromFrame({ type: "proto:toggle", elementKey, box, additive: shift });
}

/** The comment bubble open by the prototype. */
function bubble() {
  return screen.getByRole("dialog", { name: /^Comment on/ });
}

/** The floating dock at the bottom of the review, which holds every review control. */
function dock() {
  return screen.getByRole("region", { name: "Review controls" });
}

/** The dock's comments: the count and its list (with Comment on this screen), Send to agent, and what the send says. */
function bar() {
  return within(dock()).getByRole("group", { name: "Comments" });
}

/** The screen the review shows, as the browser window's address bar says it. */
function address(dialog: HTMLElement) {
  return within(dialog).getByLabelText("Address");
}

/** As the Manager on the pending approvals: the role chosen in the dock, then the prototype navigated there (Preview). */
function toPendingAsManager(dialog: HTMLElement) {
  fireEvent.change(within(dialog).getByLabelText("Role"), { target: { value: "manager" } });
  fromFrame({ type: "proto:navigate", screenId: "screen.pending" });
}

/** The bar's list of every queued comment, expanded. */
function commentList() {
  const toggle = within(bar()).getByRole("button", { name: /^\d+ comments?$/ });
  if (toggle.getAttribute("aria-expanded") !== "true") fireEvent.click(toggle);
  return within(bar()).getByRole("list", { name: "Queued comments" });
}

/** The keyboard's whole-screen comment: "Comment on this screen" in the bar's list. */
function commentOnScreen() {
  commentList();
  fireEvent.click(within(bar()).getByRole("button", { name: "Comment on this screen" }));
}

/** A spot of the prototype's document, and where it is in the frame's viewport while the document is scrolled by `scroll`. */
const SPOT = { x: 300, y: 700 };

/** A click on empty space in the prototype at `at` of its document, as the frame reports it (Comment mode only). */
function clickScreen(at = SPOT, scroll = { x: 0, y: 500 }) {
  fromFrame({ type: "proto:screen-click", point: { x: at.x - scroll.x, y: at.y - scroll.y }, at });
}

/** A click on a whole-screen comment's pin, as the frame reports it: the comment's number (none: the hollow pin). */
function clickScreenPin(requests: number[], at = SPOT, scroll = { x: 0, y: 500 }) {
  fromFrame({ type: "proto:screen-pin", requests, point: { x: at.x - scroll.x, y: at.y - scroll.y }, at });
}

function addComment(text: string) {
  fireEvent.change(within(bubble()).getByLabelText("Comment"), { target: { value: text } });
  fireEvent.click(within(bubble()).getByRole("button", { name: "Add" }));
}

beforeEach(() => {
  queryClient = new QueryClient();
  invalidate = vi.spyOn(queryClient, "invalidateQueries");
  prototypes = appPrototypes([C], files, null);
  chat = idle;
  send.mockClear();
  send.mockResolvedValue(true);
  openChat.mockClear();
  turnEnds.clear();
  reviews.current = createReviewStore({
    onTurnEnd: (fn) => {
      turnEnds.add(fn);
      return () => turnEnds.delete(fn);
    },
  });
});

afterEach(cleanup);

describe("the Prototype tab", () => {
  it("lists each web application with its prototype's status", () => {
    prototypes = [
      ...appPrototypes(["admin-web"], { [manifestPath("admin-web")]: "{" , [sourcePath("admin-web")]: "x" }, null),
      ...appPrototypes([C], files, revisingIn("/prototype expense-web")),
      ...appPrototypes(["kiosk-web"], {}, null),
    ];
    render(<Harness />);
    const rows = within(screen.getByRole("list", { name: "Prototypes" })).getAllByRole("listitem");
    expect(rows.map((r) => within(r).getByText(/^(Invalid|Revising…|No prototype|Ready)$/).textContent)).toEqual(["Invalid", "Revising…", "No prototype"]);
    expect(within(rows[0]!).getByLabelText(/prototype\.json is invalid/)).toHaveTextContent("The last prototype couldn't be rendered.");
    expect(within(rows[0]!).queryByRole("button", { name: "Review" })).toBeNull();
    expect(within(rows[0]!).getByRole("button", { name: "Try again" })).toBeInTheDocument();
    expect(within(rows[1]!).getByRole("button", { name: "Review" })).toBeEnabled();
    expect(screen.queryByRole("button", { name: "Update" })).toBeNull();
    expect(within(rows[2]!).getByRole("button", { name: "Make prototype" })).toBeInTheDocument();
  });

  it("says the prototype is being made while its first turn runs", () => {
    prototypes = appPrototypes([C], {}, revisingIn("/prototype expense-web"));
    render(<Harness />);
    expect(screen.getByText("Making prototype…")).toBeInTheDocument();
    expect(screen.getByLabelText("Working on the prototype")).toBeInTheDocument();
  });

  it("shows a prototype the running turn is still writing as in progress, not broken", () => {
    prototypes = [
      ...appPrototypes([C], { [manifestPath(C)]: SAMPLE_MANIFEST }, revisingIn("/prototype")),
      ...appPrototypes(["admin-web"], {}, revisingIn("/prototype")),
    ];
    chat = { ...idle, turn: { phase: "running", turnId: "t1", instruction: "/prototype" } };
    render(<Harness />);
    const rows = within(screen.getByRole("list", { name: "Prototypes" })).getAllByRole("listitem");
    expect(within(rows[0]!).queryByText("The last prototype couldn't be rendered.")).toBeNull();
    expect(within(rows[0]!).getByText("The agent is working on it.")).toBeInTheDocument();
    expect(within(rows[1]!).getByText("Making prototype…")).toBeInTheDocument();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("says why Make prototype waits while another turn runs", () => {
    prototypes = appPrototypes([C], {}, null);
    chat = busy;
    render(<Harness />);
    expect(screen.getByRole("button", { name: "Make prototype" })).toBeDisabled();
    expect(screen.getByText(/The agent is busy with a turn/)).toBeInTheDocument();
  });

  it("says there is no prototype yet and the spec and design come first", () => {
    prototypes = [];
    render(<Harness />);
    expect(screen.getByText(/Finish the spec and design a web application first/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Make prototype/ })).toBeNull();
  });

  it("offers Make prototype in the empty state once the design has a web application", () => {
    prototypes = appPrototypes([C], {}, null);
    render(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: "Make prototype" }));
    expect(openChat).toHaveBeenCalled();
    expect(send).toHaveBeenCalledWith("acme-expenses", "/prototype expense-web", { kind: "prototype" });
  });
});

describe("the full-screen review", () => {
  it("opens from Review over the whole console, and closes with Close and with Escape", async () => {
    render(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: "Review" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("Prototype · Acme Expenses")).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Close" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    fireEvent.click(screen.getByRole("button", { name: "Review" }));
    fireEvent.keyDown(await screen.findByRole("dialog"), { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("leaves Escape pressed in the prototype to the frame: closes only on the frame's proto:escape", async () => {
    const { dialog, post } = await openReview();
    // A key the console's document gets with the frame element as its target was aimed at the prototype (focus is
    // in it), so it is the prototype's, not the dialog's: only the frame says when Escape went unused.
    fireEvent.keyDown(frame(), { key: "Escape" });
    expect(screen.getByRole("dialog")).toBeInTheDocument();

    // Comment mode: the frame's Escape closes the comment bubble first, then clears the selection, then closes the review.
    annotate(dialog);
    clickElement("btn.new-claim");
    fromFrame({ type: "proto:escape" });
    expect(screen.getByRole("dialog", { name: "Prototype · Acme Expenses" })).toBeInTheDocument();
    expect(screen.queryByRole("dialog", { name: /^Comment on/ })).toBeNull();
    expect(lastView(post).selectedKeys).toEqual(["btn.new-claim"]);
    fromFrame({ type: "proto:escape" });
    expect(lastView(post).selectedKeys).toEqual([]);
    expect(screen.getByRole("dialog", { name: "Prototype · Acme Expenses" })).toBeInTheDocument();
    fromFrame({ type: "proto:escape" });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("covers the frame with a loading state until the prototype first draws, so an early click is not lost", async () => {
    render(<Harness initial={C} />);
    const dialog = await screen.findByRole("dialog");
    await waitFor(() => expect(frame()).toBeInTheDocument());
    // The runtime has not started yet, then it has but the app has not drawn.
    expect(within(dialog).getByRole("status")).toHaveTextContent("Starting the prototype…");
    expect(frame()).toHaveAttribute("aria-busy", "true");
    fromFrame({ type: "proto:ready" });
    expect(within(dialog).getByRole("status")).toHaveTextContent("Starting the prototype…");
    fromFrame({ type: "proto:rendered", screenId: "screen.my-claims", elements: [] });
    expect(within(dialog).queryByRole("status")).toBeNull();
    expect(frame()).toHaveAttribute("aria-busy", "false");

    // A frame document that reloads starts again; one that fails to load stops covering and shows why.
    fromFrame({ type: "proto:ready" });
    expect(within(dialog).getByRole("status")).toBeInTheDocument();
    fromFrame({ type: "proto:error", message: "prototype.tsx failed" });
    expect(within(dialog).queryByRole("status")).toBeNull();
    expect(within(dialog).getByRole("alert")).toHaveTextContent("prototype.tsx failed");
  });

  it("stops waiting on a frame that neither draws nor says why, and says it did not start", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      render(<Harness initial={C} />);
      const dialog = await screen.findByRole("dialog");
      await waitFor(() => expect(frame()).toBeInTheDocument());
      fromFrame({ type: "proto:ready" });
      expect(within(dialog).getByRole("status")).toBeInTheDocument();
      act(() => {
        vi.advanceTimersByTime(PROTOTYPE_START_TIMEOUT_MS);
      });
      expect(within(dialog).queryByRole("status")).toBeNull();
      expect(within(dialog).getByRole("alert")).toHaveTextContent("The prototype didn't start");
    } finally {
      vi.useRealTimers();
    }
  });

  it("draws the prototype in the console's scheme", async () => {
    try {
      render(<Harness initial={C} dark />);
      await screen.findByRole("dialog");
      await waitFor(() => expect(frame()).toBeInTheDocument());
      const post = vi.spyOn(frame().contentWindow!, "postMessage");
      fromFrame({ type: "proto:ready" });
      await waitFor(() =>
        expect(post).toHaveBeenCalledWith(expect.objectContaining({ type: "proto:load", view: expect.objectContaining({ colorScheme: "dark" }) }), "*"),
      );
    } finally {
      localStorage.clear();
    }
  });

  it("says why instead of drawing a blank frame when the prototype is invalid", async () => {
    prototypes = appPrototypes([C], { [manifestPath(C)]: SAMPLE_MANIFEST }, null);
    render(<Harness initial={C} />);
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/prototype\.tsx is missing/)).toBeInTheDocument();
    expect(screen.queryByTitle("Acme Expenses prototype app")).toBeNull();
  });

  it("acts in Preview and only selects in Comment mode", async () => {
    const { dialog, post } = await openReview();

    // Preview: a press in the app navigates; a stray toggle selects nothing.
    fromFrame({ type: "proto:navigate", screenId: "screen.new-claim" });
    expect(address(dialog)).toHaveTextContent("prototype://screen.new-claim");
    fromFrame({ type: "proto:toggle", elementKey: "btn.submit" });
    expect(lastView(post)).toMatchObject({ mode: "preview", screenId: "screen.new-claim", selectedKeys: [] });

    // Comment mode: a click selects (and the comment bubble says what), and a navigation is ignored.
    annotate(dialog);
    fromFrame({ type: "proto:rendered", screenId: "screen.new-claim", elements: [{ key: "btn.submit", label: "Submit claim" }] });
    clickElement("btn.submit");
    fromFrame({ type: "proto:navigate", screenId: "screen.my-claims" });
    expect(address(dialog)).toHaveTextContent("prototype://screen.new-claim");
    expect(lastView(post)).toMatchObject({ mode: "annotate", screenId: "screen.new-claim", selectedKeys: ["btn.submit"] });
    expect(bubble()).toHaveAccessibleName("Comment on Submit claim");
  });

  it("frames the prototype in a browser window whose address follows the screen", async () => {
    const { dialog } = await openReview();
    const window = within(dialog).getByRole("region", { name: "Acme Expenses prototype" });
    expect(within(window).getByLabelText("Address")).toHaveTextContent("prototype://screen.my-claims");
    expect(within(window).getByTitle("Acme Expenses prototype app")).toBeInTheDocument();

    fromFrame({ type: "proto:navigate", screenId: "screen.new-claim" });
    expect(within(window).getByLabelText("Address")).toHaveTextContent("prototype://screen.new-claim");
  });

  it("keeps the header to the title and Close, and every control in one dock: view, mode, then comments", async () => {
    const { dialog } = await openReview();
    const header = within(dialog).getByRole("heading", { name: "Prototype · Acme Expenses" }).closest("header")!;
    expect(within(header).getAllByRole("button").map((b) => b.getAttribute("aria-label") ?? b.textContent)).toEqual(["Close"]);
    expect(within(header).queryByRole("combobox")).toBeNull();

    const groups = within(dock()).getAllByRole("group");
    expect(groups.map((g) => g.getAttribute("aria-label"))).toEqual(["View", "Mode", "Comments"]);
    const controls = (group: HTMLElement) => [...group.querySelectorAll("button, select")].map((c) => c.getAttribute("aria-label") ?? c.textContent);
    expect(controls(groups[0]!)).toEqual(["Role", "State", "Reset data"]);
    expect(controls(groups[1]!)).toEqual(["Preview", "Comment"]);
    expect(controls(groups[2]!)).toEqual(["0 comments", "Send to agent"]);
    fireEvent.mouseOver(within(dock()).getByRole("button", { name: "Reset data" }));
    expect(await screen.findByRole("tooltip")).toHaveTextContent("Reset data");
  });

  it("has no screen or flow picker: the prototype is navigated by clicking through it", async () => {
    const { dialog } = await openReview();
    expect(within(dialog).queryByLabelText("Screen")).toBeNull();
    expect(within(dialog).queryByLabelText("Flow")).toBeNull();
    expect(within(dialog).queryByRole("button", { name: /go to/i })).toBeNull();
    expect(within(dialog).getAllByRole("combobox").map((c) => c.getAttribute("aria-label"))).toEqual(["Role", "State"]);
  });

  it("queues comments with numbered pins, and removes one", async () => {
    const { dialog, post } = await openReview();
    annotate(dialog);
    clickElement("btn.new-claim");
    addComment("Call it Submit a claim");
    clickElement("btn.new-claim");
    addComment("Too much white space");
    expect(lastView(post)).toMatchObject({ selectedKeys: [], pins: { "btn.new-claim": [1, 2] } });
    expect(bar()).toHaveTextContent("2 comments");
    const queue = commentList();
    expect(within(queue).getAllByRole("listitem")).toHaveLength(2);
    fireEvent.click(within(queue).getByRole("button", { name: "Remove comment 1" }));
    expect(within(queue).getAllByRole("listitem")).toHaveLength(1);
    expect(bar()).toHaveTextContent("1 comment");
    expect(within(bar()).getByRole("button", { name: "Send to agent" })).toBeEnabled();
  });

  it("sends every request as one typed /prototype turn, keeps the review open, and opens the chat", async () => {
    const { dialog } = await openReview();
    toPendingAsManager(dialog);
    annotate(dialog);
    fromFrame({
      type: "proto:rendered",
      screenId: "screen.pending",
      elements: [{ key: "btn.reject", label: "Reject" }, { key: "btn.approve", label: "Approve" }],
    });
    clickElement("btn.reject");
    clickElement("btn.approve", { shift: true });
    addComment("Put Approve on the right");
    fireEvent.change(within(dialog).getByLabelText("State"), { target: { value: "state.empty" } });
    clickElement("empty.pending");
    addComment("Say who to ask when nothing waits");
    expect(bar()).toHaveTextContent("2 comments");
    fireEvent.click(within(bar()).getByRole("button", { name: "Send to agent" }));

    await waitFor(() => expect(openChat).toHaveBeenCalled());
    expect(screen.getByRole("dialog", { name: "Prototype · Acme Expenses" })).toBeInTheDocument();
    expect(send).toHaveBeenCalledWith("acme-expenses", "/prototype expense-web", {
      kind: "prototype",
      feedback: {
        prototypeHash: prototypeHash(SAMPLE_MANIFEST, SAMPLE_SOURCE),
        component: C,
        requests: [
          { screenId: "screen.pending", roleId: "manager", stateId: "state.default", elementIds: ["btn.reject", "btn.approve"], text: "Put Approve on the right" },
          { screenId: "screen.pending", roleId: "manager", stateId: "state.empty", elementIds: ["empty.pending"], text: "Say who to ask when nothing waits" },
        ],
      },
    });
    // The cards see the turn running as soon as the server has it.
    expect(invalidate).toHaveBeenCalledWith({ queryKey: designKey("acme-expenses") });

    // Sent: the queue starts again.
    expect(bar()).toHaveTextContent("0 comments");
  });

  it("records the revision it showed as reviewed in this browser", async () => {
    await openReview();
    const hash = prototypeHash(SAMPLE_MANIFEST, SAMPLE_SOURCE);
    await waitFor(() => expect(unreviewed("acme-expenses", { [C]: hash })).toEqual([]));
  });

  it("refuses Send while a turn is running, says why, and keeps the queue", async () => {
    chat = busy;
    const { dialog } = await openReview();
    annotate(dialog);
    clickElement("btn.new-claim");
    addComment("Too much white space");
    fireEvent.click(within(bar()).getByRole("button", { name: "Send to agent" }));
    expect(within(bar()).getByRole("alert")).toHaveTextContent(/working on another turn.*kept/);
    expect(send).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(within(commentList()).getAllByRole("listitem")).toHaveLength(1);
  });

  it("keeps the queue when the chat does not take the turn, and across closing and opening again", async () => {
    send.mockResolvedValue(false);
    const { dialog } = await openReview();
    annotate(dialog);
    clickElement("btn.new-claim");
    addComment("Too much white space");
    fireEvent.click(within(bar()).getByRole("button", { name: "Send to agent" }));
    expect(await within(bar()).findByRole("alert")).toHaveTextContent(/weren't sent/);
    fireEvent.click(within(dialog).getByRole("button", { name: "Close" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    fireEvent.click(screen.getByRole("button", { name: "Review" }));
    await screen.findByRole("dialog");
    expect(bar()).toHaveTextContent("1 comment");
  });
});

describe("commenting in place", () => {
  const ELEMENTS = [
    { key: "btn.reject", label: "Reject" },
    { key: "btn.approve", label: "Approve" },
  ];

  async function annotating() {
    const opened = await openReview();
    toPendingAsManager(opened.dialog);
    annotate(opened.dialog);
    fromFrame({ type: "proto:rendered", screenId: "screen.pending", elements: ELEMENTS });
    return opened;
  }

  it("opens a comment bubble at the clicked element, with the input focused", async () => {
    await annotating();
    expect(screen.queryByRole("dialog", { name: /^Comment on/ })).toBeNull();
    clickElement("btn.reject");
    expect(bubble()).toHaveAccessibleName("Comment on Reject");
    expect(within(bubble()).getByLabelText("Comment")).toHaveFocus();
  });

  it("places the bubble by the element, where the frame sits in the console, and follows both", async () => {
    await annotating();
    /** The bubble's left edge in the console's viewport, as it is placed (jsdom lays nothing out; the placement is set inline). */
    const bubbleLeft = async () => {
      await act(() => new Promise((resolve) => setTimeout(resolve, 0)));
      const placed = /translate\((-?[\d.]+)px/.exec(bubble().parentElement!.style.transform);
      return Number(placed?.[1]);
    };
    const frameAt = (left: number) =>
      vi.spyOn(frame(), "getBoundingClientRect").mockReturnValue({ left, top: 64, right: left + 800, bottom: 664, width: 800, height: 600, x: left, y: 64, toJSON: () => ({}) });

    frameAt(100);
    clickElement("btn.reject", { box: { x: 40, y: 120, width: 160, height: 36 } });
    expect(await bubbleLeft()).toBe(140);

    // The prototype scrolled or redrew: the frame re-reports where the element is.
    fromFrame({ type: "proto:geometry", boxes: { "btn.reject": { x: 240, y: 20, width: 160, height: 36 } } });
    expect(await bubbleLeft()).toBe(340);

    // The console's window resized and the frame moved with it.
    frameAt(20);
    act(() => {
      window.dispatchEvent(new Event("resize"));
    });
    expect(await bubbleLeft()).toBe(260);
  });

  it("opens nothing in Preview: a click there acts", async () => {
    await openReview();
    clickElement("btn.new-claim");
    expect(screen.queryByRole("dialog", { name: /^Comment on/ })).toBeNull();
  });

  it("adds Shift-clicked elements to the open comment and names them all; a plain click starts another", async () => {
    const { post } = await annotating();
    clickElement("btn.reject");
    fireEvent.change(within(bubble()).getByLabelText("Comment"), { target: { value: "Swap these" } });
    clickElement("btn.approve", { shift: true });
    expect(bubble()).toHaveAccessibleName("Comment on Reject, Approve");
    expect(within(bubble()).getByLabelText("Comment")).toHaveValue("Swap these");
    expect(lastView(post).selectedKeys).toEqual(["btn.reject", "btn.approve"]);

    clickElement("btn.approve");
    expect(bubble()).toHaveAccessibleName("Comment on Approve");
    expect(lastView(post).selectedKeys).toEqual(["btn.approve"]);
  });

  it("queues the comment with Add, closes the bubble and leaves its numbered pin", async () => {
    const { post } = await annotating();
    clickElement("btn.reject");
    expect(within(bubble()).getByRole("button", { name: "Add" })).toBeDisabled();
    addComment("Ask for a reason");
    expect(screen.queryByRole("dialog", { name: /^Comment on/ })).toBeNull();
    expect(lastView(post)).toMatchObject({ selectedKeys: [], pins: { "btn.reject": [1] } });
    expect(within(commentList()).getByRole("listitem")).toHaveTextContent("Ask for a reason");
  });

  it.each([
    ["Cmd", { metaKey: true }],
    ["Ctrl", { ctrlKey: true }],
  ])("queues the comment with %s+Enter", async (_name, modifier) => {
    const { post } = await annotating();
    clickElement("btn.approve");
    const input = within(bubble()).getByLabelText("Comment");
    fireEvent.change(input, { target: { value: "Make it green" } });
    fireEvent.keyDown(input, { key: "Enter", ...modifier });
    expect(screen.queryByRole("dialog", { name: /^Comment on/ })).toBeNull();
    expect(lastView(post).pins).toEqual({ "btn.approve": [1] });
  });

  it("closes the bubble with Escape, then clears the selection, then closes the review", async () => {
    const { dialog, post } = await annotating();
    clickElement("btn.reject");
    fireEvent.keyDown(within(bubble()).getByLabelText("Comment"), { key: "Escape" });
    expect(screen.queryByRole("dialog", { name: /^Comment on/ })).toBeNull();
    expect(lastView(post).selectedKeys).toEqual(["btn.reject"]);
    fireEvent.keyDown(dialog, { key: "Escape" });
    expect(lastView(post).selectedKeys).toEqual([]);
    expect(screen.getByRole("dialog", { name: "Prototype · Acme Expenses" })).toBeInTheDocument();
    fireEvent.keyDown(dialog, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("reopens the bubble on a selection kept after it closed, when an element is Shift-clicked", async () => {
    await annotating();
    clickElement("btn.reject");
    fireEvent.keyDown(within(bubble()).getByLabelText("Comment"), { key: "Escape" });
    clickElement("btn.approve", { shift: true });
    expect(bubble()).toHaveAccessibleName("Comment on Reject, Approve");
  });

  it("holds a comment to the length limit and counts down near it", async () => {
    await annotating();
    clickElement("btn.reject");
    const input = within(bubble()).getByLabelText("Comment");
    expect(input).toHaveAttribute("maxlength", String(MAX_FEEDBACK_TEXT));
    expect(within(bubble()).queryByText(`10 / ${MAX_FEEDBACK_TEXT}`)).toBeNull();
    fireEvent.change(input, { target: { value: "x".repeat(MAX_FEEDBACK_TEXT - 10) } });
    expect(within(bubble()).getByText(`${MAX_FEEDBACK_TEXT - 10} / ${MAX_FEEDBACK_TEXT}`)).toBeInTheDocument();
  });

  it("refuses another comment once the queue is full, and says why", async () => {
    await annotating();
    for (let i = 0; i < MAX_FEEDBACK_REQUESTS; i++) {
      clickElement("btn.reject");
      addComment(`Comment ${i + 1}`);
    }
    clickElement("btn.approve");
    fireEvent.change(within(bubble()).getByLabelText("Comment"), { target: { value: "One more" } });
    expect(within(bubble()).getByRole("button", { name: "Add" })).toBeDisabled();
    expect(within(bar()).getByRole("note")).toHaveTextContent(`The queue is full (${MAX_FEEDBACK_REQUESTS} comments)`);
    commentList();
    expect(within(bar()).getByRole("button", { name: "Comment on this screen" })).toBeDisabled();
  });
});

describe("the send bar", () => {
  it("replaces the side panel: the prototype takes the full width, and the bar counts and sends", async () => {
    const { dialog } = await openReview();
    annotate(dialog);
    expect(within(dialog).queryByRole("complementary")).toBeNull();
    expect(bar()).toHaveTextContent("0 comments");
    expect(within(bar()).getByRole("button", { name: "Send to agent" })).toBeDisabled();
    clickElement("btn.new-claim");
    addComment("Call it Submit a claim");
    expect(bar()).toHaveTextContent("1 comment");
    expect(within(bar()).getByRole("button", { name: "Send to agent" })).toBeEnabled();
  });

  it("comments on the whole screen from the list for the keyboard, even from Preview, and leaves no pin", async () => {
    const { dialog, post } = await openReview();
    commentOnScreen();
    expect(within(dialog).getByRole("button", { name: "Comment" })).toHaveAttribute("aria-pressed", "true");
    expect(bubble()).toHaveAccessibleName("Comment on My claims (whole screen)");
    expect(within(bubble()).getByLabelText("Comment")).toHaveFocus();
    addComment("Too busy overall");
    expect(screen.queryByRole("dialog", { name: /^Comment on/ })).toBeNull();
    expect(lastView(post).pins).toEqual({});
    expect(lastView(post)).not.toHaveProperty("screenPins");
    expect(within(commentList()).getByRole("listitem")).toHaveTextContent(/Too busy overall.*Whole screen/);

    fireEvent.click(within(bar()).getByRole("button", { name: "Send to agent" }));
    await waitFor(() => expect(send).toHaveBeenCalled());
    expect(send.mock.calls[0]![2]).toMatchObject({
      feedback: { requests: [{ screenId: "screen.my-claims", roleId: "employee", stateId: "state.default", elementIds: [], text: "Too busy overall" }] },
    });
  });

  it("opens nothing on a toggle that names no element", async () => {
    const { dialog } = await openReview();
    annotate(dialog);
    fromFrame({ type: "proto:toggle", elementKey: "" });
    expect(screen.queryByRole("dialog", { name: /^Comment on/ })).toBeNull();
  });

  it("lists every comment across screens, roles and states, by its elements' labels; an entry goes there and opens its bubble", async () => {
    const { dialog } = await openReview();
    toPendingAsManager(dialog);
    fireEvent.change(within(dialog).getByLabelText("State"), { target: { value: "state.empty" } });
    annotate(dialog);
    fromFrame({ type: "proto:rendered", screenId: "screen.pending", elements: [{ key: "btn.reject", label: "Reject" }] });
    clickElement("btn.reject");
    addComment("Ask for a reason");
    fireEvent.change(within(dialog).getByLabelText("Role"), { target: { value: "employee" } });
    fireEvent.change(within(dialog).getByLabelText("State"), { target: { value: "state.default" } });
    commentOnScreen();
    addComment("Too busy overall");

    const entries = within(commentList()).getAllByRole("listitem");
    expect(entries.map((e) => e.textContent)).toEqual([
      expect.stringMatching(/Ask for a reason.*Pending approvals · Manager · Nothing to show · Reject/),
      expect.stringMatching(/Too busy overall.*My claims · Employee · Default · Whole screen/),
    ]);

    fireEvent.click(within(entries[0]!).getByRole("button", { name: /Ask for a reason/ }));
    expect(address(dialog)).toHaveTextContent("prototype://screen.pending");
    expect((within(dialog).getByLabelText("Role") as HTMLSelectElement).value).toBe("manager");
    expect((within(dialog).getByLabelText("State") as HTMLSelectElement).value).toBe("state.empty");
    fromFrame({ type: "proto:geometry", boxes: { "btn.reject": BOX } });
    const opened = screen.getByRole("dialog", { name: "Comment 1" });
    expect(opened).toHaveTextContent("Ask for a reason");

    // A whole-screen comment opens at the bar.
    fireEvent.click(within(commentList()).getByRole("button", { name: /Too busy overall/ }));
    expect(address(dialog)).toHaveTextContent("prototype://screen.my-claims");
    expect(screen.getByRole("dialog", { name: "Comment 2" })).toHaveTextContent("Too busy overall");
    expect(screen.queryByRole("dialog", { name: "Comment 1" })).toBeNull();
  });

  it("switches between the Preview and Comment tools by button, V and C, and shows Comment mode only while in it", async () => {
    const { dialog, post } = await openReview();
    const preview = within(dialog).getByRole("button", { name: "Preview" });
    const comment = within(dialog).getByRole("button", { name: "Comment" });
    const signals = () => [within(dialog).queryByText("Comment mode"), within(dock()).queryByText("Click anything to comment")];
    expect(preview).toHaveAttribute("aria-pressed", "true");
    expect(signals()).toEqual([null, null]);

    fireEvent.mouseOver(comment);
    expect(await screen.findByRole("tooltip")).toHaveTextContent(/Comment.*C/);
    fireEvent.click(comment);
    expect(comment).toHaveAttribute("aria-pressed", "true");
    expect(preview).toHaveAttribute("aria-pressed", "false");
    expect(lastView(post).mode).toBe("annotate");
    for (const signal of signals()) expect(signal).toBeVisible();

    fireEvent.keyDown(dialog, { key: "v" });
    expect(preview).toHaveAttribute("aria-pressed", "true");
    expect(lastView(post).mode).toBe("preview");
    expect(signals()).toEqual([null, null]);
    // V stays in Preview; C toggles Comment.
    fireEvent.keyDown(dialog, { key: "V" });
    expect(preview).toHaveAttribute("aria-pressed", "true");
    fireEvent.keyDown(dialog, { key: "c" });
    expect(comment).toHaveAttribute("aria-pressed", "true");
    fireEvent.click(preview);
    expect(preview).toHaveAttribute("aria-pressed", "true");
    expect(signals()).toEqual([null, null]);
  });

  it("says how to start when there are no comments", async () => {
    await openReview();
    fireEvent.click(within(bar()).getByRole("button", { name: "0 comments" }));
    expect(within(bar()).getByRole("list", { name: "Queued comments" })).toHaveTextContent(
      "No comments yet. Press C or choose Comment, then click anything on the screen: an element, or empty space for the whole screen.",
    );
    expect(within(bar()).getByRole("button", { name: "Comment on this screen" })).toBeEnabled();
  });

  it("does not go to Preview with V while typing", async () => {
    const { dialog } = await openReview();
    annotate(dialog);
    clickElement("btn.new-claim");
    fireEvent.keyDown(within(bubble()).getByLabelText("Comment"), { key: "v" });
    expect(within(dialog).getByRole("button", { name: "Comment" })).toHaveAttribute("aria-pressed", "true");
  });

  it("toggles Comment mode with C, but not while typing", async () => {
    const { dialog } = await openReview();
    const mode = () => within(dialog).getByRole("button", { name: "Comment" }).getAttribute("aria-pressed");
    fireEvent.keyDown(dialog, { key: "c" });
    expect(mode()).toBe("true");
    clickElement("btn.new-claim");
    fireEvent.keyDown(within(bubble()).getByLabelText("Comment"), { key: "c" });
    expect(mode()).toBe("true");
    fireEvent.keyDown(dialog, { key: "C" });
    expect(mode()).toBe("false");
    fireEvent.keyDown(dialog, { key: "c", metaKey: true });
    expect(mode()).toBe("false");
  });
});

/** A click in the console away from the open bubble, once it has settled in (not the very event that opened it). */
async function clickAway(dialog: HTMLElement) {
  await act(() => new Promise((resolve) => setTimeout(resolve, 0)));
  fireEvent.mouseDown(within(dialog).getByText("Prototype · Acme Expenses"));
  fireEvent.click(within(dialog).getByText("Prototype · Acme Expenses"));
}

/** A click on a pin in the prototype, as the frame reports it: its element and the comment numbers it shows (none: the draft pin). */
function clickPin(key: string, requests: number[]) {
  fromFrame({ type: "proto:pin", key, requests, box: BOX });
}

/** What the host last asked the frame to focus. */
function lastFocus(post: { mock: { calls: unknown[][] } }) {
  return post.mock.calls.map((c) => c[0] as { type: string }).filter((m) => m.type === "proto:focus").at(-1);
}

describe("pins and drafts", () => {
  const ELEMENTS = [
    { key: "btn.reject", label: "Reject" },
    { key: "btn.approve", label: "Approve" },
  ];

  async function annotating() {
    const opened = await openReview();
    toPendingAsManager(opened.dialog);
    annotate(opened.dialog);
    fromFrame({ type: "proto:rendered", screenId: "screen.pending", elements: ELEMENTS });
    return opened;
  }

  /** The review opened again after it closed, as the Manager on the pending approvals. */
  async function reopen() {
    fireEvent.click(screen.getByRole("button", { name: "Review" }));
    const again = await screen.findByRole("dialog");
    await waitFor(() => expect(frame()).toBeInTheDocument());
    const post = vi.spyOn(frame().contentWindow!, "postMessage");
    fromFrame({ type: "proto:ready" });
    await waitFor(() => expect(post).toHaveBeenCalledWith(expect.objectContaining({ type: "proto:load" }), "*"));
    toPendingAsManager(again);
    return post;
  }

  function type(text: string) {
    fireEvent.change(within(bubble()).getByLabelText("Comment"), { target: { value: text } });
  }

  function escapeBubble() {
    fireEvent.keyDown(within(bubble()).getByLabelText("Comment"), { key: "Escape" });
  }

  it("opens a queued comment from its pin, and edits it in place", async () => {
    const { post } = await annotating();
    clickElement("btn.reject");
    addComment("Ask for a reason");
    clickPin("btn.reject", [1]);

    const opened = screen.getByRole("dialog", { name: "Comment 1" });
    expect(opened).toHaveTextContent("Ask for a reason");
    fireEvent.click(within(opened).getByRole("button", { name: "Edit" }));
    fireEvent.change(within(opened).getByLabelText("Comment"), { target: { value: "Ask why, in a sentence" } });
    fireEvent.click(within(opened).getByRole("button", { name: "Save" }));

    expect(screen.getByRole("dialog", { name: "Comment 1" })).toHaveTextContent("Ask why, in a sentence");
    expect(within(commentList()).getByRole("listitem")).toHaveTextContent("Ask why, in a sentence");
    expect(lastView(post).pins).toEqual({ "btn.reject": [1] });
  });

  it("opens a pin in Preview too, and Remove drops the comment and renumbers the rest", async () => {
    const { dialog, post } = await annotating();
    clickElement("btn.reject");
    addComment("Ask for a reason");
    clickElement("btn.approve");
    addComment("Make it green");
    fireEvent.click(within(dialog).getByRole("button", { name: "Preview" }));

    clickPin("btn.reject", [1]);
    expect(lastView(post).mode).toBe("preview");
    fireEvent.click(within(screen.getByRole("dialog", { name: "Comment 1" })).getByRole("button", { name: "Remove" }));
    expect(screen.queryByRole("dialog", { name: /^Comment \d/ })).toBeNull();
    expect(lastView(post).pins).toEqual({ "btn.approve": [1] });
    expect(lastFocus(post)).toEqual({ type: "proto:focus", key: "btn.reject" });
    clickPin("btn.approve", [1]);
    expect(screen.getByRole("dialog", { name: "Comment 1" })).toHaveTextContent("Make it green");
  });

  it("closes an opened comment with Escape and puts focus back on its pin", async () => {
    const { post } = await annotating();
    clickElement("btn.reject");
    addComment("Ask for a reason");
    clickPin("btn.reject", [1]);
    fireEvent.keyDown(screen.getByRole("dialog", { name: "Comment 1" }), { key: "Escape" });
    expect(screen.queryByRole("dialog", { name: "Comment 1" })).toBeNull();
    expect(screen.getByRole("dialog", { name: "Prototype · Acme Expenses" })).toBeInTheDocument();
    expect(lastFocus(post)).toEqual({ type: "proto:focus", key: "btn.reject", requests: [1] });
  });

  it("puts focus back on the element when a new comment closes with Escape or is added", async () => {
    const { post } = await annotating();
    clickElement("btn.approve");
    escapeBubble();
    expect(lastFocus(post)).toEqual({ type: "proto:focus", key: "btn.approve" });
    clickElement("btn.reject");
    addComment("Ask for a reason");
    expect(lastFocus(post)).toEqual({ type: "proto:focus", key: "btn.reject" });
  });

  it("puts focus back on the element after a click away that left it nowhere, but not when the click took it", async () => {
    const { dialog, post } = await annotating();
    clickElement("btn.approve");
    const focuses = () => post.mock.calls.filter((c) => (c[0] as { type: string }).type === "proto:focus").length;
    const before = focuses();
    // A click on the dialog's own text takes no focus: what had it (the bubble's input) is going away.
    await clickAway(dialog);
    expect(screen.queryByRole("dialog", { name: /^Comment on/ })).toBeNull();
    expect(lastFocus(post)).toEqual({ type: "proto:focus", key: "btn.approve" });

    expect(focuses()).toBe(before + 1);
    clickElement("btn.reject");
    const reopened = focuses();
    // A click on a control takes focus there; the bubble leaves it be.
    const comment = within(dock()).getByRole("button", { name: "Reset data" });
    await act(() => new Promise((resolve) => setTimeout(resolve, 0)));
    comment.focus();
    fireEvent.mouseDown(within(dialog).getByText("Prototype · Acme Expenses"));
    fireEvent.click(within(dialog).getByText("Prototype · Acme Expenses"));
    expect(screen.queryByRole("dialog", { name: /^Comment on/ })).toBeNull();
    expect(focuses()).toBe(reopened);
    expect(document.activeElement).toBe(comment);
  });

  it("keeps typed text as a draft on a click away, with a hollow pin that reopens it", async () => {
    const { dialog, post } = await annotating();
    clickElement("btn.reject");
    type("Half a thought");
    await clickAway(dialog);
    expect(screen.queryByRole("dialog", { name: /^Comment on/ })).toBeNull();
    expect(lastView(post).pins).toEqual({});
    expect(lastView(post).drafts).toEqual(["btn.reject"]);
    expect(within(bar()).queryByRole("button", { name: /^[1-9]\d* comments?$/ })).toBeNull();

    clickPin("btn.reject", []);
    expect(bubble()).toHaveAccessibleName("Comment on Reject");
    expect(within(bubble()).getByLabelText("Comment")).toHaveValue("Half a thought");
  });

  it("restores the draft when its element is clicked again, and reopens it from Preview in Comment mode", async () => {
    const { dialog } = await annotating();
    clickElement("btn.reject");
    type("Half a thought");
    escapeBubble();
    clickElement("btn.reject");
    expect(within(bubble()).getByLabelText("Comment")).toHaveValue("Half a thought");

    escapeBubble();
    fireEvent.click(within(dialog).getByRole("button", { name: "Preview" }));
    clickPin("btn.reject", []);
    expect(within(dialog).getByRole("button", { name: "Comment" })).toHaveAttribute("aria-pressed", "true");
    expect(within(bubble()).getByLabelText("Comment")).toHaveValue("Half a thought");
  });

  it("keeps the text as a draft when a plain click moves to another element, and Shift-click carries it", async () => {
    const { post } = await annotating();
    clickElement("btn.reject");
    type("About Reject");
    clickElement("btn.approve");
    expect(bubble()).toHaveAccessibleName("Comment on Approve");
    expect(within(bubble()).getByLabelText("Comment")).toHaveValue("");
    expect(lastView(post).drafts).toEqual(["btn.reject"]);

    type("Both of them");
    clickElement("btn.reject", { shift: true });
    expect(bubble()).toHaveAccessibleName("Comment on Approve, Reject");
    expect(within(bubble()).getByLabelText("Comment")).toHaveValue("Both of them");
  });

  it("just closes an empty bubble, leaving no draft", async () => {
    const { dialog, post } = await annotating();
    clickElement("btn.reject");
    await clickAway(dialog);
    expect(screen.queryByRole("dialog", { name: /^Comment on/ })).toBeNull();
    expect(lastView(post).drafts).toBeUndefined();
  });

  it("drops the draft when it is added as a comment", async () => {
    const { post } = await annotating();
    clickElement("btn.reject");
    type("Half a thought");
    escapeBubble();
    clickElement("btn.reject");
    addComment("A whole thought");
    expect(lastView(post).drafts).toBeUndefined();
    expect(lastView(post).pins).toEqual({ "btn.reject": [1] });
  });

  it("never counts or sends a draft, and keeps it once the rest is sent", async () => {
    const { dialog, post } = await annotating();
    clickElement("btn.approve");
    addComment("Make it green");
    clickElement("btn.reject");
    type("Half a thought");
    await clickAway(dialog);
    expect(within(bar()).getByRole("button", { name: "1 comment" })).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: /^Send/ }));

    await waitFor(() => expect(send).toHaveBeenCalled());
    const [, , turn] = send.mock.calls[0] as [string, string, { feedback: { requests: { text: string }[] } }];
    expect(turn.feedback.requests.map((r) => r.text)).toEqual(["Make it green"]);

    await waitFor(() => expect(lastView(post).pins).toEqual({}));
    expect(lastView(post).drafts).toEqual(["btn.reject"]);
    expect(screen.getByRole("dialog", { name: "Prototype · Acme Expenses" })).toBeInTheDocument();
  });

  it("keeps drafts, and the text left open, across closing and opening the review again", async () => {
    const { dialog } = await annotating();
    clickElement("btn.approve");
    type("About Approve");
    escapeBubble();
    clickElement("btn.reject");
    type("About Reject");
    fireEvent.click(within(dialog).getByRole("button", { name: "Close" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    const post = await reopen();
    expect(lastView(post).drafts).toEqual(["btn.approve", "btn.reject"]);
  });
});

describe("the revision landing in the open review", () => {
  const PROJECT = "acme-expenses";
  const REVISED_SOURCE = SAMPLE_SOURCE.replace("Revising", "Revised") + "\n// revised\n";
  /** The sample without the New claim screen (and the submit flow through it). */
  const WITHOUT_NEW_CLAIM = (() => {
    const m = JSON.parse(SAMPLE_MANIFEST) as { screens: { id: string }[]; flows: { id: string }[] };
    m.screens = m.screens.filter((x) => x.id !== "screen.new-claim");
    m.flows = m.flows.filter((f) => f.id !== "flow.submit");
    return JSON.stringify(m, null, 2);
  })();

  /** The agent took the turn: the chat runs `/prototype expense-web` and the room's files are what it has written so far. */
  function revising(room: Record<string, string> = files) {
    chat = { ...idle, turn: { phase: "running", turnId: "t2", instruction: "/prototype expense-web" } };
    prototypes = appPrototypes([C], room, revisingIn("/prototype expense-web"));
    rerender();
  }

  /** The turn ended (`outcome`, as the chat store reports it), leaving the room's files as given. */
  function ended(room: Record<string, string>, outcome: TurnOutcome = "completed") {
    act(() => {
      for (const fn of turnEnds) fn(PROJECT, outcome);
    });
    chat = idle;
    prototypes = appPrototypes([C], room, null);
    rerender();
  }

  const revised = (source = REVISED_SOURCE, manifest = SAMPLE_MANIFEST) => ({ [manifestPath(C)]: manifest, [sourcePath(C)]: source });

  /** What the host last loaded into the frame. */
  function lastLoad(post: { mock: { calls: unknown[][] } }) {
    return post.mock.calls.map((c) => c[0] as { type: string; source?: string; version?: string; data?: unknown; view?: { screenId: string } }).filter((m) => m.type === "proto:load").at(-1)!;
  }

  /** Comments `texts` on the pending approvals' Reject, sent; the agent takes the turn. */
  async function sent(texts: string[] = ["Ask for a reason"]) {
    const opened = await openReview();
    toPendingAsManager(opened.dialog);
    annotate(opened.dialog);
    for (const text of texts) {
      clickElement("btn.reject");
      addComment(text);
    }
    fireEvent.click(within(bar()).getByRole("button", { name: "Send to agent" }));
    await waitFor(() => expect(send).toHaveBeenCalled());
    revising();
    return opened;
  }

  it("says the agent is revising on the Send button itself, waits to send, and holds comments written meanwhile", async () => {
    await sent(["Ask for a reason", "Make it red"]);
    // The button carries the state: no second label beside it.
    const busy = within(bar()).getByRole("button", { name: /Revising…/ });
    expect(busy).toBeDisabled();
    expect(within(bar()).queryByRole("button", { name: "Send to agent" })).toBeNull();
    // Read out in full to a screen reader.
    expect(bar()).toHaveTextContent("Agent is revising… (2 comments)");

    clickElement("btn.approve");
    addComment("Make it green");
    expect(bar()).toHaveTextContent("1 comment");
    expect(within(bar()).getByRole("button", { name: /Revising…/ })).toBeDisabled();
    expect(send).toHaveBeenCalledTimes(1);
  });

  it("keeps showing the revision it had while the agent writes the next one", async () => {
    const { post } = await sent();
    const loads = post.mock.calls.filter((c) => (c[0] as { type: string }).type === "proto:load").length;
    revising({ [manifestPath(C)]: SAMPLE_MANIFEST, [sourcePath(C)]: "// half written" });
    revising({ [manifestPath(C)]: SAMPLE_MANIFEST });
    expect(screen.getByTitle("Acme Expenses prototype app")).toBeInTheDocument();
    expect(post.mock.calls.filter((c) => (c[0] as { type: string }).type === "proto:load")).toHaveLength(loads);
  });

  it("swaps the landed revision in on the same screen, its data from the seed, and says so with What changed", async () => {
    const { post } = await sent(["Ask for a reason", "Make it red"]);
    ended(revised());

    await waitFor(() => expect(lastLoad(post).source).toBe(REVISED_SOURCE));
    expect(lastLoad(post)).toMatchObject({ data: undefined, view: { screenId: "screen.pending" } });
    expect(bar()).not.toHaveTextContent("Agent is revising");
    expect(within(bar()).getByRole("button", { name: "Send to agent" })).toBeDisabled();
    const toast = screen.getByRole("alert");
    expect(toast).toHaveTextContent("Updated · 2 comments addressed");

    fireEvent.click(within(toast).getByRole("button", { name: "What changed" }));
    expect(openChat).toHaveBeenCalled();
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("falls back to the role's entry screen when the revision took the screen away", async () => {
    const { dialog, post } = await openReview();
    fromFrame({ type: "proto:navigate", screenId: "screen.new-claim" });
    commentOnScreen();
    addComment("Too long a form");
    fireEvent.click(within(bar()).getByRole("button", { name: "Send to agent" }));
    await waitFor(() => expect(send).toHaveBeenCalled());
    revising();
    ended(revised(REVISED_SOURCE, WITHOUT_NEW_CLAIM));

    await waitFor(() => expect(lastLoad(post).source).toBe(REVISED_SOURCE));
    expect(lastLoad(post).view).toMatchObject({ screenId: "screen.my-claims" });
    expect(address(dialog)).toHaveTextContent("prototype://screen.my-claims");
  });

  it("flags held comments whose element the revision took away, to remove or keep on the screen", async () => {
    await sent();
    clickElement("btn.reject");
    addComment("Reason is required");
    clickElement("btn.approve");
    addComment("Make it green");
    clickElement("btn.approve");
    addComment("And bigger");
    ended(revised());
    fromFrame({ type: "proto:rendered", screenId: "screen.pending", elements: [{ key: "btn.approve", label: "Approve" }] });

    expect(bar()).toHaveTextContent("1 comment points at an element no longer on this screen.");
    let entries = within(commentList()).getAllByRole("listitem");
    expect(entries[0]).toHaveTextContent("Element no longer on this screen");
    expect(entries[1]).not.toHaveTextContent("Element no longer on this screen");

    fireEvent.click(within(entries[0]!).getByRole("button", { name: "Keep as screen comment" }));
    entries = within(commentList()).getAllByRole("listitem");
    expect(entries[0]).toHaveTextContent(/Reason is required.*Whole screen/);
    expect(entries[0]).not.toHaveTextContent("Element no longer on this screen");

    // Another goes the same way and is removed.
    ended(revised(REVISED_SOURCE + "\n// again\n"));
    fromFrame({ type: "proto:rendered", screenId: "screen.pending", elements: [] });
    entries = within(commentList()).getAllByRole("listitem");
    expect(entries.filter((e) => e.textContent?.includes("Element no longer on this screen"))).toHaveLength(2);
    fireEvent.click(within(entries[1]!).getByRole("button", { name: "Remove comment 2" }));
    expect(bar()).toHaveTextContent("2 comments");
  });

  it("sends comments held during the revision on the revision that landed", async () => {
    await sent();
    clickElement("btn.approve");
    addComment("Make it green");
    ended(revised());
    fromFrame({ type: "proto:rendered", screenId: "screen.pending", elements: [{ key: "btn.approve", label: "Approve" }] });

    fireEvent.click(within(bar()).getByRole("button", { name: "Send to agent" }));
    await waitFor(() => expect(send).toHaveBeenCalledTimes(2));
    expect(send.mock.calls[1]![2]).toMatchObject({
      feedback: { prototypeHash: prototypeHash(SAMPLE_MANIFEST, REVISED_SOURCE), requests: [{ elementIds: ["btn.approve"], text: "Make it green" }] },
    });
  });

  it("marks only the comments written on an earlier version", async () => {
    await sent();
    clickElement("btn.approve");
    addComment("Make it green");
    ended(revised());
    fromFrame({ type: "proto:rendered", screenId: "screen.pending", elements: [{ key: "btn.approve", label: "Approve" }] });
    clickElement("btn.approve");
    addComment("And bigger");

    expect(bar()).toHaveTextContent("1 comment was written on an earlier version of the prototype.");
    const entries = within(commentList()).getAllByRole("listitem");
    expect(entries[0]).toHaveTextContent("Written on an earlier version");
    expect(entries[1]).not.toHaveTextContent("Written on an earlier version");
  });

  it("flags no orphan until the frame reports what the landed revision draws", async () => {
    await sent();
    clickElement("btn.approve");
    addComment("Make it green");
    // The last the frame said of the old revision: no Approve.
    fromFrame({ type: "proto:rendered", screenId: "screen.pending", elements: [{ key: "btn.reject", label: "Reject" }] });
    ended(revised());
    expect(bar()).not.toHaveTextContent("no longer on this screen");

    fromFrame({ type: "proto:rendered", screenId: "screen.pending", elements: [{ key: "btn.reject", label: "Reject" }] });
    expect(bar()).toHaveTextContent("1 comment points at an element no longer on this screen.");
  });

  it("flags no orphan from a report of the revision before, arriving after the swap", async () => {
    const { post } = await sent();
    clickElement("btn.approve");
    addComment("Make it green");
    ended(revised());
    await waitFor(() => expect(lastLoad(post)).toMatchObject({ source: REVISED_SOURCE, version: prototypeHash(SAMPLE_MANIFEST, REVISED_SOURCE) }));

    // Drawn by the old revision before the frame loaded the new one: no Approve.
    fromFrame({ type: "proto:rendered", version: prototypeHash(SAMPLE_MANIFEST, SAMPLE_SOURCE), screenId: "screen.pending", elements: [{ key: "btn.reject", label: "Reject" }] });
    expect(bar()).not.toHaveTextContent("no longer on this screen");

    fromFrame({ type: "proto:rendered", version: prototypeHash(SAMPLE_MANIFEST, REVISED_SOURCE), screenId: "screen.pending", elements: [{ key: "btn.reject", label: "Reject" }] });
    expect(bar()).toHaveTextContent("1 comment points at an element no longer on this screen.");
  });

  it("puts the sent comments back with the reason and Retry when the turn fails, and keeps the revision showing", async () => {
    const { post } = await sent(["Ask for a reason"]);
    clickElement("btn.approve");
    addComment("Make it green");
    const loads = post.mock.calls.filter((c) => (c[0] as { type: string }).type === "proto:load").length;
    ended(files, "failed");

    expect(within(bar()).getByRole("alert")).toHaveTextContent(/wasn't updated.*Your comments are back/);
    expect(within(commentList()).getAllByRole("listitem").map((e) => e.textContent)).toEqual([
      expect.stringContaining("Ask for a reason"),
      expect.stringContaining("Make it green"),
    ]);
    expect(post.mock.calls.filter((c) => (c[0] as { type: string }).type === "proto:load")).toHaveLength(loads);
    expect(screen.queryByText(/^Updated/)).toBeNull();

    fireEvent.click(within(bar()).getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(send).toHaveBeenCalledTimes(2));
    expect((send.mock.calls[1]![2] as { feedback: { requests: { text: string }[] } }).feedback.requests.map((r) => r.text)).toEqual([
      "Ask for a reason",
      "Make it green",
    ]);
  });

  it("shows what a failed turn wrote before it stopped, and gives the comments back on it", async () => {
    const { post } = await sent(["Ask for a reason"]);
    clickElement("btn.approve");
    addComment("Make it green");
    ended(revised(), "failed");

    await waitFor(() => expect(lastLoad(post).source).toBe(REVISED_SOURCE));
    expect(within(bar()).getByRole("alert")).toHaveTextContent(/stopped partway, so you're seeing the changes it made before it stopped\. Your comments are back/);
    expect(bar()).toHaveTextContent("2 comments were written on an earlier version of the prototype.");

    fireEvent.click(within(bar()).getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(send).toHaveBeenCalledTimes(2));
    expect(send.mock.calls[1]![2]).toMatchObject({ feedback: { prototypeHash: prototypeHash(SAMPLE_MANIFEST, REVISED_SOURCE) } });
  });

  it("keeps the places of the comments out with the agent: none can be added past the limit while it revises", async () => {
    await sent(Array.from({ length: MAX_FEEDBACK_REQUESTS }, (_, i) => `Comment ${i + 1}`));
    clickElement("btn.approve");
    fireEvent.change(within(bubble()).getByLabelText("Comment"), { target: { value: "One more" } });
    expect(within(bubble()).getByRole("button", { name: "Add" })).toBeDisabled();
    expect(within(bar()).getByRole("note")).toHaveTextContent(`The queue is full (${MAX_FEEDBACK_REQUESTS} comments`);
    commentList();
    expect(within(bar()).getByRole("button", { name: "Comment on this screen" })).toBeDisabled();
  });

  it("gives a failed batch back with the comments held meanwhile as one batch Retry can send", async () => {
    await sent(["Ask for a reason"]);
    for (let i = 0; i < MAX_FEEDBACK_REQUESTS - 1; i++) {
      clickElement("btn.approve");
      addComment(`Held ${i + 1}`);
    }
    clickElement("btn.approve");
    fireEvent.change(within(bubble()).getByLabelText("Comment"), { target: { value: "One more" } });
    expect(within(bubble()).getByRole("button", { name: "Add" })).toBeDisabled();
    ended(files, "failed");

    expect(bar()).toHaveTextContent(`${MAX_FEEDBACK_REQUESTS} comments`);
    fireEvent.click(within(bar()).getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(send).toHaveBeenCalledTimes(2));
    expect((send.mock.calls[1]![2] as { feedback: { requests: unknown[] } }).feedback.requests).toHaveLength(MAX_FEEDBACK_REQUESTS);
  });

  it("keeps the last good revision showing when the new one is invalid, and gives the comments back", async () => {
    await sent(["Ask for a reason"]);
    ended({ [manifestPath(C)]: "{" , [sourcePath(C)]: REVISED_SOURCE });

    expect(screen.getByTitle("Acme Expenses prototype app")).toBeInTheDocument();
    expect(within(bar()).getByRole("alert")).toHaveTextContent(/prototype\.json is invalid/);
    expect(bar()).toHaveTextContent("1 comment");
    expect(within(bar()).getByRole("button", { name: "Retry" })).toBeEnabled();
  });

  it("shows the revising state when opened again mid-revision, then the landed revision", async () => {
    const { dialog } = await sent(["Ask for a reason"]);
    fireEvent.click(within(dialog).getByRole("button", { name: "Close" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    fireEvent.click(screen.getByRole("button", { name: "Review" }));
    await screen.findByRole("dialog");
    expect(bar()).toHaveTextContent("Agent is revising… (1 comment)");
    ended(revised());
    expect(screen.getByRole("alert")).toHaveTextContent("Updated · 1 comment addressed");
  });
});

describe("leaving the Prototype tab mid-revision", () => {
  /** The tab goes away (another tab of the project, or another page), and comes back later, with the review open. */
  function leave() {
    cleanup();
  }
  async function comeBack() {
    render(<Harness initial={C} />);
    return screen.findByRole("dialog");
  }

  async function sentAndLeft() {
    const { dialog } = await openReview();
    annotate(dialog);
    clickElement("btn.reject");
    addComment("Ask for a reason");
    fireEvent.click(within(bar()).getByRole("button", { name: "Send to agent" }));
    await waitFor(() => expect(send).toHaveBeenCalled());
    chat = { ...idle, turn: { phase: "running", turnId: "t2", instruction: "/prototype expense-web" } };
    prototypes = appPrototypes([C], files, revisingIn("/prototype expense-web"));
    rerender();
    leave();
  }

  it("shows the batch still out with the agent when the tab comes back mid-turn", async () => {
    await sentAndLeft();
    await comeBack();
    expect(bar()).toHaveTextContent("Agent is revising… (1 comment)");
  });

  it("gives a batch whose turn failed while the tab was away back, with the reason, when it comes back", async () => {
    await sentAndLeft();
    act(() => {
      for (const fn of turnEnds) fn("acme-expenses", "failed");
    });
    chat = idle;
    prototypes = appPrototypes([C], files, null);
    await comeBack();

    expect(within(bar()).getByRole("alert")).toHaveTextContent(/Your comments are back/);
    expect(within(commentList()).getAllByRole("listitem").map((e) => e.textContent)).toEqual([expect.stringContaining("Ask for a reason")]);
    expect(within(bar()).getByRole("button", { name: "Retry" })).toBeEnabled();
  });
});

describe("a whole-screen comment", () => {
  it("keeps typed text as a draft on a click away, and Comment on this screen restores it", async () => {
    const { dialog } = await openReview();
    commentOnScreen();
    fireEvent.change(within(bubble()).getByLabelText("Comment"), { target: { value: "Too busy" } });
    await clickAway(dialog);
    expect(screen.queryByRole("dialog", { name: /^Comment on/ })).toBeNull();
    expect(bar()).toHaveTextContent("0 comments");

    commentOnScreen();
    expect(within(bubble()).getByLabelText("Comment")).toHaveValue("Too busy");
  });
});

describe("a whole-screen comment at a spot of the screen", () => {
  /** What the host last asked the frame to focus among whole-screen comments' pins. */
  const lastScreenPinFocus = (post: { mock: { calls: unknown[][] } }) =>
    post.mock.calls.map((c) => c[0] as { type: string }).filter((m) => m.type === "proto:focus-screen-pin").at(-1);

  it("opens on a click on empty space in Comment mode, at the spot, with a hollow pin there while it is written", async () => {
    const { dialog, post } = await openReview();
    annotate(dialog);
    vi.spyOn(frame(), "getBoundingClientRect").mockReturnValue({ left: 100, top: 64, right: 900, bottom: 664, width: 800, height: 600, x: 100, y: 64, toJSON: () => ({}) });
    clickScreen(SPOT, { x: 0, y: 500 });
    expect(bubble()).toHaveAccessibleName("Comment on My claims (whole screen)");
    expect(within(bubble()).getByLabelText("Comment")).toHaveFocus();
    expect(lastView(post).screenPins).toEqual([{ at: SPOT }]);

    /** The bubble's left edge in the console's viewport, as placed inline (jsdom lays nothing out). */
    const bubbleLeft = async () => {
      await act(() => new Promise((resolve) => setTimeout(resolve, 0)));
      return Number(/translate\((-?[\d.]+)px/.exec(bubble().parentElement!.style.transform)?.[1]);
    };
    expect(await bubbleLeft()).toBe(400);
    // The prototype scrolled sideways: the spot moved with it, and the bubble follows.
    fromFrame({ type: "proto:geometry", boxes: {}, scroll: { x: 50, y: 500 } });
    expect(await bubbleLeft()).toBe(350);
  });

  it("opens nothing on a click on empty space in Preview, which the frame should not report", async () => {
    const { post } = await openReview();
    clickScreen();
    expect(screen.queryByRole("dialog", { name: /^Comment on/ })).toBeNull();
    expect(lastView(post)).not.toHaveProperty("screenPins");
  });

  it("leaves a numbered pin at the spot once added, and sends the comment without the spot", async () => {
    const { dialog, post } = await openReview();
    annotate(dialog);
    clickScreen();
    addComment("Too busy overall");
    expect(screen.queryByRole("dialog", { name: /^Comment on/ })).toBeNull();
    expect(lastView(post).screenPins).toEqual([{ at: SPOT, number: 1 }]);
    expect(lastView(post).pins).toEqual({});
    expect(within(commentList()).getByRole("listitem")).toHaveTextContent(/Too busy overall.*Whole screen/);

    fireEvent.click(within(bar()).getByRole("button", { name: "Send to agent" }));
    await waitFor(() => expect(send).toHaveBeenCalled());
    expect((send.mock.calls[0]![2] as { feedback: { requests: unknown[] } }).feedback.requests).toEqual([
      { screenId: "screen.my-claims", roleId: "employee", stateId: "state.default", elementIds: [], text: "Too busy overall" },
    ]);
  });

  it("opens its pin to read, edit and remove it, putting focus back on the pin on Escape", async () => {
    const { dialog, post } = await openReview();
    annotate(dialog);
    clickScreen();
    addComment("Too busy");
    clickScreenPin([1]);
    const opened = screen.getByRole("dialog", { name: "Comment 1" });
    expect(opened).toHaveTextContent("Too busy");
    fireEvent.click(within(opened).getByRole("button", { name: "Edit" }));
    fireEvent.change(within(opened).getByLabelText("Comment"), { target: { value: "Far too busy" } });
    fireEvent.click(within(opened).getByRole("button", { name: "Save" }));
    expect(screen.getByRole("dialog", { name: "Comment 1" })).toHaveTextContent("Far too busy");
    fireEvent.keyDown(screen.getByRole("dialog", { name: "Comment 1" }), { key: "Escape" });
    expect(screen.queryByRole("dialog", { name: "Comment 1" })).toBeNull();
    expect(lastScreenPinFocus(post)).toEqual({ type: "proto:focus-screen-pin", requests: [1] });

    clickScreenPin([1]);
    fireEvent.click(within(screen.getByRole("dialog", { name: "Comment 1" })).getByRole("button", { name: "Remove" }));
    expect(bar()).toHaveTextContent("0 comments");
    expect(lastView(post)).not.toHaveProperty("screenPins");
  });

  it("closes an open bubble on a click on empty space, keeping typed text as the screen's draft at its spot, which its hollow pin reopens", async () => {
    const { dialog, post } = await openReview();
    annotate(dialog);
    clickScreen();
    fireEvent.change(within(bubble()).getByLabelText("Comment"), { target: { value: "Half a thought" } });
    const elsewhere = { x: 20, y: 40 };
    clickScreen(elsewhere);
    expect(screen.queryByRole("dialog", { name: /^Comment on/ })).toBeNull();
    expect(lastView(post).screenPins).toEqual([{ at: SPOT }]);
    expect(bar()).toHaveTextContent("0 comments");

    clickScreenPin([]);
    expect(within(bubble()).getByLabelText("Comment")).toHaveValue("Half a thought");
    addComment("Half a thought, finished");
    expect(lastView(post).screenPins).toEqual([{ at: SPOT, number: 1 }]);
  });

  it("lists and sends comments made on several screens and as another role as one batch, and draws each screen's pins again there", async () => {
    const { dialog, post } = await openReview();
    // My claims: an element, and a spot.
    annotate(dialog);
    clickElement("btn.new-claim");
    addComment("Call it Submit a claim");
    clickScreen();
    addComment("Too busy overall");
    // To the new claim form by clicking through the prototype.
    fireEvent.click(within(dialog).getByRole("button", { name: "Preview" }));
    fromFrame({ type: "proto:navigate", screenId: "screen.new-claim" });
    expect(lastView(post)).toMatchObject({ screenId: "screen.new-claim", pins: {} });
    expect(lastView(post)).not.toHaveProperty("screenPins");
    annotate(dialog);
    clickElement("btn.submit");
    addComment("Say what happens next");
    const onForm = { x: 40, y: 900 };
    clickScreen(onForm);
    addComment("Group the fields");
    // As the Manager, on the pending approvals.
    fireEvent.click(within(dialog).getByRole("button", { name: "Preview" }));
    toPendingAsManager(dialog);
    annotate(dialog);
    clickElement("btn.reject");
    addComment("Ask for a reason");

    expect(within(commentList()).getAllByRole("listitem")).toHaveLength(5);
    fireEvent.click(within(bar()).getByRole("button", { name: "Send to agent" }));
    await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
    const on = (screenId: string, roleId: string, elementIds: string[], text: string) => ({ screenId, roleId, stateId: "state.default", elementIds, text });
    expect((send.mock.calls[0]![2] as { feedback: { requests: unknown[] } }).feedback.requests).toEqual([
      on("screen.my-claims", "employee", ["btn.new-claim"], "Call it Submit a claim"),
      on("screen.my-claims", "employee", [], "Too busy overall"),
      on("screen.new-claim", "employee", ["btn.submit"], "Say what happens next"),
      on("screen.new-claim", "employee", [], "Group the fields"),
      on("screen.pending", "manager", ["btn.reject"], "Ask for a reason"),
    ]);
  });

  it("draws each screen's pins again when the reviewer comes back to it", async () => {
    const { dialog, post } = await openReview();
    annotate(dialog);
    clickScreen();
    addComment("Too busy overall");
    fireEvent.click(within(dialog).getByRole("button", { name: "Preview" }));
    fromFrame({ type: "proto:navigate", screenId: "screen.new-claim" });
    expect(lastView(post)).not.toHaveProperty("screenPins");
    fromFrame({ type: "proto:navigate", screenId: "screen.my-claims" });
    expect(lastView(post).screenPins).toEqual([{ at: SPOT, number: 1 }]);
  });
});
