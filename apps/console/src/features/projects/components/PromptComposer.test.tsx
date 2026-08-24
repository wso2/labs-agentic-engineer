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

import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { PromptComposer } from "./PromptComposer";

// A host that owns the state the composer is controlled by, so a remove or an
// attach is exercised end to end rather than asserted on a spy's arguments.
function Host({ initial = [] as File[] }) {
  const [prompt, setPrompt] = useState("");
  const [files, setFiles] = useState<File[]>(initial);
  return (
    <PromptComposer
      prompt={prompt}
      onPromptChange={setPrompt}
      files={files}
      onFilesChange={setFiles}
      onSubmit={vi.fn()}
    />
  );
}

function attach(names: string[]): void {
  const input = document.querySelector<HTMLInputElement>("input[type=file]");
  expect(input).not.toBeNull();
  fireEvent.change(input!, {
    target: { files: names.map((name) => new File(["content"], name)) },
  });
}

describe("PromptComposer attachment cards (#383)", () => {
  it("shows the file name and a type badge, and no file size", () => {
    render(<Host />);
    attach(["Anjana Income Expense All Years USD Tax.pdf"]);

    expect(
      screen.getByText("Anjana Income Expense All Years USD Tax.pdf"),
    ).toBeInTheDocument();
    expect(screen.getByText("PDF")).toBeInTheDocument();
    // Size is a validation concern: an oversized file never becomes a card, so
    // the card has nothing to say about bytes.
    expect(screen.queryByText(/\d+\s?(B|KB|MB)\b/)).not.toBeInTheDocument();
  });

  it("badges each attachment by its own extension", () => {
    render(<Host />);
    attach(["prd.md", "mockup.png"]);

    expect(screen.getByText("MD")).toBeInTheDocument();
    expect(screen.getByText("PNG")).toBeInTheDocument();
  });

  // The control is opacity-toggled on hover rather than mounted on hover, so
  // it stays in the tab order for keyboard users, who never hover at all.
  it("keeps the remove control in the accessibility tree without hovering", () => {
    render(<Host />);
    attach(["prd.md"]);

    const remove = screen.getByRole("button", { name: "Remove prd.md" });
    expect(remove).toBeInTheDocument();

    fireEvent.click(remove);
    expect(screen.queryByText("prd.md")).not.toBeInTheDocument();
  });

  it("accepts a drop anywhere on the composer, not just a separate zone", () => {
    render(<Host />);
    // Dropped ON THE TEXTAREA — the deepest thing a user is likely to aim at,
    // and the point of folding the old dashed zone into the box: the handler
    // is on the container, so the event only lands because it bubbles.
    fireEvent.drop(screen.getByRole("textbox"), {
      dataTransfer: { files: [new File(["x"], "notes.txt")] },
    });

    expect(screen.getByText("notes.txt")).toBeInTheDocument();
    expect(screen.getByText("TXT")).toBeInTheDocument();
  });

  // Notices are keyed and dismissed BY POSITION, because one selection can
  // reject two files under the same name; name identity would collapse them
  // into one notice and then close both at once.
  it("raises one notice per rejected file and dismisses them independently", () => {
    render(<Host />);
    attach(["spec.docx", "spec.docx", "prd.md"]);

    // The supported file still lands.
    expect(screen.getByText("prd.md")).toBeInTheDocument();
    expect(screen.getAllByText(/was not attached/i)).toHaveLength(2);

    fireEvent.click(screen.getAllByRole("button", { name: /close/i })[0]!);
    expect(screen.getAllByText(/was not attached/i)).toHaveLength(1);
  });

  // Lower-casing the reason turned "Larger than 5 MB" into "5 mb" and mangled
  // the user's own file-name casing in the collision reason.
  it("renders the rejection reason verbatim, units and casing intact", () => {
    render(<Host />);
    const input = document.querySelector<HTMLInputElement>("input[type=file]")!;
    const oversized = new File(["x"], "huge.pdf");
    Object.defineProperty(oversized, "size", { value: 6 * 1024 * 1024 });
    fireEvent.change(input, { target: { files: [oversized] } });

    expect(screen.getByText(/Larger than 5 MB/)).toBeInTheDocument();
  });

  it("keeps Start disabled until the prompt is more than whitespace", () => {
    render(<Host />);
    const start = screen.getByRole("button", { name: "Start" });
    expect(start).toBeDisabled();

    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "   " },
    });
    expect(start).toBeDisabled();

    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "A todo app" },
    });
    expect(start).toBeEnabled();
  });

  // Attaching is optional and documents alone are not a brief: the typed idea
  // stays the anchor (grilling decision 7, unchanged by the v2 reversal).
  it("does not enable Start on attachments alone", () => {
    render(<Host />);
    attach(["prd.md"]);

    expect(screen.getByRole("button", { name: "Start" })).toBeDisabled();
  });
});
