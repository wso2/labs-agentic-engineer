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

import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { components } from "../../../generated/aep-api";

type ComponentDependencies = components["schemas"]["ComponentDependencies"];
type ProjectDependencyReadiness =
  components["schemas"]["ProjectDependencyReadiness"];

const invalidateQueries = vi.fn();
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ invalidateQueries }),
}));

let mockDesign: ComponentDependencies[] = [];
let mockDesignPending = false;
let mockDesignError = false;
const refetchDesign = vi.fn();
vi.mock("../../spec/api/queries", () => ({
  useDesignDependencies: () => ({
    data: mockDesignError ? undefined : mockDesign,
    isPending: mockDesignPending,
    isError: mockDesignError,
    isSuccess: !mockDesignPending && !mockDesignError,
    error: mockDesignError ? new Error("the design read is down") : null,
    refetch: refetchDesign,
  }),
}));

let mockReadiness: ProjectDependencyReadiness | undefined;
let mockReadinessPending = false;
let mockReadinessError = false;
const refetchReadiness = vi.fn();
// The save the dialog drives. Its own module is mocked because the dialog is
// reused verbatim from the Deployments page — this test drives the real dialog.
const saveMutate = vi.fn();
vi.mock("../../projects/api/queries", () => ({
  useProjectDependencyReadiness: () => ({
    data: mockReadinessError ? undefined : mockReadiness,
    isPending: mockReadinessPending,
    isError: mockReadinessError,
    isSuccess: !mockReadinessPending && !mockReadinessError,
    error: mockReadinessError ? new Error("readiness is down") : null,
    refetch: refetchReadiness,
  }),
  useSaveConnectionValues: () => ({
    mutate: saveMutate,
    reset: vi.fn(),
    isPending: false,
    isError: false,
    error: null,
  }),
}));

import { ExternalResources } from "./ExternalResources";

function external(name: string, keys: string[], description?: string) {
  return {
    kind: "external",
    name,
    ...(description && { description }),
    config: keys.map((key) => ({ key })),
  };
}

function design(...deps: ReturnType<typeof external>[]): ComponentDependencies[] {
  return [{ componentName: "catalog-api", dependencies: deps }];
}

function readiness(
  ...deps: { name: string; state: string; missingKeys?: string[] }[]
): ProjectDependencyReadiness {
  return {
    configured: deps.every((d) => d.state === "configured"),
    dependencies: deps.map((d) => ({
      name: d.name,
      state: d.state as ProjectDependencyReadiness["dependencies"][number]["state"],
      missingKeys: d.missingKeys ?? [],
    })),
  };
}

afterEach(() => {
  mockDesign = [];
  mockDesignPending = false;
  mockDesignError = false;
  mockReadiness = undefined;
  mockReadinessPending = false;
  mockReadinessError = false;
  invalidateQueries.mockClear();
  refetchDesign.mockClear();
  refetchReadiness.mockClear();
  saveMutate.mockClear();
});

function renderSection() {
  render(<ExternalResources projectName="acme" />);
}

