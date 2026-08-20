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

import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "../../../generated/aep-api";

type ComponentDependencies = components["schemas"]["ComponentDependencies"];
type ProjectDependencyReadiness =
  components["schemas"]["ProjectDependencyReadiness"];

const saveMutate = vi.fn();
const saveReset = vi.fn();
let saveState = mutationState();
let dependencies: ComponentDependencies[] = [];
let readiness: ProjectDependencyReadiness = { configured: false, dependencies: [] };
let designPending = false;
let designError = false;
let readinessError = false;

function mutationState(
  overrides: Partial<{ isPending: boolean; isError: boolean; error: Error | null }> = {},
) {
  return {
    mutate: saveMutate,
    reset: saveReset,
    isPending: false,
    isError: false,
    error: null,
    ...overrides,
  };
}

vi.mock("../../spec/api/queries", () => ({
  useDesignDependencies: () => ({
    data: designError ? undefined : dependencies,
    isPending: designPending,
    isError: designError,
    error: designError ? new Error("design read failed") : null,
    refetch: vi.fn(),
  }),
}));

vi.mock("../../projects/api/queries", () => ({
  useProjectDependencyReadiness: () => ({
    data: readinessError ? undefined : readiness,
    isPending: false,
    isError: readinessError,
    error: readinessError ? new Error("readiness read failed") : null,
    refetch: vi.fn(),
  }),
  useSaveConnectionValues: () => saveState,
}));

import { ExternalResources } from "./ExternalResources";

function externalDependencies(...names: string[]): ComponentDependencies[] {
  return names.map((name, index) => ({
    componentName: `component-${index + 1}`,
    dependencies: [
      {
        kind: "external",
        name,
        description: `${name} account`,
        config: [
          { key: "REGION", secret: false, defaultValue: "us-east-1" },
          { key: "API_KEY", secret: true, description: `${name} secret` },
        ],
      },
    ],
  }));
}

function renderSection(connections?: string) {
  render(
    <ExternalResources projectName="acme" {...(connections ? { connections } : { connections: undefined })} />,
  );
}

function row(name: string) {
  return screen.getByRole("listitem", { name });
}

beforeEach(() => {
  saveState = mutationState();
});

afterEach(() => {
  dependencies = [];
  readiness = { configured: false, dependencies: [] };
  designPending = false;
  designError = false;
  readinessError = false;
  saveMutate.mockClear();
  saveReset.mockClear();
});

