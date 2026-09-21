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

// Router replaced so the internal-link chip renders as a plain anchor whose
// href is the resolved route path, and the PageHeader back-link as a plain
// anchor — no RouterProvider needed (mirrors NotFound.test.tsx).
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
  useNavigate: () => navigate,
}));

const navigate = vi.fn();

// Every existing test in this file assumes the page and every promote/
// configure action within it are otherwise reachable — only a dedicated "no
// permission" test flips this.
const hasBuild = vi.hoisted(() => ({ current: true }));
vi.mock("../../../auth/permissions", () => ({
  useHasAnyPermission: () => hasBuild.current,
  useHasPermission: () => hasBuild.current,
}));

// The version ledger, for the Milestone cell — the Builds surfaces' own read.
let mockBuilds: components["schemas"]["BuildSummary"][] = [];
// The newest run's story — a run parked at the deploy gate is the board's
// "on hold" (ADR-0032). Empty by default: nothing parked.
let mockRuns: MilestoneRunView[] = [];
// A story per tag, for the case where the deployed version is not the build's
// — the card reads the deployed one's own; anything unlisted answers `mockRuns`.
let mockRunsByTag: Record<string, MilestoneRunView[]> = {};
let mockRunsError = false;
let mockRunsPending = false;
const mockRunsRefetch = vi.fn();
vi.mock("../../builds/api/queries", () => ({
  useBuilds: () => ({ data: mockBuilds, isPending: false, isError: false }),
  useBuildRuns: (_p: string, tag?: string) => ({
    data: mockRunsError || mockRunsPending ? undefined : { runs: mockRunsByTag[tag ?? ""] ?? mockRuns },
    isPending: mockRunsPending && Boolean(tag),
    isError: mockRunsError && Boolean(tag),
    error: mockRunsError ? new Error("runs down") : null,
    refetch: mockRunsRefetch,
  }),
}));

import { DeploymentsPage } from "./DeploymentsPage";

type ProjectStatus = components["schemas"]["ProjectStatus"];
type DeployStage = components["schemas"]["DeployStage"];
type ComponentDependencies = components["schemas"]["ComponentDependencies"];
type ExternalResourceDTO = components["schemas"]["ExternalResourceDTO"];
type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type ProjectDependencyReadiness = components["schemas"]["ProjectDependencyReadiness"];

// The roles read is NOT mocked here on purpose: the Test users panel left this
// page for the environment page (ADR-0032), and a card that reached for it
// would throw for want of a QueryClient — which is the assertion.

// Query hooks replaced wholesale — no QueryClientProvider / MSW needed, only the
// rendering under test is real (mirrors TasksList.test.tsx).
let mockDeploy: DeployStage = {
  version: "v1",
  status: "deployed",
  components: { total: 1, ready: 1 },
  validation: "none",
};

// The component/binding join. One serving binding by default; the on-hold
// case empties it, because a parked run has deployed nothing.
const DEFAULT_DEPLOYMENTS = [
  {
    componentName: "storefront",
    environment: "development",
    status: "Ready",
    endpointUrl: "https://storefront.dev.example.com",
  },
];
let mockDeployments = DEFAULT_DEPLOYMENTS;

// The design's dependency read (the promote dialog's connection list, and
// the Configure button's own gate) — overridden per test; defaults to one
// required external connection, reset in beforeEach so a test that mutates
// it can't bleed into the next.
const DEFAULT_DEPENDENCIES: ComponentDependencies[] = [
  {
    componentName: "storefront",
    dependencies: [
      {
        kind: "external",
        name: "stripe",
        config: [
          { key: "STRIPE_SECRET_KEY", description: "Secret key", secret: true },
        ],
      },
    ],
  },
];
let mockDependencies: ComponentDependencies[] = DEFAULT_DEPENDENCIES;
let mockDependenciesPending = false;

// Org catalog for Registered vs Project External. Default empty / no envCells
// so the fixture `stripe` stays a Project External (re-collect test).
let mockExternalCatalog: ExternalResourceDTO[] = [];
let mockExternalCatalogPending = false;
let mockExternalCatalogError = false;

function status(): ProjectStatus {
  return {
    phase: "components",
    repoStatus: "ready",
    repoUrl: "https://github.com/acme/demo",
    hasSpec: true,
    hasDesign: true,
    hasTasks: true,
    specStatus: "approved",
    spec: { exists: true, version: "v1", dirty: false, design: true, agent: "" },
    build: { version: mockBuildVersion, status: "succeeded" },
    deploy: mockDeploy,
  };
}
// The BUILD version — the newest run's tag, which the aggregate's validation
// describes. v1 with the deployed version by default; a test moves it ahead.
let mockBuildVersion = "v1";

// The connection-values dialog's mutation is mocked at module level so opening
// it needs no QueryClientProvider; mutate is captured for the save assertion.
const mockMutate = vi.fn();

vi.mock("../api/queries", () => ({
  // The platform's pipeline, in promotion order — what `useEnvironments`
  // serves. Two environments here because that is the pipeline these tests
  // describe, not because the console knows only two.
  useEnvironments: () => ({
    data:
      mockEnvironmentsState === "ready"
        ? mockEnvironments
        : mockEnvironmentsState === "empty"
          ? []
          : undefined,
    isPending: mockEnvironmentsState === "pending",
    isError: mockEnvironmentsState === "error",
    error: mockEnvironmentsState === "error" ? new Error("gateway down") : null,
    refetch: mockEnvironmentsRefetch,
  }),
  useSaveConnectionValues: () => ({
    mutate: mockMutate,
    isPending: false,
    isError: false,
    error: null,
    reset: vi.fn(),
  }),
  useProjectComponents: () => ({
    data: { items: [{ name: "storefront", displayName: "Storefront", type: "web-application" }] },
    isPending: false,
    isError: false,
    error: null,
    refetch: vi.fn(),
  }),
  useComponentsDeployments: () => ({
    isPending: false,
    deployments: mockDeployments,
    failedCount: 0,
  }),
  useProjectStatus: () => ({
    data: mockStatusState === "ready" ? status() : undefined,
    isPending: mockStatusState === "pending",
    isError: mockStatusState === "error",
  }),
  useProjectDependencyReadiness: () => ({
    data: mockReadiness,
    isPending: mockReadinessPending,
    isError: false,
  }),
}));

