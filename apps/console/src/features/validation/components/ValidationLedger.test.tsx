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
import type { components } from "../../../generated/aep-api";

type ValidationSummary = components["schemas"]["ValidationSummary"];

const navigate = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children?: React.ReactNode }) => <a>{children}</a>,
  useNavigate: () => navigate,
  createLink:
    () =>
    ({
      to,
      params,
      children,
    }: {
      to: string;
      params?: Record<string, string>;
      children?: React.ReactNode;
    }) => (
      <a href={to.replace(/\$(\w+)/g, (_, key: string) => params?.[key] ?? "")}>
        {children}
      </a>
    ),
}));

let mockRows: ValidationSummary[] = [];
let mockState = { isPending: false, isError: false };
const refetch = vi.fn();
vi.mock("../api/queries", () => ({
  useValidations: () => ({
    data: mockRows,
    isPending: mockState.isPending,
    isError: mockState.isError,
    error: mockState.isError ? new Error("boom") : null,
    refetch,
  }),
}));

import { ValidationLedger } from "./ValidationLedger";

const row = (over: Partial<ValidationSummary> = {}): ValidationSummary => ({
  tag: "v1",
  milestoneNumber: 1,
  state: "passed",
  startedAt: "2026-08-14T16:20:00Z",
  endedAt: "2026-08-14T16:52:47Z",
  ...over,
});

beforeEach(() => {
  mockRows = [];
  mockState = { isPending: false, isError: false };
  navigate.mockClear();
});

describe("ValidationLedger", () => {
  // The row set is the whole reason the page was rebuilt: the old one could
  // only ever show the newest milestone.
  it("lists every version, including one never validated", () => {
    mockRows = [row(), row({ tag: "v0.9", milestoneNumber: 2, state: "none", startedAt: null, endedAt: null })];
    render(<ValidationLedger projectName="p" />);

    expect(screen.getByText("v1")).toBeInTheDocument();
    expect(screen.getByText("v0.9")).toBeInTheDocument();
    // `none` has no chip — the mapper returns null for it — so the cell says it
    // in words rather than badging a non-state.
    expect(screen.getByText("Not validated")).toBeInTheDocument();
  });

  it("opens the version when its row is clicked", () => {
    mockRows = [row()];
    render(<ValidationLedger projectName="p" />);
    fireEvent.click(screen.getByText("v1"));
    expect(navigate).toHaveBeenCalledWith({
      to: "/projects/$projectName/validations/$tag",
      params: { projectName: "p", tag: "v1" },
    });
  });

  // A running attempt has no end. The dash is deliberate: a blank cell beside a
  // ticking Duration reads as a rendering fault.
  it("shows a dash for the end of an attempt still in flight", () => {
    mockRows = [row({ state: "running", endedAt: null })];
    render(<ValidationLedger projectName="p" />);
    expect(screen.getAllByText("—").length).toBeGreaterThan(0);
  });

  // ADR-0016: one mapper owns label and tone. The ledger is a consumer of it,
  // not a second reading — the drift this pins is a page that said
  // "Validation failed" while the board said "awaiting fix" for one run.
  it("labels each state with the shared vocabulary", () => {
    mockRows = [
      row({ tag: "a", state: "partial" }),
      row({ tag: "b", state: "awaiting-fix" }),
      row({ tag: "c", state: "unreported" }),
    ];
    render(<ValidationLedger projectName="p" />);
    expect(screen.getByText("Validated*")).toBeInTheDocument();
    expect(screen.getByText("Awaiting fix")).toBeInTheDocument();
    expect(screen.getByText("Validation error")).toBeInTheDocument();
  });

  it("groups states by what a reader would do about them", () => {
    mockRows = [
      row({ tag: "ok", state: "passed" }),
      row({ tag: "bad", state: "failed" }),
      row({ tag: "err", state: "unreported" }),
    ];
    render(<ValidationLedger projectName="p" />);

    fireEvent.mouseDown(screen.getByRole("combobox"));
    fireEvent.click(screen.getByRole("option", { name: "Needs attention" }));

    // `failed` and `unreported` are different verdicts and one filter: a reader
    // scanning for problems wants one, and the cell still says which.
    expect(screen.getByText("bad")).toBeInTheDocument();
    expect(screen.getByText("err")).toBeInTheDocument();
    expect(screen.queryByText("ok")).not.toBeInTheDocument();
  });

  it("says a filter matched nothing without claiming the history is empty", () => {
    mockRows = [row({ state: "passed" })];
    render(<ValidationLedger projectName="p" />);

    fireEvent.mouseDown(screen.getByRole("combobox"));
    fireEvent.click(screen.getByRole("option", { name: "Needs attention" }));

    // The whole sentence, not just the action: the filter's menu label is a
    // noun phrase, and reading it back into "No versions are …" is how this
    // line came to say "No versions are needs attention".
    expect(
      screen.getByText(
        "No versions are in need of attention. Clear the filter to see every version.",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText("Nothing to validate yet")).not.toBeInTheDocument();
  });

  it("offers a retry when the read fails", () => {
    mockState = { isPending: false, isError: true };
    render(<ValidationLedger projectName="p" />);
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(refetch).toHaveBeenCalled();
  });
});
