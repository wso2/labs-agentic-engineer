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

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { ChatInput } from "./ChatInput";

function renderInput(props: Partial<Parameters<typeof ChatInput>[0]> = {}) {
  const onSubmit = vi.fn();
  const onChange = vi.fn();
  const onFilesChange = vi.fn();
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <ChatInput
        value=""
        onChange={onChange}
        onSubmit={onSubmit}
        disabled={false}
        contextLabel="shop"
        files={[]}
        onFilesChange={onFilesChange}
        {...props}
      />
    </OxygenUIThemeProvider>,
  );
  return { onSubmit, onChange, onFilesChange };
}

afterEach(cleanup);

describe("ChatInput", () => {
  it("shows the project context label", () => {
    renderInput();
    expect(screen.getByText("shop")).toBeInTheDocument();
  });

  it("locks the composer with a hint while a teammate's turn runs", () => {
    renderInput({
      disabled: true,
      hint: "Agent is working on Sarah Perera's request…",
      value: "",
    });
    expect(screen.getByTestId("input-hint")).toHaveTextContent(
      "Agent is working on Sarah Perera's request…",
    );
    expect(screen.getByPlaceholderText("Waiting for the current turn…")).toBeDisabled();
    expect(screen.getByRole("button", { name: "Send message" })).toBeDisabled();
  });

  it("submits on Enter when there is a draft", () => {
    const { onSubmit } = renderInput({ value: "add gift wrapping" });
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter" });
    expect(onSubmit).toHaveBeenCalledOnce();
  });

  it("does not submit on Shift+Enter (newline)", () => {
    const { onSubmit } = renderInput({ value: "line one" });
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter", shiftKey: true });
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("disables send when the draft is empty", () => {
    renderInput({ value: "   " });
    expect(screen.getByRole("button", { name: "Send message" })).toBeDisabled();
  });
});

// --- Chat attachments (#428) -------------------------------------------------

/** A File of a given size without allocating the bytes. */
function fileOf(name: string, size = 16): File {
  const file = new File(["x"], name);
  Object.defineProperty(file, "size", { value: size });
  return file;
}

function picker(): HTMLInputElement {
  // The hidden input inside the paperclip's `component="label"` IconButton.
  const input = document.querySelector('input[type="file"]');
  if (!input) throw new Error("file input not rendered");
  return input as HTMLInputElement;
}

describe("ChatInput attachments", () => {
  it("offers an attach control that accepts every readable type", () => {
    renderInput();
    expect(
      screen.getByRole("button", { name: "Attach files to this message" }),
    ).toBeInTheDocument();
    const accept = picker().getAttribute("accept") ?? "";
    for (const ext of [".pdf", ".png", ".gif", ".webp", ".csv", ".yaml", ".rst"]) {
      expect(accept, ext).toContain(ext);
    }
    expect(accept).not.toContain(".docx");
  });

  it("hands accepted files to the caller", () => {
    const { onFilesChange } = renderInput();
    fireEvent.change(picker(), { target: { files: [fileOf("mockup.pdf")] } });
    expect(onFilesChange).toHaveBeenCalledOnce();
    expect(onFilesChange.mock.calls[0]?.[0].map((f: File) => f.name)).toEqual(["mockup.pdf"]);
  });

  it("shows an image as a thumbnail and never draws its name", () => {
    // A thumbnail answers "did I attach the right screenshot?"; the file name
    // ("Screenshot 2026-08-20 at 11.42.13.png") answers nothing and costs the
    // width of the whole strip.
    renderInput({ files: [fileOf("Screenshot 2026-08-20 at 11.42.13.png")] });
    expect(screen.getByTestId("attachment-preview-image")).toBeInTheDocument();
    expect(screen.queryByTestId("attachment-preview-named")).not.toBeInTheDocument();
    expect(
      screen.queryByText("Screenshot 2026-08-20 at 11.42.13.png"),
    ).not.toBeInTheDocument();
  });

  it("keeps the image's name reachable as alt text, not as visible text", () => {
    // Not drawn is not the same as lost: a thumbnail with no accessible name is
    // invisible to a screen reader.
    renderInput({ files: [fileOf("checkout-error.png")] });
    expect(screen.getByAltText("checkout-error.png")).toBeInTheDocument();
  });

  it("shows the NAME for anything a browser cannot draw", () => {
    renderInput({ files: [fileOf("revised-spec.pdf"), fileOf("rows.csv")] });
    expect(screen.getByText("revised-spec.pdf")).toBeInTheDocument();
    expect(screen.getByText("rows.csv")).toBeInTheDocument();
    expect(screen.queryByTestId("attachment-preview-image")).not.toBeInTheDocument();
  });

  it("treats a PDF as un-previewable even though the model reads it natively", () => {
    // "Native to the model" and "drawable by an <img>" are different questions
    // and coincide only for the four image types.
    renderInput({ files: [fileOf("mockups.pdf")] });
    expect(screen.getByTestId("attachment-preview-named")).toBeInTheDocument();
    expect(screen.getByText("mockups.pdf")).toBeInTheDocument();
  });

  it("mixes both kinds in one strip", () => {
    renderInput({ files: [fileOf("shot.png"), fileOf("notes.md")] });
    expect(screen.getByTestId("attachment-preview-image")).toBeInTheDocument();
    expect(screen.getByTestId("attachment-preview-named")).toBeInTheDocument();
    expect(screen.getByText("notes.md")).toBeInTheDocument();
    expect(screen.queryByText("shot.png")).not.toBeInTheDocument();
  });

  it("renders the attachment strip inside the composer, not beside it", () => {
    renderInput({ files: [fileOf("shot.png")] });
    const composer = screen.getByTestId("chat-composer-dropzone");
    expect(composer).toContainElement(screen.getByTestId("attachment-strip"));
    // The attach control and send live in the same box, so the whole thing reads
    // as one input rather than a row of three controls.
    expect(composer).toContainElement(
      screen.getByRole("button", { name: "Attach files to this message" }),
    );
    expect(composer).toContainElement(screen.getByRole("button", { name: "Send message" }));
    expect(composer).toContainElement(screen.getByRole("textbox"));
  });

  it("keeps the remove control in the tab order for keyboard users", () => {
    // Mounted, not hover-mounted: opacity-toggled so `:focus-within` can bring
    // it back for users who never hover.
    renderInput({ files: [fileOf("mockup.pdf")] });
    expect(screen.getByRole("button", { name: "Remove mockup.pdf" })).toBeInTheDocument();
  });

  it("removes by name, leaving the rest attached", () => {
    const { onFilesChange } = renderInput({ files: [fileOf("a.md"), fileOf("b.md")] });
    fireEvent.click(screen.getByRole("button", { name: "Remove a.md" }));
    expect(onFilesChange.mock.calls[0]?.[0].map((f: File) => f.name)).toEqual(["b.md"]);
  });

  it("surfaces one notice per rejected file and attaches nothing", () => {
    const { onFilesChange } = renderInput();
    fireEvent.change(picker(), {
      target: { files: [fileOf("spec.docx"), fileOf("notes.pages")] },
    });
    expect(onFilesChange).not.toHaveBeenCalled();
    expect(screen.getByText("spec.docx")).toBeInTheDocument();
    expect(screen.getByText("notes.pages")).toBeInTheDocument();
  });

  it("renders the rejection reason verbatim, not lower-cased", () => {
    renderInput();
    fireEvent.change(picker(), {
      target: { files: [fileOf("huge.pdf", 6 * 1024 * 1024)] },
    });
    // "5 mb" would be wrong, and so would mangling the user's own file name.
    expect(screen.getByText(/Larger than 5 MB/)).toBeInTheDocument();
  });

  it("still requires text — an attachment alone cannot send", () => {
    // The shared TurnSpec validator rejects an empty chat turn, so the button
    // must not become enabled just because a file is attached.
    renderInput({ value: "", files: [fileOf("mockup.pdf")] });
    expect(screen.getByRole("button", { name: "Send message" })).toBeDisabled();
  });

  it("enables send once there is text alongside the attachment", () => {
    renderInput({ value: "what is wrong here?", files: [fileOf("shot.png")] });
    expect(screen.getByRole("button", { name: "Send message" })).toBeEnabled();
  });

  it("locks the attach control while a turn runs", () => {
    renderInput({ disabled: true, files: [fileOf("mockup.pdf")] });
    // The paperclip is an IconButton with `component="label"`, and a <label> has
    // no native `disabled` — MUI marks it aria-disabled. What actually stops the
    // picker from opening is the hidden input's own `disabled`, so BOTH are
    // asserted: the announced state for assistive tech, and the real guard.
    expect(
      screen.getByRole("button", { name: "Attach files to this message" }),
    ).toHaveAttribute("aria-disabled", "true");
    expect(picker()).toBeDisabled();
    expect(screen.getByRole("button", { name: "Remove mockup.pdf" })).toBeDisabled();
  });

  it("refuses a drop while a turn runs, rather than discarding it silently", () => {
    const { onFilesChange } = renderInput({ disabled: true });
    fireEvent.drop(screen.getByTestId("chat-composer-dropzone"), {
      dataTransfer: { files: [fileOf("mockup.pdf")] },
    });
    expect(onFilesChange).not.toHaveBeenCalled();
  });

  it("accepts a drop on the composer when it is free", () => {
    const { onFilesChange } = renderInput();
    fireEvent.drop(screen.getByTestId("chat-composer-dropzone"), {
      dataTransfer: { files: [fileOf("sketch.png")] },
    });
    expect(onFilesChange.mock.calls[0]?.[0].map((f: File) => f.name)).toEqual(["sketch.png"]);
  });
});
