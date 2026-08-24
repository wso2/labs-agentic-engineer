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
import type { components } from "../../../generated/aep-api";
import { EndpointsPage } from "./EndpointsPage";

type OrgEndpointDTO = components["schemas"]["OrgEndpointDTO"];

const navigate = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigate,
}));

let queryState: {
  data?: OrgEndpointDTO[];
  isPending: boolean;
  isError: boolean;
  error?: Error | null;
  refetch: ReturnType<typeof vi.fn>;
} = { isPending: false, isError: false, refetch: vi.fn() };

vi.mock("../api/queries", () => ({
  useOrgEndpoints: () => queryState,
}));

function resetState() {
  queryState = {
    data: [],
    isPending: false,
    isError: false,
    refetch: vi.fn(),
  };
  navigate.mockClear();
}

describe("EndpointsPage", () => {
  it("shows a loading indicator", () => {
    resetState();
    queryState = { isPending: true, isError: false, refetch: vi.fn() };

    render(<EndpointsPage />);

    expect(screen.getByLabelText("Loading endpoints")).toBeInTheDocument();
  });

  it("shows empty copy with no Create control", () => {
    resetState();
    queryState = { data: [], isPending: false, isError: false, refetch: vi.fn() };

    render(<EndpointsPage />);

    expect(screen.getByText("No Marketplace Endpoints yet")).toBeInTheDocument();
    expect(
      screen.getByText(
        "Offering a new API happens inside a project. When a component is published as an org-service, it appears here.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /create|register/i }),
    ).not.toBeInTheDocument();
  });

  it("shows error with retry", () => {
    resetState();
    const refetch = vi.fn();
    queryState = {
      isPending: false,
      isError: true,
      error: new Error("boom"),
      refetch,
    };

    render(<EndpointsPage />);

    fireEvent.click(screen.getByRole("button", { name: /retry/i }));
    expect(refetch).toHaveBeenCalled();
  });

  it("navigates to the provider project overview on row click", () => {
    resetState();
    queryState = {
      isPending: false,
      isError: false,
      refetch: vi.fn(),
      data: [
        {
          name: "invoice-api",
          project: "billing",
          endpoint: "rest",
          type: "HTTP",
          namespaceVisible: true,
        },
        {
          name: "payments-api",
          project: "billing",
          endpoint: "grpc",
          type: "gRPC",
          namespaceVisible: true,
        },
      ],
    };

    render(<EndpointsPage />);

    fireEvent.click(screen.getByText("invoice-api"));
    expect(navigate).toHaveBeenCalledWith({
      to: "/projects/$projectName",
      params: { projectName: "billing" },
    });
  });
});
