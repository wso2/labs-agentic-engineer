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
import { describe, expect, it, vi } from "vitest";
import type { ElementType } from "react";
import type { components } from "../../../generated/aep-api";
import { ResourcesCatalog } from "./ResourcesCatalog";

// Router replaced so a future drawer "Used by" ProjectLink can render as a
// plain anchor (createLink pattern, cf. ResourcesSection.test).
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
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

// Every test but the dedicated permission ones below holds both resource
// permissions, so the page and its Register action read as fully reachable
// by default — mirrors SkillsSection.test.tsx's per-suite permission toggle.
const heldPermissions = vi.hoisted(() => new Set(["ae:resource-view", "ae:resource-config"]));
vi.mock("../../../auth/permissions", () => ({
  useHasPermission: (permission: string) => heldPermissions.has(permission),
  useHasAnyPermission: (permissions: string[]) =>
    permissions.some((p) => heldPermissions.has(p)),
}));

type PlatformResourceTypeDTO = components["schemas"]["PlatformResourceTypeDTO"];
type ExternalResourceDTO = components["schemas"]["ExternalResourceDTO"];

// Query hooks: replaced wholesale so the test needs neither a
// QueryClientProvider nor MSW — only the rendering under test is real.
let platformRefetch = vi.fn();
let externalRefetch = vi.fn();
let platformState: {
  data?: PlatformResourceTypeDTO[];
  isLoading: boolean;
  isError: boolean;
  error?: Error | null;
  refetch: typeof platformRefetch;
} = { data: [], isLoading: false, isError: false, refetch: platformRefetch };
let externalState: {
  data?: ExternalResourceDTO[];
  isLoading: boolean;
  isError: boolean;
  error?: Error | null;
  refetch: typeof externalRefetch;
} = { data: [], isLoading: false, isError: false, refetch: externalRefetch };

vi.mock("../../settings/api/queries", () => ({
  usePlatformResourceTypes: () => platformState,
  useExternalResources: () => externalState,
  useDeleteExternalResource: () => ({
    isPending: false,
    isError: false,
    error: null,
    mutate: vi.fn(),
    reset: vi.fn(),
  }),
}));

function resetState() {
  platformRefetch = vi.fn();
  externalRefetch = vi.fn();
  platformState = {
    data: [],
    isLoading: false,
    isError: false,
    refetch: platformRefetch,
  };
  externalState = {
    data: [],
    isLoading: false,
    isError: false,
    refetch: externalRefetch,
  };
  heldPermissions.clear();
  heldPermissions.add("ae:resource-view");
  heldPermissions.add("ae:resource-config");
}

function platformType(
  overrides: Partial<PlatformResourceTypeDTO> = {},
): PlatformResourceTypeDTO {
  return {
    name: "postgres-cnpg",
    description: "Managed Postgres via CloudNativePG",
    consumers: [{ componentName: "catalog-api", projectId: "demo-shop" }],
    ...overrides,
  };
}

function externalResource(
  overrides: Partial<ExternalResourceDTO> = {},
): ExternalResourceDTO {
  return {
    name: "stripe",
    description: "Stripe payments API",
    config: [{ key: "api_key", secret: true, description: "Secret API key" }],
    consumers: [{ projectId: "demo-shop", componentName: "checkout-api" }],
    envCells: [
      { environment: "development", key: "api_key", status: "configured" },
    ],
    ...overrides,
  };
}