// Whether the platform holds dev values for each external — the deploy gate's
// own read. Undefined (still loading) by default, so a connection's state word
// stays blank until a test says what the platform holds.
let mockReadiness: ProjectDependencyReadiness | undefined;
let mockReadinessPending = false;
// The status poll — the read the card's `version` comes off.
let mockStatusState: "ready" | "pending" | "error" = "ready";

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
let mockEnvironmentsState: "ready" | "pending" | "error" | "empty" = "ready";
const mockEnvironmentsRefetch = vi.fn();

/** A run parked at the deploy gate, short of the named values. */
function parkedRun(blocking: string[]): MilestoneRunView {
  return {
    id: "run-1",
    milestoneNumber: 1,
    milestoneTitle: "v1",
    kind: "dev",
    origin: "spec-build",
    state: "waiting",
    waitingReason: "external-values",
    blockingDependencies: blocking,
    budgets: { cyclesTotal: 0, cycleCeiling: 8, fixCycles: 0, fixCeiling: 3, conflictCycles: 0, conflictCeiling: 2 },
    cycles: [],
    createdAt: "2026-09-10T08:00:00Z",
  } as unknown as MilestoneRunView;
}

vi.mock("../../spec/api/queries", () => ({
  useDesignDependencies: () => ({
    data: mockDependenciesPending ? undefined : mockDependencies,
    isPending: mockDependenciesPending,
    isError: false,
  }),
}));

vi.mock("../../settings/api/queries", () => ({
  useExternalResources: () => ({
    data: mockExternalCatalog,
    isPending: mockExternalCatalogPending,
    isError: mockExternalCatalogError,
    error: mockExternalCatalogError ? new Error("catalog down") : null,
    refetch: vi.fn(),
  }),
}));

// The criteria/report join (#395 decision 3) — counts undefined by default (the
// fallback path); individual tests set them to assert the "n/m passed" upgrade. The
// VERDICT rides with them because `deploy.validation` folds `failed` and `unreported`
// into one `awaiting-fix`, and the banner's sentence differs for each.
let mockCounts:
  | { passed: number; failed: number; uncovered: number; total: number }
  | undefined;
let mockVerdict = "";
// Whether the attempt in flight REPAIRS that verdict (self-heal, one run repeating)
// or re-asks it (a revalidation, a fresh run row).
let mockRepairing = false;

let mockValidationPending = false;
// What the page asked the evidence hook for — the version and the validation
// word must be the card's, not the newest build's.
const evidenceArgs = vi.fn();
vi.mock("../../validation/api/counts", () => ({
  useValidationEvidence: (...args: unknown[]) => {
    evidenceArgs(...args);
    return {
      verdict: mockVerdict,
      repairing: mockRepairing,
      pending: mockValidationPending,
      ...(mockCounts ? { counts: mockCounts } : {}),
    };
  },
}));

/** A settled dev run that judged its version. */
function judgedRun(tag: string, verdict: "passed" | "partial" | "failed"): MilestoneRunView {
  return {
    id: `run-${tag}-1`,
    milestoneNumber: 1,
    milestoneTitle: tag,
    kind: "dev",
    origin: "spec-build",
    state: "succeeded",
    validation: { verdict },
    budgets: { cyclesTotal: 2, cycleCeiling: 8, fixCycles: 0, fixCeiling: 3, conflictCycles: 0, conflictCeiling: 2 },
    cycles: [],
    createdAt: "2026-09-01T08:00:00Z",
  } as unknown as MilestoneRunView;
}

beforeEach(() => {
  mockCounts = undefined;
  mockVerdict = "";
  mockRepairing = false;
  mockMutate.mockClear();
  mockDependencies = DEFAULT_DEPENDENCIES;
  mockDependenciesPending = false;
  mockExternalCatalog = [];
  mockExternalCatalogPending = false;
  mockExternalCatalogError = false;
  mockBuilds = [];
  mockDeployments = DEFAULT_DEPLOYMENTS;
  mockRuns = [];
  mockRunsByTag = {};
  mockRunsError = false;
  mockRunsPending = false;
  mockRunsRefetch.mockClear();
  mockBuildVersion = "v1";
  mockReadiness = undefined;
  mockReadinessPending = false;
  mockEnvironmentsState = "ready";
  mockEnvironmentsRefetch.mockClear();
  mockStatusState = "ready";
  mockValidationPending = false;
  evidenceArgs.mockClear();
  navigate.mockClear();
  hasBuild.current = true;
});

