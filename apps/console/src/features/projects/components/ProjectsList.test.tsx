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
import { ProjectsList } from "./ProjectsList";

type Project = components["schemas"]["Project"];

vi.mock("@tanstack/react-router", () => ({
  Link: ({ to, ...rest }: { to: string }) => <a href={to} {...rest} />,
  useNavigate: () => vi.fn(),
}));

// Every test but the dedicated permission-denial ones below holds both
// permissions, so the page and Create project read as fully reachable by
// default — mirrors SkillsSection.test.tsx's per-suite permission toggle.
const heldPermissions = vi.hoisted(
  () => new Set(["ae:requirement-view", "ae:requirement-update"]),
);
vi.mock("../../../auth/permissions", () => ({
  useHasPermission: (permission: string) => heldPermissions.has(permission),
  useHasAnyPermission: (permissions: string[]) =>
    permissions.some((p) => heldPermissions.has(p)),
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
  heldPermissions.clear();
  heldPermissions.add("ae:requirement-view");
  heldPermissions.add("ae:requirement-update");
  listItems = [];
});

const noCreatePermissionText = "You don't have permission to create a new project.";
const noAccessText = "You don't have permission to view projects.";

describe("ProjectsList — permission gate", () => {
  it("shows an enabled Create project link when the user holds ae:requirement-update, with projects", () => {
    listItems = [project({})];
    render(<ProjectsList />);

    expect(screen.getByRole("link", { name: "Create project" })).toBeInTheDocument();
  });

  it("shows Create project DISABLED (not hidden) when holding only ae:requirement-view", () => {
    heldPermissions.delete("ae:requirement-update");
    listItems = [project({})];
    render(<ProjectsList />);

    expect(screen.queryByRole("link", { name: "Create project" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create project" })).toBeDisabled();
  });

  it("shows the disabled Create project's tooltip on hover", async () => {
    heldPermissions.delete("ae:requirement-update");
    listItems = [project({})];
    render(<ProjectsList />);

    const button = screen.getByRole("button", { name: "Create project" });
    fireEvent.mouseOver(button.closest("span") ?? button);
    expect(await screen.findByText(noCreatePermissionText)).toBeInTheDocument();
  });

  it("shows Create project enabled in the true-empty state when the user holds the permission", () => {
    render(<ProjectsList />);

    expect(screen.getByRole("link", { name: "Create project" })).toBeInTheDocument();
  });

  it("shows Create project DISABLED in the true-empty state, holding only ae:requirement-view", () => {
    heldPermissions.delete("ae:requirement-update");
    render(<ProjectsList />);

    expect(screen.queryByRole("link", { name: "Create project" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create project" })).toBeDisabled();
  });

  it("shows the no-access illustration and hides everything else holding neither permission", () => {
    heldPermissions.clear();
    listItems = [project({})];
    render(<ProjectsList />);

    expect(screen.getByText(noAccessText)).toBeInTheDocument();
    expect(screen.queryByText("Todo app")).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Create project" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create project" })).not.toBeInTheDocument();
    expect(screen.queryByPlaceholderText("Search projects...")).not.toBeInTheDocument();
  });

  // Exact-match ae:requirement-view now (not an OR with ae:requirement-update):
  // a create-only caller can't see this list or open a card, so the whole
  // page renders the denied state for them — same as holding neither
  // permission — even though they could create a project from elsewhere.
  it("shows the no-access illustration holding only ae:requirement-update (no ae:requirement-view)", () => {
    heldPermissions.clear();
    heldPermissions.add("ae:requirement-update");
    listItems = [project({})];
    render(<ProjectsList />);

    expect(screen.getByText(noAccessText)).toBeInTheDocument();
    expect(screen.queryByText("Todo app")).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Create project" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create project" })).not.toBeInTheDocument();
  });

  it("keeps a project card open (canOpen) for a requirement-view-only holder", () => {
    heldPermissions.delete("ae:requirement-update");
    listItems = [project({})];
    render(<ProjectsList />);

    expect(screen.getByText("Todo app").closest("button")).not.toBeDisabled();
  });
});
