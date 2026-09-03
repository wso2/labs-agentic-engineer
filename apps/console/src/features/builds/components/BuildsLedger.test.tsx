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
  // Just enough of createLink to see where the CTA points: interpolate the
  // `$param` segments so the href is assertable.
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

// The one read the ledger derives its remaining cells from.
let mockDeploy: components["schemas"]["DeployStage"] | undefined;
vi.mock("../../projects/api/queries", () => ({
  useProjectStatus: () => ({ data: mockDeploy ? { deploy: mockDeploy } : undefined }),
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
  mockDeploy = undefined;
  mockState = { isPending: false, isError: false };
  navigate.mockClear();
  refetch.mockClear();
});

describe("BuildsLedger", () => {
  it("lists every version, newest first as the contract serves them", () => {
    mockBuilds = [
      build({ tag: "v3", status: "in_progress" }),
      build({ tag: "v2", status: "failed", reason: "Merge conflict" }),
      build({ tag: "v1" }),
    ];
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 3, ready: 3 },
      validation: "passed",
    };
    renderLedger();

    // ORDER, not membership: newest-first is the contract, and a reversed
    // table would satisfy three getByText calls just as happily.
    expect(
      screen.getAllByText(/^v[1-9]\d*$/).map((el) => el.textContent),
    ).toEqual(["v3", "v2", "v1"]);
    // The whole point of the page ADR-0021 introduced: three versions readable
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

  it("shows no Tasks column at all", () => {
    // An untagged list-tasks response cannot be attributed to versions (the
    // server leaves `lineage.specTag` empty when the query spans versions), and
    // a tag-scoped read would be one GitHub-backed request PER ROW. The
    // breakdown lives on the build page, where the read is already scoped.
    mockBuilds = [build({ tag: "v1" })];
    renderLedger();
    expect(screen.queryByText("Tasks")).toBeNull();
    expect(screen.queryByText(/of \d+ done/)).toBeNull();
  });

  it("filters a rolling-out version under Running, not just tints it", () => {
    // The filter read `build.status`, which is `completed` during a rollout, so
    // a row showing "Deploying to development" was hidden from Running.
    mockBuilds = [build({ tag: "v2" }), build({ tag: "v1" })];
    mockDeploy = {
      version: "v1",
      status: "deploying",
      components: { total: 3, ready: 1 },
      validation: "none",
    };
    renderLedger();

    fireEvent.mouseDown(screen.getByRole("combobox", { name: /status/i }));
    fireEvent.click(screen.getByRole("option", { name: "In progress" }));

    expect(screen.getByText("Deploying to development")).toBeTruthy();
    expect(screen.queryByText("Built")).toBeNull();
  });

  it("filters a failed rollout under Failed", () => {
    mockBuilds = [build({ tag: "v2" }), build({ tag: "v1" })];
    mockDeploy = {
      version: "v1",
      status: "failed",
      components: { total: 3, ready: 1 },
      validation: "none",
    };
    renderLedger();

    fireEvent.mouseDown(screen.getByRole("combobox", { name: /status/i }));
    fireEvent.click(screen.getByRole("option", { name: "Failed" }));

    expect(screen.getByText("Deploy failed")).toBeTruthy();
    expect(screen.queryByText("Built")).toBeNull();
  });

  it("does not list a still-moving version under Completed as well", () => {
    mockBuilds = [build({ tag: "v1" })];
    mockDeploy = {
      version: "v1",
      status: "deploying",
      components: { total: 3, ready: 1 },
      validation: "none",
    };
    renderLedger();

    fireEvent.mouseDown(screen.getByRole("combobox", { name: /status/i }));
    fireEvent.click(screen.getByRole("option", { name: "Completed" }));
    expect(screen.getByText(/No completed builds/)).toBeTruthy();
  });

  it("keeps a rolling-out version live, even though its BUILD is completed", () => {
    // The row tinted on build.status, which is `completed` during a rollout —
    // so it went quiet at exactly the moment it had something to say.
    mockBuilds = [build({ tag: "v1" })];
    mockDeploy = {
      version: "v1",
      status: "deploying",
      components: { total: 3, ready: 1 },
      validation: "none",
    };
    renderLedger();
    expect(screen.getByText("Deploying to development")).toBeTruthy();
  });

  it("names a failed rollout rather than calling the version Built", () => {
    mockBuilds = [build({ tag: "v1" })];
    mockDeploy = {
      version: "v1",
      status: "failed",
      components: { total: 3, ready: 1 },
      validation: "none",
    };
    renderLedger();
    expect(screen.getByText("Deploy failed")).toBeTruthy();
  });

  it("describes only the deployed version by where it reached", () => {
    mockBuilds = [build({ tag: "v2" }), build({ tag: "v1" })];
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 3, ready: 3 },
      validation: "passed",
    };
    renderLedger();

    expect(screen.getByText("Deployed to development")).toBeTruthy();
    // v2 is completed but is not the deployed version — claiming anything about
    // where it reached would be a guess.
    expect(screen.getByText("Built")).toBeTruthy();
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

  it("says a parked version waits on the READER, and lets its row go quiet", () => {
    // ADR-0023's deploy gate. `status` is `in_progress` for a parked run as
    // much as a running one, so the row used to say "Running · Coding agent"
    // on a run that had stopped and was waiting on this reader.
    mockBuilds = [build({ tag: "v2", status: "in_progress", waitingReason: "external-values" })];
    renderLedger();

    expect(screen.getByText("Waiting for configuration")).toBeTruthy();
    expect(screen.queryByText("Running · Coding agent")).toBeNull();
  });

  it("keeps a parked version under the In progress filter", () => {
    // It is not live, so it fails the liveness test the filter used to be —
    // and would then have matched NO filter and vanished from every view but
    // "All statuses", which is the version the reader most needs to find.
    mockBuilds = [
      build({ tag: "v2", status: "in_progress", waitingReason: "external-values" }),
      build({ tag: "v1" }),
    ];
    renderLedger();

    fireEvent.mouseDown(screen.getByRole("combobox", { name: /status/i }));
    fireEvent.click(screen.getByRole("option", { name: "In progress" }));

    expect(screen.getByText("Waiting for configuration")).toBeTruthy();
    expect(screen.queryByText("Built")).toBeNull();
  });

  it("teaches what a build is when there are none, and routes to the spec", () => {
    renderLedger();
    expect(screen.getByText("No builds yet")).toBeTruthy();
    // The empty state teaches WHAT a build is — it must not narrate the flow
    // ("publish", "click Build") — and the CTA is the only surface a user can
    // act on from here (#577; lexicon "Empty states").
    expect(
      screen.getByText(/hands your design to coding agents/),
    ).toBeTruthy();
    expect(screen.queryByText(/[Pp]ublish/)).toBeNull();
    expect(
      screen.getByRole("link", { name: "Go to the spec" }).getAttribute("href"),
    ).toBe("/projects/demo-shop/spec");
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
