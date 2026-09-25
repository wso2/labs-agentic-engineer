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
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
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
let mockStatusError = false;
const mockStatusRefetch = vi.fn();
let mockComponentsPending = false;
// The board's components. Two by default — a service and the web app that
// calls it; the agent cases add a third.
type BoardComponent = { name: string; displayName: string; type: string };
const defaultComponents = (): BoardComponent[] => [
  { name: "claims-api", displayName: "claims-api", type: "service" },
  { name: "approvals-web", displayName: "approvals-web", type: "web-application" },
];
let mockComponents: BoardComponent[] = defaultComponents();
let mockFailedCount = 0;

type ProjectDependencyReadiness = components["schemas"]["ProjectDependencyReadiness"];
let mockReadiness: ProjectDependencyReadiness | undefined;
let mockReadinessPending = false;
let mockReadinessError = false;
const mockReadinessRefetch = vi.fn();
const mockSaveValues = vi.fn();

vi.mock("../api/queries", () => ({
  // The platform's pipeline, in promotion order — what `useEnvironments`
  // serves. Two environments here because that is the pipeline these tests
  // describe, not because the console knows only two.
  useEnvironments: () => ({
    data: mockEnvironmentsError || mockEnvironmentsPending ? undefined : mockEnvironments,
    isPending: mockEnvironmentsPending,
    isError: mockEnvironmentsError,
    error: mockEnvironmentsError ? new Error("gateway down") : null,
    refetch: mockEnvironmentsRefetch,
  }),
  useProjectDependencyReadiness: () => ({
    data: mockReadinessPending || mockReadinessError ? undefined : mockReadiness,
    isPending: mockReadinessPending,
    isError: mockReadinessError,
    error: mockReadinessError ? new Error("readiness down") : null,
    refetch: mockReadinessRefetch,
  }),
  useSaveConnectionValues: () => ({
    mutate: mockSaveValues,
    isPending: false,
    isError: false,
    error: null,
    reset: vi.fn(),
  }),
  useProjectComponents: () => ({
    data: {
      items: mockComponents,
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
    data: mockStatusError
      ? undefined
      : {
          repoUrl: "https://github.com/acme/expense.git",
          build: { version: mockBuildVersion, status: "succeeded" },
          deploy: mockDeploy,
        },
    isPending: false,
    isError: mockStatusError,
    error: mockStatusError ? new Error("status down") : null,
    refetch: mockStatusRefetch,
  }),
}));

// The design's graph: the web app talks to the service; the service carries
// one external with values to collect and one platform resource.
type ComponentDependencies = components["schemas"]["ComponentDependencies"];
const mockDependencies: ComponentDependencies[] = [
  {
    componentName: "approvals-web",
    dependencies: [{ kind: "component", name: "claims-api" }],
  },
  {
    componentName: "claims-api",
    dependencies: [
      { kind: "platform-resource", name: "claims-db", resourceType: "postgres-cnpg" },
      { kind: "external", name: "stripe", config: [{ key: "STRIPE_SECRET_KEY", description: "Secret key", secret: true }] },
    ],
  },
];
let mockDependenciesPending = false;
vi.mock("../../spec/api/queries", () => ({
  useDesignDependencies: () => ({
    data: mockDependenciesPending ? undefined : mockDependencies,
    isPending: mockDependenciesPending,
    isError: false,
    refetch: vi.fn(),
  }),
}));
vi.mock("../../settings/api/queries", () => ({
  useExternalResources: () => ({ data: [], isPending: false, isError: false, refetch: vi.fn() }),
}));

type BuildSummary = components["schemas"]["BuildSummary"];
const LEDGER: BuildSummary[] = [
  { tag: "v1", milestoneNumber: 3, status: "completed", startedAt: "2026-08-14T16:20:00Z" },
];
let mockBuilds: BuildSummary[] = LEDGER;
let mockBuildsPending = false;
let mockBuildsError = false;
const mockBuildsRefetch = vi.fn();

let mockRuns: MilestoneRunView[] = [];
let mockRunsPending = false;
let mockRunsError = false;
let mockBuildVersion = "v1";
const mockRunsRefetch = vi.fn();
vi.mock("../../builds/api/queries", () => ({
  useBuilds: () => ({
    data: mockBuildsPending || mockBuildsError ? undefined : mockBuilds,
    isPending: mockBuildsPending,
    isError: mockBuildsError,
    error: mockBuildsError ? new Error("ledger down") : null,
    refetch: mockBuildsRefetch,
  }),
  useBuildRuns: () => ({
    data: mockRunsPending || mockRunsError ? undefined : { runs: mockRuns },
    isPending: mockRunsPending,
    isError: mockRunsError,
    error: mockRunsError ? new Error("runs down") : null,
    refetch: mockRunsRefetch,
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

// The roles read behind the Test users panel (ADR-0032: the panel lives on this
// page now). Empty by default so a green development shows the Thunder sentence
// alone; the panel's own cases inject accounts.
type ProjectTestUserState = components["schemas"]["ProjectTestUserState"];
let mockTestUsers: ProjectTestUserState[] = [];
let mockRolesPending = false;
// The project's sign-in as the test app performs it; absent by default, as it
// is for a project whose design declares no sign-in resource.
let mockSignIn: { issuer: string; clientId: string } | undefined;
vi.mock("../../spec/api/roles", () => ({
  resourceServerOf: (live: { resourceServer?: string } | undefined) => live?.resourceServer,
  useProjectRoles: () => ({
    data: {
      directoryAvailable: true,
      roles: [],
      testUsers: mockTestUsers,
      resourceServer: "https://aep.wso2.com/orgs/acme/projects/expense",
      signIn: mockSignIn,
    },
    isPending: mockRolesPending,
    isError: false,
  }),
  useRevealTestUserPassword: () => ({
    mutateAsync: vi.fn(async (username: string) => ({ username, password: "mocknotreal", rotatedAt: null })),
    isPending: false,
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

import { DeploymentEnvironmentPage } from "./DeploymentEnvironmentPage";

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

// The platform's pipeline, in promotion order. Two environments because that
// is the pipeline these tests describe, not because the console knows two.
const mockEnvironments = [
  {
    name: "development",
    displayName: "Development",
    isProduction: false,
    validation: "on" as const,
    position: 0,
    promotesTo: "production",
  },
  {
    name: "production",
    displayName: "Production",
    isProduction: true,
    validation: "off" as const,
    position: 1,
  },
];
let mockEnvironmentsError = false;
let mockEnvironmentsPending = false;
const mockEnvironmentsRefetch = vi.fn();

beforeEach(() => {
  mockEnvironmentsError = false;
  mockEnvironmentsPending = false;
  mockEnvironmentsRefetch.mockClear();
  mockDeploy = {
    version: "v1",
    status: "deployed",
    components: { total: 2, ready: 2 },
    validation: "passed",
  };
  mockDeployments = devDeployments();
  mockStatusError = false;
  mockStatusRefetch.mockClear();
  mockBuilds = LEDGER;
  mockBuildsPending = false;
  mockBuildsError = false;
  mockBuildsRefetch.mockClear();
  mockComponentsPending = false;
  mockComponents = defaultComponents();
  mockFailedCount = 0;
  mockRuns = [];
  mockRunsPending = false;
  mockRunsError = false;
  mockBuildVersion = "v1";
  mockRunsRefetch.mockClear();
  mockCounts = undefined;
  mockTestUsers = [];
  mockRolesPending = false;
  mockSignIn = undefined;
  mockReadiness = undefined;
  mockReadinessPending = false;
  mockReadinessError = false;
  mockReadinessRefetch.mockClear();
  mockDependenciesPending = false;
  mockSaveValues.mockClear();
  openApiDialog.mockClear();
});

describe("DeploymentEnvironmentPage", () => {
  it("is titled as the environment itself, with the project and what runs here under it", () => {
    render(<DeploymentEnvironmentPage projectName="expense" environment="development" />);
    expect(screen.getByRole("heading", { name: "Development Environment" })).toBeInTheDocument();
    expect(screen.getByText(/expense · running v1 since /)).toBeInTheDocument();
    expect(screen.getByText("Try it out")).toBeInTheDocument();
  });

  it("names the four sections in order, and calls them dependencies", () => {
    render(<DeploymentEnvironmentPage projectName="expense" environment="development" />);
    const headings = screen.getAllByRole("heading", { level: 3 }).map((h) => h.textContent);
    expect(headings).toEqual(["Deployment", "Try it out", "Dependencies", "Past deployments"]);
    expect(screen.queryByText(/Connections/)).not.toBeInTheDocument();
  });

  it("lists the design's dependencies, with Edit where values are collected", () => {
    render(<DeploymentEnvironmentPage projectName="expense" environment="development" />);
    expect(screen.getByText("2 dependencies · values for Development")).toBeInTheDocument();
    expect(screen.getByRole("row", { name: "claims-db" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Edit stripe values" })).toBeInTheDocument();
  });

  it("lists what has run on the entry environment, from the version ledger", () => {
    render(<DeploymentEnvironmentPage projectName="expense" environment="development" />);
    const section = screen.getByRole("region", { name: "Past deployments" });
    expect(within(section).getByTestId("history-version").textContent).toBe("v1");
    expect(within(section).getByText("Running now")).toBeInTheDocument();
    expect(within(section).getByRole("button", { name: /Roll back/ })).toBeDisabled();
    expect(
      within(section).queryByText("No earlier deployments are recorded for this environment."),
    ).not.toBeInTheDocument();
  });

  it("admits a later environment has no recorded past, rather than borrowing one", () => {
    mockDeployments = [
      {
        componentName: "claims-api",
        environment: "production",
        status: "Ready",
        releaseName: "claims-api-prod",
        createdAt: "2026-08-15T09:00:00Z",
      },
    ];
    render(<DeploymentEnvironmentPage projectName="expense" environment="production" />);
    const section = screen.getByRole("region", { name: "Past deployments" });
    expect(
      within(section).getByText("No earlier deployments are recorded for this environment."),
    ).toBeInTheDocument();
    // The entry environment's v1 is not this environment's past.
    expect(within(section).queryByText("v1")).not.toBeInTheDocument();
    expect(within(section).getByTestId("history-version").textContent).toBe("Unknown");
  });

  it("holds section 1 while the version ledger is still out, rather than omitting its cells", () => {
    // Omission MEANS "this environment has no such fact" on this page, so an
    // entry environment whose ledger has not answered must not render as one
    // that has no milestone and no build stamp.
    mockBuildsPending = true;

    render(<DeploymentEnvironmentPage projectName="expense" environment="development" />);

    const deployment = within(screen.getByRole("region", { name: "Deployment" }));
    expect(deployment.getByTestId("deployment-summary-skeleton")).toBeInTheDocument();
    expect(deployment.queryByText("Version unknown")).not.toBeInTheDocument();
  });

  it("says why section 1 has no milestone when the version ledger is down, and offers one Retry", () => {
    mockBuildsError = true;

    render(<DeploymentEnvironmentPage projectName="expense" environment="development" />);

    const deployment = within(screen.getByRole("region", { name: "Deployment" }));
    // Omission means "no such fact" on this page, so silence here would read
    // as a later environment's page.
    expect(
      deployment.getByText(/They exist; the console could not fetch them — retry under Past/),
    ).toBeInTheDocument();
    // Not a skeleton for ever: nothing is coming on its own.
    expect(deployment.queryByTestId("deployment-summary-skeleton")).not.toBeInTheDocument();
    // One Retry for one query — section 4 carries it, and says what failed.
    const history = within(screen.getByRole("region", { name: "Past deployments" }));
    expect(history.getByText(/The version ledger could not be read: ledger down/)).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Retry" })).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(mockBuildsRefetch).toHaveBeenCalled();
  });

  it("says the status read failed, with a Retry, instead of shimmering for ever or guessing", () => {
    mockStatusError = true;

    render(<DeploymentEnvironmentPage projectName="expense" environment="development" />);

    expect(screen.getByText(/The project's status could not be read: status down/)).toBeInTheDocument();
    const deployment = within(screen.getByRole("region", { name: "Deployment" }));
    // Neither a permanent skeleton nor "Version unknown" — that is a claim
    // about a settled read, and this read never landed.
    expect(deployment.queryByTestId("deployment-summary-skeleton")).not.toBeInTheDocument();
    expect(deployment.queryByText("Version unknown")).not.toBeInTheDocument();
    expect(deployment.getByText(/Couldn't be read/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(mockStatusRefetch).toHaveBeenCalled();
  });

  it("keeps a later environment's one true row when the build ledger is down", () => {
    // Nothing on a later environment is read from the version ledger, so its
    // failure has nothing to do with what runs there — and must not hide it
    // behind a sentence claiming otherwise.
    mockBuildsError = true;
    mockDeployments = [
      {
        componentName: "claims-api",
        environment: "production",
        status: "Ready",
        releaseName: "claims-api-prod",
        createdAt: "2026-08-15T09:00:00Z",
      },
    ];

    render(<DeploymentEnvironmentPage projectName="expense" environment="production" />);

    const section = within(screen.getByRole("region", { name: "Past deployments" }));
    expect(section.getByText("Running now")).toBeInTheDocument();
    expect(
      section.getByText("No earlier deployments are recorded for this environment."),
    ).toBeInTheDocument();
    expect(screen.queryByText(/The version ledger could not be read/)).not.toBeInTheDocument();
  });

  it("gives one version one milestone, in section 1 and in section 4 alike", () => {
    // A version still building is filtered out of the past-deployments fold
    // and comes back as the running row — which used to arrive with no
    // milestone while section 1 read one from the same ledger row.
    mockBuilds = [
      { tag: "v1", milestoneNumber: 3, status: "in_progress", startedAt: "2026-08-14T16:20:00Z" },
    ];

    render(<DeploymentEnvironmentPage projectName="expense" environment="development" />);

    expect(
      within(screen.getByRole("region", { name: "Deployment" })).getByRole("link", {
        name: "Milestone #3",
      }),
    ).toBeInTheDocument();
    expect(
      within(screen.getByRole("region", { name: "Past deployments" })).getByRole("link", {
        name: "Milestone #3",
      }),
    ).toBeInTheDocument();
  });

  it("gives each component its own way in", () => {
    render(<DeploymentEnvironmentPage projectName="expense" environment="development" />);

    expect(screen.getAllByText(/2 of 2 components live/).length).toBeGreaterThan(0);
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

    render(<DeploymentEnvironmentPage projectName="expense" environment="production" />);

    // A later environment states its own version — which nothing names — and
    // borrows neither the entry environment's milestone nor its commit.
    expect(screen.getByRole("heading", { name: "Production Environment" })).toBeInTheDocument();
    expect(screen.getByText(/expense · running since /)).toBeInTheDocument();
    const deployment = within(screen.getByRole("region", { name: "Deployment" }));
    expect(deployment.getByText("Version unknown")).toBeInTheDocument();
    expect(deployment.queryByText("Milestone")).not.toBeInTheDocument();
    expect(deployment.queryByText("Commit")).not.toBeInTheDocument();
    expect(deployment.queryByText("Validation")).not.toBeInTheDocument();
    expect(screen.getAllByText(/1 of 1 components live/).length).toBeGreaterThan(0);
    // Only the bound component is listed for production.
    expect(screen.queryByText("approvals-web")).not.toBeInTheDocument();
  });

  it("is honest about an empty environment", () => {
    mockDeployments = [];

    render(<DeploymentEnvironmentPage projectName="expense" environment="production" />);

    expect(
      screen.getByText(/Nothing deployed here yet — promote a validated version/),
    ).toBeInTheDocument();
  });

  it("does not call an environment empty when the reads that would say so failed", () => {
    // Every list-deployments read failed, so nothing is bound — but that is
    // ignorance, not emptiness, and the page must not report it as emptiness.
    mockDeployments = [];
    mockFailedCount = 2;

    render(<DeploymentEnvironmentPage projectName="expense" environment="development" />);

    expect(
      screen.getByText(/Deployments for 2 components could not be loaded/),
    ).toBeInTheDocument();
    expect(screen.queryByText(/Nothing deployed here yet/)).not.toBeInTheDocument();
  });

  it("still says nothing is deployed when every read answered", () => {
    mockDeployments = [];
    mockFailedCount = 0;

    render(<DeploymentEnvironmentPage projectName="expense" environment="development" />);

    expect(
      screen.getByText(/Nothing deployed here yet — agents deploy to Development/),
    ).toBeInTheDocument();
    expect(screen.queryByText(/could not be loaded/)).not.toBeInTheDocument();
  });

  it("rejects a segment that names no environment", () => {
    render(<DeploymentEnvironmentPage projectName="expense" environment="staging" />);

    expect(screen.getByText("No environment called staging")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Back to Deployments" })).toHaveAttribute(
      "href",
      "/projects/expense/deployments",
    );
  });
});

describe("DeploymentEnvironmentPage — test users", () => {
  it("carries the test users when every component in development is live", () => {
    mockTestUsers = [
      {
        username: "test-viewer",
        roles: ["Viewer"],
        exists: true,
        owned: true,
        supplied: false,
      },
    ];
    render(<DeploymentEnvironmentPage projectName="expense" environment="development" />);
    // Inside the web app's panel, the accounts that sign in to it.
    expect(screen.getByText("Sign in with a test user")).toBeInTheDocument();
    expect(screen.getByText("1 account · one per role · Development only")).toBeInTheDocument();
    expect(screen.getByText("test-viewer")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Reveal the password for test-viewer" })).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Open Thunder Console to add or remove real accounts" }),
    ).toBeInTheDocument();
  });

  it("carries them under a `none` aggregate too, when every binding is live", () => {
    // A version deployed before the aggregate existed, or after its run
    // settled: the aggregate tracks no rollout, the bindings are Ready, and
    // the row folds them to Deployed — an app a test user can sign in to.
    mockDeploy = { version: "v1", status: "none", components: { total: 2, ready: 0 }, validation: "none" };
    mockTestUsers = [
      { username: "test-viewer", roles: ["Viewer"], exists: true, owned: true, supplied: false },
    ];
    render(<DeploymentEnvironmentPage projectName="expense" environment="development" />);
    expect(screen.getByText("Sign in with a test user")).toBeInTheDocument();
    expect(screen.getByText("test-viewer")).toBeInTheDocument();
  });

  it("keeps the panel off a converging development, and off production", () => {
    mockDeploy = { ...mockDeploy, status: "deploying" };
    const { unmount } = render(
      <DeploymentEnvironmentPage projectName="expense" environment="development" />,
    );
    expect(screen.queryByText("Sign in with a test user")).toBeNull();
    unmount();

    mockDeploy = { ...mockDeploy, status: "deployed" };
    mockDeployments = devDeployments().map((d) => ({ ...d, environment: "production" }));
    render(<DeploymentEnvironmentPage projectName="expense" environment="production" />);
    expect(screen.queryByText("Sign in with a test user")).toBeNull();
  });
});

describe("DeploymentEnvironmentPage — try it out (ADR-0032)", () => {
  // A service panel is its identity, its URL and its Try API — by product
  // decision its individual endpoints are not listed inline; a person reads
  // them in the contract viewer Try API opens.
  it("gives a service its URL and its Try API, and lists no endpoints inline", async () => {
    const writeText = vi.fn<(text: string) => Promise<void>>(async () => undefined);
    Object.assign(navigator, { clipboard: { writeText } });

    render(<DeploymentEnvironmentPage projectName="expense" environment="development" />);

    expect(screen.getByRole("link", { name: /api.dev.expense.localhost\/claims/ })).toHaveAttribute(
      "href",
      "https://api.dev.expense.localhost/claims",
    );
    fireEvent.click(screen.getByRole("button", { name: "Copy the URL of claims-api" }));
    await waitFor(() =>
      expect(writeText).toHaveBeenCalledWith("https://api.dev.expense.localhost/claims"),
    );

    fireEvent.click(screen.getByRole("button", { name: "Try claims-api API" }));
    expect(openApiDialog).toHaveBeenLastCalledWith("claims-api");

    // No inline list, no search, no method filter, no per-row curl — and no
    // endpoint count in the header, which only a contract read could know.
    expect(screen.queryByRole("list", { name: "claims-api endpoints" })).toBeNull();
    expect(screen.queryByRole("textbox", { name: /Search claims-api endpoints/ })).toBeNull();
    expect(screen.queryByRole("group", { name: "Filter by method" })).toBeNull();
    expect(screen.queryByRole("button", { name: /Copy a curl for/ })).toBeNull();
    expect(screen.queryByTestId("endpoints-skeleton")).toBeNull();
    expect(screen.getByText(/· service$/)).toBeInTheDocument();
    expect(screen.queryByText(/endpoints?$/)).toBeNull();
  });

  it("leads with the web app, whatever order the board hands the components in", () => {
    // The board lists claims-api first; the page a person opens comes first.
    render(<DeploymentEnvironmentPage projectName="expense" environment="development" />);

    const web = screen.getByRole("link", { name: "Visit approvals-web" });
    const service = screen.getByRole("button", { name: "Try claims-api API" });
    expect(web.compareDocumentPosition(service) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    // The test users still ride in the web app's own panel — ahead of the
    // services, not in a block of their own at the end.
    const users = screen.getByText("Sign in with a test user");
    expect(users.compareDocumentPosition(service) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("gives the web app its Visit, its URL copy, and who it talks to", async () => {
    const writeText = vi.fn<(text: string) => Promise<void>>(async () => undefined);
    Object.assign(navigator, { clipboard: { writeText } });

    render(<DeploymentEnvironmentPage projectName="expense" environment="development" />);

    expect(screen.getByRole("link", { name: "Visit approvals-web" })).toHaveAttribute(
      "href",
      "https://approvals.dev.expense.localhost",
    );
    expect(screen.getByText(/Talks to/)).toHaveTextContent("Talks to claims-api on this environment");
    fireEvent.click(screen.getByRole("button", { name: "Copy the URL of approvals-web" }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith("https://approvals.dev.expense.localhost"));
  });

  // An agent has no page of its own, so the platform's test app is its page:
  // the console hands it everything public it needs to sign a person in as
  // one of the project's test users and reach the agent's gateway URL.
  describe("an agent is tried in the test app", () => {
    const agent = (): Deployment => ({
      componentName: "triage",
      environment: "development",
      status: "Ready",
      releaseName: "triage-v1-4e8a0d6",
      endpointUrl: "https://gw.dev.expense.localhost/expense-triage-http",
      createdAt: "2026-08-14T16:55:00Z",
    });
    beforeEach(() => {
      mockComponents = [...defaultComponents(), { name: "triage", displayName: "triage", type: "ai-agent" }];
      mockDeployments = [...devDeployments(), agent()];
      mockTestUsers = [
        { username: "test-engineer", roles: ["Engineer"], scopes: ["triage:use"], exists: true, owned: true, supplied: false },
      ];
    });

    it("carries the sign-in coordinates, the test users' scopes and the gateway URL", () => {
      mockSignIn = { issuer: "http://default-idp.amp.localhost:8080", clientId: "aep-dp-x-r-y" };
      render(<DeploymentEnvironmentPage projectName="expense" environment="development" />);

      const link = screen.getByRole("link", { name: "Try triage" });
      const href = new URL(link.getAttribute("href") ?? "");
      expect(href.origin).toBe("http://tryit.aep.localhost:8095");
      const query = new URLSearchParams(href.hash.slice("#/agent?".length));
      expect(query.get("project")).toBe("expense");
      expect(query.get("component")).toBe("triage");
      expect(query.get("issuer")).toBe("http://default-idp.amp.localhost:8080");
      expect(query.get("client_id")).toBe("aep-dp-x-r-y");
      expect(query.get("resource")).toBe("https://aep.wso2.com/orgs/acme/projects/expense");
      expect(query.get("scopes")).toBe("openid profile email triage:use");
      expect(query.get("endpoint")).toBe("https://gw.dev.expense.localhost/expense-triage-http");
      expect(link).toHaveAttribute("target", "_blank");
      // The accounts still render once, in the web app's panel, not again in the agent's.
      expect(screen.getAllByText("Sign in with a test user")).toHaveLength(1);
    });

    it("offers no launch the app could not honour: no sign-in client, or no account to sign in as", () => {
      render(<DeploymentEnvironmentPage projectName="expense" environment="development" />);
      expect(screen.queryByRole("link", { name: "Try triage" })).toBeNull();

      cleanup();
      mockSignIn = { issuer: "http://default-idp.amp.localhost:8080", clientId: "aep-dp-x-r-y" };
      mockTestUsers = [];
      render(<DeploymentEnvironmentPage projectName="expense" environment="development" />);
      expect(screen.queryByRole("link", { name: "Try triage" })).toBeNull();
    });
  });
});

describe("DeploymentEnvironmentPage — the deployed version's own verdict (#776 review)", () => {
  it("holds the validation cell while the deployed version's run story is out", () => {
    mockBuildVersion = "v2";
    mockRunsPending = true;

    render(<DeploymentEnvironmentPage projectName="expense" environment="development" />);

    // A skeleton in the verdict cell, not a word: the read that would settle
    // it is still out.
    expect(
      within(screen.getByRole("region", { name: "Deployment" })).getByTestId(
        "validation-cell-skeleton",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText("Not run")).not.toBeInTheDocument();
  });

  it("says the verdict is unavailable, with a retry, when the run story fails", () => {
    mockBuildVersion = "v2";
    mockRunsError = true;

    render(<DeploymentEnvironmentPage projectName="expense" environment="development" />);

    expect(screen.getByText(/The version's run story could not be loaded: runs down/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(mockRunsRefetch).toHaveBeenCalled();
    expect(screen.queryByText("Not run")).not.toBeInTheDocument();
    // Section 1's verdict cell carries the word — there is no second copy of
    // it beside the title.
    expect(
      within(screen.getByRole("region", { name: "Deployment" })).getByText("Unavailable"),
    ).toBeInTheDocument();
  });
});

describe("DeploymentEnvironmentPage — the environments read", () => {
  it("says the list could not be read, with a Retry, instead of calling a real environment unknown", () => {
    mockEnvironmentsError = true;

    render(<DeploymentEnvironmentPage projectName="expense" environment="production" />);

    expect(
      screen.getByText(/The platform's environments could not be read: gateway down/),
    ).toBeInTheDocument();
    // A failed read is not a verdict on the segment.
    expect(screen.queryByText("No environment called production")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(mockEnvironmentsRefetch).toHaveBeenCalled();
  });

  it("does not accuse a real environment of not existing while the environments read is still pending", () => {
    // "staging" is not in the ready-state pipeline (development, production) —
    // if the pending guard were dropped, the segment would read as unknown
    // the instant this query starts, on every slow connection.
    mockEnvironmentsPending = true;

    render(<DeploymentEnvironmentPage projectName="expense" environment="staging" />);

    expect(screen.queryByText("No environment called staging")).not.toBeInTheDocument();
    // Taken at its word while the list is still out: the page waits (the
    // board is one row per environment, so it cannot yet say what is or
    // is not bound to this one).
    expect(screen.getByLabelText("Loading deployments")).toBeInTheDocument();
  });

  it("waits rather than claiming a populated environment has nothing deployed while the pipeline is still loading", () => {
    // Production genuinely has something bound — this is the case that
    // exposes the lie: without the environments.isPending gate, `row` comes
    // back undefined for EVERY environment (environmentRows maps over the
    // as-yet-empty list), so a fully-deployed environment reads as empty.
    mockEnvironmentsPending = true;
    mockDeployments = [
      {
        componentName: "claims-api",
        environment: "production",
        status: "Ready",
        releaseName: "claims-api-prod",
        createdAt: "2026-08-15T09:00:00Z",
      },
    ];

    render(<DeploymentEnvironmentPage projectName="expense" environment="production" />);

    expect(screen.getByLabelText("Loading deployments")).toBeInTheDocument();
    expect(screen.queryByText(/Nothing deployed here yet/)).not.toBeInTheDocument();
    expect(screen.queryByText("No environment called production")).not.toBeInTheDocument();
  });
});
