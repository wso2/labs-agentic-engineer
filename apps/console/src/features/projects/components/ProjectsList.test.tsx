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

import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "../../../generated/aep-api";
import { ProjectsList } from "./ProjectsList";

type Project = components["schemas"]["Project"];

vi.mock("@tanstack/react-router", () => ({
  Link: ({ to, ...rest }: { to: string }) => <a href={to} {...rest} />,
  useNavigate: () => vi.fn(),
}));

// Every existing test in this file assumes project creation is otherwise
// reachable — only the dedicated "no permission" tests below flip this.
const requirementUpdatePermission = vi.hoisted(() => ({ current: true }));
vi.mock("../../../auth/permissions", () => ({
  useHasPermission: () => requirementUpdatePermission.current,
}));

let listItems: Project[] = [];
vi.mock("../api/queries", () => ({
  useProjectsList: () => ({
    data: { pages: [{ items: listItems }] },
    isPending: false,
    isError: false,
    error: null,
    refetch: vi.fn(),
    hasNextPage: false,
    fetchNextPage: vi.fn(),
    isFetchingNextPage: false,
  }),
  useDeleteProject: () => ({
    mutate: vi.fn(),
    isPending: false,
    isError: false,
    error: null,
    reset: vi.fn(),
  }),
}));

function project(overrides: Partial<Project>): Project {
  return {
    name: "todo-app",
    displayName: "Todo app",
    description: "A todo app",
    createdAt: "2026-06-01T12:00:00Z",
    ...overrides,
  };
}

beforeEach(() => {
  requirementUpdatePermission.current = true;
  listItems = [];
});

const noPermissionText = "You don't have permission to create a new project.";

describe("ProjectsList — permission gate", () => {
  it("shows Create project and no banner when the user holds ae:requirement-update, with projects", () => {
    listItems = [project({})];
    render(<ProjectsList />);

    expect(screen.getByRole("link", { name: "Create project" })).toBeInTheDocument();
    expect(screen.queryByText(noPermissionText)).not.toBeInTheDocument();
  });

  it("hides Create project and shows a banner instead, without ae:requirement-update, with projects", () => {
    requirementUpdatePermission.current = false;
    listItems = [project({})];
    render(<ProjectsList />);

    expect(
      screen.queryByRole("link", { name: "Create project" }),
    ).not.toBeInTheDocument();
    expect(screen.getByText(noPermissionText)).toBeInTheDocument();
  });

  it("shows Create project in the true-empty state when the user holds the permission", () => {
    render(<ProjectsList />);

    expect(screen.getByRole("link", { name: "Create project" })).toBeInTheDocument();
    expect(screen.queryByText(noPermissionText)).not.toBeInTheDocument();
  });

  it("hides Create project in the true-empty state and shows a banner instead", () => {
    requirementUpdatePermission.current = false;
    render(<ProjectsList />);

    expect(
      screen.queryByRole("link", { name: "Create project" }),
    ).not.toBeInTheDocument();
    expect(screen.getByText(noPermissionText)).toBeInTheDocument();
  });
});
