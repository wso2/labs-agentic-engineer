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

import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { ElementType } from "react";
import type { components } from "../../../generated/aep-api";
import { OverviewDependencies } from "./OverviewDependencies";

// Router replaced so the drawer's "Used by" ProjectLink renders as a plain
// anchor (createLink pattern, cf. ResourcesSection.test).
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

type WorkloadDependencyDTO = components["schemas"]["WorkloadDependencyDTO"];
type PlatformResourceTypeDTO = components["schemas"]["PlatformResourceTypeDTO"];
type ExternalResourceDTO = components["schemas"]["ExternalResourceDTO"];

const CURRENT_PROJECT = "demo-shop";
const PROVIDER_PROJECT = "gym-tracker";
const PROVIDER_COMPONENT = "gym-api";

const platformPostgres: PlatformResourceTypeDTO = {
  name: "postgres-cnpg",
  description: "Managed Postgres via CloudNativePG",
  consumers: [],
};

const externalStripe: ExternalResourceDTO = {
  name: "stripe",
  description: "Stripe payments API",
  config: [],
  consumers: [],
};

const someDependencies: WorkloadDependencyDTO[] = [
  {
    kind: "resource",
    tag: "platform",
    ref: "postgres-cnpg",
    name: "postgres-cnpg",
  },
  {
    kind: "resource",
    tag: "external",
    ref: "stripe",
    name: "stripe",
  },
  {
    kind: "org-service",
    name: "gym-api",
    project: PROVIDER_PROJECT,
    component: PROVIDER_COMPONENT,
  },
];

let depsState: {
  data?: WorkloadDependencyDTO[];
  isPending: boolean;
  isError: boolean;
  error?: Error | null;
  refetch: ReturnType<typeof vi.fn>;
};
let platformState: {
  data?: PlatformResourceTypeDTO[];
  isPending: boolean;
  isError: boolean;
};
let externalState: {
  data?: ExternalResourceDTO[];
  isPending: boolean;
  isError: boolean;
};
const openApiCalls: {
  projectName: string;
  componentName: string;
  enabled: boolean;
}[] = [];
const refetch = vi.fn();

vi.mock("../api/queries", () => ({
  useWorkloadDependencies: () => depsState,
  useComponentOpenApi: (
    projectName: string,
    componentName: string,
    enabled: boolean,
  ) => {
    openApiCalls.push({ projectName, componentName, enabled });
    return {
      data: undefined,
      isLoading: false,
      isError: false,
      error: null,
    };
  },
}));

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
  refetch.mockReset();
  openApiCalls.length = 0;
  depsState = {
    data: [],
    isPending: false,
    isError: false,
    error: null,
    refetch,
  };
  platformState = {
    data: [platformPostgres],
    isPending: false,
    isError: false,
  };
  externalState = {
    data: [externalStripe],
    isPending: false,
    isError: false,
  };
}

describe("OverviewDependencies", () => {
  it("shows the empty state and does not treat it as an error", () => {
    resetState();

    render(<OverviewDependencies projectName={CURRENT_PROJECT} />);

    expect(screen.getByText("No deployed dependencies")).toBeInTheDocument();
    expect(
      screen.getByText(
        /rows appear after a component has deployed/i,
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/unresolved design declarations stay off this list/i),
    ).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("opens the resource drawer with the catalog type name on a resource click", () => {
    resetState();
    depsState = {
      data: someDependencies,
      isPending: false,
      isError: false,
      refetch,
    };

    render(<OverviewDependencies projectName={CURRENT_PROJECT} />);

    expect(screen.queryByLabelText("Close")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /postgres-cnpg/ }));

    expect(screen.getAllByText("postgres-cnpg")).toHaveLength(2);
    expect(screen.getByText("Managed Postgres via CloudNativePG")).toBeInTheDocument();
    expect(screen.getByLabelText("Close")).toBeInTheDocument();
  });

  it("opens the resource drawer on an external resource click", () => {
    resetState();
    depsState = {
      data: someDependencies,
      isPending: false,
      isError: false,
      refetch,
    };

    render(<OverviewDependencies projectName={CURRENT_PROJECT} />);

    expect(screen.queryByLabelText("Close")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /stripe/ }));

    expect(screen.getAllByText("stripe")).toHaveLength(2);
    expect(screen.getByText("Stripe payments API")).toBeInTheDocument();
    expect(screen.getByLabelText("Close")).toBeInTheDocument();
  });

  it("exposes each row as a button a keyboard user can activate", () => {
    resetState();
    depsState = {
      data: someDependencies,
      isPending: false,
      isError: false,
      refetch,
    };

    render(<OverviewDependencies projectName={CURRENT_PROJECT} />);

    const row = screen.getByRole("button", { name: /postgres-cnpg/ });
    expect(row.tagName).toBe("BUTTON");
    row.focus();
    expect(row).toHaveFocus();
    fireEvent.click(row);

    expect(screen.getByLabelText("Close")).toBeInTheDocument();
  });

  it("opens the OpenAPI dialog with the provider project and component", () => {
    resetState();
    depsState = {
      data: someDependencies,
      isPending: false,
      isError: false,
      refetch,
    };

    render(<OverviewDependencies projectName={CURRENT_PROJECT} />);

    fireEvent.click(screen.getByRole("button", { name: /gym-api/ }));

    expect(
      screen.getByText(`${PROVIDER_COMPONENT} · API contract`),
    ).toBeInTheDocument();
    expect(openApiCalls).toContainEqual({
      projectName: PROVIDER_PROJECT,
      componentName: PROVIDER_COMPONENT,
      enabled: true,
    });
    expect(openApiCalls).not.toContainEqual({
      projectName: CURRENT_PROJECT,
      componentName: PROVIDER_COMPONENT,
      enabled: true,
    });
  });

  it("shows a loading indicator while the query is pending", () => {
    resetState();
    depsState = {
      isPending: true,
      isError: false,
      refetch,
    };

    render(<OverviewDependencies projectName={CURRENT_PROJECT} />);

    expect(screen.getByLabelText("Loading dependencies")).toBeInTheDocument();
    expect(screen.queryByText("No deployed dependencies")).not.toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("shows an error alert with retry when the query fails", () => {
    resetState();
    depsState = {
      isPending: false,
      isError: true,
      error: new Error("upstream unavailable"),
      refetch,
    };

    render(<OverviewDependencies projectName={CURRENT_PROJECT} />);

    expect(screen.getByRole("alert")).toHaveTextContent("upstream unavailable");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(refetch).toHaveBeenCalledTimes(1);
  });

  it("opens the drawer from the workload row when the catalog has no matching type", () => {
    resetState();
    depsState = {
      data: [
        {
          kind: "resource",
          tag: "platform",
          ref: "mystery-db",
          name: "mystery-db",
        },
      ],
      isPending: false,
      isError: false,
      refetch,
    };

    render(<OverviewDependencies projectName={CURRENT_PROJECT} />);

    fireEvent.click(screen.getByRole("button", { name: /mystery-db/ }));

    expect(screen.getAllByText("mystery-db")).toHaveLength(2);
    expect(screen.getByLabelText("Close")).toBeInTheDocument();
  });
});
