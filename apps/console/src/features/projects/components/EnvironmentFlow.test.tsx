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

import type { ElementType, ReactNode } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { components } from "../../../generated/aep-api";

// Router replaced so every RouterLink renders as a plain anchor whose href is
// the resolved route path — no RouterProvider needed (mirrors
// DeploymentsPage.test.tsx).
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
  Link: ({
    to,
    params,
    children,
    ...rest
  }: {
    to: string;
    params?: Record<string, unknown>;
    children?: ReactNode;
  } & Record<string, unknown>) => {
    let href = to;
    for (const [key, value] of Object.entries(params ?? {})) {
      href = href.replace(`$${key}`, String(value));
    }
    return (
      <a href={href} {...rest}>
        {children}
      </a>
    );
  },
}));

import { environmentRows } from "../lib/deploymentLedger";
import { promoteStep } from "../lib/deploymentFlow";
import type { EnvironmentInfo } from "../lib/environments";
import type { DeploymentBoard } from "../lib/deploymentRows";
import { EnvironmentFlow } from "./EnvironmentFlow";

type DeployStage = components["schemas"]["DeployStage"];

// A three-environment pipeline: the middle one does NOT validate, and only
// the last has no promotion target. Nothing here is a console constant — the
// flow reads every one of these facts off the list.
const threeEnvs: EnvironmentInfo[] = [
  {
    name: "development",
    displayName: "Development",
    isProduction: false,
    validation: "on",
    position: 0,
    promotesTo: "staging",
  },
  {
    name: "staging",
    displayName: "Staging",
    isProduction: false,
    validation: "off",
    position: 1,
    promotesTo: "production",
  },
  {
    name: "production",
    displayName: "Production",
    isProduction: true,
    validation: "off",
    position: 2,
  },
];

/** One component, serving in the pipeline's entry environment. */
function board(): DeploymentBoard {
  return new Map([
    [
      "development",
      [
        {
          componentName: "api",
          displayName: "API",
          kind: "success" as const,
          deployment: {
            componentName: "api",
            environment: "development",
            status: "Ready",
            createdAt: "2026-09-16T09:00:00Z",
          },
        },
      ],
    ],
  ]);
}

function props(
  environments: EnvironmentInfo[],
  overrides: { validation?: DeployStage["validation"] } = {},
) {
  const deploy: DeployStage = {
    components: { ready: 1, total: 1 },
    status: "deployed",
    validation: overrides.validation ?? "passed",
    version: "v4",
  };
  const rows = environmentRows(board(), environments, deploy);
  const target = rows[1];
  return {
    projectName: "expense",
    environments,
    rows,
    deploy,
    version: "v4",
    validation: { verdict: deploy.validation, repairing: false },
    hold: null,
    componentTypes: new Map([["api", "service"]]),
    connections: [],
    promote: target ? promoteStep(deploy, target, [], {}, null, "v4") : null,
    pending: { deploy: false, connections: false, validation: false, hold: false },
    onPromote: vi.fn(),
    onTryOut: vi.fn(),
    onConfigureConnection: vi.fn(),
    onConfigurePromoteTarget: vi.fn(),
  };
}

