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

import { describe, expect, it } from "vitest";
import { render, screen, within } from "@testing-library/react";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import type { SpecFileEntry } from "../api/mapping";
import { SpecFileList } from "./SpecFileList";

/** The list as `SpecView` hands it over: deduped and sorted by path. */
function entries(...paths: string[]): SpecFileEntry[] {
  return paths
    .map((path) => ({ path, sha: "sha", group: "requirements" as const }))
    .sort((a, b) => a.path.localeCompare(b.path));
}

function renderList(files: SpecFileEntry[]) {
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <SpecFileList
        files={files}
        selection={null}
        onSelect={() => {}}
        onAddArtifact={() => {}}
        onRegenerateDesign={() => {}}
        deriving={false}
        failed={false}
      />
    </OxygenUIThemeProvider>,
  );
  // The Requirements group's own rows, in render order.
  const nav = screen.getByRole("navigation", { name: "Spec files" });
  return within(nav)
    .getAllByRole("button")
    .map((b) => b.textContent)
    .filter((t): t is string => Boolean(t) && t !== "");
}

describe("SpecFileList — the PRD leads Requirements", () => {
  it("puts the PRD first even though features/ sorts above it by path", () => {
    const rows = renderList(
      entries(
        "specs/requirements/features/approvals.md",
        "specs/requirements/prd.md",
        "specs/requirements/features/receipts.md",
      ),
    );
    expect(rows.slice(0, 3)).toEqual(["prd.md", "approvals.md", "receipts.md"]);
  });

  it("keeps the rest in path order behind it", () => {
    const rows = renderList(
      entries(
        "specs/requirements/zebra.md",
        "specs/requirements/prd.md",
        "specs/requirements/alpha.md",
      ),
    );
    expect(rows.slice(0, 3)).toEqual(["prd.md", "alpha.md", "zebra.md"]);
  });

  it("is untroubled by a project whose PRD has not been written yet", () => {
    const rows = renderList(entries("specs/requirements/features/receipts.md"));
    expect(rows[0]).toBe("receipts.md");
  });
});