describe("ExternalResources", () => {
  it("summarises what is still wanted, and names each dependency", () => {
    mockDesign = design(
      external("stripe", ["api_key"], "Payments"),
      external("sendgrid", ["token"]),
    );
    mockReadiness = readiness(
      { name: "stripe", state: "configured" },
      { name: "sendgrid", state: "unset", missingKeys: ["token"] },
    );
    renderSection();

    expect(screen.getByText("External resources")).toBeInTheDocument();
    expect(screen.getByText("1 of 2 need configuration")).toBeInTheDocument();
    expect(screen.getByText("stripe")).toBeInTheDocument();
    expect(screen.getByText("Payments")).toBeInTheDocument();
    expect(screen.getByText("sendgrid")).toBeInTheDocument();
    expect(screen.getByText("1 setting outstanding")).toBeInTheDocument();
  });

  // THE regression this section exists to avoid: a dependency no build has
  // authored yet reads as `not-provisioned`, and nothing moves it off that
  // state on its own — no background process provisions an external. Reading it
  // as "provisioning, come back later" would hide the button forever.
  it("offers Configure for a dependency no build has authored yet", () => {
    mockDesign = design(external("stripe", ["api_key"]));
    mockReadiness = readiness({
      name: "stripe",
      state: "not-provisioned",
      missingKeys: ["api_key"],
    });
    renderSection();

    expect(screen.getByText("1 of 1 need configuration")).toBeInTheDocument();
    expect(screen.getByText("Needs configuration")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Configure now: stripe" }),
    ).toBeInTheDocument();
    expect(screen.queryByText(/Provisioning/)).not.toBeInTheDocument();
  });

  // WCAG 2.5.3 Label in Name: the accessible name must CONTAIN the visible text,
  // or a voice-control user reading "Configure now" off the screen says a name
  // the button does not answer to. The row name is appended, never substituted.
  it("keeps each button's accessible name a superset of what it reads", () => {
    mockDesign = design(external("stripe", ["api_key"]), external("dhl", ["key"]));
    mockReadiness = readiness(
      { name: "stripe", state: "unset", missingKeys: ["api_key"] },
      { name: "dhl", state: "configured" },
    );
    renderSection();

    const outstanding = screen.getByRole("button", { name: /stripe/ });
    expect(outstanding).toHaveTextContent("Configure now");
    expect(outstanding.getAttribute("aria-label")).toContain("Configure now");

    const configured = screen.getByRole("button", { name: /dhl/ });
    expect(configured).toHaveTextContent("Edit configuration");
    expect(configured.getAttribute("aria-label")).toContain("Edit configuration");
  });

  it("offers Edit configuration once a dependency is configured", () => {
    mockDesign = design(external("stripe", ["api_key"]));
    mockReadiness = readiness({ name: "stripe", state: "configured" });
    renderSection();

    expect(screen.getByText("1 of 1 configured")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Edit configuration for stripe" }),
    ).toBeInTheDocument();
  });

  it("collects values through the shared dialog and refreshes readiness", () => {
    mockDesign = design(external("stripe", ["api_key"]));
    mockReadiness = readiness({
      name: "stripe",
      state: "unset",
      missingKeys: ["api_key"],
    });
    renderSection();

    fireEvent.click(screen.getByRole("button", { name: "Configure now: stripe" }));
    // The dialog opens on the dependency's own schema.
    expect(screen.getByText("Configure — stripe")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("api_key"), {
      target: { value: "sk_live_1" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save values" }));

    expect(saveMutate).toHaveBeenCalledWith(
      {
        name: "stripe",
        environment: "development",
        values: { api_key: "sk_live_1" },
      },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );

    // The page owns the confirmation and the cache invalidation, which only
    // happen once the save actually succeeded.
    const { onSuccess } = saveMutate.mock.calls[0]![1] as {
      onSuccess: () => void;
    };
    act(() => onSuccess());
    expect(
      screen.getByText(/Configuration saved — the deployment no longer waits/),
    ).toBeInTheDocument();
    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: [
        "projects",
        "detail",
        "acme",
        "dependency-readiness",
        "development",
      ],
    });
  });

  // A platform resource's credentials are the platform's own — the run's
  // provisioning gates report it, and there is nothing here to type in.
  it("says nothing at all when the design has no external dependencies", () => {
    mockDesign = [
      {
        componentName: "catalog-api",
        dependencies: [
          {
            kind: "platform-resource",
            name: "orders-db",
            resourceType: "postgres-cnpg",
            config: [{ key: "url" }],
          },
        ],
      },
    ];
    mockReadiness = readiness();
    renderSection();

    expect(screen.queryByText("External resources")).not.toBeInTheDocument();
  });

  it("waits for both reads rather than flashing an empty section", () => {
    mockDesignPending = true;
    mockReadinessPending = true;
    renderSection();

    expect(screen.getByText("External resources")).toBeInTheDocument();
    expect(
      screen.getByLabelText("Loading the external resources"),
    ).toBeInTheDocument();
  });

  // A failed readiness read must never render as "nothing to configure": the
  // deploy still waits on values this page would then be silent about.
  it("reports a failed readiness read, with a retry", () => {
    mockDesign = design(external("stripe", ["api_key"]));
    mockReadinessError = true;
    renderSection();

    expect(
      screen.getByText(/Failed to load which external resources still need configuration/),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(refetchReadiness).toHaveBeenCalled();
  });

  // FINDING 7 — the header chip and the summary used to render ABOVE the error
  // branch off rows defaulted from the design alone, so a failed read printed
  // "3 of 3 need configuration" and "the version is not deployed until every one of
  // them has its development values" directly on top of "Failed to load which
  // external resources still need configuration". A fabricated certainty, stated more
  // confidently than the truth it sat on.
  it("claims to know nothing about what is outstanding when readiness fails", () => {
    mockDesign = design(
      external("stripe", ["api_key"]),
      external("sendgrid", ["token"]),
      external("twilio", ["sid"]),
    );
    mockReadinessError = true;
    renderSection();

    expect(screen.queryByText(/need configuration$/)).not.toBeInTheDocument();
    expect(screen.queryByText(/of 3/)).not.toBeInTheDocument();
    expect(
      screen.queryByText(/The version is not deployed until every one of them/),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("Needs configuration")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Configure/ }),
    ).not.toBeInTheDocument();
    // What the DESIGN read alone genuinely supports, and no more.
    expect(
      screen.getByText(/This project declares 3 external dependencies/),
    ).toBeInTheDocument();
  });

  // Stale rows from react-query's last good readiness payload are no better a
  // basis for a summary than fabricated ones: the error says the state is
  // unknown, so the section must not caption it.
  it("drops a stale readiness summary once the read has failed", () => {
    mockDesign = design(external("stripe", ["api_key"]));
    mockReadiness = readiness({
      name: "stripe",
      state: "unset",
      missingKeys: ["api_key"],
    });
    mockReadinessError = true;
    renderSection();

    expect(screen.queryByText("1 of 1 need configuration")).not.toBeInTheDocument();
    expect(
      screen.getByText(/Failed to load which external resources still need configuration/),
    ).toBeInTheDocument();
  });

  // FINDING 6 — a Registered External holds its values on the ORG catalog
  // record: the project-scoped save 409s on it and the deploy gate excludes it,
  // so readiness omits it. The section must omit it too, or it offers a button
  // that fails under a headline contradicting an unblocked gate.
  it("renders only the externals the readiness read names", () => {
    mockDesign = design(
      external("stripe", ["api_key"]),
      external("acme-registered", ["ACME_TOKEN"], "Registered on the org"),
    );
    mockReadiness = readiness({
      name: "stripe",
      state: "unset",
      missingKeys: ["api_key"],
    });
    renderSection();

    expect(screen.getByText("1 of 1 need configuration")).toBeInTheDocument();
    expect(screen.getByText("stripe")).toBeInTheDocument();
    expect(screen.queryByText("acme-registered")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /acme-registered/ }),
    ).not.toBeInTheDocument();
  });

  // ...and when EVERY external is one the project cannot supply, there is
  // genuinely nothing to ask for, so the section says nothing at all.
  it("says nothing when readiness names no external this project can supply", () => {
    mockDesign = design(external("acme-registered", ["ACME_TOKEN"]));
    mockReadiness = readiness();
    renderSection();

    expect(screen.queryByText("External resources")).not.toBeInTheDocument();
  });

  // FINDING 8 — the design read used to swallow its errors and resolve to [],
  // so a transient failure produced no rows, no pending flag and no error, and
  // the empty guard removed the whole section. A run parked in `waiting` tells
  // the user to "add them in the External resources section on this page"; that
  // section has to be there to be wrong about.
  it("stays on the page, and says why, when the design read fails", () => {
    mockDesignError = true;
    mockReadiness = readiness({
      name: "stripe",
      state: "unset",
      missingKeys: ["api_key"],
    });
    renderSection();

    expect(screen.getByText("External resources")).toBeInTheDocument();
    expect(
      screen.getByText(/Failed to load this project's external dependencies/),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(refetchDesign).toHaveBeenCalled();
  });

  // Both reads down: nothing is known, and the section says exactly that once —
  // not a design count it cannot support, and not two competing error cards.
  it("reports a single failure when both reads are down", () => {
    mockDesignError = true;
    mockReadinessError = true;
    renderSection();

    expect(
      screen.getByText(/Failed to load this project's external resources/),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(/This project declares/),
    ).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(refetchDesign).toHaveBeenCalled();
    expect(refetchReadiness).toHaveBeenCalled();
  });

  // The exception to finding 8: the design read SUCCEEDED and declares no
  // external at all. Readiness enumerates from that same design, so nothing is
  // at stake and an error card would be pure noise.
  it("stays silent when readiness fails for a project with no externals", () => {
    mockDesign = [];
    mockReadinessError = true;
    renderSection();

    expect(screen.queryByText("External resources")).not.toBeInTheDocument();
  });
});