describe("ExternalResources", () => {
  it("renders one row per external resource, deduped across consumers", () => {
    dependencies = [
      ...externalDependencies("stripe", "twilio"),
      ...externalDependencies("stripe"),
    ];
    readiness = {
      configured: false,
      dependencies: [
        { name: "stripe", state: "unset", missingKeys: ["API_KEY"] },
        { name: "twilio", state: "unset", missingKeys: ["API_KEY"] },
      ],
    };

    renderSection();

    expect(screen.getAllByRole("listitem")).toHaveLength(2);
    expect(within(row("stripe")).getByText("stripe account")).toBeInTheDocument();
  });

  it("renders NOTHING when the design declares no external resources", () => {
    dependencies = [
      {
        componentName: "web",
        dependencies: [
          { kind: "platform-resource", name: "orders-db", resourceType: "postgres-cnpg" },
        ],
      },
    ];

    renderSection();

    expect(screen.queryByText("External resources")).not.toBeInTheDocument();
  });

  it("renders nothing while the design read is still in flight", () => {
    designPending = true;

    renderSection();

    expect(screen.queryByText("External resources")).not.toBeInTheDocument();
  });

  it("reports a failed design read instead of pretending there are none", () => {
    designError = true;

    renderSection();

    expect(
      screen.getByText(/failed to load this project's external resources/i),
    ).toBeInTheDocument();
  });

  it("offers Configure only where a person can act", () => {
    dependencies = externalDependencies("stripe", "twilio", "resend");
    readiness = {
      configured: false,
      dependencies: [
        { name: "stripe", state: "unset", missingKeys: ["API_KEY", "REGION"] },
        { name: "twilio", state: "configured", missingKeys: [] },
        { name: "resend", state: "not-provisioned", missingKeys: [] },
      ],
    };

    renderSection();

    expect(within(row("stripe")).getByText("Needs values")).toBeInTheDocument();
    expect(within(row("stripe")).getByText("2 values")).toBeInTheDocument();
    expect(
      within(row("stripe")).getByRole("button", { name: "Configure for stripe" }),
    ).toBeInTheDocument();

    expect(within(row("twilio")).getByText("Configured")).toBeInTheDocument();
    expect(
      within(row("twilio")).getByRole("button", { name: "Update values for twilio" }),
    ).toBeInTheDocument();

    expect(within(row("resend")).getByText("Provisioning")).toBeInTheDocument();
    expect(within(row("resend")).queryByRole("button")).not.toBeInTheDocument();
  });

  it("shows a resource with no readiness as unknown, and offers no save", () => {
    dependencies = externalDependencies("stripe");
    readiness = { configured: false, dependencies: [] };

    renderSection();

    expect(within(row("stripe")).getByText("Unknown")).toBeInTheDocument();
    expect(within(row("stripe")).queryByRole("button")).not.toBeInTheDocument();
  });

  it("matches readiness whose casing differs from the design's", () => {
    // externalResourceRows dedupes shared externals on a lowercased name but
    // keeps the first declaration's casing, so the row name and the readiness
    // name can legitimately differ in case. Exact matching left the row
    // `unknown`, which withholds Configure — the developer could not supply
    // values for a resource the platform was waiting on.
    dependencies = externalDependencies("Stripe");
    readiness = {
      configured: false,
      dependencies: [{ name: "stripe", state: "unset", missingKeys: ["API_KEY"] }],
    };

    renderSection();

    expect(within(row("Stripe")).queryByText("Unknown")).not.toBeInTheDocument();
    expect(
      within(row("Stripe")).getByRole("button", { name: /Configure for Stripe/i }),
    ).toBeInTheDocument();
  });

  it("summarises what is outstanding, and collapses once nothing is", () => {
    dependencies = externalDependencies("stripe", "twilio");
    readiness = {
      configured: false,
      dependencies: [
        { name: "stripe", state: "unset", missingKeys: ["API_KEY"] },
        { name: "twilio", state: "configured", missingKeys: [] },
      ],
    };

    renderSection();
    expect(screen.getByText("1 of 2 need values")).toBeInTheDocument();
    // Something is outstanding, so the rows are on screen without being asked for.
    expect(screen.getByRole("listitem", { name: "stripe" })).toBeVisible();
  });

  it("collapses to a receipt when every resource is configured", () => {
    dependencies = externalDependencies("stripe", "twilio");
    readiness = {
      configured: true,
      dependencies: [
        { name: "stripe", state: "configured", missingKeys: [] },
        { name: "twilio", state: "configured", missingKeys: [] },
      ],
    };

    renderSection();

    expect(screen.getByText("2 of 2 configured")).toBeInTheDocument();
    // The header stays reachable — rotating a key must not become a hunt.
    expect(screen.getByText("External resources")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /external resources/i }),
    ).toHaveAttribute("aria-expanded", "false");
  });

  it("opens the shared values dialog on Configure, with the union of declared keys", () => {
    dependencies = [
      {
        componentName: "checkout-api",
        dependencies: [
          { kind: "external", name: "stripe", config: [{ key: "REGION", secret: false }] },
        ],
      },
      {
        componentName: "checkout-worker",
        dependencies: [
          { kind: "external", name: "stripe", config: [{ key: "API_KEY", secret: true }] },
        ],
      },
    ];
    readiness = {
      configured: false,
      dependencies: [
        { name: "stripe", state: "unset", missingKeys: ["REGION", "API_KEY"] },
      ],
    };

    renderSection();
    fireEvent.click(screen.getByRole("button", { name: "Configure for stripe" }));

    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByText(/Configure — stripe/)).toBeInTheDocument();
    expect(within(dialog).getByLabelText("REGION")).toBeInTheDocument();
    expect(within(dialog).getByLabelText("API_KEY")).toHaveAttribute("type", "password");
  });

  it("seeds a plain default but never a secret one", () => {
    dependencies = [
      {
        componentName: "checkout-api",
        dependencies: [
          {
            kind: "external",
            name: "stripe",
            config: [
              { key: "REGION", secret: false, defaultValue: "us-east-1" },
              { key: "API_KEY", secret: true, defaultValue: "must-not-render" },
            ],
          },
        ],
      },
    ];
    readiness = {
      configured: false,
      dependencies: [{ name: "stripe", state: "unset", missingKeys: ["API_KEY"] }],
    };

    renderSection();
    fireEvent.click(screen.getByRole("button", { name: "Configure for stripe" }));

    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByLabelText("REGION")).toHaveValue("us-east-1");
    expect(within(dialog).getByLabelText("API_KEY")).toHaveValue("");
    expect(within(dialog).getByRole("button", { name: "Save values" })).toBeDisabled();
  });

  it("saves the resource's development values and confirms it by name", async () => {
    dependencies = externalDependencies("stripe");
    readiness = {
      configured: false,
      dependencies: [{ name: "stripe", state: "unset", missingKeys: ["API_KEY"] }],
    };
    // The dialog reports success through its onSaved callback, which is the seam
    // the page's confirmation hangs off.
    saveMutate.mockImplementation((_vars, opts) => opts?.onSuccess?.());

    renderSection();
    fireEvent.click(screen.getByRole("button", { name: "Configure for stripe" }));

    const dialog = screen.getByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("API_KEY"), {
      target: { value: "stripe-secret" },
    });
    const save = within(dialog).getByRole("button", { name: "Save values" });
    await waitFor(() => expect(save).toBeEnabled());
    fireEvent.click(save);

    expect(saveMutate).toHaveBeenCalledWith(
      {
        name: "stripe",
        environment: "development",
        values: { REGION: "us-east-1", API_KEY: "stripe-secret" },
      },
      expect.anything(),
    );
    expect(
      await screen.findByText("Values saved — stripe is configured."),
    ).toBeInTheDocument();
  });

  it("opens the named resource's dialog straight from ?connections=<name>", async () => {
    dependencies = externalDependencies("stripe", "twilio");
    readiness = {
      configured: false,
      dependencies: [
        { name: "stripe", state: "unset", missingKeys: ["API_KEY"] },
        { name: "twilio", state: "unset", missingKeys: ["API_KEY"] },
      ],
    };

    renderSection("twilio");

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/Configure — twilio/)).toBeInTheDocument();
  });

  it("will not open a resource the platform is still provisioning", () => {
    dependencies = externalDependencies("algolia");
    readiness = {
      configured: false,
      dependencies: [{ name: "algolia", state: "not-provisioned", missingKeys: [] }],
    };

    renderSection("algolia");

    // The row offers no button because a save would be rejected; a link is not
    // a way around that. The section still expands so the reason is on screen.
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(within(row("algolia")).getByText("Provisioning")).toBeInTheDocument();
  });

  it("degrades an unknown ?connections value to an expanded section", () => {
    dependencies = externalDependencies("stripe");
    readiness = {
      configured: true,
      dependencies: [{ name: "stripe", state: "configured", missingKeys: [] }],
    };

    renderSection("does-not-exist");

    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /external resources/i }),
    ).toHaveAttribute("aria-expanded", "true");
  });

  it("lists the resources with unknown states when readiness fails", () => {
    dependencies = externalDependencies("stripe");
    readinessError = true;

    renderSection();

    expect(screen.getByText(/failed to load readiness/i)).toBeInTheDocument();
    expect(within(row("stripe")).getByText("Unknown")).toBeInTheDocument();
  });
});