describe("ResourcesCatalog", () => {
  it("shows a combined loading indicator while either query is pending", () => {
    resetState();
    platformState = { ...platformState, isLoading: true };

    render(<ResourcesCatalog />);

    expect(screen.getByLabelText("Loading resources")).toBeInTheDocument();
  });

  it("shows a combined error alert with Retry that refetches both queries", () => {
    resetState();
    platformState = {
      ...platformState,
      isError: true,
      error: new Error("boom"),
    };

    render(<ResourcesCatalog />);

    expect(screen.getByRole("alert")).toHaveTextContent("boom");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(platformRefetch).toHaveBeenCalledTimes(1);
    expect(externalRefetch).toHaveBeenCalledTimes(1);
  });

  it("shows a catalog-empty state when both lists are empty", () => {
    resetState();

    render(<ResourcesCatalog />);

    expect(screen.getByText("No resources")).toBeInTheDocument();
    expect(screen.getByText(/the catalog is empty/i)).toBeInTheDocument();
    expect(screen.queryByText("No platform resources")).not.toBeInTheDocument();
    expect(screen.queryByText("No external resources")).not.toBeInTheDocument();
    expect(screen.queryByRole("tab")).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Register" })).toHaveAttribute(
      "href",
      "/resources/register",
    );
  });

  it("renders one unified grid; only platform types carry the Platform chip", () => {
    resetState();
    platformState = {
      ...platformState,
      data: [platformType()],
    };
    externalState = {
      ...externalState,
      data: [
        externalResource(),
        externalResource({
          name: "github",
          description: "GitHub API token for repository access",
          config: [{ key: "token", secret: true }],
          consumers: [],
          envCells: [],
        }),
      ],
    };

    render(<ResourcesCatalog />);

    expect(screen.getByText("postgres-cnpg")).toBeInTheDocument();
    expect(screen.getByText("stripe")).toBeInTheDocument();
    expect(screen.getByText("github")).toBeInTheDocument();
    expect(screen.getAllByText("Platform")).toHaveLength(1);

    const postgresCard = screen.getByText("postgres-cnpg").closest(".MuiCard-root");
    expect(postgresCard).not.toBeNull();
    expect(within(postgresCard as HTMLElement).getByText("Platform")).toBeInTheDocument();

    const stripeCard = screen.getByText("stripe").closest(".MuiCard-root");
    expect(stripeCard).not.toBeNull();
    expect(within(stripeCard as HTMLElement).queryByText("Platform")).not.toBeInTheDocument();

    const githubCard = screen.getByText("github").closest(".MuiCard-root");
    expect(githubCard).not.toBeNull();
    expect(within(githubCard as HTMLElement).queryByText("Platform")).not.toBeInTheDocument();

    expect(screen.queryByRole("tab")).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Register" })).toHaveAttribute(
      "href",
      "/resources/register",
    );
    expect(screen.queryByRole("button", { name: /edit/i })).not.toBeInTheDocument();
  });

  // The name is the organization's word for the resource; the provider is the
  // system it actually is, and a card that shows only the name hides it.
  it("names the provider on an external card, and platform types have none", () => {
    resetState();
    platformState = { ...platformState, data: [platformType()] };
    externalState = {
      ...externalState,
      data: [externalResource({ provider: "Stripe" })],
    };

    render(<ResourcesCatalog />);

    const stripeCard = screen.getByText("stripe").closest(".MuiCard-root");
    expect(within(stripeCard as HTMLElement).getByText("Stripe")).toBeInTheDocument();
    const postgresCard = screen.getByText("postgres-cnpg").closest(".MuiCard-root");
    expect(within(postgresCard as HTMLElement).queryByText("Stripe")).not.toBeInTheDocument();
  });

  // A project's own resources are not records: they follow the records in
  // their own section, each naming its project, so the organization can take
  // one over from here.
  it("lists a project's own resources under Held by projects, naming the project", () => {
    resetState();
    externalState = {
      ...externalState,
      data: [
        externalResource({ provider: "Stripe", scope: "org" }),
        externalResource({
          name: "fx-rates",
          provider: "Open Exchange Rates",
          scope: "project",
          project: "team-expenses",
          envCells: [],
        }),
      ],
    };

    render(<ResourcesCatalog />);

    const held = screen.getByRole("region", { name: "Held by projects" });
    expect(within(held).getByText("fx-rates")).toBeInTheDocument();
    expect(within(held).getByText("held by team-expenses")).toBeInTheDocument();
    expect(within(held).queryByText("stripe")).not.toBeInTheDocument();
    expect(screen.getByText("stripe")).toBeInTheDocument();
  });

  it("shows no Held by projects section when every external row is a record", () => {
    resetState();
    externalState = {
      ...externalState,
      data: [externalResource({ scope: "org" })],
    };

    render(<ResourcesCatalog />);

    expect(screen.queryByRole("region", { name: "Held by projects" })).not.toBeInTheDocument();
  });

  it("clamps a long card description to two lines and keeps the full text on title", () => {
    resetState();
    const long =
      "A dedicated PostgreSQL database cluster provisioned inside the platform " +
      "(CloudNativePG). Declare on the service that owns the data. Extra sentence " +
      "so the copy is longer than two lines on a catalog card.";
    platformState = {
      ...platformState,
      data: [platformType({ description: long })],
    };

    render(<ResourcesCatalog />);

    const desc = screen.getByText(long);
    expect(desc).toHaveAttribute("title", long);
    const css = getComputedStyle(desc);
    expect(css.overflow).toBe("hidden");
    expect(css.webkitLineClamp).toBe("2");
    expect(css.maxHeight).toBe("2lh");
  });

  it("opens the catalog-type drawer when a card is clicked", async () => {
    resetState();
    externalState = {
      ...externalState,
      data: [externalResource()],
    };

    render(<ResourcesCatalog />);

    expect(screen.queryByLabelText("Close")).not.toBeInTheDocument();

    fireEvent.click(screen.getByText("stripe"));

    expect(screen.getAllByText("stripe")).toHaveLength(2);
    expect(screen.getByLabelText("Close")).toBeInTheDocument();

    fireEvent.click(screen.getByLabelText("Close"));
    await waitFor(() =>
      expect(screen.queryByLabelText("Close")).not.toBeInTheDocument(),
    );
  });

  it("shows an insufficient-permissions message and renders no catalog content holding neither resource permission", () => {
    resetState();
    heldPermissions.clear();
    platformState = { ...platformState, data: [platformType()] };
    externalState = { ...externalState, data: [externalResource()] };

    render(<ResourcesCatalog />);

    expect(
      screen.getByText("You don't have permission to view resources."),
    ).toBeInTheDocument();
    expect(screen.queryByText("postgres-cnpg")).not.toBeInTheDocument();
    expect(screen.queryByText("stripe")).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Register" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Register" })).not.toBeInTheDocument();
  });

  it("renders the catalog for a view-only caller, with Register disabled", () => {
    resetState();
    heldPermissions.delete("ae:resource-config");
    platformState = { ...platformState, data: [platformType()] };

    render(<ResourcesCatalog />);

    expect(screen.getByText("postgres-cnpg")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Register" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Register" })).toBeDisabled();
  });
});
