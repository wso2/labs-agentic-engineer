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
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
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

// The version ledger, for the Milestone cell — the Builds surfaces' own read.
let mockBuilds: components["schemas"]["BuildSummary"][] = [];
vi.mock("../../builds/api/queries", () => ({
  useBuilds: () => ({ data: mockBuilds, isPending: false, isError: false }),
}));

import { DeploymentsPage } from "./DeploymentsPage";

type ProjectStatus = components["schemas"]["ProjectStatus"];
type DeployStage = components["schemas"]["DeployStage"];
type ComponentDependencies = components["schemas"]["ComponentDependencies"];
type ExternalResourceDTO = components["schemas"]["ExternalResourceDTO"];
type ProjectTestUserState = components["schemas"]["ProjectTestUserState"];

const MOCK_PASSWORD = "mocknotreal";
const THUNDER_CONSOLE_USERS = "http://localhost:8097/console/users";

// Roles hooks — overridable so green-deploy fixtures stay Thunder-only by
// default, and Test-users cases can inject owned rows / reveal answers.
let mockTestUsers: ProjectTestUserState[] = [];
const mockReveal = vi.fn(
  async (username: string) => ({
    username,
    password: MOCK_PASSWORD,
    rotatedAt: null,
  }),
);

vi.mock("../../spec/api/roles", () => ({
  useProjectRoles: () => ({
    data: {
      directoryAvailable: true,
      roles: [],
      testUsers: mockTestUsers,
    },
    isPending: false,
    isError: false,
  }),
  useRevealTestUserPassword: () => ({
    mutateAsync: mockReveal,
    isPending: false,
  }),
}));

// Query hooks replaced wholesale — no QueryClientProvider / MSW needed, only the
// rendering under test is real (mirrors TasksList.test.tsx).
let mockDeploy: DeployStage = {
  version: "v1",
  status: "deployed",
  components: { total: 1, ready: 1 },
  validation: "none",
};

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
    designStatus: "approved",
    spec: { exists: true, version: "v1", dirty: false, design: true, agent: "" },
    build: { version: "v1", status: "succeeded" },
    deploy: mockDeploy,
  };
}

// The connection-values dialog's mutation is mocked at module level so opening
// it needs no QueryClientProvider; mutate is captured for the save assertion.
const mockMutate = vi.fn();

vi.mock("../api/queries", () => ({
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
    deployments: [
      {
        componentName: "storefront",
        environment: "development",
        status: "Ready",
        endpointUrl: "https://storefront.dev.example.com",
      },
    ],
    failedCount: 0,
  }),
  useProjectStatus: () => ({ data: status() }),
}));

