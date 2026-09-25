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
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@tanstack/react-router", () => ({
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

describe("IssuesPage", () => {
  it("uses the project header and renders the issue list", () => {
    render(<IssuesPage projectName="shop" />);

    expect(screen.getByRole("heading", { name: "Issues" })).toBeInTheDocument();
    expect(screen.getByText("Shop")).toBeInTheDocument();
    expect(screen.getByText("Issues list for shop")).toBeInTheDocument();
    expect(screen.queryByText("Issues is on its way")).not.toBeInTheDocument();
  });
});
