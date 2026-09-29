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

import type { ElementType } from "react";
import { fireEvent, screen } from "@testing-library/react";
import {
  allPermissionsExcept,
  renderWithPermissions,
} from "../../../auth/testing";
import { beforeEach, describe, expect, it, vi } from "vitest";

const navigate = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigate,
  Link: ({
    to,
    params,
    children,
  }: {
    to: string;
    params?: Record<string, unknown>;
    children?: React.ReactNode;
  }) => {
    let href = to;
    for (const [key, value] of Object.entries(params ?? {})) {
      href = href.replace(`$${key}`, String(value));
    }
    return <a href={href}>{children}</a>;
  },
  createLink: (Component: ElementType) =>
    function MockLink(props: Record<string, unknown>) {
      return <Component component="a" {...props} />;
    },
}));

vi.mock("../api/queries", () => ({
  useProject: () => ({ data: { name: "shop", displayName: "Shop" } }),
}));

vi.mock("../../issues/components/IssuesList", () => ({
  IssuesList: ({ projectName }: { projectName: string }) => (
    <div>Issues list for {projectName}</div>
  ),
}));

import { IssuesPage } from "./IssuesPage";

beforeEach(() => {
  navigate.mockClear();
});

// Every test but the dedicated permission ones below holds every permission,
// so the page reads as reachable.
const renderPage = (permissions?: Iterable<string>) =>
  renderWithPermissions(<IssuesPage projectName="shop" />, permissions);

describe("IssuesPage", () => {
  it("uses the project header and renders the issue list", () => {
    renderPage();

    expect(screen.getByRole("heading", { name: "Issues" })).toBeInTheDocument();
    expect(screen.getByText("Shop")).toBeInTheDocument();
    expect(screen.getByText("Issues list for shop")).toBeInTheDocument();
    expect(screen.queryByText("Issues is on its way")).not.toBeInTheDocument();
  });

  // Exact-match ae:build-view, the same permission list-issues is gated on.
  // A full replacement, not an overlay: the list never renders, so the read
  // behind it is never fired at a server that would refuse it.
  it("blocks the whole page for a caller lacking ae:build-view", () => {
    renderPage(allPermissionsExcept("ae:build-view"));

    expect(
      screen.getByText("You don't have access to this project's issues"),
    ).toBeInTheDocument();
    expect(screen.queryByText("Issues list for shop")).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Issues" })).not.toBeInTheDocument();
  });

  it("navigates to the project overview from the restricted page", () => {
    renderPage(allPermissionsExcept("ae:build-view"));

    fireEvent.click(screen.getByRole("button", { name: "Back to project overview" }));

    expect(navigate).toHaveBeenCalledWith({
      to: "/projects/$projectName",
      params: { projectName: "shop" },
    });
  });
});
