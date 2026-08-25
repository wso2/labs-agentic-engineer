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

type BuildSummary = components["schemas"]["BuildSummary"];

const navigate = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children?: React.ReactNode }) => <a>{children}</a>,
  useNavigate: () => navigate,
}));

let mockBuilds: BuildSummary[] = [];
let mockState = { isPending: false, isError: false };
const refetch = vi.fn();
vi.mock("../api/queries", () => ({
  useBuilds: () => ({
    data: mockBuilds,
    isPending: mockState.isPending,
    isError: mockState.isError,
    error: mockState.isError ? new Error("boom") : null,
    refetch,
  }),
}));

import { BuildsLedger } from "./BuildsLedger";

const build = (over: Partial<BuildSummary> = {}): BuildSummary => ({
  tag: "v1",
  milestoneNumber: 1,
  status: "completed",
  startedAt: "2026-08-14T16:20:00Z",
  completedAt: "2026-08-14T16:52:47Z",
  ...over,
});

const renderLedger = () => render(<BuildsLedger projectName="demo-shop" />);

beforeEach(() => {
  mockBuilds = [];
  mockState = { isPending: false, isError: false };
  navigate.mockClear();
  refetch.mockClear();
});

describe("BuildsLedger", () => {
  it("lists every version, newest first as the contract serves them", () => {
    mockBuilds = [
      build({ tag: "v3", status: "in_progress", milestoneTitle: "Order history" }),
      build({ tag: "v2", status: "failed", reason: "Merge conflict" }),
      build({ tag: "v1", deployedTo: ["development"] }),
    ];
    renderLedger();

    // ORDER, not membership: newest-first is the contract, and a reversed
    // table would satisfy three getByText calls just as happily.
    expect(
      screen.getAllByText(/^v[1-9]\d*$/).map((el) => el.textContent),
    ).toEqual(["v3", "v2", "v1"]);
    // The whole point of the page ADR-0020 introduced: three versions readable
    // at once, which the now-first page could never show.
    expect(screen.getByText("Running · Coding agent")).toBeTruthy();
    expect(screen.getByText("Failed · Merge conflict")).toBeTruthy();
    expect(screen.getByText("Deployed to development")).toBeTruthy();
  });

  it("opens a version's build page when its row is clicked", () => {
    mockBuilds = [build({ tag: "v2" })];
    renderLedger();

    fireEvent.click(screen.getByText("v2"));
    expect(navigate).toHaveBeenCalledWith({
      to: "/projects/$projectName/builds/$tag",
      params: { projectName: "demo-shop", tag: "v2" },
    });
  });

  it("shows a queued version without claiming it started", () => {
    // `startedAt` on a queued row is the ENQUEUE time; rendering it as a start
    // would be a claim the contract explicitly warns against.
    mockBuilds = [build({ tag: "v4", status: "queued" })];
    renderLedger();

    expect(screen.getByText("Queued · next")).toBeTruthy();
    expect(screen.getAllByText("—").length).toBeGreaterThanOrEqual(2);
  });

  it("renders the task count when the ledger carries it", () => {
    mockBuilds = [build({ taskCounts: { total: 7, done: 3 } })];
    renderLedger();
    expect(screen.getByText("3 of 7 done")).toBeTruthy();
  });

  it("makes no task claim when counts are absent", () => {
    mockBuilds = [build()];
    renderLedger();
    expect(screen.queryByText(/of 0 done/)).toBeNull();
  });

  it("filters by status, and says so rather than looking empty", () => {
    mockBuilds = [
      build({ tag: "v2", status: "in_progress" }),
      build({ tag: "v1" }),
    ];
    renderLedger();

    // MUI renders `select` as a button + popover listbox, not a native
    // <select>, so this drives it the way a reader would.
    fireEvent.mouseDown(screen.getByRole("combobox", { name: /status/i }));
    fireEvent.click(screen.getByRole("option", { name: "Failed" }));

    // Filtered-to-nothing must NOT borrow the "no builds yet" copy — the reader
    // would think their history had vanished.
    expect(screen.getByText(/No failed builds/)).toBeTruthy();
    expect(screen.queryByText("No builds yet")).toBeNull();

    fireEvent.click(screen.getByText("Clear filter"));
    expect(screen.getByText("v2")).toBeTruthy();
  });

  it("teaches how to get a first build when there are none", () => {
    renderLedger();
    expect(screen.getByText("No builds yet")).toBeTruthy();
  });

  it("offers a retry when the ledger fails to load", () => {
    mockState = { isPending: false, isError: true };
    renderLedger();

    fireEvent.click(screen.getByText("Retry"));
    expect(refetch).toHaveBeenCalled();
  });

  it("keeps the back link reachable while loading", () => {
    mockState = { isPending: true, isError: false };
    renderLedger();
    expect(screen.getByText("Back to Overview")).toBeTruthy();
  });
});