describe("EnvironmentFlow", () => {
  it("draws one card per environment, in the platform's order", () => {
    render(<EnvironmentFlow {...props(threeEnvs)} />);
    const names = screen.getAllByTestId("environment-card-name").map((n) => n.textContent);
    expect(names).toEqual(["Development", "Staging", "Production"]);
  });

  it("numbers the steps per environment, dropping validation where it is off", () => {
    render(<EnvironmentFlow {...props(threeEnvs)} />);
    const staging = within(screen.getByTestId("environment-card-staging"));
    expect(staging.queryByText("Validation")).not.toBeInTheDocument();
    expect(staging.getByLabelText("Step 2, Promote to Production")).toBeInTheDocument();
  });

  // The numbers are a CONSEQUENCE of stepsFor, not a constant: Staging skips
  // validation, so its promote is step 2 where Development's is step 3.
  it("numbers each card's steps off its own environment, not off a fixed three", () => {
    render(<EnvironmentFlow {...props(threeEnvs)} />);
    const development = within(screen.getByTestId("environment-card-development"));
    expect(development.getByLabelText(/^Step 2, Validation/)).toBeInTheDocument();
    expect(development.getByLabelText("Step 3, Promote to Staging")).toBeInTheDocument();
    const staging = within(screen.getByTestId("environment-card-staging"));
    expect(staging.getByLabelText("Step 2, Promote to Production")).toBeInTheDocument();
    expect(staging.queryByLabelText(/^Step 3/)).not.toBeInTheDocument();
  });

  // The version comes off the status poll, a read the flow does not make and
  // must not reason from the absence of: a null promote while it is out is
  // "not known yet", never "nothing is deployed here".
  it("withholds the promote step while the read that names the version is out", () => {
    const base = props(threeEnvs);
    render(
      <EnvironmentFlow
        {...base}
        version=""
        promote={null}
        pending={{ ...base.pending, deploy: true }}
      />,
    );
    const development = within(screen.getByTestId("environment-card-development"));
    expect(development.getByTestId("promote-skeleton")).toBeInTheDocument();
    expect(
      development.queryByText(/Available once a version is deployed/),
    ).not.toBeInTheDocument();
  });

  // A populated target is not a running one — a Staging whose bindings all
  // failed must not be reported as running a version of its own.
  it("does not call a failed target a running one", () => {
    const base = props(threeEnvs);
    const failing = new Map(board());
    failing.set("staging", [
      {
        componentName: "api",
        displayName: "API",
        kind: "error" as const,
        deployment: {
          componentName: "api",
          environment: "staging",
          status: "DeploymentFailed",
          createdAt: "2026-09-16T10:00:00Z",
        },
      },
    ]);
    render(
      <EnvironmentFlow
        {...base}
        rows={environmentRows(failing, threeEnvs, base.deploy)}
        promote={null}
      />,
    );
    const development = within(screen.getByTestId("environment-card-development"));
    expect(
      development.queryByText("Staging runs a version of its own."),
    ).not.toBeInTheDocument();
    expect(
      development.getByText("Staging has a deployment of its own — Deploy failed."),
    ).toBeInTheDocument();
  });

  it("gives the last environment no promote step", () => {
    render(<EnvironmentFlow {...props(threeEnvs)} />);
    const production = within(screen.getByTestId("environment-card-production"));
    expect(production.queryByText(/^Promote to/)).not.toBeInTheDocument();
    expect(
      production.getByText("Last environment in the pipeline — nothing to promote to."),
    ).toBeInTheDocument();
  });

  it("names the promote target and the version on the button", () => {
    render(<EnvironmentFlow {...props(threeEnvs)} />);
    expect(screen.getByRole("button", { name: "Promote v4 to Staging" })).toBeInTheDocument();
  });

  it("makes Promote primary and Try it out secondary once validation has passed", () => {
    render(<EnvironmentFlow {...props(threeEnvs, { validation: "passed" })} />);
    expect(screen.getByRole("button", { name: /Promote v4 to Staging/ })).toHaveClass(
      "MuiButton-contained",
    );
    expect(screen.getByRole("link", { name: /Try it out/ })).toHaveClass("MuiButton-outlined");
  });

  // The brief wrote this case as `cancelled`, which `canPromote` deliberately
  // treats as PROMOTABLE — a person stopped the judging, and that is theirs to
  // override. `none` is the contract's word for a verdict that is expected and
  // has not arrived: the unknown verdict the case is actually about.
  it("keeps Try it out primary and Promote unavailable while the verdict is unknown", () => {
    render(<EnvironmentFlow {...props(threeEnvs, { validation: "none" })} />);
    expect(screen.getByRole("link", { name: /Try it out/ })).toHaveClass("MuiButton-contained");
    expect(screen.getByRole("button", { name: /Promote v4 to Staging/ })).toBeDisabled();
  });

  it("opens the environment page from the card, and not from a button inside it", () => {
    const onTryOut = vi.fn();
    render(<EnvironmentFlow {...props(threeEnvs)} onTryOut={onTryOut} />);
    fireEvent.click(screen.getByTestId("environment-card-staging"));
    expect(onTryOut).toHaveBeenCalledWith("staging");
    onTryOut.mockClear();
    fireEvent.click(screen.getByRole("button", { name: /Promote v4 to Staging/ }));
    expect(onTryOut).not.toHaveBeenCalled();
  });

  // Every Try-it-now link names the card's OWN environment. A pipeline whose
  // entry environment is not called "development" must not send the reader to
  // a dead-end page.
  it("links Try it out at the card's own environment, whatever it is called", () => {
    const renamed: EnvironmentInfo[] = [
      { ...threeEnvs[0]!, name: "qa", displayName: "QA" },
      ...threeEnvs.slice(1),
    ];
    const base = props(renamed);
    render(
      <EnvironmentFlow
        {...base}
        rows={environmentRows(new Map([["qa", board().get("development")!]]), renamed, base.deploy)}
      />,
    );
    expect(screen.getByRole("link", { name: /Try it out/ })).toHaveAttribute(
      "href",
      "/projects/expense/deployments/qa",
    );
  });

  // Every element of the approved design's EMPTY card (the UAT/Production
  // ones): a noun for a title, the chip that says what is there, the sentence
  // that names what would fill it, a validation step that says it has not run,
  // and a promote control drawn disabled rather than left out.
  it("draws an empty environment the way the design does", () => {
    render(<EnvironmentFlow {...props(threeEnvs)} />);
    const staging = within(screen.getByTestId("environment-card-staging"));
    expect(staging.getByText("Deployment")).toBeInTheDocument();
    expect(staging.getByText("Nothing deployed")).toBeInTheDocument();
    expect(
      staging.getByText("Nothing running yet. v4 on Development is ready to promote here."),
    ).toBeInTheDocument();
    expect(staging.getByRole("button", { name: /^Promote to Production/ })).toBeDisabled();

    // Production sits two hops from the only deployed version, so its own
    // upstream (Staging) runs nothing and the sentence says what it takes.
    const production = within(screen.getByTestId("environment-card-production"));
    expect(
      production.getByText("Nothing running yet. Only a version that reached Staging can be promoted here."),
    ).toBeInTheDocument();
  });

  // Development validates, so its empty-state counterpart is the chip and the
  // sentence on step 2 — never a blank step.
  it("says a validation has not run rather than leaving the step bare", () => {
    const validating: EnvironmentInfo[] = [
      threeEnvs[0]!,
      { ...threeEnvs[1]!, validation: "on" },
      threeEnvs[2]!,
    ];
    render(<EnvironmentFlow {...props(validating)} />);
    const staging = within(screen.getByTestId("environment-card-staging"));
    expect(staging.getByLabelText("Step 2, Validation, Not run")).toBeInTheDocument();
    expect(staging.getByText("Runs once something is deployed here.")).toBeInTheDocument();
  });

  // The card's header is the name and the word Environment — everything else
  // the design puts inside the steps, where the reader is already looking.
  it("keeps the header to the name and the word Environment", () => {
    render(<EnvironmentFlow {...props(threeEnvs)} />);
    const development = within(screen.getByTestId("environment-card-development"));
    expect(development.getByRole("heading", { name: "Development" })).toBeInTheDocument();
    expect(development.getByText("Environment")).toBeInTheDocument();
    // The deployment's stamp belongs to the Deployed step's chip; the header
    // carried a second copy of it as "2h ago".
    expect(development.queryByText(/ago$/)).not.toBeInTheDocument();
  });

  // A single-environment pipeline is a real pipeline: one card, no arrow, no
  // second card with an empty title.
  it("draws one card for a single-environment pipeline", () => {
    const only: EnvironmentInfo[] = [
      { name: "qa", displayName: "QA", isProduction: true, validation: "off", position: 0 },
    ];
    render(<EnvironmentFlow {...props(only)} />);
    expect(screen.getAllByTestId("environment-card-name").map((n) => n.textContent)).toEqual(["QA"]);
    expect(screen.queryByRole("button", { name: /Promote/ })).not.toBeInTheDocument();
  });
});
