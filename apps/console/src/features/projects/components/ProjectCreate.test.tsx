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
import { beforeEach, describe, expect, it, vi } from "vitest";

const navigate = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigate,
}));

// The mutation doubles below are plain objects the component reads flags
// from; each test primes them for the state it exercises. Create always
// succeeds instantly (the confirm step is reached by then), upload behavior
// is per-test.
const createProject = {
  mutate: vi.fn(
    (
      body: { name: string },
      opts?: { onSuccess?: (p: { name: string }) => void },
    ) => opts?.onSuccess?.({ name: body.name }),
  ),
  reset: vi.fn(),
  isPending: false,
  isError: false,
  error: null as Error | null,
};
const uploadReferences = {
  mutate: vi.fn(),
  reset: vi.fn(),
  isPending: false,
  isError: false,
  error: null as Error | null,
};
vi.mock("../api/queries", () => ({
  useCreateProject: () => createProject,
  useGithubOrg: () => ({ data: "acme" }),
  useUploadReferences: () => uploadReferences,
}));

import { ApiRequestError } from "../../../api/errors";
import { ProjectCreate } from "./ProjectCreate";

function attachAll(names: string[], content = "content"): void {
  const input = document.querySelector<HTMLInputElement>("input[type=file]");
  expect(input).not.toBeNull();
  fireEvent.change(input!, {
    target: { files: names.map((name) => new File([content], name)) },
  });
}

function attach(name: string, content = "content"): void {
  attachAll([name], content);
}

