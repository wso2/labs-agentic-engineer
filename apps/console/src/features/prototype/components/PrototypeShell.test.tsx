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

// Annotate (#817): the inspector, the composer, the queue and Send all, driven
// through the shell the page renders. The reducer's rules are pinned in
// viewState.test.ts; this is what a reviewer sees them do.

import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { expenseApproval as model } from "../testing/fixtures";
import { ShellHarness, type ShellHarnessProps } from "../testing/ShellHarness";

afterEach(cleanup);

const byId = (id: string) => document.querySelector<HTMLElement>(`[data-prototype-component-id="${id}"]`)!;
const inspector = () => screen.queryByRole("complementary", { name: "Feedback" });
const enterAnnotate = () => fireEvent.click(screen.getByRole("button", { name: "Annotate" }));

function renderShell(props: Partial<ShellHarnessProps> = {}) {
  return render(<ShellHarness model={model} {...props} />);
}

function queue(text: string) {
  fireEvent.change(within(inspector()!).getByRole("textbox", { name: "Request" }), { target: { value: text } });
  fireEvent.click(within(inspector()!).getByRole("button", { name: "Add request" }));
}

describe("PrototypeShell — Annotate", () => {
  it("shows the inspector only in Annotate", () => {
    renderShell();
    expect(inspector()).toBeNull();
    enterAnnotate();
    expect(inspector()).not.toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    expect(inspector()).toBeNull();
  });

  it("is about the whole screen until something is selected", () => {
    renderShell();
    enterAnnotate();
    expect(within(inspector()!).getByText("Whole screen")).toBeInTheDocument();
    expect(within(inspector()!).getByText("On Approval queue")).toBeInTheDocument();
  });

  it("outlines and labels each selection, several at once, and a second click deselects", () => {
    renderShell();
    enterAnnotate();
    fireEvent.click(byId("btn.export"));
    fireEvent.click(byId("stat.overdue"));
    expect(byId("btn.export")).toHaveAttribute("aria-pressed", "true");
    expect(within(byId("btn.export")).getByTestId("selection-label")).toHaveTextContent("Export");
    const about = within(inspector()!).getByLabelText("Request is about");
    expect(within(about).getAllByRole("button").map((c) => c.textContent)).toEqual(["Export", "Overdue"]);
    expect(within(about).queryByText("Whole screen")).toBeNull();

    fireEvent.click(byId("btn.export"));
    expect(byId("btn.export")).toHaveAttribute("aria-pressed", "false");
    expect(within(byId("btn.export")).queryByTestId("selection-label")).toBeNull();
  });

  it("keeps Add request disabled while the request is blank", () => {
    renderShell();
    enterAnnotate();
    const add = within(inspector()!).getByRole("button", { name: "Add request" });
    expect(add).toBeDisabled();
    fireEvent.change(within(inspector()!).getByRole("textbox", { name: "Request" }), { target: { value: "   " } });
    expect(add).toBeDisabled();
    fireEvent.change(within(inspector()!).getByRole("textbox", { name: "Request" }), { target: { value: "Rename" } });
    expect(add).toBeEnabled();
  });

  it("queues a card naming the screen and components, pins them, and starts the next request fresh", () => {
    renderShell();
    enterAnnotate();
    fireEvent.click(byId("btn.export"));
    queue("Rename to Download");
    queue("Too busy");

    const cards = within(inspector()!).getAllByRole("listitem");
    expect(cards).toHaveLength(2);
    expect(within(cards[0]!).getByText("Approval queue")).toBeInTheDocument();
    expect(within(cards[0]!).getByText("Export")).toBeInTheDocument();
    expect(within(cards[0]!).getByText("Rename to Download")).toBeInTheDocument();
    expect(within(cards[1]!).getByText("Whole screen")).toBeInTheDocument();
    // The selection was used up by the first request.
    expect(byId("btn.export")).toHaveAttribute("aria-pressed", "false");
    // Numbered pins on queued components, while annotating only.
    expect(within(byId("btn.export")).getByTestId("request-pin")).toHaveTextContent("1");
    fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    expect(within(byId("btn.export")).queryByTestId("request-pin")).toBeNull();
  });

  it("removes a queued request", () => {
    renderShell();
    enterAnnotate();
    queue("One");
    queue("Two");
    fireEvent.click(within(inspector()!).getByRole("button", { name: "Remove request 1" }));
    const cards = within(inspector()!).getAllByRole("listitem");
    expect(cards).toHaveLength(1);
    expect(within(cards[0]!).getByText("Two")).toBeInTheDocument();
  });

  it("has one Send all for the whole queue", () => {
    const onSendAll = vi.fn();
    renderShell({ onSendAll });
    enterAnnotate();
    expect(within(inspector()!).getByRole("button", { name: "Send all" })).toBeDisabled();
    queue("One");
    queue("Two");
    const sends = within(inspector()!).getAllByRole("button", { name: /^Send all/ });
    expect(sends).toHaveLength(1);
    fireEvent.click(sends[0]!);
    expect(onSendAll).toHaveBeenCalledTimes(1);
  });

  it("holds the mode and refuses to queue or send while an agent turn runs", () => {
    renderShell({ request: { mode: "annotate" }, busy: true });
    expect(screen.getByRole("button", { name: "Preview" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Annotate" })).toBeDisabled();
    fireEvent.change(within(inspector()!).getByRole("textbox", { name: "Request" }), { target: { value: "Rename" } });
    expect(within(inspector()!).getByRole("button", { name: "Add request" })).toBeDisabled();
    expect(within(inspector()!).getByText(/An agent is working on this project/)).toBeInTheDocument();
  });

  it("says the batch is on its way while it sends, and why it did not land when it fails", () => {
    const { rerender } = renderShell({ request: { mode: "annotate" }, sending: true });
    expect(within(inspector()!).getByRole("button", { name: "Sending…" })).toBeDisabled();
    rerender(<ShellHarness model={model} request={{ mode: "annotate" }} error="Your requests are still queued." />);
    expect(within(inspector()!).getByText("Your requests are still queued.")).toBeInTheDocument();
  });
});
