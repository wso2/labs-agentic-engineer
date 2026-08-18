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

import { render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { projectKeys } from "../api/keys";

const invalidateQueries = vi.fn();

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQueryClient: () => ({ invalidateQueries }),
  };
});

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children?: React.ReactNode }) => <a>{children}</a>,
}));

let mockDeployStatus = "none";

vi.mock("../api/queries", () => ({
  useProject: () => ({ data: { name: "shop", displayName: "Shop" } }),
  useProjectStatus: () => ({
    data: {
      phase: "components",
      repoStatus: "ready",
      repoUrl: "",
      hasSpec: true,
      hasDesign: true,
      hasTasks: true,
      specStatus: "approved",
      designStatus: "approved",
      spec: { exists: true, version: "v1", dirty: false, design: true },
      build: { version: "v1", status: "succeeded" },
      deploy: {
        version: "v1",
        status: mockDeployStatus,
        components: { total: 1, ready: 0 },
        validation: "none",
      },
    },
    refetch: vi.fn(),
  }),
  useProjectComponents: () => ({
    data: { items: [] },
    isPending: false,
    isError: false,
    refetch: vi.fn(),
  }),
}));

vi.mock("./ComponentsList", () => ({ ComponentsList: () => null }));
vi.mock("./OverviewPipeline", () => ({ OverviewPipeline: () => null }));
vi.mock("./RecentActivity", () => ({ RecentActivity: () => null }));

import { ProjectOverview } from "./ProjectOverview";

describe("ProjectOverview — deploy transition", () => {
  beforeEach(() => {
    invalidateQueries.mockClear();
    mockDeployStatus = "none";
  });

  it("invalidates components (including deployment URLs) when deploy status changes", () => {
    const { rerender } = render(<ProjectOverview projectName="shop" />);
    mockDeployStatus = "deployed";
    rerender(<ProjectOverview projectName="shop" />);
    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: projectKeys.components("shop"),
    });
  });
});
