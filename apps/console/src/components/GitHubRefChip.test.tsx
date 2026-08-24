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
import { GitHubRefChip } from "./GitHubRefChip";

const ISSUE = "https://github.com/acme/demo/issues/12";
const PULL = "https://github.com/acme/demo/pull/41";

describe("GitHubRefChip", () => {
  it("shows the number and opens the host page in a new tab", () => {
    render(<GitHubRefChip kind="issue" number={12} url={ISSUE} />);
    const link = screen.getByRole("link", { name: "GitHub issue #12" });
    expect(link).toHaveAttribute("href", ISSUE);
    expect(link).toHaveAttribute("target", "_blank");
    // Without noreferrer the opened tab can reach back through window.opener.
    expect(link).toHaveAttribute("rel", "noreferrer");
    // The number is VISIBLE, not only in the accessible name — a bare icon does
    // not say which issue it opens.
    expect(screen.getByText("#12")).toBeInTheDocument();
  });

  // The two kinds are shown side by side, so they must not be twins: an issue and
  // the pull request answering it differ by glyph, not only by digits.
  it("gives each kind its own default name", () => {
    const { unmount } = render(<GitHubRefChip kind="issue" number={12} url={ISSUE} />);
    expect(screen.getByRole("link", { name: "GitHub issue #12" })).toBeInTheDocument();
    unmount();
    render(<GitHubRefChip kind="pull" number={41} url={PULL} />);
    expect(screen.getByRole("link", { name: "Pull request #41" })).toBeInTheDocument();
  });

  // A page can show the SAME pull request twice — once as "the one for this
  // validation", once as "the one this cycle produced" — so the caller owns the
  // words and the two stay distinguishable to a screen reader.
  it("lets the caller name what it points at", () => {
    render(
      <>
        <GitHubRefChip kind="pull" number={41} url={PULL} name="Validation pull request" />
        <GitHubRefChip kind="pull" number={41} url={PULL} name="Cycle 2 pull request" />
      </>,
    );
    expect(
      screen.getByRole("link", { name: "Validation pull request #41" }),
    ).toHaveAttribute("href", PULL);
    expect(
      screen.getByRole("link", { name: "Cycle 2 pull request #41" }),
    ).toHaveAttribute("href", PULL);
  });

  it("stops the click there when the caller asks", () => {
    const onClick = vi.fn((e: { stopPropagation: () => void }) => {
      e.stopPropagation();
    });
    const onRowClick = vi.fn();
    render(
      <div onClick={onRowClick}>
        <GitHubRefChip kind="pull" number={41} url={PULL} onClick={onClick} />
      </div>,
    );
    fireEvent.click(screen.getByRole("link", { name: "Pull request #41" }));
    expect(onClick).toHaveBeenCalled();
    // The row's handler must NOT fire — that is what keeps a chip inside an
    // accordion summary from collapsing the section it sits in.
    expect(onRowClick).not.toHaveBeenCalled();
  });
});