vi.mock("../../spec/api/queries", () => ({
  useDesignDependencies: () => ({ data: mockDependencies, isPending: false }),
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

vi.mock("../../validation/api/counts", () => ({
  useValidationEvidence: () => ({
    verdict: mockVerdict,
    repairing: mockRepairing,
    ...(mockCounts ? { counts: mockCounts } : {}),
  }),
}));

beforeEach(() => {
  mockCounts = undefined;
  mockVerdict = "";
  mockRepairing = false;
  mockMutate.mockClear();
  mockDependencies = DEFAULT_DEPENDENCIES;
  mockExternalCatalog = [];
  mockExternalCatalogPending = false;
  mockExternalCatalogError = false;
  mockTestUsers = [];
  mockReveal.mockClear();
  mockBuilds = [];
  navigate.mockClear();
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
    // The ledger's cell names the lifecycle, not a verdict.
    expect(screen.getByText("awaiting fix")).toBeInTheDocument();
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
  // this say "Nothing reported yet", as if the version had never been judged.
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
    expect(screen.queryByText(/Nothing reported yet/)).not.toBeInTheDocument();
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
      screen.getByText("Nothing reported yet — the validation attempt is still running."),
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
    // The dev card's fact line and the aggregate's word on the rollout — which
    // the ledger row repeats, so the chip appears twice.
    expect(screen.getByText(/1 of 1 components live/)).toBeInTheDocument();
    // Two chips — the card's and the row's — beside the ledger's column header.
    expect(screen.getByRole("columnheader", { name: "Deployed" })).toBeInTheDocument();
    expect(
      screen.getAllByText("Deployed").filter((el) => el.closest("th") === null),
    ).toHaveLength(2);
    // Production is empty, gated, and counts the live configuration it needs.
    expect(
      screen.getByText("Only a version whose validation has passed can be promoted here."),
    ).toBeInTheDocument();
    expect(screen.getByText("0 of 1 live configuration values set")).toBeInTheDocument();
    // The ledger: one row, development, with the milestone read off the
    // version ledger and a validation cell.
    const row = screen.getByRole("row", { name: "Open Development deployment" });
    expect(within(row).getByText("v1")).toBeInTheDocument();
    expect(within(row).getByText("Milestone #3")).toBeInTheDocument();
    expect(within(row).getByText("validated")).toBeInTheDocument();
    // Nothing runs in production, so it has no ledger row.
    expect(
      screen.queryByRole("row", { name: "Open Production deployment" }),
    ).not.toBeInTheDocument();
  });

  it("opens the environment's page from its ledger row", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "passed",
    };

    render(<DeploymentsPage projectName="acme" />);

    fireEvent.click(screen.getByRole("row", { name: "Open Development deployment" }));
    expect(navigate).toHaveBeenCalledWith({
      to: "/projects/$projectName/deployments/$environment",
      params: { projectName: "acme", environment: "development" },
    });
  });

  it("upgrades the validation cell and banner with criteria counts", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "passed",
    };
    mockCounts = { passed: 12, failed: 0, uncovered: 0, total: 12 };

    render(<DeploymentsPage projectName="acme" />);

    expect(screen.getByText("12 / 12 passed")).toBeInTheDocument();
    // The tile's own sentence, word for word — the banner used to write its own,
    // which is how a settled FAILURE came to lead with the count that passed.
    expect(
      screen.getByText("All 12 criteria were covered by a test and passed."),
    ).toBeInTheDocument();
  });

  it("tints the ledger row and says Deploying while the rollout converges", () => {
    mockDeploy = {
      version: "v2",
      status: "deploying",
      components: { total: 1, ready: 0 },
      validation: "none",
    };

    render(<DeploymentsPage projectName="acme" />);

    expect(screen.getAllByText("Deploying")).toHaveLength(2);
    const row = screen.getByRole("row", { name: "Open Development deployment" });
    // A verdict is expected and has not arrived: the cell says so, and no
    // promotion is offered.
    expect(within(row).getByText("Not run")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Promote v2 to production/ })).toBeDisabled();
  });
});

