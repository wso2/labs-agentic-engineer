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

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import type { AskQuestionInput } from "@aep/agent-stream";
import type { ChatItem } from "../chatLog";
import type { ProjectChat } from "../chatStore";

// The Questions card's list (ADR-0002 / #879): every open question at once,
// answerable as they arrive, sent as one message once all are answered.

let chat: ProjectChat;
const answer = vi.fn(async () => true);
const retry = vi.fn();
vi.mock("../useProjectChat", () => ({ useProjectChat: () => chat, chatStore: { answer, retry } }));
const navigate = vi.fn();
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }));

// jsdom lays nothing out, so it has no scrollIntoView.
Element.prototype.scrollIntoView = vi.fn();

const { QuestionsList } = await import("./QuestionsList");

afterEach(() => {
  cleanup();
  answer.mockClear();
  navigate.mockReset();
});

const WHO: AskQuestionInput = { question: "Who approves a claim?", options: [{ label: "The manager" }, { label: "Finance" }] };
const NOTIFY: AskQuestionInput = { question: "How are people notified?", options: [{ label: "In-app only" }, { label: "Email" }] };

let n = 0;
function show(
  items: ChatItem[],
  phase: "idle" | "starting" | "running" = "idle",
  project = { fresh: true },
  status: Partial<Pick<ProjectChat, "status" | "error">> = {},
) {
  if (project.fresh) n += 1;
  chat = {
    status: "ready",
    error: null,
    ...status,
    items,
    turn: phase === "idle" ? { phase } : phase === "starting" ? { phase, instruction: "x" } : { phase, turnId: "t2" },
  } as ProjectChat;
  // A project per render: drafts are kept per project and question.
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <QuestionsList projectName={`acme-${n}`} />
    </OxygenUIThemeProvider>,
  );
}

const batch = (extra: Partial<Extract<ChatItem, { kind: "question" }>> = {}): ChatItem => ({
  kind: "question",
  id: "t1:q:c1",
  turnId: "t1",
  toolCallId: "c1",
  questions: [WHO, NOTIFY],
  streaming: false,
  ...extra,
});

