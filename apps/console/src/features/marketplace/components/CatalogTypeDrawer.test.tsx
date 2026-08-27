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

import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ElementType } from "react";
import type { components } from "../../../generated/aep-api";
import { CatalogTypeDrawer } from "./CatalogTypeDrawer";

const navigate = vi.fn();

// Router replaced so the "Used by" ProjectLink renders as a plain anchor
// (createLink pattern, cf. ResourceDrawer.test). useNavigate is the Edit
// seam: registered stripe goes to the register form with ?name=.
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigate,
  createLink: (Component: ElementType) =>
    function MockLink({
      to,
      params,
      ...rest
    }: { to: string; params?: Record<string, unknown> } & Record<string, unknown>) {
      let href = to;
      for (const [key, value] of Object.entries(params ?? {})) {
        href = href.replace(`$${key}`, String(value));
      }
      return <Component component="a" href={href} {...rest} />;
    },
}));

type ExternalResourceDTO = components["schemas"]["ExternalResourceDTO"];
type PlatformResourceTypeDTO = components["schemas"]["PlatformResourceTypeDTO"];

const mutate = vi.fn();
const reset = vi.fn();
let deleteState: {
  isPending: boolean;
  isError: boolean;
  error: Error | null;
  mutate: typeof mutate;
  reset: typeof reset;
};

vi.mock("../../settings/api/queries", () => ({
  useDeleteExternalResource: () => deleteState,
}));

function resetDeleteState() {
  mutate.mockReset();
  reset.mockReset();
  deleteState = { isPending: false, isError: false, error: null, mutate, reset };
}

function platformResource(
  overrides: Partial<PlatformResourceTypeDTO> = {},
): PlatformResourceTypeDTO {
  return {
    name: "postgres-cnpg",
    description: "Managed Postgres via CloudNativePG",
    parameters: { size: { type: "string", description: "Storage size" } },
    outputs: ["connectionUrl"],
    consumers: [],
    ...overrides,
  };
}

function registeredExternal(
  overrides: Partial<ExternalResourceDTO> = {},
): ExternalResourceDTO {
  return {
    name: "stripe",
    description: "Stripe payments API",
    consumptionInstructions: "Use the secret key as Bearer.",
    config: [
      { key: "STRIPE_API_KEY", secret: true, description: "Secret API key" },
      { key: "STRIPE_WEBHOOK_ID", secret: false },
    ],
    consumers: [],
    envCells: [
      {
        environment: "development",
        key: "STRIPE_API_KEY",
        status: "configured",
        // Wrongly included: the drawer must never render cell values.
        value: "leaked-secret-value",
      },
      { environment: "production", key: "STRIPE_API_KEY", status: "unset" },
    ],
    resourceDocs: [{ type: "openapi", url: "https://example.com/stripe/openapi.yaml" }],
    instances: [{ project: "demo-shop", environment: "development", status: "Ready" }],
    ...overrides,
  };
}

function projectExternal(
  overrides: Partial<ExternalResourceDTO> = {},
): ExternalResourceDTO {
  return {
    name: "github",
    description: "GitHub API token for repository access",
    config: [{ key: "token", secret: true, description: "Personal access token" }],
    consumers: [],
    envCells: [],
    ...overrides,
  };
}

describe("CatalogTypeDrawer", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    resetDeleteState();
  });

  it("platform: no Delete resource and no Edit", () => {
    render(
      <CatalogTypeDrawer
        kind="platform"
        resource={platformResource()}
        open
        onClose={vi.fn()}
      />,
    );

    expect(screen.queryByRole("button", { name: "Delete resource" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /edit/i })).not.toBeInTheDocument();
    expect(screen.getByText("Parameters (inputs)")).toBeInTheDocument();
    expect(screen.getByText("Outputs")).toBeInTheDocument();
  });

  it("registered: consumption instructions, Configured/Unset only, docs, unused Delete", () => {
    render(
      <CatalogTypeDrawer
        kind="external"
        resource={registeredExternal()}
        open
        onClose={vi.fn()}
      />,
    );

    expect(screen.getByText("Use the secret key as Bearer.")).toBeInTheDocument();
    expect(screen.getAllByText("Configured").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Unset").length).toBeGreaterThan(0);
    expect(screen.queryByText("leaked-secret-value")).not.toBeInTheDocument();
    expect(screen.getByText("openapi")).toBeInTheDocument();
    expect(screen.getByText("https://example.com/stripe/openapi.yaml")).toBeInTheDocument();
    expect(screen.getByText("demo-shop")).toBeInTheDocument();
    expect(screen.getByText("Ready")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Delete resource" })).toBeEnabled();
    expect(screen.getByRole("button", { name: /edit/i })).toBeInTheDocument();
  });

  it("renders a path pointer as text without a file body", () => {
    render(
      <CatalogTypeDrawer
        kind="external"
        resource={registeredExternal({
          resourceDocs: [{ type: "documentation", path: "stripe/README.md" }],
        })}
        open
        onClose={vi.fn()}
      />,
    );

    expect(screen.getByText("documentation")).toBeInTheDocument();
    expect(screen.getByText("stripe/README.md")).toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "stripe/README.md" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("# Stripe")).not.toBeInTheDocument();
  });

  it("registered: Edit navigates to the register form with the logical name", () => {
    render(
      <CatalogTypeDrawer
        kind="external"
        resource={registeredExternal()}
        open
        onClose={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: /edit/i }));

    expect(navigate).toHaveBeenCalledWith({
      to: "/resources/register/form",
      search: { name: "stripe" },
    });
  });

  it("project external: Connection-values note, no env-cell matrix, unused Delete, no Edit", () => {
    render(
      <CatalogTypeDrawer
        kind="external"
        resource={projectExternal()}
        open
        onClose={vi.fn()}
      />,
    );

    expect(screen.queryByText("Configured")).not.toBeInTheDocument();
    expect(screen.queryByText("Unset")).not.toBeInTheDocument();
    expect(
      screen.getByText(
        "Environment values for this Project External resource are set on the project Connection values dialog.",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /edit/i })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Delete resource" })).toBeEnabled();
  });

  it("either External kind with consumers: Delete is disabled", () => {
    const consumers = [{ componentName: "checkout-api", projectId: "acme" }];
    const { rerender } = render(
      <CatalogTypeDrawer
        kind="external"
        resource={registeredExternal({ consumers })}
        open
        onClose={vi.fn()}
      />,
    );

    expect(screen.getByRole("button", { name: "Delete resource" })).toBeDisabled();

    rerender(
      <CatalogTypeDrawer
        kind="external"
        resource={projectExternal({ consumers })}
        open
        onClose={vi.fn()}
      />,
    );

    expect(screen.getByRole("button", { name: "Delete resource" })).toBeDisabled();
  });

  it("unused External: confirm then mutate(name)", () => {
    const onClose = vi.fn();
    render(
      <CatalogTypeDrawer
        kind="external"
        resource={registeredExternal({ consumers: [] })}
        open
        onClose={onClose}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Delete resource" }));

    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByText("Delete stripe?")).toBeInTheDocument();

    fireEvent.click(within(dialog).getByRole("button", { name: "Delete resource" }));

    expect(mutate).toHaveBeenCalledTimes(1);
    expect(mutate.mock.calls[0]?.[0]).toBe("stripe");

    const options = mutate.mock.calls[0]?.[1] as { onSuccess?: () => void };
    options.onSuccess?.();
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
