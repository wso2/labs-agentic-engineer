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

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ComponentOpenApiDialog } from "./ComponentOpenApiDialog";

// The contract read and the catalog read are both react-query hooks and this
// file renders without a QueryClientProvider; each has its own test. What
// belongs HERE is that the dialog joins them onto the viewer, which is the step
// that was missing — the column existed and nothing passed it a value.
const mockUseComponentOpenApi = vi.fn();
vi.mock("../api/queries", () => ({
  useComponentOpenApi: (...args: unknown[]) => mockUseComponentOpenApi(...args),
}));

const mockUseApiViewSecurity = vi.fn();
vi.mock("../../spec/hooks/useApiViewSecurity", () => ({
  useApiViewSecurity: (...args: unknown[]) => mockUseApiViewSecurity(...args),
}));

const SPEC = `openapi: 3.0.3
info:
  title: Orders API
  version: 1.0.0
components:
  securitySchemes:
    oauth2:
      type: oauth2
      flows: {}
security:
  - oauth2: []
paths:
  /orders:
    get:
      summary: List orders
      security:
        - oauth2: ["orders:read"]
      responses:
        "200":
          description: ok
`;

const RESOURCE_SERVER = "https://aep.wso2.com/orgs/acme/projects/shop";

beforeEach(() => {
  vi.clearAllMocks();
  mockUseComponentOpenApi.mockReturnValue({
    data: { spec: SPEC },
    isLoading: false,
    isError: false,
    error: null,
  });
  mockUseApiViewSecurity.mockReturnValue({
    roles: { "orders:read": { roles: ["Shopper"], note: "own rows" } },
    resourceServer: RESOURCE_SERVER,
  });
});

afterEach(cleanup);

function open() {
  render(
    <ComponentOpenApiDialog
      projectName="shop"
      componentName="orders-api"
      onClose={vi.fn()}
    />,
  );
}

describe("ComponentOpenApiDialog", () => {
  it("names the roles that grant an operation's scope", () => {
    open();

    expect(screen.getByText("orders:read")).toBeInTheDocument();
    expect(screen.getByText("Shopper · own rows")).toBeInTheDocument();
  });

  it("names the audience the scopes are granted on", () => {
    open();

    expect(screen.getByText(`aud ${RESOURCE_SERVER}`)).toBeInTheDocument();
  });

  it("reads the catalog for this project, and only while it is open", () => {
    open();
    expect(mockUseApiViewSecurity).toHaveBeenLastCalledWith({
      projectName: "shop",
      active: true,
    });

    cleanup();
    render(
      <ComponentOpenApiDialog
        projectName="shop"
        componentName={null}
        onClose={vi.fn()}
      />,
    );
    expect(mockUseApiViewSecurity).toHaveBeenLastCalledWith({
      projectName: "shop",
      active: false,
    });
  });

  it("still renders the contract when the platform knows neither", () => {
    mockUseApiViewSecurity.mockReturnValue({
      roles: undefined,
      resourceServer: undefined,
    });
    open();

    expect(screen.getByText("Orders API")).toBeInTheDocument();
    expect(screen.queryByText(/^aud /)).not.toBeInTheDocument();
    expect(screen.queryByText(/Shopper/)).not.toBeInTheDocument();
  });
});