describe("DeploymentsPage — validation", () => {
  // A run mid-self-heal. `awaiting-fix` folds `failed` and `unreported` into one
  // word, so the banner reads the RUN's verdict for the numbers and names the
  // implementation as what is being fixed — the state used to render as
  // "This deployment\'s verdict: awaiting fix.", a lifecycle value announced as a
  // verdict, over a stage note claiming the system "was checked".
  it("says what failed and what is being done, while the loop is healing", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "awaiting-fix",
    };
    mockVerdict = "failed";
    mockCounts = { passed: 4, failed: 2, uncovered: 0, total: 6 };

    render(<DeploymentsPage projectName="acme" />);

    expect(
      screen.getByText(
        "2 of 6 criteria failed. The implementation is being fixed. Validation will run again.",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText(/verdict: awaiting fix/)).not.toBeInTheDocument();
    // "Runs again" belongs to the banner's own sentence, whose wording the
    // Validation page's tile shares and must keep — nothing else on the card
    // restates it.
    expect(screen.queryByText(/Runs again/)).not.toBeInTheDocument();
  });

  // A SETTLED failure. The banner wrote its own sentence for these and led with the
  // count that PASSED ("Validation failed — 4 of 6 criteria passed on this
  // deployment"), while the tile on the Validation page led with the failures — one
  // outcome, two voices and two headline numbers, depending which surface you were on.
  it("leads a settled failure with the failures, in the tile's own words", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "failed",
    };
    mockCounts = { passed: 4, failed: 2, uncovered: 0, total: 6 };

    render(<DeploymentsPage projectName="acme" />);

    expect(
      screen.getByText(
        "2 of 6 criteria failed. The run stopped here, so the milestone stays open for the fix.",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText(/criteria passed on this deployment/)).not.toBeInTheDocument();
  });

  // Re-running validation on an already-PASSED version. The verdict lives on an
  // older run row — a revalidation is a fresh one — so reading the asking run made
  // this say the agent had reported nothing, as if the version had never been judged.
  it("shows the last result while a revalidation re-asks a passed version", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "running",
    };
    mockVerdict = "passed";
    mockCounts = { passed: 6, failed: 0, uncovered: 0, total: 6 };

    render(<DeploymentsPage projectName="acme" />);

    expect(
      screen.getByText("All 6 criteria passed in the last attempt. Validation is running again."),
    ).toBeInTheDocument();
    expect(screen.queryByText(/validation agent is running/)).not.toBeInTheDocument();
    // Nothing was fixed — that clause belongs to a repair, not a re-ask.
    expect(screen.queryByText(/fixed and deployed/)).not.toBeInTheDocument();
  });

  // A FIRST attempt: live, validating, no verdict on the row yet. The banner used to
  // fall through to its verdict-naming fallback here and render "This deployment's
  // verdict: validating." — a lifecycle value announced as a verdict.
  it("does not call a first running attempt a verdict", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "running",
    };
    mockVerdict = "";

    render(<DeploymentsPage projectName="acme" />);

    expect(
      screen.getByText("The validation agent is running."),
    ).toBeInTheDocument();
    expect(screen.queryByText(/verdict: validating/)).not.toBeInTheDocument();
  });

  // The same fold, the other way: nothing is filed for an `unreported` attempt, so
  // promising a fix would name work that does not exist.
  it("promises a retry, not a fix, when the repeated verdict was unreported", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "awaiting-fix",
    };
    mockVerdict = "unreported";

    render(<DeploymentsPage projectName="acme" />);

    expect(
      screen.getByText(
        "The validation report couldn't be generated. Validation will run again.",
      ),
    ).toBeInTheDocument();
  });

  it("routes a PASSED validation to the Validation page", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "passed",
    };

    render(<DeploymentsPage projectName="acme" />);

    // ONE way into the Validation page from this card, on the rail's own stage.
    // There used to be a second — a pill in the Dev environment panel — which said
    // "Awaiting fix" with no subject in a card about deployments, and carried less
    // than the row it duplicated.
    const link = screen.getByRole("link", { name: /View validations/ });
    expect(link).toHaveAttribute("href", "/projects/acme/validation");
    expect(link).not.toHaveAttribute("target");
  });

  it("renders no verdict when there is nothing to validate", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "none",
    };

    render(<DeploymentsPage projectName="acme" />);

    // The rail's Validation STAGE is still on screen (it is a stage of the
    // story), but with no verdict there is no banner and no report link.
    expect(screen.queryByText(/View validations/)).not.toBeInTheDocument();
  });
});

describe("DeploymentsPage — environment board", () => {
  it("seats each environment on a card with what it runs", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "passed",
    };
    mockBuilds = [
      { tag: "v1", milestoneNumber: 3, status: "completed", startedAt: "2026-08-14T16:20:00Z" },
    ];

    render(<DeploymentsPage projectName="acme" />);

    // The two cards, named as places.
    expect(screen.getByRole("heading", { name: "Development" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Production" })).toBeInTheDocument();
    // The dev card's first step says what is live and invites the try, and
    // LEADS with the version, in the block under the Deployment step's title —
    // the milestone read off the version ledger, linked to its GitHub page.
    const development = screen.getByTestId("environment-card-development");
    // The count lives in the Components group's headline, not in prose above it.
    expect(within(development).getByRole("group", { name: "Components — 1 of 1 live" })).toBeInTheDocument();
    const vm = within(development).getByTestId("version-block");
    expect(within(vm).getByText("Version v1")).toBeInTheDocument();
    expect(within(vm).getByRole("link", { name: "Milestone #3" })).toHaveAttribute(
      "href",
      "https://github.com/acme/demo/milestone/3",
    );
    // Development's group reads unknown values as not set while the
    // readiness read is out — and it is Development's alone.
    expect(within(development).getByRole("group", { name: "Dependencies — 0 of 1 set" })).toBeInTheDocument();
    // Production is empty and stays so: it lists nothing, and its own card
    // says what has to happen before anything runs there.
    const production = screen.getByTestId("environment-card-production");
    expect(
      within(production).getByText("Nothing running yet. v1 on Development is ready to promote here."),
    ).toBeInTheDocument();
    expect(within(production).queryByRole("group", { name: /^Components/ })).not.toBeInTheDocument();
    expect(within(production).queryByRole("group", { name: /^Dependencies/ })).not.toBeInTheDocument();
    // …and a card with nothing bound carries no version block at all.
    expect(within(production).queryByTestId("version-block")).not.toBeInTheDocument();
  });

  it("upgrades the validation banner with criteria counts", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "passed",
    };
    mockCounts = { passed: 12, failed: 0, uncovered: 0, total: 12 };

    render(<DeploymentsPage projectName="acme" />);

    // The tile's own sentence, word for word — the banner used to write its own,
    // which is how a settled FAILURE came to lead with the count that passed.
    expect(
      screen.getByText("All 12 criteria were covered by a test and passed."),
    ).toBeInTheDocument();
  });

  it("says Deploying on the card while the rollout converges, and withholds promotion", () => {
    mockDeploy = {
      version: "v2",
      status: "deploying",
      components: { total: 1, ready: 0 },
      validation: "none",
    };

    render(<DeploymentsPage projectName="acme" />);

    // The card's status chip and step 1's title both say it.
    const development = screen.getByTestId("environment-card-development");
    expect(within(development).getAllByText("Deploying").length).toBeGreaterThan(0);
    // A verdict is expected and has not arrived, so no promotion is offered —
    // with the reason beside the disabled button.
    expect(screen.getByRole("button", { name: /Promote v2 to Production/ })).toBeDisabled();
    expect(screen.getByText("Unavailable until v2 deploys and validates")).toBeInTheDocument();
  });
});

