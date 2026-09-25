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
import type { components } from "../generated/aep-api";
import { ProjectStatusBadge } from "./ProjectStatusBadge";

type ProjectStatus = components["schemas"]["ProjectStatus"];

const params = vi.hoisted(() => ({ current: {} as { projectName?: string } }));
const query = vi.hoisted(() => ({
  current: { data: undefined } as { data: ProjectStatus | undefined },
}));

vi.mock("@tanstack/react-router", () => ({
  useParams: () => params.current,
}));

vi.mock("../features/projects/api/queries", () => ({
  useProjectStatus: () => query.current,
}));

function status(overrides: Partial<ProjectStatus>): ProjectStatus {
  return {
    phase: "tasks",
    spec: { exists: true, version: "v1", dirty: false },
    build: { version: "", status: "none" },
    deploy: { version: "", status: "none" },
    ...overrides,
  } as ProjectStatus;
}

beforeEach(() => {
  params.current = { projectName: "demo-shop" };
  query.current = { data: undefined };
});

describe("ProjectStatusBadge", () => {
  it("renders nothing outside a project", () => {
    params.current = {};
    query.current = { data: status({}) };
    const { container } = render(<ProjectStatusBadge />);
    expect(container).toBeEmptyDOMElement();
  });

  // A word appearing beside the project name a moment after the page settles
  // is worse than one that arrives quietly with the rest of the header.
  it("renders nothing while the first status read is in flight", () => {
    const { container } = render(<ProjectStatusBadge />);
    expect(container).toBeEmptyDOMElement();
  });

  // Before the first build there is no delivery state, and the spec's is not
  // this chip's to report — a v2 spec can be amended while v1 builds, so one
  // line cannot carry both. The overview's spec leg owns it.
  it("renders nothing before the first build", () => {
    query.current = { data: status({}) };
    const { container } = render(<ProjectStatusBadge />);
    expect(container).toBeEmptyDOMElement();
  });

  it("names the project's state, announced as a status", () => {
    query.current = {
      data: status({ build: { version: "v1", status: "running" } } as Partial<ProjectStatus>),
    };
    render(<ProjectStatusBadge />);
    expect(screen.getByRole("status")).toHaveTextContent("Building");
  });

  // The one thing this chip does that the shared StatusChip cannot: a state
  // the platform is still working on turns, a settled one does not — including
  // a settled FAILURE, which is the case a naive "not success → spin" would
  // get wrong.
  it.each([
    ["Building", { build: { version: "v1", status: "running" } }, true],
    ["Deploying", {
      build: { version: "v1", status: "succeeded" },
      deploy: { version: "v1", status: "deploying", components: { total: 3, ready: 1 } },
    }, true],
    ["Built", { build: { version: "v1", status: "succeeded" } }, false],
    ["Build failed", { build: { version: "v1", status: "failed" } }, false],
    ["Active", {
      build: { version: "v1", status: "succeeded" },
      deploy: {
        version: "v1",
        status: "deployed",
        components: { total: 1, ready: 1 },
        validation: "passed",
      },
    }, false],
  ] as const)("%s spins: %s", (label, overrides, spins) => {
    query.current = { data: status(overrides as Partial<ProjectStatus>) };
    const { container } = render(<ProjectStatusBadge />);
    expect(screen.getByRole("status")).toHaveTextContent(label);
    expect(!!container.querySelector(".MuiCircularProgress-root")).toBe(spins);
  });
});
