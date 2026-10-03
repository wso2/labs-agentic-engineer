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

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import type { components } from "../../../generated/aep-api";
type ConfigProjection = components["schemas"]["ConfigProjection"];

const mutate = vi.fn();

vi.mock("../../settings/api/queries", () => ({
  useSaveAiSettings: () => ({
    mutate,
    reset: vi.fn(),
    isPending: false,
    isError: false,
    error: null,
  }),
  useTestConnection: () => ({ mutate: vi.fn(), reset: vi.fn(), isPending: false, isError: false, error: null }),
  useConnectGitHubPat: () => ({ mutate: vi.fn(), isPending: false, isError: false }),
  useSyncSkills: () => ({ mutate: vi.fn(), isPending: false, isError: false }),
}));

vi.mock("../../../auth/SessionContext", () => ({
  useSession: () => ({
    user: { name: "Dev", email: "dev@acme.example" },
    orgHandle: "acme",
    signOut: vi.fn(),
  }),
}));

const { OnboardingWizard } = await import("./OnboardingWizard");

const github: ConfigProjection["gitProvider"] = {
  kind: "github",
  mode: "pat",
  status: "connected",
  githubLogin: "acme-dev",
  connectedAt: "2026-06-01T12:00:00Z",
};

// An org whose GitHub step is done and which has no model connection: the
// coding agent is what GET /config returns when nobody has chosen.
function config(over: Partial<ConfigProjection> = {}) {
  return {
    gitProvider: github,
    llm: null,
    llmFormats: [
      {
        kind: "anthropic",
        defaultBaseURL: "https://api.anthropic.com/v1",
        defaultModel: "claude-sonnet-5",
        runtimes: ["claude-code", "opencode"],
      },
      { kind: "openai-compatible", defaultBaseURL: null, defaultModel: "glm-5.3", runtimes: ["opencode"] },
    ],
    agents: {
      runtime: "claude-code",
      availableRuntimes: ["claude-code", "opencode"],
      subscription: null,
      updatedAt: null,
      updatedBy: null,
    },
    idp: {
      kind: "platform",
      issuer: "https://idp.aep.local",
      jwksUrl: "https://idp.aep.local/.well-known/jwks.json",
      hasClientSecret: false,
      publisherClientId: "aep-console",
    },
    ...over,
  } satisfies ConfigProjection;
}

function renderWizard(c: ConfigProjection) {
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <OnboardingWizard config={c} onComplete={vi.fn()} />
    </OxygenUIThemeProvider>,
  );
}

const continueButton = () => screen.getByRole("button", { name: "Continue" });
const type = (label: string, value: string) =>
  fireEvent.change(screen.getByLabelText(label), { target: { value } });

beforeEach(() => mutate.mockReset());
afterEach(cleanup);

describe("OnboardingWizard's Connect a model step", () => {
  it("opens on GitHub for an org with nothing connected", () => {
    renderWizard(config({ gitProvider: null }));
    expect(screen.getByRole("heading", { name: "Connect GitHub" })).toBeInTheDocument();
    expect(screen.queryByLabelText("Base URL")).not.toBeInTheDocument();
  });

  it("resumes at step 2 for an org with GitHub and no connection", () => {
    renderWizard(config());
    expect(screen.getByText("Connect a model")).toBeInTheDocument();
    expect(screen.queryByText("Set up AI agents")).not.toBeInTheDocument();
    expect(screen.getByText(/Connect the model your agents will use/)).toBeInTheDocument();
    expect(screen.getByLabelText("Base URL")).toHaveValue("https://api.anthropic.com/v1");
    expect(screen.getByLabelText("Model")).toHaveValue("claude-sonnet-5");
    expect(screen.getByRole("radio", { name: /Claude Code/ })).toBeChecked();
    expect(screen.queryByText(/was disconnected/)).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Save" })).not.toBeInTheDocument();
  });

  it("shows the card's contents without its frame, header or footer", () => {
    renderWizard(config());
    expect(screen.queryByRole("heading", { name: "AI agents" })).not.toBeInTheDocument();
    expect(screen.queryByText("not connected")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Discard changes" })).not.toBeInTheDocument();
  });

  it("says the connection was disconnected when the org had one", () => {
    renderWizard(config({ llmDisconnectedAt: "2026-09-20T08:00:00Z" }));
    expect(screen.getByText("Your model connection was disconnected")).toBeInTheDocument();
    expect(screen.getByText("Agents cannot run until a connection is saved.")).toBeInTheDocument();
    expect(screen.getByLabelText("API key")).toBeInTheDocument();
  });

  it("holds Continue until the draft has a key", () => {
    renderWizard(config());
    expect(continueButton()).toBeDisabled();
    type("Model", "claude-haiku-4-5");
    expect(continueButton()).toBeDisabled();
  });

  it("Continue saves the whole connection on Anthropic's defaults", () => {
    renderWizard(config());
    type("API key", "sk-ant-api03-new-key-1234");
    fireEvent.click(continueButton());

    expect(mutate).toHaveBeenCalledTimes(1);
    expect(mutate.mock.calls[0]?.[0]).toEqual({
      llm: {
        kind: "anthropic",
        baseURL: "https://api.anthropic.com/v1",
        model: "claude-sonnet-5",
        apiKey: "sk-ant-api03-new-key-1234",
      },
    });
  });

  it("Continue saves an Ollama connection with OpenCode in one save", () => {
    renderWizard(config());
    fireEvent.click(screen.getByRole("button", { name: "OpenAI-compatible" }));
    type("Base URL", "https://ollama.com/v1");
    type("API key", "ollama-key-0123456789");
    fireEvent.click(continueButton());

    expect(mutate.mock.calls[0]?.[0]).toEqual({
      llm: { kind: "openai-compatible", baseURL: "https://ollama.com/v1", model: "glm-5.3", apiKey: "ollama-key-0123456789" },
      agents: { runtime: "opencode" },
    });
  });

  it("moves to skills once the connection is saved", () => {
    renderWizard(
      config({
        llm: {
          kind: "anthropic",
          baseURL: "https://api.anthropic.com/v1",
          model: "claude-sonnet-5",
          keyPreview: "wxyz",
          connectedAt: "2026-09-26T08:00:00Z",
          updatedAt: "2026-09-26T08:00:00Z",
          updatedBy: "dev@acme.example",
          priced: true,
          capabilities: {
            claudeSubscription: true,
            webSearch: "anthropic-server-tool",
            imageInput: "yes",
            nativePdf: true,
            generatedAgents: true,
          },
        },
      }),
    );
    expect(screen.queryByLabelText("Base URL")).not.toBeInTheDocument();
  });
});