describe("DeploymentsPage — connections", () => {
  it("re-collects an external connection's values from the Development card", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "passed",
    };

    render(<DeploymentsPage projectName="acme" />);

    // The Development card's connections group carries the dev Configure,
    // named per connection for screen readers; the Production card's own
    // Configure for the same connection says so in its name.
    fireEvent.click(screen.getByRole("button", { name: "Configure stripe" }));
    const dialog = screen.getByRole("dialog");
    expect(
      within(dialog).getByText("Configure — stripe"),
    ).toBeInTheDocument();

    // Write-only: the field opens empty and masked, never echoing a stored
    // value; Save enables once every value is set.
    // The key labels the field; the description renders as helper text.
    const field = within(dialog).getByLabelText("STRIPE_SECRET_KEY");
    expect(within(dialog).getByText("Secret key")).toBeInTheDocument();
    expect(field).toHaveAttribute("type", "password");
    expect(field).toHaveValue("");
    const saveButton = within(dialog).getByRole("button", { name: /Save values/ });
    expect(saveButton).toBeDisabled();
    fireEvent.change(field, { target: { value: "sk_live_real" } });
    expect(saveButton).toBeEnabled();
    fireEvent.click(saveButton);

    expect(mockMutate).toHaveBeenCalledWith(
      {
        name: "stripe",
        environment: "development",
        values: { STRIPE_SECRET_KEY: "sk_live_real" },
      },
      expect.anything(),
    );
  });

  it("shows platform-provisioned connections without an update action", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "passed",
    };
    mockDependencies = [
      {
        componentName: "storefront",
        dependencies: [
          { kind: "component", name: "orders-api" },
          { kind: "platform-resource", name: "shop-db", resourceType: "postgres-cnpg" },
          // Config-carrying but platform-owned: no Configure, no
          // "provisioned" inference — the platform manages its credentials.
          {
            kind: "platform-resource",
            name: "shop-auth",
            resourceType: "thunder-app",
            config: [{ key: "CLIENT_SECRET", secret: true }],
          },
        ],
      },
    ];

    render(<DeploymentsPage projectName="acme" />);

    // The Development card lists the platform resource and offers nothing to
    // configure in development. The identity app's production value IS
    // collected — the promote dialog asks for it — so step 3 names it.
    expect(screen.getByText("shop-db")).toBeInTheDocument();
    expect(screen.getByText("postgres-cnpg")).toBeInTheDocument();
    expect(screen.getByText("Provisioned")).toBeInTheDocument();
    expect(screen.getByText("Platform-managed")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Configure shop-db" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Configure shop-auth" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Configure shop-auth for Production" })).toBeInTheDocument();
  });

  // Registered External: org catalog row with non-empty envCells — values live
  // on the org plane, so Deployments must not offer Connection values dialog.
  it("hides Configure for a Registered external connection", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "passed",
    };
    mockExternalCatalog = [
      {
        name: "stripe",
        config: [{ key: "STRIPE_SECRET_KEY", secret: true }],
        consumers: [],
        envCells: [
          {
            environment: "development",
            key: "STRIPE_SECRET_KEY",
            status: "configured",
          },
        ],
      },
    ];

    render(<DeploymentsPage projectName="acme" />);

    expect(
      screen.queryByRole("button", { name: "Configure stripe" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  // While the org catalog is still loading, registeredNames is empty — a
  // Registered row must not flash Configure (which would open the project
  // values dialog for a name that might already live on the org plane).
  it("does not show Configure for an external while the org catalog is loading", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "passed",
    };
    mockExternalCatalogPending = true;
    mockExternalCatalog = [];

    render(<DeploymentsPage projectName="acme" />);

    expect(
      screen.queryByRole("button", { name: "Configure stripe" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  // Catalog error leaves registeredNames empty the same way pending does —
  // fail closed so a Registered name cannot open the project values dialog.
  it("does not show Configure for an external when the org catalog fails", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "passed",
    };
    mockExternalCatalogError = true;
    mockExternalCatalog = [];

    render(<DeploymentsPage projectName="acme" />);

    expect(
      screen.queryByRole("button", { name: "Configure stripe" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.getByText(/Failed to load org catalog/i)).toBeInTheDocument();
  });

  // Project External under a new name: empty/omitted catalog envCells — still
  // opens the values dialog (and POSTs values; never register).
  it("opens Configure for a Project External connection", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "passed",
    };
    mockDependencies = [
      {
        componentName: "storefront",
        dependencies: [
          {
            kind: "external",
            name: "acme-stripe",
            config: [
              {
                key: "STRIPE_SECRET_KEY",
                description: "Secret key",
                secret: true,
              },
            ],
          },
        ],
      },
    ];
    mockExternalCatalog = [
      {
        name: "acme-stripe",
        config: [{ key: "STRIPE_SECRET_KEY", secret: true }],
        consumers: [],
        // Empty envCells = Project External (same as omitted).
        envCells: [],
      },
    ];

    render(<DeploymentsPage projectName="acme" />);

    fireEvent.click(
      screen.getByRole("button", { name: "Configure acme-stripe" }),
    );
    const dialog = screen.getByRole("dialog");
    expect(
      within(dialog).getByText("Configure — acme-stripe"),
    ).toBeInTheDocument();
  });
});

describe("DeploymentsPage — promotion", () => {
  // The reported bug. A build finishes, every binding goes Ready, and the deploy
  // aggregate reports `deployed` — but the validation cycle has not started, so
  // `validation` is still `none`. For that whole window (the reconcile sweep that
  // starts the validation run ticks once a minute) this button offered production
  // a version nothing had checked.
  it("does not offer promotion while a verdict is still expected", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "none",
    };

    render(<DeploymentsPage projectName="acme" />);

    expect(
      screen.getByRole("button", { name: /Promote v1 to Production/ }),
    ).toBeDisabled();
  });

  // The other half, and why `none` could not simply be blocked on its own: a person
  // who cancelled the judging has already made this call, and with no revalidate
  // control in the console a permanently dead button would strand the version.
  it("offers promotion once a person has cancelled the judging", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "cancelled",
    };
    // No values to collect, so validation's say is the whole gate.
    mockDependencies = [];

    render(<DeploymentsPage projectName="acme" />);

    expect(
      screen.getByRole("button", { name: /Promote v1 to Production/ }),
    ).toBeEnabled();
  });

  // ADR-0032: the missing value is step 3's blocker line, with Configure inline;
  // the dialog opens ON that connection, and Promote enables once it is set.
  it("names the missing value on step 3, collects it, and then enables Promote", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "passed",
    };

    render(<DeploymentsPage projectName="acme" />);

    const promote = screen.getByRole("button", { name: /Promote v1 to Production/ });
    expect(promote).toBeDisabled();
    expect(screen.getByText("1 value missing")).toBeInTheDocument();
    expect(screen.getByText("stripe has no Production value")).toBeInTheDocument();
    expect(screen.getByText("Enabled once the value is set")).toBeInTheDocument();

    // Named for the promotion target, so it is never mistaken for the dev re-collect.
    fireEvent.click(screen.getByRole("button", { name: "Configure stripe for Production" }));

    const dialog = screen.getByRole("dialog");
    expect(
      within(dialog).getByText(/1 connection needs production values/),
    ).toBeInTheDocument();
    const field = within(dialog).getByLabelText(/STRIPE_SECRET_KEY/);
    expect(field).toHaveFocus();
    const confirm = within(dialog).getByRole("button", { name: /^Promote$/ });
    expect(confirm).toBeDisabled();
    fireEvent.change(field, { target: { value: "sk_live_x" } });
    expect(confirm).toBeEnabled();

    // Back on the board the blocker is gone and the step is ready. The dialog
    // is still leaving (MUI's exit transition) and keeps the page aria-hidden
    // meanwhile, so the board is queried with hidden elements included; the
    // board's button precedes the portal in DOM order.
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(screen.queryByText("stripe has no Production value")).not.toBeInTheDocument();
    expect(
      screen.getAllByRole("button", { name: /Promote v1 to Production/, hidden: true })[0],
    ).toBeEnabled();
  });

  it("disables the promote entry point while validation is failing", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "failed",
    };

    render(<DeploymentsPage projectName="acme" />);

    expect(
      screen.getByRole("button", { name: /Promote v1 to Production/ }),
    ).toBeDisabled();
  });

  it("counts platform-provisioned connections as already set", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "passed",
    };
    mockDependencies = [
      {
        componentName: "storefront",
        dependencies: [
          // Component wiring never surfaces as a connection…
          { kind: "component", name: "orders-api" },
          // …a config-less platform resource needs nothing…
          { kind: "platform-resource", name: "shop-db", resourceType: "postgres-cnpg" },
          // …and a defaulted key arrives already set.
          {
            kind: "external",
            name: "stripe",
            config: [{ key: "KEY", description: "Key", defaultValue: "k" }],
          },
        ],
      },
    ];

    render(<DeploymentsPage projectName="acme" />);

    // Nothing is missing, so step 3 names no blocker and Promote is ready.
    expect(screen.queryByText(/has no production value/)).not.toBeInTheDocument();

    fireEvent.click(
      screen.getByRole("button", { name: /Promote v1 to Production/ }),
    );
    const dialog = screen.getByRole("dialog");
    expect(
      within(dialog).getByText("Provisioned by platform"),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByRole("button", { name: /^Promote$/ }),
    ).toBeEnabled();
  });
});

