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
// only the dedicated "no permission" tests below flip this to false.
const gitHubConfigPermission = vi.hoisted(() => ({ current: true }));
vi.mock("../../../auth/permissions", () => ({
  useHasPermission: () => gitHubConfigPermission.current,
}));

vi.mock("../../../components/NoPermissionIllustration", () => ({
  NoPermissionIllustration: () => <svg data-testid="no-permission-illustration" />,
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

  // Without ae:github-config, nothing about the connection — connected or
  // not — is shown at all, not merely disabled: the form, the org/identity
  // info, and the connect/disconnect actions are all absent.
  it("shows no GitHub info at all without ae:github-config, connected", () => {
    gitHubConfigPermission.current = false;
    renderCard(connectedProvider);

    expect(screen.queryByLabelText(/personal access token/i)).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/GitHub organization name/)).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Replace token" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Disconnect" })).not.toBeInTheDocument();
    expect(screen.queryByText("acme-dev")).not.toBeInTheDocument();
    expect(screen.queryByText(/Connected as/)).not.toBeInTheDocument();
  });

  it("shows no GitHub info at all without ae:github-config, not connected", () => {
    gitHubConfigPermission.current = false;
    renderCard(null);

    expect(screen.queryByLabelText("Personal access token")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Connect" })).not.toBeInTheDocument();
  });

  it("shows the no-permission illustration and message instead", () => {
    gitHubConfigPermission.current = false;
    renderCard(connectedProvider);

    expect(screen.getByTestId("no-permission-illustration")).toBeInTheDocument();
    expect(
      screen.getByText("You don't have permission to view GitHub settings."),
    ).toBeInTheDocument();
  });

  it("still shows the GitHub header even when denied", () => {
    gitHubConfigPermission.current = false;
    renderCard(connectedProvider);
    expect(screen.getByText("GitHub")).toBeInTheDocument();
    // The status chip reveals whether GitHub is connected — itself
    // information a denied caller should not see.
    expect(screen.queryByText("connected")).not.toBeInTheDocument();
    expect(screen.queryByText("not connected")).not.toBeInTheDocument();
  });
});
