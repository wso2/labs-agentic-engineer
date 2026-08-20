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
const mockUseSaveConnectionValues = vi.fn();
let mutationStatesByName = new Map<string, ReturnType<typeof mutationState>>();
let dependencies: ComponentDependencies[] = [];
let readiness: ProjectDependencyReadiness = { configured: false, dependencies: [] };

vi.mock("../../spec/api/queries", () => ({
  useDesignDependencies: () => ({
    data: dependencies,
    isPending: false,
    isError: false,
    error: null,
    refetch: vi.fn(),
  }),
}));

vi.mock("../../projects/api/queries", () => ({
  useProjectDependencyReadiness: () => ({
    data: readiness,
    isPending: false,
    isError: false,
    error: null,
    refetch: vi.fn(),
  }),
  useSaveConnectionValues: (...args: unknown[]) =>
    mockUseSaveConnectionValues(...args),
}));

import { ConnectionConfiguration } from "./ConnectionConfiguration";

function externalDependencies(...names: string[]): ComponentDependencies[] {
  return names.map((name, index) => ({
    componentName: `component-${index + 1}`,
    dependencies: [
      {
        kind: "external",
        name,
        description: `${name} connection`,
        config: [
          { key: "REGION", secret: false, defaultValue: "us-east-1" },
          { key: "API_KEY", secret: true, description: `${name} secret` },
        ],
      },
    ],
  }));
}

function renderConfiguration() {
  render(<ConnectionConfiguration projectName="acme" open />);
}

afterEach(() => {
  dependencies = [];
  readiness = { configured: false, dependencies: [] };
  saveMutate.mockClear();
  mockUseSaveConnectionValues.mockReset();
  mutationStatesByName = new Map();
});

function mutationState(
  overrides: Partial<{
    isPending: boolean;
    isError: boolean;
    error: Error | null;
  }> = {},
) {
  return {
    mutate: saveMutate,
    isPending: false,
    isError: false,
    error: null,
    ...overrides,
  };
}

beforeEach(() => {
  mockUseSaveConnectionValues.mockImplementation(
    (_projectName: string, connectionName: string) =>
      mutationStatesByName.get(connectionName) ?? mutationState(),
  );
});