describe("DeploymentsPage — the flow (ADR-0032)", () => {
  it("reads top to bottom as deployed → validation → promote, with Try it out", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "running",
    };
    mockBuilds = [
      { tag: "v1", milestoneNumber: 3, status: "completed", startedAt: "2026-08-14T16:20:00Z" },
    ];
    mockReadiness = {
      configured: true,
      dependencies: [{ name: "stripe", state: "configured", missingKeys: [] }],
    };

    render(<DeploymentsPage projectName="acme" />);

    const flow = screen.getByRole("list", { name: "Development flow" });
    const steps = within(flow).getAllByRole("listitem");
    expect(steps.map((s) => s.getAttribute("aria-label"))).toEqual([
      "Step 1, Deployed",
      "Step 2, Validation, Running",
      "Step 3, Promote to Production",
    ]);
    // The Deployment step LEADS with the version and its milestone.
    expect(within(steps[0]!).getByText("Version v1")).toBeInTheDocument();
    expect(within(steps[0]!).getByRole("link", { name: "Milestone #3" })).toBeInTheDocument();
    // Step 1: the components and the dependencies, then the one primary action.
    expect(within(steps[0]!).getByRole("group", { name: "Components — 1 of 1 live" })).toBeInTheDocument();
    expect(within(steps[0]!).getByText("Storefront")).toBeInTheDocument();
    expect(within(steps[0]!).getByText("web app")).toBeInTheDocument();
    expect(within(steps[0]!).getByText("Live")).toBeInTheDocument();
    expect(within(steps[0]!).getByRole("group", { name: "Dependencies — 1 of 1 set" })).toBeInTheDocument();
    expect(within(steps[0]!).getByText("Set")).toBeInTheDocument();
    const tryIt = within(steps[0]!).getByRole("link", { name: /Try it out/ });
    expect(tryIt).toHaveAttribute("href", "/projects/acme/deployments/development");
    expect(tryIt).not.toHaveAttribute("aria-disabled", "true");
    // The button stands alone, as the design draws it: no caption beside it,
    // and no prose above the version block restating the counts.
    expect(
      screen.queryByText("Opens the deployment view: app, endpoints, test users"),
    ).not.toBeInTheDocument();
    expect(screen.queryByText(/You can try them now/)).not.toBeInTheDocument();
    // Step 2 keeps the shared sentence and the one link.
    expect(within(steps[1]!).getByText("The validation agent is running.")).toBeInTheDocument();
    expect(within(steps[1]!).getByRole("link", { name: /View validations/ })).toBeInTheDocument();
    // Step 3 waits on validation, and says so.
    expect(within(steps[2]!).getByRole("button", { name: /Promote v1 to Production/ })).toBeDisabled();
    expect(within(steps[2]!).getByText("Enabled when validation passes")).toBeInTheDocument();
    // The test users left the card for the environment page.
    expect(screen.queryByText(/Test users for agents/)).not.toBeInTheDocument();
    expect(screen.queryByText("Thunder Console")).not.toBeInTheDocument();
  });

  it("says a settled verdict on step 2 with its counts", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "passed",
    };
    mockCounts = { passed: 25, failed: 0, uncovered: 0, total: 25 };

    render(<DeploymentsPage projectName="acme" />);

    const flow = screen.getByRole("list", { name: "Development flow" });
    expect(within(flow).getByRole("listitem", { name: "Step 2, Validation, Passed · 25 of 25" })).toBeInTheDocument();
  });

  // Artboard 9c: the newest run is parked at the deploy gate. The board says so
  // where the reader is — until now only the Builds page named the park.
  it("reads on hold when the run is parked on a dependency value", () => {
    mockDeploy = {
      version: "v1",
      status: "none",
      components: { total: 1, ready: 0 },
      validation: "none",
    };
    mockRuns = [parkedRun(["stripe"])];
    mockDeployments = [];
    mockReadiness = {
      configured: false,
      dependencies: [{ name: "stripe", state: "unset", missingKeys: ["STRIPE_SECRET_KEY"] }],
    };

    render(<DeploymentsPage projectName="acme" />);

    const flow = screen.getByRole("list", { name: "Development flow" });
    const steps = within(flow).getAllByRole("listitem");
    expect(steps[0]).toHaveAttribute("aria-label", "Step 1, Deploy, On hold");
    expect(screen.getByText("Needs a value for stripe before deploying")).toBeInTheDocument();
    expect(
      screen.getByText("storefront depends on it. Deployment continues automatically once it is set."),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Deployment is on hold until one dependency value is set. It continues automatically."),
    ).toBeInTheDocument();
    // The component names what it waits on; the connection is Missing.
    expect(within(steps[0]!).getByRole("group", { name: "Components — 0 of 1 deployed · on hold" })).toBeInTheDocument();
    expect(within(steps[0]!).getByText("Needs stripe")).toBeInTheDocument();
    expect(within(steps[0]!).getByText("Missing")).toBeInTheDocument();
    // Try it out is drawn, and disabled.
    expect(within(steps[0]!).getByRole("link", { name: /Try it out/ })).toHaveAttribute("aria-disabled", "true");
    // Steps 2 and 3 are inactive with one line each.
    expect(within(steps[1]!).getByText("Runs once something is deployed here.")).toBeInTheDocument();
    expect(within(steps[2]!).getByText("Unavailable until v1 deploys and validates")).toBeInTheDocument();
    // Production explains itself in its OWN card's first step: a version
    // arrives there by promotion, never by a build landing in it.
    expect(
      screen.getByText("Nothing running yet. v1 on Development is ready to promote here."),
    ).toBeInTheDocument();

    // The notice's Configure is the dev re-collect for the blocking connection.
    fireEvent.click(within(steps[0]!).getByRole("button", { name: "Configure" }));
    expect(within(screen.getByRole("dialog")).getByText("Configure — stripe")).toBeInTheDocument();
  });

  it("holds skeletons for the connections and the promote step while the design read is out", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "passed",
    };
    mockDependenciesPending = true;

    render(<DeploymentsPage projectName="acme" />);

    expect(screen.queryByRole("group", { name: /^Dependencies/ })).not.toBeInTheDocument();
    expect(screen.getByTestId("connections-skeleton")).toBeInTheDocument();
    // Step 3 cannot know what is missing yet, so it does not say nothing is.
    expect(screen.queryByText(/has no production value/)).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Promote v1 to Production/ })).not.toBeInTheDocument();
    expect(screen.getByTestId("promote-skeleton")).toBeInTheDocument();
    // …and step 1 is still the flow's first step, with its components.
    expect(screen.getByRole("group", { name: "Components — 1 of 1 live" })).toBeInTheDocument();
  });

  it("holds a skeleton on step 2 while the validation evidence is out", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "passed",
    };
    mockValidationPending = true;

    render(<DeploymentsPage projectName="acme" />);

    expect(screen.getByTestId("validation-skeleton")).toBeInTheDocument();
    expect(screen.queryByText(/View validations/)).not.toBeInTheDocument();
    expect(screen.queryByText("Passed · 4 of 4")).not.toBeInTheDocument();
  });
});

