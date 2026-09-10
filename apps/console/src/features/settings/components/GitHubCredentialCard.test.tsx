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
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "../../../generated/aep-api";
import { GitHubCredentialCard } from "./GitHubCredentialCard";

type GitProviderProjection = components["schemas"]["GitProviderProjection"];

// Every existing test in this file assumes the card is otherwise operable —
// only the dedicated "no permission" test below flips this to false.
const gitHubConfigPermission = vi.hoisted(() => ({ current: true }));
vi.mock("../../../auth/permissions", () => ({
  useHasPermission: () => gitHubConfigPermission.current,
}));

vi.mock("../api/queries", () => ({
  useConnectGitHubPat: () => ({ mutate: vi.fn(), isPending: false, isError: false, error: null }),
  useDisconnectGitProvider: () => ({ mutate: vi.fn(), isPending: false, isError: false, error: null }),
}));

const connectedProvider: GitProviderProjection = {
  kind: "github",
  mode: "pat",
  status: "connected",
  githubLogin: "acme-dev",
  identityLogin: "acme-dev",
  identityName: "Acme Dev",
  identityEmail: "dev@acme.example",
  connectedAt: "2026-06-01T12:00:00Z",
  lastValidatedAt: "2026-07-01T09:00:00Z",
  selectedRepos: [],
};

function renderCard(gitProvider: GitProviderProjection | null) {
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <GitHubCredentialCard gitProvider={gitProvider} />
    </OxygenUIThemeProvider>,
  );
}

beforeEach(() => {
  gitHubConfigPermission.current = true;
});

describe("GitHubCredentialCard — permission gate", () => {
  it("renders an operable form when the user holds ae:github-config", () => {
    renderCard(null);

    expect(screen.getByLabelText("Personal access token")).toBeEnabled();
    // Regex: MUI's `required` prop appends a visible "*" to the label's text
    // content, so an exact string match against the label alone never hits.
    expect(screen.getByLabelText(/GitHub organization name/)).toBeEnabled();
    expect(screen.getByLabelText("show token")).toBeEnabled();
  });

  it("disables the token/org fields, reveal toggle, and connect/disconnect actions without ae:github-config", () => {
    gitHubConfigPermission.current = false;
    renderCard(connectedProvider);

    expect(screen.getByLabelText("Replace personal access token")).toBeDisabled();
    expect(screen.getByLabelText(/GitHub organization name/)).toBeDisabled();
    expect(screen.getByLabelText("show token")).toBeDisabled();
    expect(screen.getByRole("button", { name: "Replace token" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Disconnect" })).toBeDisabled();
  });

  it("shows no permission banner when the user holds ae:github-config", () => {
    renderCard(connectedProvider);
    expect(
      screen.queryByText("You don't have permission to change these settings."),
    ).not.toBeInTheDocument();
  });

  it("shows a permission banner when the user lacks ae:github-config", () => {
    gitHubConfigPermission.current = false;
    renderCard(connectedProvider);
    expect(
      screen.getByText("You don't have permission to change these settings."),
    ).toBeInTheDocument();
  });
});
