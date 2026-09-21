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

import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AlertsList } from "./AlertsList";

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
}));

// Every test but the dedicated "no permission" one below holds
// ae:observability-view, so the page reads as reachable by default.
const hasObservabilityAccess = vi.hoisted(() => ({ current: true }));
vi.mock("../../../auth/permissions", () => ({
  useHasPermission: () => hasObservabilityAccess.current,
}));

let alertsResult: {
  data?: { pages: { items?: unknown[]; nextCursor?: string | null }[] };
  isPending: boolean;
  isError: boolean;
  error?: Error;
  hasNextPage?: boolean;
  isFetchingNextPage?: boolean;
} = { isPending: false, isError: false };
vi.mock("../api/queries", () => ({
  useAlertsInfinite: () => alertsResult,
}));

afterEach(() => {
  hasObservabilityAccess.current = true;
});

describe("AlertsList", () => {
  it("shows a loading spinner while the first page loads", () => {
    alertsResult = { isPending: true, isError: false };
    render(<AlertsList />);
    expect(screen.getByRole("progressbar")).toBeInTheDocument();
  });

  it("shows the error state with the server detail", () => {
    alertsResult = {
      isPending: false,
      isError: true,
      error: new Error("rca service unavailable"),
    };
    render(<AlertsList />);
    expect(
      screen.getByText(/Failed to load alerts: rca service unavailable/),
    ).toBeInTheDocument();
  });

  it("shows the empty state when there are no reports", () => {
    alertsResult = {
      isPending: false,
      isError: false,
      data: { pages: [{ items: [] }] },
    };
    render(<AlertsList />);
    expect(screen.getByText("No alerts yet")).toBeInTheDocument();
  });

  it("lists reports when the page loads", () => {
    alertsResult = {
      isPending: false,
      isError: false,
      data: {
        pages: [
          {
            items: [
              {
                id: "r1",
                title: "Payments 5xx spike",
                summary: "Elevated error rate on checkout",
                project: "storefront",
                classification: "code-fix",
                createdAt: "2026-09-01T00:00:00Z",
              },
            ],
          },
        ],
      },
    };
    render(<AlertsList />);
    expect(screen.getByText("Payments 5xx spike")).toBeInTheDocument();
  });

  it("shows an insufficient-permissions message and renders no alert content without ae:observability-view", () => {
    hasObservabilityAccess.current = false;
    // A direct-URL visit must never flash real data even if a prior fetch
    // cached it — the mocked result deliberately carries data here.
    alertsResult = {
      isPending: false,
      isError: false,
      data: {
        pages: [
          {
            items: [
              {
                id: "r1",
                title: "Payments 5xx spike",
                summary: "Elevated error rate",
                project: "storefront",
                createdAt: "2026-09-01T00:00:00Z",
              },
            ],
          },
        ],
      },
    };
    render(<AlertsList />);

    expect(
      screen.getByText("You don't have permission to view alerts."),
    ).toBeInTheDocument();
    expect(screen.queryByText("Payments 5xx spike")).not.toBeInTheDocument();
  });
});