describe("DeploymentsPage — the card's version (review round)", () => {
  it("keeps steps 2 and 3 about the deployed version while a newer build runs", () => {
    // v1 serves while v2 builds. The aggregate's validation is v2's — `none`,
    // nothing has judged it — but the card is v1's, whose own run story says
    // it passed. Reading the aggregate here drew v1 as never validated and
    // withheld its promotion on v2's account.
    mockBuildVersion = "v2";
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "none",
    };
    mockRunsByTag = { v1: [judgedRun("v1", "passed")] };
    mockVerdict = "passed";

    render(<DeploymentsPage projectName="acme" />);

    expect(evidenceArgs).toHaveBeenCalledWith("acme", "v1", "passed");
    const flow = screen.getByRole("list", { name: "Development flow" });
    const steps = within(flow).getAllByRole("listitem");
    expect(steps[1]).toHaveAttribute("aria-label", "Step 2, Validation, Passed");
    expect(screen.queryByText("Starts automatically now that the deployment is live.")).not.toBeInTheDocument();
    // Step 3 is v1's too: validated, so only the missing value stands in the way.
    expect(within(steps[2]!).getByText("Enabled once the value is set")).toBeInTheDocument();
    expect(screen.queryByText("Enabled when validation passes")).not.toBeInTheDocument();
    // …and the Deployment step leads with the version it is about.
    expect(screen.getByText("Version v1")).toBeInTheDocument();
  });

  it("says a failed run-story read rather than drawing the ordinary state over it", () => {
    // Without the newest run the board cannot tell a park from a pending
    // deployment, so it must not quietly draw the latter.
    mockDeploy = {
      version: "",
      status: "none",
      components: { total: 1, ready: 0 },
      validation: "none",
    };
    mockDeployments = [];
    mockRunsError = true;

    render(<DeploymentsPage projectName="acme" />);

    expect(
      screen.getByText(/The version's run story could not be loaded: runs down/),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(mockRunsRefetch).toHaveBeenCalled();
  });

  // The deployed version's verdict is its own run story's while it is behind
  // the build — a read that can be out, or fail. Neither is "Not run".
  it("holds the verdict while the deployed version's own run story is still out", () => {
    mockDeploy = { version: "v1", status: "deployed", components: { total: 1, ready: 1 }, validation: "running" };
    mockBuildVersion = "v2";
    mockRunsPending = true;

    render(<DeploymentsPage projectName="acme" />);

    expect(screen.queryByText("Not run")).not.toBeInTheDocument();
    expect(screen.getByTestId("validation-skeleton")).toBeInTheDocument();
    expect(screen.getByTestId("promote-skeleton")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Promote v1 to Production/ })).not.toBeInTheDocument();
  });

  it("says the verdict is unavailable, and withholds promotion, when the deployed version's run story fails", () => {
    mockDeploy = { version: "v1", status: "deployed", components: { total: 1, ready: 1 }, validation: "running" };
    mockBuildVersion = "v2";
    mockRunsError = true;

    render(<DeploymentsPage projectName="acme" />);

    expect(screen.getByText(/The version's run story could not be loaded: runs down/)).toBeInTheDocument();
    expect(screen.queryByText("Not run")).not.toBeInTheDocument();
    const flow = screen.getByRole("list", { name: "Development flow" });
    const steps = within(flow).getAllByRole("listitem");
    expect(within(steps[1]!).getByText("The run story could not be loaded, so this version's verdict is unknown.")).toBeInTheDocument();
    // An unknown verdict is not a permission to promote.
    expect(within(steps[2]!).getByRole("button", { name: /Promote v1 to Production/ })).toBeDisabled();
    expect(within(steps[2]!).getByText("Unavailable until v1's run story loads")).toBeInTheDocument();
  });

  it("names production's own status over its live count, not Running for every populated card", () => {
    mockDeployments = [
      ...DEFAULT_DEPLOYMENTS,
      { componentName: "storefront", environment: "production", status: "Failed", endpointUrl: "" },
    ];

    render(<DeploymentsPage projectName="acme" />);

    // The Production card is headed by ITS OWN status word, read off its own
    // fold — a populated environment is not a running one.
    const production = screen.getByTestId("environment-card-production");
    expect(within(production).getAllByText("Deploy failed").length).toBeGreaterThan(0);
    expect(within(production).queryByText("Running")).not.toBeInTheDocument();
  });
});

describe("DeploymentsPage — the environments read", () => {
  it("waits rather than claiming nothing is deployed while the pipeline is still loading", () => {
    mockEnvironmentsState = "pending";

    render(<DeploymentsPage projectName="acme" />);

    // The flow is one card per environment, so with the list still out the
    // page knows nothing yet — and must not say the project is undeployed.
    expect(screen.getByTestId("environment-flow-skeleton")).toBeInTheDocument();
    expect(screen.queryByText(/Nothing deployed yet/)).not.toBeInTheDocument();
  });

  it("says the environments could not be read, with a Retry, rather than an empty flow", () => {
    mockEnvironmentsState = "error";

    render(<DeploymentsPage projectName="acme" />);

    expect(
      screen.getByText(/The deployment pipeline could not be loaded: gateway down/),
    ).toBeInTheDocument();
    expect(screen.queryByText(/Nothing deployed yet/)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(mockEnvironmentsRefetch).toHaveBeenCalled();
  });

  // The state that shimmered forever: the read SUCCEEDED and named no
  // environment. `rows.length === 0` looks identical to a read still out, so
  // the flow drew its skeleton and never settled — "loading" and "empty" were
  // the same picture, and the reader could not tell which.
  it("says the platform has no environments once the read has settled empty", () => {
    mockEnvironmentsState = "empty";

    render(<DeploymentsPage projectName="acme" />);

    expect(
      screen.getByText(/This organization has no deployment environments yet/),
    ).toBeInTheDocument();
    // Settled, not waiting: no shimmer, and no error either.
    expect(screen.queryByTestId("environment-flow-skeleton")).not.toBeInTheDocument();
    expect(screen.queryByTestId("environment-flow")).not.toBeInTheDocument();
    expect(
      screen.queryByText(/The deployment pipeline could not be loaded/),
    ).not.toBeInTheDocument();
    // And it does not reach past what it knows: the project has components,
    // so "Nothing deployed yet" would be a different (and wrong) claim.
    expect(screen.queryByText(/Nothing deployed yet/)).not.toBeInTheDocument();
  });
});

describe("DeploymentsPage — the page is the pipeline", () => {
  // The status poll is where `version` comes from, and `promote` is null
  // without a version. Both an unsettled poll and a failed one leave the page
  // not knowing — and neither is "nothing is deployed here".
  it.each(["pending", "error"] as const)(
    "withholds the promote step rather than denying a deployment while the status poll is %s",
    (state) => {
      mockStatusState = state;

      render(<DeploymentsPage projectName="acme" />);

      expect(screen.getByTestId("promote-skeleton")).toBeInTheDocument();
      expect(
        screen.queryByText("Available once a version is deployed to Development."),
      ).not.toBeInTheDocument();
      // Step 2 reads the VERDICT off the same poll, so it withholds too. It
      // used to state a lifecycle position — "Starts automatically now that
      // the deployment is live." — over a project whose validation had
      // already passed or failed, while step 3 below it correctly shimmered.
      expect(screen.getByTestId("validation-skeleton")).toBeInTheDocument();
      expect(
        screen.queryByText("Starts automatically now that the deployment is live."),
      ).not.toBeInTheDocument();
    },
  );

  it("is the flow and nothing under it — the version ledger has left the page", () => {
    render(<DeploymentsPage projectName="expense" />);

    expect(screen.getByTestId("environment-flow")).toBeInTheDocument();
    expect(
      screen.queryByText("every version this project built, newest first"),
    ).not.toBeInTheDocument();
  });

  it("holds a skeleton while the environment list is out, rather than guessing two cards", () => {
    mockEnvironmentsState = "pending";

    render(<DeploymentsPage projectName="expense" />);

    expect(screen.getByTestId("environment-flow-skeleton")).toBeInTheDocument();
    // Not one card, not two: the page does not know how many there are, so it
    // names no environment at all.
    expect(screen.queryByText("Development")).not.toBeInTheDocument();
    expect(screen.queryByTestId("environment-flow")).not.toBeInTheDocument();
  });

  it("says the pipeline could not be read, with a retry, rather than an empty page", () => {
    mockEnvironmentsState = "error";

    render(<DeploymentsPage projectName="expense" />);

    expect(
      screen.getByText(/The deployment pipeline could not be loaded/),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(mockEnvironmentsRefetch).toHaveBeenCalled();
  });
});

// The version and its milestone LEAD the Deployment step (the design's `.vm`
// block). Everything in it is read off a settled read or it is not said: an
// unsettled status poll draws a skeleton, a settled poll that named no version
// says so in words, and the second line is omitted outright rather than
// printing a dash where a stamp or a sha should be.
describe("DeploymentsPage — the version block", () => {
  /** A merged coding cycle — the one record of the commit a version shipped. */
  function mergedRun(sha: string): MilestoneRunView {
    return {
      id: "run-merged",
      milestoneNumber: 3,
      milestoneTitle: "v1",
      kind: "dev",
      origin: "spec-build",
      state: "succeeded",
      budgets: { cyclesTotal: 1, cycleCeiling: 8, fixCycles: 0, fixCeiling: 3, conflictCycles: 0, conflictCeiling: 2 },
      cycles: [
        { id: "cycle-1", kind: "coding", attempts: 1, mergeSha: sha, createdAt: "2026-09-16T12:00:00Z" },
      ],
      createdAt: "2026-09-16T11:00:00Z",
    } as unknown as MilestoneRunView;
  }

  it("carries the build stamp and the commit, both linked to the project's repository", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "passed",
    };
    mockBuilds = [
      {
        tag: "v1",
        milestoneNumber: 3,
        status: "completed",
        startedAt: "2026-09-16T11:00:00Z",
        completedAt: "2026-09-16T12:10:00Z",
      },
    ];
    mockRuns = [mergedRun("4e8a0d6f9c1b2a3d4e5f60718293a4b5c6d7e8f9")];

    render(<DeploymentsPage projectName="acme" />);

    const vm = within(screen.getByTestId("environment-card-development")).getByTestId(
      "version-block",
    );
    expect(within(vm).getByText("Version v1")).toBeInTheDocument();
    expect(within(vm).getByRole("link", { name: "Milestone #3" })).toHaveAttribute(
      "href",
      "https://github.com/acme/demo/milestone/3",
    );
    // The stamp is locale-formatted, so the assertion is on the SHAPE of the
    // line — "Built <something> · commit <short sha>" — and on the link.
    expect(vm.textContent).toMatch(/Built .+ · commit 4e8a0d6/);
    expect(within(vm).getByRole("link", { name: "4e8a0d6" })).toHaveAttribute(
      "href",
      "https://github.com/acme/demo/commit/4e8a0d6f9c1b2a3d4e5f60718293a4b5c6d7e8f9",
    );
  });

  it("says Version unknown, and omits the second line, when the reads name neither", () => {
    // Something IS bound — so the block renders — but the poll settled without
    // a version, the ledger holds no build for it, and no cycle merged.
    mockDeploy = {
      version: "",
      status: "none",
      components: { total: 1, ready: 1 },
      validation: "none",
    };
    mockBuildVersion = "";

    render(<DeploymentsPage projectName="acme" />);

    const vm = within(screen.getByTestId("environment-card-development")).getByTestId(
      "version-block",
    );
    expect(within(vm).getByText("Version unknown")).toBeInTheDocument();
    // No dash, no empty stamp, no placeholder sha — the line is simply absent.
    expect(vm.textContent).not.toMatch(/Built/);
    expect(vm.textContent).not.toMatch(/commit/);
    expect(within(vm).queryByRole("link")).not.toBeInTheDocument();
  });

  it("omits the built line while the commit is still out rather than placeholding it", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "passed",
    };
    mockBuilds = [{ tag: "v1", milestoneNumber: 3, status: "completed", startedAt: "2026-09-16T11:00:00Z" }];
    mockRunsPending = true;

    render(<DeploymentsPage projectName="acme" />);

    const vm = within(screen.getByTestId("environment-card-development")).getByTestId(
      "version-block",
    );
    expect(within(vm).getByText("Version v1")).toBeInTheDocument();
    // The build has no completion stamp and the run story has not settled, so
    // there is nothing true to put on the second line.
    expect(vm.textContent).not.toMatch(/commit/);
  });

  it("skeletons the block while the status poll that names the version is unsettled", () => {
    mockStatusState = "pending";

    render(<DeploymentsPage projectName="acme" />);

    expect(screen.getByTestId("version-block-skeleton")).toBeInTheDocument();
    expect(screen.queryByText("Version unknown")).not.toBeInTheDocument();
    expect(screen.queryByTestId("version-block")).not.toBeInTheDocument();
  });
});

describe("DeploymentsPage — permission gate", () => {
  it("blocks the whole page for a user lacking ae:build-view", () => {
    hasBuild.current = false;
    render(<DeploymentsPage projectName="acme" />);

    expect(
      screen.getByText("You don't have access to this project's deployments"),
    ).toBeInTheDocument();
  });
});
