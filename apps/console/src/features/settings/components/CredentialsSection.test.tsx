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
import { CredentialsSection } from "./CredentialsSection";

// Every test but the dedicated "no permission" one below holds at least one
// of the two credentials permissions, so the section reads as reachable —
// mirrors SkillsSection.test.tsx's per-suite permission toggle. CredentialsSection
// itself only cares whether EITHER is held (each card gates its own half).
const heldPermissions = vi.hoisted(() => new Set(["ae:github-config"]));
vi.mock("../../../auth/permissions", () => ({
  useHasAnyPermission: (permissions: string[]) =>
    permissions.some((p) => heldPermissions.has(p)),
}));

// The cards' own rendering (permission gating, connect/disconnect forms) is
// covered by their own test files — stubbed here so this file only tests
// CredentialsSection's own loading/error/no-access states.
vi.mock("./GitHubCredentialCard", () => ({
  GitHubCredentialCard: () => <div data-testid="github-card" />,
}));
vi.mock("./AnthropicCredentialCard", () => ({
  AnthropicCredentialCard: () => <div data-testid="anthropic-card" />,
}));

let configResult: {
  data?: { gitProvider: null; llm: null; codingLlm: null };
  isLoading: boolean;
  isError: boolean;
  error?: Error;
} = { isLoading: false, isError: false };
vi.mock("../api/queries", () => ({
  useConfig: () => configResult,
}));

afterEach(() => {
  heldPermissions.clear();
  heldPermissions.add("ae:github-config");
});

describe("CredentialsSection", () => {
  it("renders both cards when the config loads with either permission held", () => {
    configResult = {
      isLoading: false,
      isError: false,
      data: { gitProvider: null, llm: null, codingLlm: null },
    };
    render(<CredentialsSection />);

    expect(screen.getByTestId("github-card")).toBeInTheDocument();
    expect(screen.getByTestId("anthropic-card")).toBeInTheDocument();
  });

  it("shows a loading spinner while config loads", () => {
    configResult = { isLoading: true, isError: false };
    render(<CredentialsSection />);

    expect(screen.getByRole("progressbar")).toBeInTheDocument();
    expect(screen.queryByTestId("github-card")).not.toBeInTheDocument();
  });

  it("shows the error state with the server detail", () => {
    configResult = {
      isLoading: false,
      isError: true,
      error: new Error("config unavailable"),
    };
    render(<CredentialsSection />);

    expect(screen.getByText("config unavailable")).toBeInTheDocument();
  });

  it("shows an insufficient-permissions message and renders neither card holding neither permission", () => {
    heldPermissions.clear();
    configResult = {
      isLoading: false,
      isError: false,
      data: { gitProvider: null, llm: null, codingLlm: null },
    };
    render(<CredentialsSection />);

    expect(
      screen.getByText("You don't have permission to view credentials."),
    ).toBeInTheDocument();
    expect(screen.queryByTestId("github-card")).not.toBeInTheDocument();
    expect(screen.queryByTestId("anthropic-card")).not.toBeInTheDocument();
  });
});
