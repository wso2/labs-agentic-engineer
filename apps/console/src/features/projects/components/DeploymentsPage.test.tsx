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
}));

import { DeploymentsPage } from "./DeploymentsPage";

type ProjectStatus = components["schemas"]["ProjectStatus"];
type DeployStage = components["schemas"]["DeployStage"];
type ComponentDependencies = components["schemas"]["ComponentDependencies"];

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
    spec: { exists: true, version: "v1", dirty: false, design: true },
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
    // The note answers the "when?" the sentence leaves out, and must not claim the
    // system was already checked.
    // The note is a WAIT and nothing more. It renders BEFORE the banner, so it names
    // the implementation rather than saying "the fix" — which has no antecedent yet —
    // and leaves "runs again" to the banner, whose sentence the Validation page's
    // tile shares and must keep.
    expect(screen.getByText("Waits for the implementation fix to deploy.")).toBeInTheDocument();
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
    const link = screen.getByRole("link", { name: /View full report/ });
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
    expect(screen.queryByText(/View full report/)).not.toBeInTheDocument();
  });
});

describe("DeploymentsPage — story rail", () => {
  it("tells the three-stage story with the dev version and rollout facts", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "passed",
    };

    render(<DeploymentsPage projectName="acme" />);

    // The card header is the deploy-lifecycle chip itself — no heading.
    expect(screen.getByText("Deployed")).toBeInTheDocument();
    // The place stages name themselves as environments (#401 feedback);
    // Validation is the check between them, not a place.
    expect(screen.getByText("Development environment")).toBeInTheDocument();
    expect(screen.getByText("Validation")).toBeInTheDocument();
    expect(screen.getByText("Production environment")).toBeInTheDocument();
    // The side panel's scoped section label keeps the short name.
    expect(screen.getByText("Production")).toBeInTheDocument();
    expect(screen.getByText("1 of 1 components ready")).toBeInTheDocument();
    // The side panel's at-a-glance facts.
    expect(screen.getByText("1 / 1 ready")).toBeInTheDocument();
    expect(screen.getByText("Nothing deployed yet")).toBeInTheDocument();
  });

  it("upgrades the validation fact and banner with criteria counts", () => {
    mockDeploy = {
      version: "v1",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "passed",
    };
    mockCounts = { passed: 12, failed: 0, uncovered: 0, total: 12 };

    render(<DeploymentsPage projectName="acme" />);

    expect(screen.getByText("12/12 passed")).toBeInTheDocument();
    // The tile's own sentence, word for word — the banner used to write its own,
    // which is how a settled FAILURE came to lead with the count that passed.
    expect(
      screen.getByText("All 12 criteria were covered by a test and passed."),
    ).toBeInTheDocument();
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
});

describe("DeploymentsPage — promotion", () => {
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

    expect(screen.getByText("2 / 2 set")).toBeInTheDocument();

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
