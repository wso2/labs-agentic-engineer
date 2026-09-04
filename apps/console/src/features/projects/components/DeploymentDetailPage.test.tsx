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
import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "../../../generated/aep-api";

type Deployment = components["schemas"]["Deployment"];
type DeployStage = components["schemas"]["DeployStage"];
type MilestoneRunView = components["schemas"]["MilestoneRunView"];

// Router replaced so links render as plain anchors whose href is the resolved
// route path — no RouterProvider needed (mirrors DeploymentsPage.test.tsx).
vi.mock("@tanstack/react-router", () => ({
  createLink: (Component: ElementType) =>
    function MockLink({
      to,
      params,
      ...rest
    }: {
      to: string;
      params?: Record<string, unknown>;
    } & Record<string, unknown>) {
      let href = to;
      for (const [key, value] of Object.entries(params ?? {})) {
        href = href.replace(`$${key}`, String(value));
      }
      return <Component component="a" href={href} {...rest} />;
    },
  Link: ({ children }: { children?: React.ReactNode }) => <a>{children}</a>,
}));

let mockDeploy: DeployStage = {
  version: "v1",
  status: "deployed",
  components: { total: 2, ready: 2 },
  validation: "passed",
};
let mockDeployments: Deployment[] = [];
let mockComponentsPending = false;
let mockFailedCount = 0;

vi.mock("../api/queries", () => ({
  useProjectComponents: () => ({
    data: {
      items: [
        { name: "claims-api", displayName: "claims-api", type: "service" },
        { name: "approvals-web", displayName: "approvals-web", type: "web-application" },
      ],
    },
    isPending: mockComponentsPending,
    isError: false,
    error: null,
    refetch: vi.fn(),
  }),
  useComponentsDeployments: () => ({
    isPending: false,
    deployments: mockDeployments,
    failedCount: mockFailedCount,
  }),
  useProjectStatus: () => ({
    data: {
      repoUrl: "https://github.com/acme/expense.git",
      build: { version: "v1", status: "succeeded" },
      deploy: mockDeploy,
    },
  }),
}));

let mockRuns: MilestoneRunView[] = [];
let mockRunsPending = false;
vi.mock("../../builds/api/queries", () => ({
  useBuilds: () => ({
    data: [{ tag: "v1", milestoneNumber: 3, status: "completed", startedAt: "2026-08-14T16:20:00Z" }],
    isPending: false,
    isError: false,
  }),
  useBuildRuns: () => ({
    data: mockRunsPending ? undefined : { runs: mockRuns },
    isPending: mockRunsPending,
    isError: false,
  }),
}));

let mockCounts:
  | { passed: number; failed: number; uncovered: number; total: number }
  | undefined;
vi.mock("../../validation/api/counts", () => ({
  useValidationEvidence: () => ({
    verdict: "passed",
    repairing: false,
    ...(mockCounts ? { counts: mockCounts } : {}),
  }),
}));

// The contract viewer is a dialog over its own query; only its opening is
// under test here.
const openApiDialog = vi.fn();
vi.mock("./ComponentOpenApiDialog", () => ({
  ComponentOpenApiDialog: (props: { componentName: string | null }) => {
    openApiDialog(props.componentName);
    return null;
  },
}));

import { DeploymentDetailPage } from "./DeploymentDetailPage";

const devDeployments = (): Deployment[] => [
  {
    componentName: "claims-api",
    environment: "development",
    status: "Ready",
    releaseName: "claims-api-v1-4e8a0d6",
    endpointUrl: "https://api.dev.expense.localhost/claims",
    createdAt: "2026-08-14T16:54:00Z",
  },
  {
    componentName: "approvals-web",
    environment: "development",
    status: "Ready",
    releaseName: "approvals-web-v1-4e8a0d6",
    endpointUrl: "https://approvals.dev.expense.localhost",
    createdAt: "2026-08-14T16:52:00Z",
  },
];

beforeEach(() => {
  mockDeploy = {
    version: "v1",
    status: "deployed",
    components: { total: 2, ready: 2 },
    validation: "passed",
  };
  mockDeployments = devDeployments();
  mockComponentsPending = false;
  mockFailedCount = 0;
  mockRuns = [];
  mockRunsPending = false;
  mockCounts = undefined;
  openApiDialog.mockClear();
});