describe("DeploymentsPage — connections", () => {
  it("re-collects an external connection's values from the side panel", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "passed",
    };

    render(<DeploymentsPage projectName="acme" />);

    // Exactly ONE Configure on screen — the side panel's connection action,
    // named per connection for screen readers; the rail rows stay uniform
    // with no per-row extras.
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

    expect(screen.getByText("shop-db (postgres-cnpg)")).toBeInTheDocument();
    expect(screen.getByText("provisioned")).toBeInTheDocument();
    expect(screen.getByText("platform-managed")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Configure/ }),
    ).not.toBeInTheDocument();
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
      screen.getByRole("button", { name: /Promote v1 to production/ }),
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

    render(<DeploymentsPage projectName="acme" />);

    expect(
      screen.getByRole("button", { name: /Promote v1 to production/ }),
    ).toBeEnabled();
  });

  it("opens the promote dialog and gates Promote on required values", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "passed",
    };

    render(<DeploymentsPage projectName="acme" />);

    fireEvent.click(
      screen.getByRole("button", { name: /Promote v1 to production/ }),
    );

    const dialog = screen.getByRole("dialog");
    expect(
      within(dialog).getByText(/1 connection needs production values/),
    ).toBeInTheDocument();
    const promote = within(dialog).getByRole("button", { name: /^Promote$/ });
    expect(promote).toBeDisabled();

    fireEvent.change(within(dialog).getByLabelText(/STRIPE_SECRET_KEY/), {
      target: { value: "sk_live_x" },
    });
    expect(promote).toBeEnabled();
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
      screen.getByRole("button", { name: /Promote v1 to production/ }),
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

    // The production card's readiness line counts the provisioned one as set.
    expect(screen.getByText("2 of 2 live configuration values set")).toBeInTheDocument();

    fireEvent.click(
      screen.getByRole("button", { name: /Promote v1 to production/ }),
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

describe("DeploymentsPage — Test users", () => {
  it("hides SignInPanel when deploy is not green", () => {
    mockDeploy = {
      version: "v1",
      status: "deploying",
      components: { total: 1, ready: 1 },
      validation: "none",
    };
    mockTestUsers = [
      {
        username: "test-viewer",
        roleName: "Viewer",
        coldStart: true,
        exists: true,
        owned: true,
        supplied: false,
      },
    ];

    render(<DeploymentsPage projectName="acme" />);

    expect(
      screen.queryByText("Test users for agents on this environment"),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("Thunder Console")).not.toBeInTheDocument();
    expect(screen.queryByText("test-viewer")).not.toBeInTheDocument();
  });

  it("shows Thunder Console only when deploy is green and store is empty", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "none",
    };
    mockTestUsers = [];

    render(<DeploymentsPage projectName="acme" />);

    const link = screen.getByRole("link", {
      name: "Open Thunder Console to add or remove real accounts",
    });
    expect(link).toHaveAttribute("href", THUNDER_CONSOLE_USERS);
    expect(link).toHaveAttribute("target", "_blank");
    expect(
      screen.queryByText("Test users for agents on this environment"),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Reveal/i }),
    ).not.toBeInTheDocument();
  });

  it("shows Thunder only when gate has no owned published user", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "none",
    };
    mockTestUsers = [
      {
        username: "test-viewer",
        roleName: "Viewer",
        coldStart: true,
        exists: false,
        owned: false,
        supplied: false,
      },
    ];

    render(<DeploymentsPage projectName="acme" />);

    expect(
      screen.getByRole("link", {
        name: "Open Thunder Console to add or remove real accounts",
      }),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("Test users for agents on this environment"),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("test-viewer")).not.toBeInTheDocument();
  });

  it("lists owned published users and reveals password when deploy is green", async () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "none",
    };
    mockTestUsers = [
      {
        username: "test-viewer",
        roleName: "Viewer",
        coldStart: true,
        exists: true,
        owned: true,
        supplied: false,
      },
    ];

    render(<DeploymentsPage projectName="acme" />);

    expect(
      screen.getByText("Test users for agents on this environment"),
    ).toBeInTheDocument();
    // The card carries the count; the accounts live in the dialog behind it,
    // so a many-role app cannot grow this card past the ledger beside it.
    expect(screen.getByText("1 account, one per role")).toBeInTheDocument();
    expect(screen.queryByText("test-viewer")).not.toBeInTheDocument();
    const thunder = screen.getByRole("link", {
      name: "Open Thunder Console to add or remove real accounts",
    });
    expect(thunder).toHaveAttribute("href", THUNDER_CONSOLE_USERS);
    expect(thunder).toHaveAttribute("target", "_blank");

    fireEvent.click(screen.getByRole("button", { name: "View test users" }));
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByText("test-viewer")).toBeInTheDocument();

    fireEvent.click(
      within(dialog).getByRole("button", {
        name: "Reveal the password for test-viewer",
      }),
    );
    expect(mockReveal).toHaveBeenCalledWith("test-viewer");
    await waitFor(() => {
      expect(within(dialog).getByText(MOCK_PASSWORD)).toBeInTheDocument();
    });
    fireEvent.click(
      within(dialog).getByRole("button", {
        name: "Hide the password for test-viewer",
      }),
    );
    expect(screen.queryByText(MOCK_PASSWORD)).not.toBeInTheDocument();
    // Masked, not gone — the row holds its place in the table.
    expect(within(dialog).getByText("**********")).toBeInTheDocument();
  });

  it("keeps Thunder / Test users copy and omits Roles-gate and account actions", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "none",
    };
    mockTestUsers = [
      {
        username: "test-viewer",
        roleName: "Viewer",
        coldStart: true,
        exists: true,
        owned: true,
        supplied: false,
      },
    ];

    render(<DeploymentsPage projectName="acme" />);

    expect(screen.getByText(/user accounts/)).toBeInTheDocument();
    expect(screen.getByText(/Test users/)).toBeInTheDocument();
    expect(screen.queryByText(/Roles gate/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/^Add$/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/Rotate/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/Delete/i)).not.toBeInTheDocument();
  });
});
