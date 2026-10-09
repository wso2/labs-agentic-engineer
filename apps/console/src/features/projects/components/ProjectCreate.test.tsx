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
// succeeds instantly (the details step is reached by then), upload behavior
// is per-test.
const createProject = {
  mutate: vi.fn(
    (body: { name: string }, opts?: { onSuccess?: (p: { name: string }) => void }) =>
      opts?.onSuccess?.({ name: body.name }),
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
  useGithubOrg: () => "acme",
  useUploadReferences: () => uploadReferences,
}));

// The chat holds the released kickoff; the create flow only asks for it.
const releaseKickoff = vi.fn();
vi.mock("../../agent-chat/useProjectChat", () => ({
  releaseKickoff: (projectName: string) => releaseKickoff(projectName),
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

function toDetails(): void {
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
}

beforeEach(() => {
  vi.clearAllMocks();
  uploadReferences.isError = false;
  uploadReferences.error = null;
});

describe("ProjectCreate reference documents (#383)", () => {
  it("shows an attached file in the composer", () => {
    render(<ProjectCreate />);
    attach("prd.md");
    expect(screen.getByText(/prd\.md/)).toBeTruthy();
  });

  it("rejects an unsupported extension with a per-file notice", () => {
    render(<ProjectCreate />);
    attach("spec.odt");
    expect(screen.queryByText(/spec\.odt \(/)).toBeNull();
    expect(screen.getByText(/files are accepted/i)).toBeTruthy();
  });

  // Two rejections can carry one name: the same unsupported file picked twice
  // in a selection. Each gets its own notice, and dismissing one leaves the
  // other standing.
  it("keeps one notice per rejected file, dismissed one at a time", () => {
    render(<ProjectCreate />);
    attachAll(["spec.odt", "spec.odt"]);
    expect(screen.getAllByText(/was not attached/i)).toHaveLength(2);

    fireEvent.click(screen.getAllByRole("button", { name: /close/i })[0]!);
    expect(screen.getAllByText(/was not attached/i)).toHaveLength(1);
  });

  it("creates without an upload when nothing is attached", () => {
    render(<ProjectCreate />);
    typePrompt();
    toDetails();
    fireEvent.click(screen.getByRole("button", { name: "Create project" }));
    expect(uploadReferences.mutate).not.toHaveBeenCalled();
    expect(navigate).toHaveBeenCalled();
  });

  it("navigates to the project once the upload succeeds", () => {
    uploadReferences.mutate.mockImplementationOnce(
      (_vars: { projectName: string; files: File[] }, opts?: { onSuccess?: () => void }) =>
        opts?.onSuccess?.(),
    );
    render(<ProjectCreate />);
    attach("prd.md");
    typePrompt();
    toDetails();
    fireEvent.click(screen.getByRole("button", { name: "Create project" }));

    expect(uploadReferences.mutate).toHaveBeenCalledTimes(1);
    expect(navigate).toHaveBeenCalledWith(expect.objectContaining({ to: "/projects/$projectName" }));
  });

  it("uploads after create and, on failure, offers Retry and Continue", () => {
    // The double records the call but never succeeds; the component re-renders
    // reading isError once the flow has marked the project created.
    uploadReferences.isError = true;
    uploadReferences.error = new Error("boom");
    render(<ProjectCreate />);
    attach("prd.md");
    typePrompt();
    toDetails();
    fireEvent.click(screen.getByRole("button", { name: "Create project" }));

    expect(uploadReferences.mutate).toHaveBeenCalledTimes(1);
    expect(navigate).not.toHaveBeenCalled();
    expect(screen.getByText(/uploading the reference documents failed/i)).toBeTruthy();

    // Retry replaces the create action and re-fires only the upload.
    fireEvent.click(screen.getByRole("button", { name: "Retry upload" }));
    expect(uploadReferences.mutate).toHaveBeenCalledTimes(2);
    expect(createProject.mutate).toHaveBeenCalledTimes(1);

    // The explicit escape navigates without the documents, releasing the
    // kickoff the platform held for them.
    fireEvent.click(screen.getByRole("button", { name: "Continue without documents" }));
    expect(navigate).toHaveBeenCalled();
    expect(releaseKickoff).toHaveBeenCalledWith("todo");
  });
});

// The journey starts itself (#562): the platform fires `/start` server-side,
// and this page's job is only to say whether to wait for the documents, then
// land the user where they can watch it happen.
describe("ProjectCreate: handing the journey over (#562)", () => {
  it("lands on the overview with the agent chat open", () => {
    render(<ProjectCreate />);
    typePrompt();
    toDetails();
    fireEvent.click(screen.getByRole("button", { name: "Create project" }));

    expect(navigate).toHaveBeenCalledWith(
      expect.objectContaining({
        to: "/projects/$projectName",
        search: { chat: "open" },
      }),
    );
    // The platform fired the kickoff from the create itself.
    expect(releaseKickoff).not.toHaveBeenCalled();
  });

  it("sends the prompt with the create", () => {
    render(<ProjectCreate />);
    typePrompt();
    toDetails();
    fireEvent.click(screen.getByRole("button", { name: "Create project" }));

    expect(createProject.mutate).toHaveBeenCalledWith(
      expect.objectContaining({ prompt: "A todo app" }),
      expect.anything(),
    );
  });

  // Documents are the primary brief, so the create call has to say they are
  // coming; otherwise the kickoff interviews before they land.
  it("declares pending references so the platform holds the kickoff", () => {
    render(<ProjectCreate />);
    attach("prd.md");
    typePrompt();
    toDetails();
    fireEvent.click(screen.getByRole("button", { name: "Create project" }));

    expect(createProject.mutate).toHaveBeenCalledWith(
      expect.objectContaining({ referencesPending: true }),
      expect.anything(),
    );
  });

  // Nothing to wait for: the kickoff fires from the create call itself, and a
  // field claiming otherwise would hold it forever.
  it("declares nothing when no documents are attached", () => {
    render(<ProjectCreate />);
    typePrompt();
    toDetails();
    fireEvent.click(screen.getByRole("button", { name: "Create project" }));

    expect(createProject.mutate).toHaveBeenCalledWith(
      expect.not.objectContaining({ referencesPending: expect.anything() }),
      expect.anything(),
    );
  });
});

// The create flow's copy and its one field-level failure (#561).
describe("ProjectCreate copy (#561)", () => {
  beforeEach(() => {
    createProject.isPending = false;
    createProject.isError = false;
    createProject.error = null;
  });

  /** Walk the prompt step with an example, so the name/repo step is on screen. */
  function reachNameStep() {
    render(<ProjectCreate />);
    fireEvent.click(screen.getByRole("button", { name: /Expense approval/ }));
    toDetails();
  }

  it("fills the prompt from an example without leaving the step", () => {
    render(<ProjectCreate />);
    fireEvent.click(screen.getByRole("button", { name: /Employee onboarding/ }));
    expect(screen.getByRole("textbox")).toHaveValue(
      "Track each new hire's onboarding tasks across IT, HR and facilities, with reminders for overdue items",
    );
    expect(screen.getByRole("button", { name: "Continue" })).toBeEnabled();
    expect(createProject.mutate).not.toHaveBeenCalled();
  });

  it("offers examples for the persona, not consumer apps", () => {
    render(<ProjectCreate />);
    expect(screen.getByText("Expense approval")).toBeInTheDocument();
    expect(screen.getByText("Employee onboarding")).toBeInTheDocument();
    expect(screen.getByText("Triage agent")).toBeInTheDocument();
    // The placeholder is an example too.
    expect(screen.getByPlaceholderText(/service desk/)).toBeInTheDocument();
  });

  it("suggests the project and repository names from the prompt", () => {
    reachNameStep();
    expect(screen.getByLabelText("Project name")).toHaveValue("employees-submit-expense");
    expect(screen.getByLabelText("Repository name")).toHaveValue("employees-submit-expense");
  });

  it("labels the idea as the prompt, on one line however long it is", () => {
    reachNameStep();
    const echo = screen.getByTitle(/payroll/);
    expect(echo).toHaveTextContent(/^Prompt: Employees submit expense claims/);
    const css = getComputedStyle(echo);
    expect(css.whiteSpace).toBe("nowrap");
    expect(css.textOverflow).toBe("ellipsis");
    expect(css.overflow).toBe("hidden");
  });

  it("says the repository is created, rather than implying it exists", () => {
    reachNameStep();
    expect(
      screen.getByText(/Agentic Engineer creates this repository in your organization/),
    ).toBeInTheDocument();
  });

  it("flags a name the platform would refuse", () => {
    reachNameStep();
    fireEvent.change(screen.getByLabelText("Project name"), { target: { value: "Acme Expenses" } });
    expect(screen.getAllByText(/Lowercase letters, digits, and dashes/)).toHaveLength(2);
    expect(screen.getByRole("button", { name: "Create project" })).toBeDisabled();
  });

  it("names what is being made while it waits", () => {
    createProject.isPending = true;
    reachNameStep();
    expect(screen.getByRole("button", { name: /Creating your project/ })).toBeInTheDocument();
  });

  it("puts a taken repository name on the field, naming the org", () => {
    createProject.isError = true;
    createProject.error = new ApiRequestError({ code: "conflict", message: "server wording" }, "fallback");
    reachNameStep();
    expect(
      screen.getByText("That repository name already exists in acme. Pick another."),
    ).toBeInTheDocument();
    // A field failure is not a page failure: the Alert stays away, and the
    // BFF's own wording is not shown twice.
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.queryByText("server wording")).not.toBeInTheDocument();
  });

  // A create the platform could not clear of an earlier delete is not a
  // taken name; it says to wait, and leaves the name field alone.
  it("says an earlier delete is still finishing, not that the name is taken", () => {
    createProject.isError = true;
    createProject.error = new ApiRequestError(
      { code: "project_delete_pending", message: "server wording" },
      "fallback",
    );
    reachNameStep();
    expect(screen.getByRole("alert")).toHaveTextContent(
      "An earlier delete of this project is still finishing. Try again in a minute.",
    );
    expect(screen.queryByText(/already exists/)).not.toBeInTheDocument();
    expect(screen.queryByText("server wording")).not.toBeInTheDocument();
  });

  // Not connected is the user's to fix in Settings, not a failure to
  // read out; the server's wording is replaced by the console's.
  it("sends a create refused for no GitHub connection to Settings' GitHub section", () => {
    createProject.isError = true;
    createProject.error = new ApiRequestError(
      { code: "github_not_connected", message: "server wording" },
      "fallback",
    );
    reachNameStep();
    expect(screen.getByRole("alert")).toHaveTextContent("Connect GitHub to continue");
    expect(screen.queryByText("server wording")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Connect GitHub" }));
    expect(navigate).toHaveBeenCalledWith({ to: "/settings", search: { section: "github" } });
  });

  it("offers Try again while AE Studio restarts, and it creates again", () => {
    createProject.isError = true;
    createProject.error = new ApiRequestError(
      { code: "ae_studio_unavailable", message: "server wording" },
      "fallback",
    );
    reachNameStep();
    expect(screen.getByRole("alert")).toHaveTextContent("AE Studio is restarting — try again");
    createProject.mutate.mockClear();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(createProject.mutate).toHaveBeenCalledTimes(1);
  });

  it("names the operator, not the server, when AE Studio is misconfigured", () => {
    createProject.isError = true;
    createProject.error = new ApiRequestError(
      { code: "ae_studio_misconfigured", message: "server wording" },
      "fallback",
    );
    reachNameStep();
    expect(screen.getByRole("alert")).toHaveTextContent(
      "AE Studio is misconfigured — contact your administrator",
    );
    expect(screen.queryByText("server wording")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  });

  it("still shows an Alert for a failure the user cannot fix in the form", () => {
    createProject.isError = true;
    createProject.error = new ApiRequestError({ code: "internal_error", message: "boom" }, "fallback");
    reachNameStep();
    expect(screen.getByRole("alert")).toHaveTextContent("boom");
  });
});
