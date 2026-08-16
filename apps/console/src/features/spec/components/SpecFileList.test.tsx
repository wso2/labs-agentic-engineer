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

// The file nav as journey stepper (#485): empty groups render GHOST entries
// naming the upcoming artifact and when it will exist, and the labels point
// forward as real files graduate. A failed derivation must say so instead —
// an error state never dresses up as a plan.

import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { SpecFileEntry } from "../api/mapping";
import { SpecFileList } from "./SpecFileList";

const noop = vi.fn();

function renderList(files: SpecFileEntry[], failed = false) {
  return render(
    <SpecFileList
      files={files}
      selection={null}
      onSelect={noop}
      onAddArtifact={noop}
      onRegenerateDesign={noop}
      failed={failed}
    />,
  );
}

const PRD: SpecFileEntry = {
  path: "specs/requirements/prd.md",
  sha: "abc",
  group: "requirements",
};

describe("SpecFileList — ghost entries", () => {
  it("names every upcoming artifact with a when-label on a fresh project", () => {
    renderList([]);

    const ghosts = screen.getAllByTestId("ghost-file");
    expect(ghosts).toHaveLength(5);
    expect(screen.getByText("prd.md")).toBeInTheDocument();
    expect(screen.getByText("written after your answers")).toBeInTheDocument();
    expect(screen.getByText("architecture")).toBeInTheDocument();
    // Before the PRD exists, the design ghosts derive from it.
    expect(screen.getAllByText("derived from the PRD")).toHaveLength(2);
    expect(screen.getByText("design.md · security.md")).toBeInTheDocument();
    expect(screen.getByText("components/…")).toBeInTheDocument();
    expect(screen.getByText("criteria")).toBeInTheDocument();
    expect(screen.getByText("minted after design")).toBeInTheDocument();
  });

  it("graduates: a real prd.md replaces its ghost and points the next ghost forward", () => {
    renderList([PRD]);

    // prd.md is now a real nav row, not a ghost.
    expect(screen.getByRole("button", { name: "prd.md" })).toBeInTheDocument();
    expect(screen.queryByText("written after your answers")).not.toBeInTheDocument();
    expect(screen.getAllByTestId("ghost-file")).toHaveLength(4);
    // The architecture ghost re-labels to the step that actually gates it.
    expect(screen.getByText("next — after you review the PRD")).toBeInTheDocument();
  });

  it("says Derivation failed instead of ghosts when the last turn failed", () => {
    renderList([], true);

    expect(screen.queryAllByTestId("ghost-file")).toHaveLength(0);
    expect(screen.getAllByText("Derivation failed")).toHaveLength(3);
  });

  it("renders no ghost for a group that has real files", () => {
    renderList([
      PRD,
      {
        path: "specs/validation/validation-criteria.json",
        sha: "def",
        group: "validation",
      },
    ]);

    // Only the Designs group is still pending → its 3 ghosts remain.
    expect(screen.getAllByTestId("ghost-file")).toHaveLength(3);
    expect(screen.queryByText("minted after design")).not.toBeInTheDocument();
  });
});