function typePrompt(): void {
  // By role, not by placeholder: the placeholder is copy (#561 changed it) and
  // a behavior test should not break when the wording does.
  fireEvent.change(screen.getByRole("textbox"), {
    target: { value: "A todo app" },
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  uploadReferences.isError = false;
  uploadReferences.error = null;
});

describe("ProjectCreate reference documents (#383)", () => {
  it("shows an attached file as a card in the composer", () => {
    render(<ProjectCreate />);
    attach("prd.md");
    expect(screen.getByText(/prd\.md/)).toBeTruthy();
  });

  it("rejects an unsupported extension with a per-file notice", () => {
    render(<ProjectCreate />);
    attach("spec.docx");
    expect(screen.queryByText(/spec\.docx \(/)).toBeNull();
    expect(screen.getByText(/files are accepted/i)).toBeTruthy();
  });

  // Two rejections can carry one name — the same unsupported file picked twice
  // in a selection. Each gets its own notice, and dismissing one leaves the
  // other standing.
  it("keeps one notice per rejected file, dismissed one at a time", () => {
    render(<ProjectCreate />);
    attachAll(["spec.docx", "spec.docx"]);
    expect(screen.getAllByText(/was not attached/i)).toHaveLength(2);

    fireEvent.click(screen.getAllByRole("button", { name: /close/i })[0]!);
    expect(screen.getAllByText(/was not attached/i)).toHaveLength(1);
  });

  it("creates without an upload when nothing is attached", () => {
    render(<ProjectCreate />);
    typePrompt();
    fireEvent.click(screen.getByRole("button", { name: "Start" }));
    fireEvent.click(screen.getByRole("button", { name: "Create project" }));
    expect(uploadReferences.mutate).not.toHaveBeenCalled();
    expect(navigate).toHaveBeenCalled();
  });

  // The success path was unproven: the double never invoked onSuccess, so
  // nothing showed that a completed upload actually reaches the project.
  it("navigates to the project once the upload succeeds", () => {
    uploadReferences.mutate.mockImplementationOnce(
      (
        _vars: { projectName: string; files: File[] },
        opts?: { onSuccess?: () => void },
      ) => opts?.onSuccess?.(),
    );
    render(<ProjectCreate />);
    attach("prd.md");
    typePrompt();
    fireEvent.click(screen.getByRole("button", { name: "Start" }));
    fireEvent.click(screen.getByRole("button", { name: "Create project" }));

    expect(uploadReferences.mutate).toHaveBeenCalledTimes(1);
    expect(navigate).toHaveBeenCalledWith(
      expect.objectContaining({ to: "/projects/$projectName" }),
    );
  });

  it("uploads after create and, on failure, offers Retry and Continue", () => {
    // The double records the call but never succeeds; the component re-renders
    // reading isError once the flow has marked the project created.
    uploadReferences.isError = true;
    uploadReferences.error = new Error("boom");
    render(<ProjectCreate />);
    attach("prd.md");
    typePrompt();
    fireEvent.click(screen.getByRole("button", { name: "Start" }));
    fireEvent.click(screen.getByRole("button", { name: "Create project" }));

    expect(uploadReferences.mutate).toHaveBeenCalledTimes(1);
    expect(navigate).not.toHaveBeenCalled();
    expect(
      screen.getByText(/uploading the reference documents failed/i),
    ).toBeTruthy();

    // Retry replaces the create action and re-fires only the upload.
    fireEvent.click(screen.getByRole("button", { name: "Retry upload" }));
    expect(uploadReferences.mutate).toHaveBeenCalledTimes(2);
    expect(createProject.mutate).toHaveBeenCalledTimes(1);

    // The explicit escape navigates without the documents.
    fireEvent.click(
      screen.getByRole("button", { name: "Continue without documents" }),
    );
    expect(navigate).toHaveBeenCalled();
  });
});


// The create flow's copy and its one field-level failure (#561). Shares the
// mutation doubles above — `createProject.mutate` resolves instantly, so
// reaching the name step is a single click on an example.
describe("ProjectCreate copy (#561)", () => {
  beforeEach(() => {
    createProject.isPending = false;
    createProject.isError = false;
    createProject.error = null;
  });

  /** Walk the prompt step so the name/repo step is on screen. */
  function reachNameStep() {
    render(<ProjectCreate />);
    fireEvent.click(screen.getByRole("button", { name: /Expense approval/ }));
  }

  it("asks for the idea without promising what happens next", () => {
    render(<ProjectCreate />);
    expect(
      screen.getByText(/Describe it in your own words — rough is fine/),
    ).toBeInTheDocument();
    // The journey explains itself as it happens (#522); the create page does
    // not narrate it, and never claims design starts first.
    expect(screen.queryByText(/deriving its design/)).not.toBeInTheDocument();
  });

  it("offers examples for the persona, not consumer apps", () => {
    render(<ProjectCreate />);
    expect(screen.getByText("Expense approval")).toBeInTheDocument();
    expect(screen.getByText("Employee onboarding")).toBeInTheDocument();
    expect(screen.getByText("Triage agent")).toBeInTheDocument();
    // The placeholder is an example too — it carried a hair-salon booking
    // system, which models the product as a consumer app generator.
    expect(screen.getByPlaceholderText(/service desk/)).toBeInTheDocument();
    expect(screen.queryByPlaceholderText(/hair salon/)).not.toBeInTheDocument();
  });

  it("labels the idea as the prompt, on one line however long it is", () => {
    reachNameStep();
    const echo = screen.getByText(/^Prompt:/);
    expect(echo).toHaveTextContent(/^Prompt: Employees submit expense claims/);
    const css = getComputedStyle(echo);
    expect(css.whiteSpace).toBe("nowrap");
    expect(css.textOverflow).toBe("ellipsis");
    expect(css.overflow).toBe("hidden");
    expect(echo.getAttribute("title")).toContain("payroll");
  });

  it("says the repository is created, rather than implying it exists", () => {
    reachNameStep();
    expect(
      screen.getByText(/Agentic Engineer creates this repository in your organization/),
    ).toBeInTheDocument();
  });

  it("names what is being made while it waits", () => {
    createProject.isPending = true;
    render(<ProjectCreate />);
    fireEvent.click(screen.getByRole("button", { name: /Expense approval/ }));
    expect(
      screen.getByRole("button", { name: /Creating your project/ }),
    ).toBeInTheDocument();
  });

  it("puts a taken repository name on the field, naming the org", () => {
    createProject.isError = true;
    createProject.error = new ApiRequestError(
      { code: "conflict", message: "server wording" },
      "fallback",
    );
    reachNameStep();
    expect(
      screen.getByText("That repository name already exists in acme — pick another."),
    ).toBeInTheDocument();
    // A field failure is not a page failure: the Alert stays away, and the
    // BFF's own wording is not shown twice.
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.queryByText("server wording")).not.toBeInTheDocument();
  });

  it("still shows an Alert for a failure the user cannot fix in the form", () => {
    createProject.isError = true;
    createProject.error = new ApiRequestError(
      { code: "internal_error", message: "boom" },
      "fallback",
    );
    reachNameStep();
    expect(screen.getByRole("alert")).toHaveTextContent("boom");
  });
});
