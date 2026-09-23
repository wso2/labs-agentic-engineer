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
import { ImportRequirementsDialog } from "./ImportRequirementsDialog";

const mutate = vi.fn();
const reset = vi.fn();

vi.mock("../api/queries", () => ({
  useImportRequirements: () => ({
    mutate,
    isPending: false,
    isError: false,
    error: null,
    reset,
  }),
}));

describe("ImportRequirementsDialog", () => {
  it("calls mutate with the selected tarball on Import", () => {
    mutate.mockClear();
    render(
      <ImportRequirementsDialog
        open
        projectName="proj1"
        onClose={vi.fn()}
      />,
    );
    const file = new File(["bundle"], "requirements-bundle.tar.gz", {
      type: "application/gzip",
    });
    fireEvent.change(screen.getByLabelText(/choose file/i), {
      target: { files: [file] },
    });
    fireEvent.click(screen.getByRole("button", { name: "Import" }));
    expect(mutate).toHaveBeenCalledWith(file, expect.any(Object));
  });

  it("rejects non-tarball uploads locally", () => {
    mutate.mockClear();
    render(
      <ImportRequirementsDialog
        open
        projectName="proj1"
        onClose={vi.fn()}
      />,
    );
    const file = new File(["# PRD"], "prd.md", { type: "text/markdown" });
    fireEvent.change(screen.getByLabelText(/choose file/i), {
      target: { files: [file] },
    });
    fireEvent.click(screen.getByRole("button", { name: "Import" }));
    expect(mutate).not.toHaveBeenCalled();
    expect(
      screen.getByText(/upload a \.tar\.gz \/ \.tgz requirements bundle/i),
    ).toBeInTheDocument();
  });
});
