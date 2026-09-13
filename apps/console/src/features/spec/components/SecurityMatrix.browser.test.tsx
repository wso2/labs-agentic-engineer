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

/**
 * The matrix against a real layout engine.
 *
 * The jsdom suite proves the GRID — which cells are filled, which row a warning
 * lands on. It cannot prove the thing that makes a matrix readable: that every
 * mark in a column sits under that column's heading, and that the two baseline
 * rows span the grid instead of pushing it sideways. jsdom gives every element
 * a zero-sized box, so those are assertions only a browser can make.
 *
 * There is deliberately no screenshot here: the console has no visual-baseline
 * infrastructure and no committed snapshots, so a first one would fail on any
 * font or renderer difference between a laptop and CI without telling anybody
 * anything about this page. The geometry below is the part of a screenshot that
 * carries meaning, asserted in a way that does not rot.
 */

import { render } from "@testing-library/react";
import { page } from "@vitest/browser/context";
import { afterEach, describe, expect, it } from "vitest";

import {
  EXPENSE_TRACKER_REFERENCES,
  EXPENSE_TRACKER_TEXT,
} from "../lib/securityTestFixtures";
import { SecurityPanel } from "./SecurityPanel";

afterEach(() => {
  document.body.innerHTML = "";
});

/** The horizontal middle of an element's box. */
function centerX(element: Element): number {
  const box = element.getBoundingClientRect();
  return box.left + box.width / 2;
}

function renderMatrix(): void {
  render(
    <SecurityPanel
      securityJson={EXPENSE_TRACKER_TEXT}
      references={EXPENSE_TRACKER_REFERENCES}
      roomLive
      writeSecurityJson={() => {}}
    />,
  );
}

describe("the permission matrix, laid out", () => {
  it("puts every mark in a column under that column's heading", async () => {
    renderMatrix();

    for (const role of ["Employee", "Approver"]) {
      const heading = page
        .getByRole("columnheader", { name: role, exact: true })
        .element();
      const marks = [
        "claims:read",
        "claims:submit",
        "reports:read",
        "reports:export",
      ].map((handle) =>
        page
          .getByRole("checkbox", {
            name: `${role} grants ${handle}`,
            exact: true,
          })
          .element(),
      );
      for (const mark of marks) {
        expect(Math.abs(centerX(mark) - centerX(heading))).toBeLessThan(2);
      }
    }
  });

  it("spans the baseline rows across the grid rather than widening it", async () => {
    renderMatrix();

    const table = page.getByRole("table").element();
    const baselineCell = page
      .getByText("any signed-in user")
      .element()
      .closest("td")!.nextElementSibling!;

    const tableBox = table.getBoundingClientRect();
    const cellBox = baselineCell.getBoundingClientRect();
    // The spanning cell reaches the table's right edge...
    expect(Math.abs(cellBox.right - tableBox.right)).toBeLessThan(4);
    // ...and the grid itself does not scroll sideways.
    expect(table.scrollWidth).toBeLessThanOrEqual(table.clientWidth + 1);
  });
});