describe("ConnectionConfiguration", () => {
  it("renders every external dependency card once", () => {
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

    renderConfiguration();

    expect(screen.getByRole("region", { name: "stripe" })).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "twilio" })).toBeInTheDocument();
    expect(screen.getAllByRole("heading", { name: "stripe" })).toHaveLength(1);
  });

  it("shows the external dependency description on its card", () => {
    dependencies = externalDependencies("stripe");
    readiness = {
      configured: false,
      dependencies: [
        { name: "stripe", state: "unset", missingKeys: ["API_KEY"] },
      ],
    };

    renderConfiguration();

    expect(
      within(screen.getByRole("region", { name: "stripe" })).getByText(
        "stripe connection",
      ),
    ).toBeInTheDocument();
  });

  it("renders the union of keys declared by a shared external dependency", () => {
    dependencies = [
      {
        componentName: "checkout-api",
        dependencies: [
          {
            kind: "external",
            name: "stripe",
            config: [{ key: "REGION", secret: false }],
          },
        ],
      },
      {
        componentName: "checkout-worker",
        dependencies: [
          {
            kind: "external",
            name: "stripe",
            config: [{ key: "API_KEY", secret: true }],
          },
        ],
      },
    ];
    readiness = {
      configured: false,
      dependencies: [
        {
          name: "stripe",
          state: "unset",
          missingKeys: ["REGION", "API_KEY"],
        },
      ],
    };

    renderConfiguration();

    const card = screen.getByRole("region", { name: "stripe" });
    expect(within(card).getByLabelText("REGION")).toBeInTheDocument();
    expect(within(card).getByLabelText("API_KEY")).toHaveAttribute(
      "type",
      "password",
    );
  });

  it("shows configured and missing-value statuses on their own cards", () => {
    dependencies = externalDependencies("stripe", "twilio");
    readiness = {
      configured: false,
      dependencies: [
        { name: "stripe", state: "configured", missingKeys: [] },
        { name: "twilio", state: "unset", missingKeys: ["API_KEY"] },
      ],
    };

    renderConfiguration();

    expect(within(screen.getByRole("region", { name: "stripe" })).getByText("Configured")).toBeInTheDocument();
    expect(within(screen.getByRole("region", { name: "twilio" })).getByText("Needs values")).toBeInTheDocument();
  });

  it("explains platform provisioning and prevents saving until it completes", () => {
    dependencies = externalDependencies("stripe");
    readiness = {
      configured: false,
      dependencies: [
        { name: "stripe", state: "not-provisioned", missingKeys: [] },
      ],
    };

    renderConfiguration();

    expect(screen.getByText(/platform is provisioning this connection/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save stripe values" })).toBeDisabled();
  });

  it("shows missing readiness as neutral and prevents saving before server truth", () => {
    dependencies = externalDependencies("stripe");
    readiness = { configured: false, dependencies: [] };

    renderConfiguration();

    const card = screen.getByRole("region", { name: "stripe" });
    expect(within(card).getByText("Readiness unknown")).toBeInTheDocument();
    expect(within(card).queryByText("Needs values")).not.toBeInTheDocument();
    fireEvent.change(within(card).getByLabelText("API_KEY"), {
      target: { value: "secret" },
    });
    const save = within(card).getByRole("button", {
      name: "Save stripe values",
    });
    expect(save).toBeDisabled();
    fireEvent.click(save);
    expect(saveMutate).not.toHaveBeenCalled();
  });

  it("keeps one card's pending save state off the other cards", () => {
    dependencies = externalDependencies("stripe", "twilio");
    readiness = {
      configured: false,
      dependencies: [
        { name: "stripe", state: "configured", missingKeys: [] },
        { name: "twilio", state: "configured", missingKeys: [] },
      ],
    };
    mutationStatesByName.set("stripe", mutationState({ isPending: true }));

    renderConfiguration();

    fireEvent.change(
      within(screen.getByRole("region", { name: "twilio" })).getByLabelText(
        "API_KEY",
      ),
      { target: { value: "twilio-secret" } },
    );

    expect(
      within(screen.getByRole("region", { name: "stripe" })).getByRole(
        "button",
        { name: "Saving…" },
      ),
    ).toBeDisabled();
    expect(
      within(screen.getByRole("region", { name: "twilio" })).getByRole(
        "button",
        { name: "Save twilio values" },
      ),
    ).not.toBeDisabled();
  });

  it("keeps one card's save error off the other cards", () => {
    dependencies = externalDependencies("stripe", "twilio");
    readiness = {
      configured: false,
      dependencies: [
        { name: "stripe", state: "configured", missingKeys: [] },
        { name: "twilio", state: "configured", missingKeys: [] },
      ],
    };
    mutationStatesByName.set(
      "stripe",
      mutationState({
        isError: true,
        error: new Error("Stripe save failed"),
      }),
    );

    renderConfiguration();

    expect(
      within(screen.getByRole("region", { name: "stripe" })).getByText(
        "Stripe save failed",
      ),
    ).toBeInTheDocument();
    expect(
      within(screen.getByRole("region", { name: "twilio" })).queryByText(
        "Stripe save failed",
      ),
    ).not.toBeInTheDocument();
  });

  it("never seeds a secret field from a design default", () => {
    dependencies = [
      {
        componentName: "checkout-api",
        dependencies: [
          {
            kind: "external",
            name: "stripe",
            config: [
              {
                key: "API_KEY",
                secret: true,
                defaultValue: "must-not-render",
              },
            ],
          },
        ],
      },
    ];
    readiness = {
      configured: false,
      dependencies: [
        { name: "stripe", state: "unset", missingKeys: ["API_KEY"] },
      ],
    };

    renderConfiguration();

    expect(screen.getByLabelText("API_KEY")).toHaveValue("");
    expect(screen.getByRole("button", { name: "Save stripe values" })).toBeDisabled();
  });

  it("saves only the completed connection's development values", async () => {
    dependencies = externalDependencies("stripe", "twilio");
    readiness = {
      configured: false,
      dependencies: [
        { name: "stripe", state: "unset", missingKeys: ["API_KEY"] },
        { name: "twilio", state: "unset", missingKeys: ["API_KEY"] },
      ],
    };

    renderConfiguration();

    fireEvent.change(within(screen.getByRole("region", { name: "stripe" })).getByLabelText("API_KEY", { selector: "input" }), {
      target: { value: "stripe-secret" },
    });

    const saveStripe = screen.getByRole("button", { name: "Save stripe values" });
    await waitFor(() => expect(saveStripe).toBeEnabled());
    expect(screen.getByRole("button", { name: "Save twilio values" })).toBeDisabled();

    fireEvent.click(saveStripe);

    expect(saveMutate).toHaveBeenCalledWith({
      name: "stripe",
      environment: "development",
      values: { REGION: "us-east-1", API_KEY: "stripe-secret" },
    });
  });
});
