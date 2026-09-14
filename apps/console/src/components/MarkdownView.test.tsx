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
import { describe, expect, it, vi } from "vitest";
import { MarkdownView, specLinkPath } from "./MarkdownView";

describe("MarkdownView", () => {
  it("renders headings, paragraphs, and code as their semantic elements", () => {
    const markdown = [
      "# Heading",
      "",
      "A paragraph with `inline code`.",
      "",
      "```",
      "const x = 1;",
      "```",
    ].join("\n");

    render(<MarkdownView>{markdown}</MarkdownView>);

    expect(
      screen.getByRole("heading", { level: 1, name: "Heading" }),
    ).toBeInTheDocument();
    expect(screen.getByText(/A paragraph with/)).toBeInTheDocument();
    expect(screen.getByText("inline code").tagName).toBe("CODE");
    expect(screen.getByText("const x = 1;").closest("pre")).not.toBeNull();
  });

  it("renders nothing for empty content", () => {
    const { container } = render(<MarkdownView>{""}</MarkdownView>);
    expect(container.textContent).toBe("");
  });
});

describe("MarkdownView — links into the spec (ADR-0028)", () => {
  const md = "Needs your input\n\n- [currency-service](aep://spec/specs/design/dependencies/currency-service/dependency.json) — select a provider\n- [docs](https://example.com/docs)";

  it("names the document an aep://spec link points at, and nothing else", () => {
    expect(specLinkPath("aep://spec/specs/design/dependencies/x/dependency.json")).toBe("specs/design/dependencies/x/dependency.json");
    expect(specLinkPath("aep://spec/etc/passwd")).toBeNull();
    expect(specLinkPath("https://example.com")).toBeNull();
    expect(specLinkPath(undefined)).toBeNull();
  });

  it("turns a spec link into a click that opens the document, and leaves other links alone", () => {
    const onSpecLink = vi.fn();
    render(<MarkdownView onSpecLink={onSpecLink}>{md}</MarkdownView>);
    fireEvent.click(screen.getByRole("link", { name: "currency-service" }));
    expect(onSpecLink).toHaveBeenCalledWith("specs/design/dependencies/currency-service/dependency.json");
    expect(screen.getByRole("link", { name: "docs" })).toHaveAttribute("href", "https://example.com/docs");
  });

  it("renders a spec link as plain text where nothing can open it", () => {
    render(<MarkdownView>{md}</MarkdownView>);
    expect(screen.queryByRole("link", { name: "currency-service" })).not.toBeInTheDocument();
    expect(screen.getByText(/currency-service/)).toBeInTheDocument();
  });
});