describe("QuestionsList", () => {
  it("shows each option with a radio, or a checkbox where several may be picked, that follows the pick", () => {
    show([batch({ questions: [WHO, { ...NOTIFY, multiSelect: true }] })]);
    const [who, notify] = screen.getAllByRole("listitem");
    expect(who!.querySelectorAll("input[type=radio]")).toHaveLength(2);
    expect(notify!.querySelectorAll("input[type=checkbox]")).toHaveLength(2);
    fireEvent.click(screen.getByRole("radio", { name: /Finance/ }));
    expect((who!.querySelectorAll("input[type=radio]")[1] as HTMLInputElement).checked).toBe(true);
  });

  it("lists every question at once, numbered, with no pages", () => {
    show([batch()]);
    expect(screen.getByRole("heading", { name: "Questions for you" })).toBeTruthy();
    expect(screen.getAllByRole("listitem").map((li) => li.textContent)).toEqual([
      expect.stringContaining("Who approves a claim?"),
      expect.stringContaining("How are people notified?"),
    ]);
    expect(screen.queryByRole("tablist")).toBeNull();
    expect(screen.getByText("0 of 2 answered")).toBeTruthy();
  });

  it("sends every answer as one message once all are answered, then closes back to the overview", async () => {
    show([batch()]);
    fireEvent.click(screen.getByRole("radio", { name: /Finance/ }));
    fireEvent.click(screen.getByRole("radio", { name: /In-app only/ }));
    expect(screen.getByText("2 of 2 answered")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Send answers" }));
    expect(answer).toHaveBeenCalledWith(`acme-${n}`, "t1:q:c1", [{ selected: ["Finance"] }, { selected: ["In-app only"] }]);
    await vi.waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ to: "/projects/$projectName", params: { projectName: `acme-${n}` } }),
    );
  });

  it("stays open, answers and all, when the answers could not be sent", async () => {
    answer.mockResolvedValueOnce(false);
    show([batch()]);
    fireEvent.click(screen.getByRole("radio", { name: /Finance/ }));
    fireEvent.click(screen.getByRole("radio", { name: /In-app only/ }));
    fireEvent.click(screen.getByRole("button", { name: "Send answers" }));
    await vi.waitFor(() => expect(answer).toHaveBeenCalled());
    expect(navigate).not.toHaveBeenCalled();
    expect(screen.getByText("2 of 2 answered")).toBeTruthy();
  });

  it("flags what is unanswered instead of sending, and scrolls to it", () => {
    show([batch()]);
    fireEvent.click(screen.getByRole("radio", { name: /Finance/ }));
    fireEvent.click(screen.getByRole("button", { name: "Send answers" }));
    expect(answer).not.toHaveBeenCalled();
    const [first, second] = screen.getAllByRole("listitem");
    expect(first!.hasAttribute("data-unanswered")).toBe(false);
    expect(second!.textContent).toContain("Not answered");
    expect(second!.scrollIntoView).toHaveBeenCalled();
    // The flagged question's controls say so, and point at why.
    const group = screen.getByRole("radiogroup", { name: NOTIFY.question });
    expect(group.getAttribute("aria-invalid")).toBe("true");
    expect(document.getElementById(group.getAttribute("aria-errormessage")!)?.textContent).toBe("Not answered");
    expect(screen.getByRole("textbox", { name: `Your own answer to: ${NOTIFY.question}` }).getAttribute("aria-invalid")).toBe(
      "true",
    );
    expect(screen.getByRole("radiogroup", { name: WHO.question }).hasAttribute("aria-invalid")).toBe(false);
  });

  it("says once what a Send found unanswered, not each pick as it counts", () => {
    show([batch()]);
    const status = screen.getByRole("status");
    fireEvent.click(screen.getByRole("radio", { name: /Finance/ }));
    expect(status.textContent).toBe("");
    fireEvent.click(screen.getByRole("button", { name: "Send answers" }));
    expect(status.textContent).toBe("1 question not answered");
    expect(screen.getByText("1 of 2 answered").getAttribute("aria-live")).toBeNull();
  });

  it("stays on show, frozen, while the answers are on their way", () => {
    show([batch({ answers: [{ selected: ["Finance"] }, { selected: ["Email"] }] })], "starting");
    expect(screen.getByRole("heading", { name: "Questions for you" })).toBeTruthy();
    expect(screen.queryByText(/No questions waiting/)).toBeNull();
    expect((screen.getByRole("button", { name: "Send answers" }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByRole("radio", { name: /Finance/ }).getAttribute("aria-disabled")).toBe("true");
  });

  it("keeps a draft to its batch, not to a place in the log another batch can take", () => {
    show([batch()]);
    fireEvent.click(screen.getByRole("radio", { name: /Finance/ }));
    cleanup();
    // The same item id, after a replaced conversation, is another ask.
    show([batch({ toolCallId: "c2" })], "idle", { fresh: false });
    expect(screen.getByRole("radio", { name: /Finance/ }).getAttribute("aria-checked")).toBe("false");
    cleanup();
    // The same ask, read back from the history under another id, keeps it.
    show([batch({ id: "h3" })], "idle", { fresh: false });
    expect(screen.getByRole("radio", { name: /Finance/ }).getAttribute("aria-checked")).toBe("true");
  });

  it("drops the draft once the answers went", async () => {
    show([batch()]);
    fireEvent.click(screen.getByRole("radio", { name: /Finance/ }));
    fireEvent.click(screen.getByRole("radio", { name: /In-app only/ }));
    fireEvent.click(screen.getByRole("button", { name: "Send answers" }));
    await vi.waitFor(() => expect(navigate).toHaveBeenCalled());
    cleanup();
    show([batch()], "idle", { fresh: false });
    expect(screen.getByText("0 of 2 answered")).toBeTruthy();
  });

  it("says why it cannot show the questions when the conversation did not load, and retries", () => {
    show([], "idle", { fresh: true }, { status: "error", error: "Couldn't read the conversation." });
    expect(screen.getByRole("alert").textContent).toContain("Couldn't read the conversation.");
    expect(screen.queryByText(/No questions waiting/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(retry).toHaveBeenCalledWith(`acme-${n}`);
  });

  it("can be answered while the batch arrives, and sent once it is complete", () => {
    show([batch({ streaming: true, questions: [WHO] })]);
    const pick = screen.getByRole("radio", { name: /Finance/ });
    expect(pick.getAttribute("aria-disabled")).toBeNull();
    expect((screen.getByRole("button", { name: "Send answer" }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText(/Still asking/)).toBeTruthy();
  });

  it("keeps a draft when the card is closed and opened again", () => {
    show([batch()]);
    fireEvent.click(screen.getByRole("radio", { name: /Finance/ }));
    cleanup();
    render(
      <OxygenUIThemeProvider theme={OxygenTheme}>
        <QuestionsList projectName={`acme-${n}`} />
      </OxygenUIThemeProvider>,
    );
    expect(screen.getByRole("radio", { name: /Finance/ }).getAttribute("aria-checked")).toBe("true");
  });

  it("says nothing is waiting when no question is open", () => {
    show([]);
    expect(screen.getByText(/No questions waiting/)).toBeTruthy();
    cleanup();
    show([batch({ answers: [{ selected: ["Finance"] }, { selected: ["Email"] }] })], "running");
    expect(screen.getByText(/No questions waiting/)).toBeTruthy();
  });

  it("closes once a later message superseded the questions", () => {
    show([batch(), { kind: "user", id: "u2", text: "Actually, wait", state: "sent" }]);
    expect(screen.queryByRole("heading", { name: "Questions for you" })).toBeNull();
    expect(screen.getByText(/No questions waiting/)).toBeTruthy();
  });
});
