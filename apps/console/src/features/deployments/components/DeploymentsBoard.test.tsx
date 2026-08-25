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

type ProjectDeployment = components["schemas"]["ProjectDeployment"];

const navigate = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children?: React.ReactNode }) => <a>{children}</a>,
  useNavigate: () => navigate,
}));

let mockItems: ProjectDeployment[] = [];
let mockState = { isPending: false, isError: false };
const refetch = vi.fn();
vi.mock("../api/queries", () => ({
  useProjectDeployments: () => ({
    data: mockItems,
    isPending: mockState.isPending,
    isError: mockState.isError,
    error: mockState.isError ? new Error("boom") : null,
    refetch,
  }),
}));

import { DeploymentsBoard } from "./DeploymentsBoard";

const dep = (over: Partial<ProjectDeployment> = {}): ProjectDeployment => ({
  id: "dep-1",
  tag: "v1",
  environment: "development",
  status: "live",
  deployedAt: "2026-07-10T10:54:00Z",
  durationSeconds: 221,
  ...over,
});

const onPromote = vi.fn();
const renderBoard = () =>
  render(<DeploymentsBoard projectName="demo-shop" onPromote={onPromote} />);

beforeEach(() => {
  mockItems = [];
  mockState = { isPending: false, isError: false };
  navigate.mockClear();
  refetch.mockClear();
  onPromote.mockClear();
});

describe("DeploymentsBoard", () => {
  it("shows both environments as their own column", () => {
    // The whole point of ADR-0020 §5: two places with two states, on screen at
    // once, rather than one rail read end to end.
    mockItems = [dep()];
    renderBoard();

    expect(screen.getByText("Development")).toBeTruthy();
    expect(screen.getByText("Production")).toBeTruthy();
    expect(screen.getByText("Nothing deployed")).toBeTruthy();
  });

  it("offers promotion only when validation actually passed", () => {
    mockItems = [dep({ validation: { state: "passed", passed: 24, total: 24 } })];
    renderBoard();

    const promote = screen.getByRole("button", { name: /Promote v1 to production/ });
    fireEvent.click(promote);
    expect(onPromote).toHaveBeenCalledTimes(1);
  });

  it("explains the gate instead of showing a dead promote button", () => {
    mockItems = [dep({ validation: { state: "running", passed: 18, total: 24 } })];
    renderBoard();

    expect(screen.queryByRole("button", { name: /Promote/ })).toBeNull();
    // Both cards state the gate: development explains why it cannot promote,
    // production explains what would have to be true to receive one.
    expect(
      screen.getAllByText(/Only a version whose validation has passed/).length,
    ).toBe(2);
  });

  it("keeps a running validation as progress, not a verdict pill", () => {
    mockItems = [dep({ validation: { state: "running", passed: 18, total: 24 } })];
    renderBoard();
    expect(screen.getByText("18 of 24 checked")).toBeTruthy();
  });

  it("records deployments that are no longer running", () => {
    // Before this page, a failed or superseded deployment left no trace at all.
    mockItems = [
      dep({ id: "a", status: "failed", deployedAt: "2026-07-09T11:06:00Z" }),
      dep({ id: "b", status: "superseded", deployedAt: "2026-07-08T09:48:00Z" }),
    ];
    renderBoard();

    // Each appears twice — once as the environment card's state, once as the
    // table row's — which is the point: the card says what is true now, the
    // table keeps the record.
    expect(screen.getAllByText("Failed").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Superseded").length).toBeGreaterThan(0);
  });

  it("opens a deployment when its row is clicked", () => {
    mockItems = [dep({ id: "dep-dev-v1" })];
    renderBoard();

    // The tag renders in the environment card AND in the table row; the row is
    // the last of the two, and the clickable one.
    const tags = screen.getAllByText("v1");
    fireEvent.click(tags[tags.length - 1]!);
    expect(navigate).toHaveBeenCalledWith({
      to: "/projects/$projectName/deployments/$deploymentId",
      params: { projectName: "demo-shop", deploymentId: "dep-dev-v1" },
    });
  });

  it("meets a brand-new project with two empty cards, not an error", () => {
    renderBoard();
    expect(screen.getByText("No deployments yet")).toBeTruthy();
    expect(screen.getAllByText("Nothing deployed").length).toBe(2);
  });

  it("offers a retry when the list fails to load", () => {
    mockState = { isPending: false, isError: true };
    renderBoard();

    fireEvent.click(screen.getByText("Retry"));
    expect(refetch).toHaveBeenCalled();
  });

  it("picks the newest deployment for a card, not the first in the list", () => {
    mockItems = [
      dep({ id: "old", tag: "v1", deployedAt: "2026-07-08T09:48:00Z" }),
      dep({ id: "new", tag: "v2", deployedAt: "2026-07-11T15:20:00Z" }),
    ];
    renderBoard();
    // The card names v2 — a card that leaned on server ordering would say v1.
    // `Running v2` is the card's own sentence, so it is unambiguous.
    expect(screen.getByText(/Running/)).toBeTruthy();
    expect(screen.getAllByText("v2").length).toBeGreaterThan(1);
  });
});
