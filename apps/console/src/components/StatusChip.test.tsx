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

import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { StatusChip, type StatusTone } from "./StatusChip";

describe("StatusChip", () => {
  it.each([
    ["success", "MuiChip-colorSuccess"],
    ["info", "MuiChip-colorInfo"],
    ["warning", "MuiChip-colorWarning"],
    ["error", "MuiChip-colorError"],
    ["neutral", "MuiChip-colorDefault"],
    ["primary", "MuiChip-colorPrimary"],
  ] satisfies [StatusTone, string][])(
    "maps tone %s to the Oxygen Chip color class %s",
    (tone, className) => {
      render(<StatusChip label="Building" tone={tone} />);
      const chip = screen.getByText("Building").closest(".MuiChip-root");
      expect(chip).toHaveClass(className);
    },
  );

  it("renders the label as the chip's visible text", () => {
    render(<StatusChip label="On hold" tone="warning" />);
    expect(screen.getByText("On hold")).toBeInTheDocument();
  });

  it("defaults to the filled variant", () => {
    render(<StatusChip label="Done" tone="success" />);
    expect(screen.getByText("Done").closest(".MuiChip-root")).toHaveClass(
      "MuiChip-filled",
    );
  });

  it("renders outlined when requested", () => {
    render(<StatusChip label="Mixed" tone="warning" variant="outlined" />);
    expect(screen.getByText("Mixed").closest(".MuiChip-root")).toHaveClass(
      "MuiChip-outlined",
    );
  });

  // For a label that hedges with a MARK — "Validated*" — which no screen reader
  // announces, so it would be heard as "Validated" and be indistinguishable from a
  // clean pass. Visually-hidden text rather than aria-label on the root: a Chip with
  // no onClick renders a plain div with no role, and an aria-label there is ignored.
  describe("spokenLabel", () => {
    it("hides the marked label from the a11y tree and speaks the spelled-out one", () => {
      render(
        <StatusChip label="Validated*" spokenLabel="Validated, partially" tone="success" />,
      );
      // The VALUE matters: `aria-hidden="false"` would satisfy a bare existence
      // check while leaving the marked label exposed to assistive technology.
      expect(screen.getByText("Validated*")).toHaveAttribute("aria-hidden", "true");
      expect(screen.getByText("Validated, partially")).toBeInTheDocument();
    });

    it("does the same for the soft appearance the page title uses", () => {
      render(
        <StatusChip
          label="Validated*"
          spokenLabel="Validated, partially"
          tone="success"
          appearance="soft"
          dot
        />,
      );
      // The soft label nests the text inside the dot wrapper, so the hidden element
      // is an ANCESTOR — asserting presence alone would pass with nothing hidden at
      // all. This is the appearance the page title uses, so it is the one that counts.
      expect(
        screen.getByText("Validated*").closest('[aria-hidden="true"]'),
      ).toBeInTheDocument();
      expect(screen.getByText("Validated, partially")).toBeInTheDocument();
    });

    // The default stays "the name is the visible label" — no wrapper, no duplicate.
    it("leaves an unmarked label alone", () => {
      render(<StatusChip label="Validated" tone="success" />);
      expect(screen.getByText("Validated")).not.toHaveAttribute("aria-hidden");
    });
  });
});
