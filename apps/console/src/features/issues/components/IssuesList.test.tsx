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
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { IssueInfo } from "../api/queries";

let mockIssues: IssueInfo[] = [];
let mockPending = false;
let mockError: Error | null = null;

vi.mock("../api/queries", () => ({
  useProjectIssues: () => ({
    data: mockIssues,
    isPending: mockPending,
    isError: Boolean(mockError),
    error: mockError,
    refetch: vi.fn(),
  }),
}));

import { IssuesList } from "./IssuesList";

beforeEach(() => {
  mockPending = false;
  mockError = null;
  mockIssues = [
    {
      Number: 42,
      Title: "checkout-service returns 500",
      Body: "The SRE agent found a code-level issue.",
      URL: "https://github.com/acme/shop/issues/42",
      State: "open",
      Labels: ["bug", "incident", "aep"],
    },
    {
      Number: 43,
      Title: "inventory-worker fix needs verification",
      Body: "The coding agent left this open.",
      URL: "https://github.com/acme/shop/issues/43",
      State: "open",
      Labels: ["bug", "incident"],
      attentionReason: "unverified_fix",
    },
    {
      Number: 44,
      Title: "auth-service timeout recurred",
      Body: "Repeated recurrence.",
      URL: "https://github.com/acme/shop/issues/44",
      State: "open",
      Labels: ["bug", "incident"],
      attentionReason: "escalated",
    },
  ];
});

describe("IssuesList", () => {
  it("renders GitHub issue rows and labels", () => {
    render(<IssuesList projectName="shop" />);

    expect(screen.getByText("#42 checkout-service returns 500")).toBeInTheDocument();
    expect(screen.getAllByText("bug")).toHaveLength(3);
    expect(screen.getAllByRole("link", { name: /View on GitHub/i })[0]).toHaveAttribute(
      "href",
      "https://github.com/acme/shop/issues/42",
    );
  });

  it("renders server-provided attention reasons without deriving policy", () => {
    render(<IssuesList projectName="shop" />);

    expect(screen.getByText(/Needs review:/)).toBeInTheDocument();
    expect(screen.getByText(/not confident enough to close/i)).toBeInTheDocument();
    expect(screen.getByText(/Escalated:/)).toBeInTheDocument();
    expect(screen.getByText(/recurred repeatedly/i)).toBeInTheDocument();
  });

  it("renders empty and error states", () => {
    mockIssues = [];
    const { rerender } = render(<IssuesList projectName="shop" />);
    expect(screen.getByText("No issues yet")).toBeInTheDocument();

    mockError = new Error("boom");
    rerender(<IssuesList projectName="shop" />);
    expect(screen.getByText(/Failed to load issues: boom/)).toBeInTheDocument();
  });
});
