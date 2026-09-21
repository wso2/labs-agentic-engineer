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
import { beforeEach, describe, expect, it, vi } from "vitest";
import { OverviewArchitecture } from "./OverviewArchitecture";

vi.mock("@tanstack/react-router", () => ({
  createLink: (Component: ElementType) =>
    function MockLink(props: Record<string, unknown>) {
      return <Component component="a" {...props} />;
    },
}));

// Same mocking seam OverviewTrack.test.tsx uses: useHasAnyPermission reads
// useSession().permissions (permissions.ts), so stubbing the session is
// enough to drive the whole permission surface from one place.
const permissions = vi.hoisted(() => ({ current: new Set(["ae:design-view"]) }));
vi.mock("../../../auth/SessionContext", () => ({
  useSession: () => ({ orgHandle: "default", permissions: permissions.current }),
}));

const mockUseSpecFiles = vi.fn();
const mockUseSpecFileContent = vi.fn();
vi.mock("../../spec/api/queries", () => ({
  useSpecFiles: (...args: unknown[]) => mockUseSpecFiles(...args),
  useSpecFileContent: (...args: unknown[]) => mockUseSpecFileContent(...args),
}));

// The real renderer needs a DOM measurement environment this test doesn't
// have; a stub proves what reaches it, not how it draws.
vi.mock("@aep/ui-cell-diagram-view", () => ({
  CellDiagramView: () => <div data-testid="cell-diagram" />,
}));

const PENDING = { data: undefined, isPending: true, isError: false };
const EMPTY = { data: [], isPending: false, isError: false };

beforeEach(() => {
  mockUseSpecFiles.mockReset();
  mockUseSpecFileContent.mockReset();
  mockUseSpecFiles.mockReturnValue(PENDING);
  mockUseSpecFileContent.mockReturnValue(PENDING);
  permissions.current = new Set(["ae:design-view"]);
});

describe("OverviewArchitecture without ae:design-view", () => {
  it("explains the permission gap instead of a generic load failure", () => {
    permissions.current = new Set(["ae:requirement-view"]);
    render(<OverviewArchitecture projectName="proj1" />);

    expect(
      screen.getByText("No permission to view the architecture"),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("Failed to load the architecture."),
    ).not.toBeInTheDocument();
  });

  it("withholds the project name from useSpecFiles so it never fires", () => {
    permissions.current = new Set(["ae:requirement-view"]);
    render(<OverviewArchitecture projectName="proj1" />);

    expect(mockUseSpecFiles).toHaveBeenCalledWith(undefined);
  });

  // Exact-match, not OR'd: the backend's own gate on ListFiles/ReadFile (what
  // this panel reads) is exact-match ae:design-view alone
  // (permission_gate.go), same as SpecView's own page gate. ae:design is a
  // WRITE permission — every built-in role happens to bundle it with
  // ae:design-view today, but the gate must not assume that, or a caller
  // holding only ae:design lands right back in a 403 the panel can't explain.
  it("does NOT accept ae:design alone", () => {
    permissions.current = new Set(["ae:design"]);
    render(<OverviewArchitecture projectName="proj1" />);

    expect(
      screen.getByText("No permission to view the architecture"),
    ).toBeInTheDocument();
    expect(mockUseSpecFiles).toHaveBeenCalledWith(undefined);
  });
});

describe("OverviewArchitecture with ae:design-view", () => {
  it("passes the real project name through to useSpecFiles", () => {
    render(<OverviewArchitecture projectName="proj1" />);
    expect(mockUseSpecFiles).toHaveBeenCalledWith("proj1");
  });

  it("still surfaces a genuine fetch failure as 'Failed to load the architecture'", () => {
    mockUseSpecFiles.mockReturnValue({ data: undefined, isPending: false, isError: true });
    render(<OverviewArchitecture projectName="proj1" />);
    expect(screen.getByText("Failed to load the architecture.")).toBeInTheDocument();
  });

  it("shows the empty state when no design.cell exists yet", () => {
    mockUseSpecFiles.mockReturnValue(EMPTY);
    render(<OverviewArchitecture projectName="proj1" />);
    expect(screen.getByText("No architecture yet")).toBeInTheDocument();
  });
});
