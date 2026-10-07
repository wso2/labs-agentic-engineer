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
import type { ProjectChat } from "../../agent-chat/chatStore";
import { SAMPLE_MANIFEST, SAMPLE_SOURCE } from "../../../mocks/fixtures/prototype";
import { appPrototypes, manifestPath, revisingIn, sourcePath, type AppPrototype } from "../model/prototypes";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { prototypeHash } from "@wso2/prototype-kit/feedback";
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
vi.mock("../../agent-chat/useProjectChat", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../agent-chat/useProjectChat")>()),
  useProjectChat: () => chat,
  chatStore: { send: (...args: unknown[]) => send(...args) },
}));

const openChat = vi.fn();
vi.mock("../../shell/chatPanel", () => ({ useChatPanel: () => ({ open: openChat }) }));

// The theme's 2 MB runtime is the kit's browser lane's to run; the frame here only needs one.
vi.mock("../useReviewAssets", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../useReviewAssets")>()),
  useFrameRuntime: () => ({ value: "/* frame runtime */", error: null }),
}));

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
  const messages = post.mock.calls.map((c) => c[0] as { type: string; view?: { mode: string; screenId: string; selectedKeys: string[]; pins: object } });
  return messages.filter((m) => m.view).at(-1)!.view!;
}

async function openReview() {
  render(<Harness initial={C} />);
  const dialog = await screen.findByRole("dialog");
  await waitFor(() => expect(frame()).toBeInTheDocument());
  const post = vi.spyOn(frame().contentWindow!, "postMessage");
  fromFrame({ type: "proto:ready" });
  await waitFor(() => expect(post).toHaveBeenCalledWith(expect.objectContaining({ type: "proto:load" }), "*"));
  return { dialog, post };
}

function annotate(dialog: HTMLElement) {
  fireEvent.click(within(dialog).getByRole("button", { name: "Annotate" }));
}

function addRequest(dialog: HTMLElement, text: string) {
  fireEvent.change(within(dialog).getByLabelText("Request"), { target: { value: text } });
  fireEvent.click(within(dialog).getByRole("button", { name: "Add request" }));
}

