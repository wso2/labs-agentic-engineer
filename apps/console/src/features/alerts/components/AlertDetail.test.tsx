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
import { AlertDetail } from "./AlertDetail";

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
  Link: ({ children, ...rest }: Record<string, unknown> & { children?: React.ReactNode }) => (
    <a {...rest}>{children}</a>
  ),
}));

// Every test but the dedicated "no permission" one below holds
// ae:observability-view, so the page reads as reachable by default.
const hasObservabilityAccess = vi.hoisted(() => ({ current: true }));
vi.mock("../../../auth/permissions", () => ({
  useHasPermission: () => hasObservabilityAccess.current,
}));

let reportResult: {
  data?: {
    title?: string;
    project?: string;
    classification?: string;
    diagnosis?: string;
    issueNumber?: number;
    dispatched?: boolean;
    deployed?: boolean;
  };
  isPending: boolean;
  isError: boolean;
  error?: Error;
} = { isPending: false, isError: false };
vi.mock("../api/queries", () => ({
  useAlertReport: () => reportResult,
}));

afterEach(() => {
  hasObservabilityAccess.current = true;
});

describe("AlertDetail", () => {
  it("shows a loading spinner while the report loads", () => {
    reportResult = { isPending: true, isError: false };
    render(<AlertDetail alertId="r1" />);
    expect(screen.getByRole("progressbar")).toBeInTheDocument();
  });

  it("shows the error state with the server detail", () => {
    reportResult = {
      isPending: false,
      isError: true,
      error: new Error("report not found"),
    };
    render(<AlertDetail alertId="r1" />);
    expect(
      screen.getByText(/Failed to load this alert: report not found/),
    ).toBeInTheDocument();
  });

  it("renders the stepper once the report loads", () => {
    reportResult = {
      isPending: false,
      isError: false,
      data: {
        title: "Payments 5xx spike",
        project: "storefront",
        classification: "code-fix",
        diagnosis: "Elevated 5xx on /checkout",
      },
    };
    render(<AlertDetail alertId="r1" />);
    expect(screen.getByText("Alert Received")).toBeInTheDocument();
    expect(screen.getByText("Issue Created")).toBeInTheDocument();
  });

  it("shows an insufficient-permissions message and renders no report content without ae:observability-view", () => {
    hasObservabilityAccess.current = false;
    reportResult = {
      isPending: false,
      isError: false,
      data: { title: "Payments 5xx spike", diagnosis: "Elevated 5xx" },
    };
    render(<AlertDetail alertId="r1" />);

    expect(
      screen.getByText("You don't have permission to view alerts."),
    ).toBeInTheDocument();
    expect(screen.queryByText("Alert Received")).not.toBeInTheDocument();
  });
});