describe("DeploymentDetailPage", () => {
  it("names the environment and version, and links the build that shipped it", () => {
    mockCounts = { passed: 24, failed: 0, uncovered: 0, total: 24 };
    mockRuns = [
      {
        id: "run-1",
        kind: "dev",
        milestoneNumber: 3,
        createdAt: "2026-08-14T16:20:00Z",
        cycles: [
          {
            id: "c1",
            kind: "coding",
            mergeSha: "4e8a0d6f1c2b3a4d",
            createdAt: "2026-08-14T16:20:00Z",
          },
        ],
      } as MilestoneRunView,
    ];

    render(<DeploymentDetailPage projectName="expense" environment="development" />);

    // The page title and the summary card's own header both name it.
    expect(screen.getAllByRole("heading", { name: /Development · v1/ })).toHaveLength(2);
    expect(
      screen.getByRole("link", { name: "View the build that shipped this" }),
    ).toHaveAttribute("href", "/projects/expense/builds/v1");
    // The summary card's facts.
    expect(screen.getByText("Milestone #3")).toBeInTheDocument();
    expect(screen.getByText("24 / 24 passed")).toBeInTheDocument();
    // The commit that shipped it, short, linked on the repo's web root — the
    // `.git` suffix stripped from the platform's clone url.
    expect(screen.getByText("4e8a0d6")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /GitHub/ })).toHaveAttribute(
      "href",
      "https://github.com/acme/expense/commit/4e8a0d6f1c2b3a4d",
    );
  });

  it("gives each component its own way in", () => {
    render(<DeploymentDetailPage projectName="expense" environment="development" />);

    expect(screen.getByText("2 of 2 live")).toBeInTheDocument();
    // A web application is visited; a service opens its contract.
    expect(screen.getByRole("link", { name: "Visit approvals-web" })).toHaveAttribute(
      "href",
      "https://approvals.dev.expense.localhost",
    );
    fireEvent.click(screen.getByRole("button", { name: "Try claims-api API" }));
    expect(openApiDialog).toHaveBeenLastCalledWith("claims-api");
    // Every bound component's URL is on its second line.
    expect(
      screen.getByRole("link", { name: /api.dev.expense.localhost\/claims/ }),
    ).toBeInTheDocument();
    expect(screen.getByText("claims-api-v1-4e8a0d6")).toBeInTheDocument();
  });

  it("says a version has no commit rather than guessing one", () => {
    // The run story answered with no merged cycle — a version tagged before
    // the platform kept run rows.
    render(<DeploymentDetailPage projectName="expense" environment="development" />);

    const commit = screen.getByText("Commit").parentElement;
    expect(commit).not.toBeNull();
    expect(within(commit as HTMLElement).getByText("—")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /GitHub/ })).not.toBeInTheDocument();
  });

  it("reads production from its bindings, with no version and no validation", () => {
    mockDeployments = [
      {
        componentName: "claims-api",
        environment: "production",
        status: "Ready",
        releaseName: "claims-api-prod",
        createdAt: "2026-08-15T09:00:00Z",
      },
    ];

    render(<DeploymentDetailPage projectName="expense" environment="production" />);

    // No version to name: the aggregate describes development only.
    expect(screen.getByRole("heading", { name: "Production" })).toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "View the build that shipped this" }),
    ).not.toBeInTheDocument();
    expect(screen.getByText("1 of 1 live")).toBeInTheDocument();
    // Only the bound component is listed for production.
    expect(screen.queryByText("approvals-web")).not.toBeInTheDocument();
  });

  it("is honest about an empty environment", () => {
    mockDeployments = [];

    render(<DeploymentDetailPage projectName="expense" environment="production" />);

    expect(
      screen.getByText(/Nothing deployed here yet — promote a validated version/),
    ).toBeInTheDocument();
  });

  it("does not call an environment empty when the reads that would say so failed", () => {
    // Every list-deployments read failed, so nothing is bound — but that is
    // ignorance, not emptiness, and the page must not report it as emptiness.
    mockDeployments = [];
    mockFailedCount = 2;

    render(<DeploymentDetailPage projectName="expense" environment="development" />);

    expect(
      screen.getByText(/Deployments for 2 components could not be loaded/),
    ).toBeInTheDocument();
    expect(screen.queryByText(/Nothing deployed here yet/)).not.toBeInTheDocument();
  });

  it("still says nothing is deployed when every read answered", () => {
    mockDeployments = [];
    mockFailedCount = 0;

    render(<DeploymentDetailPage projectName="expense" environment="development" />);

    expect(
      screen.getByText(/Nothing deployed here yet — agents deploy to development/),
    ).toBeInTheDocument();
    expect(screen.queryByText(/could not be loaded/)).not.toBeInTheDocument();
  });

  it("rejects a segment that names no environment", () => {
    render(<DeploymentDetailPage projectName="expense" environment="staging" />);

    expect(screen.getByText("No environment called staging")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Back to Deployments" })).toHaveAttribute(
      "href",
      "/projects/expense/deployments",
    );
  });
});