beforeEach(() => {
  queryClient = new QueryClient();
  invalidate = vi.spyOn(queryClient, "invalidateQueries");
  prototypes = appPrototypes([C], files, null);
  chat = idle;
  send.mockClear();
  send.mockResolvedValue(true);
  openChat.mockClear();
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
    const { dialog } = await openReview();
    // A key the console's document gets with the frame element as its target was aimed at the prototype (focus is
    // in it), so it is the prototype's, not the dialog's: only the frame says when Escape went unused.
    fireEvent.keyDown(frame(), { key: "Escape" });
    expect(screen.getByRole("dialog")).toBeInTheDocument();

    // Annotate: the frame's Escape clears the selection first, then closes.
    annotate(dialog);
    fromFrame({ type: "proto:toggle", elementKey: "btn.new-claim" });
    fromFrame({ type: "proto:escape" });
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(within(dialog).queryByText(/^Selected:/)).toBeNull();
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

  it("acts in Preview and only selects in Annotate", async () => {
    const { dialog, post } = await openReview();
    const screenPicker = within(dialog).getByLabelText("Screen") as HTMLSelectElement;

    // Preview: a press in the app navigates; a stray toggle selects nothing.
    fromFrame({ type: "proto:navigate", screenId: "screen.new-claim" });
    expect(screenPicker.value).toBe("screen.new-claim");
    fromFrame({ type: "proto:toggle", elementKey: "btn.submit" });
    expect(lastView(post)).toMatchObject({ mode: "preview", screenId: "screen.new-claim", selectedKeys: [] });

    // Annotate: a click selects (and says what), and a navigation is ignored.
    annotate(dialog);
    fromFrame({ type: "proto:rendered", screenId: "screen.new-claim", elements: [{ key: "btn.submit", label: "Submit claim" }] });
    fromFrame({ type: "proto:toggle", elementKey: "btn.submit" });
    fromFrame({ type: "proto:navigate", screenId: "screen.my-claims" });
    expect(screenPicker.value).toBe("screen.new-claim");
    expect(lastView(post)).toMatchObject({ mode: "annotate", screenId: "screen.new-claim", selectedKeys: ["btn.submit"] });
    expect(within(dialog).getByText("Selected: Submit claim")).toBeInTheDocument();
  });

  it("frames the prototype in a browser window whose address follows the screen", async () => {
    const { dialog } = await openReview();
    const window = within(dialog).getByRole("region", { name: "Acme Expenses prototype" });
    expect(within(window).getByLabelText("Address")).toHaveTextContent("prototype://screen.my-claims");
    expect(within(window).getByTitle("Acme Expenses prototype app")).toBeInTheDocument();

    fromFrame({ type: "proto:navigate", screenId: "screen.new-claim" });
    expect(within(window).getByLabelText("Address")).toHaveTextContent("prototype://screen.new-claim");
  });

  it("queues requests with numbered pins, and removes one", async () => {
    const { dialog, post } = await openReview();
    annotate(dialog);
    fromFrame({ type: "proto:toggle", elementKey: "btn.new-claim" });
    addRequest(dialog, "Call it Submit a claim");
    addRequest(dialog, "Too much white space");
    expect(lastView(post)).toMatchObject({ selectedKeys: [], pins: { "btn.new-claim": [1] } });
    const queue = within(dialog).getByRole("list", { name: "Queued requests" });
    expect(within(queue).getAllByRole("listitem")).toHaveLength(2);
    fireEvent.click(within(dialog).getByRole("button", { name: "Remove request 1" }));
    expect(within(queue).getAllByRole("listitem")).toHaveLength(1);
    expect(within(dialog).getByRole("button", { name: "Send all (1)" })).toBeEnabled();
  });

  it("sends every request as one typed /prototype turn, closes, and opens the chat", async () => {
    const { dialog } = await openReview();
    fireEvent.change(within(dialog).getByLabelText("Flow"), { target: { value: "flow.approve" } });
    annotate(dialog);
    fromFrame({
      type: "proto:rendered",
      screenId: "screen.pending",
      elements: [{ key: "btn.reject", label: "Reject" }, { key: "btn.approve", label: "Approve" }],
    });
    fromFrame({ type: "proto:toggle", elementKey: "btn.reject" });
    fromFrame({ type: "proto:toggle", elementKey: "btn.approve" });
    addRequest(dialog, "Put Approve on the right");
    fireEvent.change(within(dialog).getByLabelText("State"), { target: { value: "state.empty" } });
    addRequest(dialog, "Say who to ask when nothing waits");
    fireEvent.click(within(dialog).getByRole("button", { name: "Send all (2)" }));

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(openChat).toHaveBeenCalled();
    expect(send).toHaveBeenCalledWith("acme-expenses", "/prototype expense-web", {
      kind: "prototype",
      feedback: {
        prototypeHash: prototypeHash(SAMPLE_MANIFEST, SAMPLE_SOURCE),
        component: C,
        requests: [
          { screenId: "screen.pending", flowId: "flow.approve", roleId: "manager", stateId: "state.default", elementIds: ["btn.reject", "btn.approve"], text: "Put Approve on the right" },
          { screenId: "screen.pending", flowId: "flow.approve", roleId: "manager", stateId: "state.empty", elementIds: [], text: "Say who to ask when nothing waits" },
        ],
      },
    });
    // The cards see the turn running as soon as the server has it.
    expect(invalidate).toHaveBeenCalledWith({ queryKey: designKey("acme-expenses") });

    // Sent: opening it again starts a new queue.
    fireEvent.click(screen.getByRole("button", { name: "Review" }));
    expect(within(await screen.findByRole("dialog")).queryByRole("list", { name: "Queued requests" })).toBeNull();
  });

  it("records the revision it showed as reviewed in this browser", async () => {
    await openReview();
    const hash = prototypeHash(SAMPLE_MANIFEST, SAMPLE_SOURCE);
    await waitFor(() => expect(unreviewed("acme-expenses", { [C]: hash })).toEqual([]));
  });

  it("refuses Send all while a turn is running, says why, and keeps the queue", async () => {
    chat = busy;
    const { dialog } = await openReview();
    annotate(dialog);
    addRequest(dialog, "Too much white space");
    fireEvent.click(within(dialog).getByRole("button", { name: "Send all (1)" }));
    expect(within(dialog).getByRole("alert")).toHaveTextContent(/working on another turn.*kept/);
    expect(send).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(within(within(dialog).getByRole("list", { name: "Queued requests" })).getAllByRole("listitem")).toHaveLength(1);
  });

  it("keeps the queue when the chat does not take the turn, and across closing and opening again", async () => {
    send.mockResolvedValue(false);
    const { dialog } = await openReview();
    annotate(dialog);
    addRequest(dialog, "Too much white space");
    fireEvent.click(within(dialog).getByRole("button", { name: "Send all (1)" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(/weren't sent/);
    fireEvent.click(within(dialog).getByRole("button", { name: "Close" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    fireEvent.click(screen.getByRole("button", { name: "Review" }));
    const again = await screen.findByRole("dialog");
    expect(within(again).getByRole("button", { name: "Send all (1)" })).toBeInTheDocument();
  });
});
