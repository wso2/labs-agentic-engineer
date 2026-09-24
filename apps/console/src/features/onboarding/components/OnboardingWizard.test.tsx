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
import {
  aiSettingsFrom,
  aiSettingsPatch,
  draftFrom,
  type AiDraft,
} from "../../settings/aiSettings";

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

// An org whose GitHub step is done and which has no Anthropic key: the model
// and coding agent are what GET /config returns when nobody has chosen.
function config(over: Partial<ConfigProjection> = {}) {
  return {
    gitProvider: github,
    llm: null,
    agents: {
      model: "claude-sonnet-5",
      runtime: "claude-code",
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
const pasteKey = (key: string) =>
  fireEvent.change(screen.getByLabelText("API key"), { target: { value: key } });

/** What the settings card's Save would send for this draft on this config. */
function expectedPatch(c: ConfigProjection, over: Partial<AiDraft>) {
  const saved = aiSettingsFrom(c);
  return aiSettingsPatch(saved, { ...draftFrom(saved), ...over });
}

beforeEach(() => mutate.mockReset());
afterEach(cleanup);

describe("OnboardingWizard's AI agents step", () => {
  it("opens on GitHub for an org with nothing connected", () => {
    renderWizard(config({ gitProvider: null }));
    expect(screen.getByRole("heading", { name: "Connect GitHub" })).toBeInTheDocument();
    expect(screen.queryByRole("combobox", { name: "Model" })).not.toBeInTheDocument();
  });

  it("shows the settings card prefilled with the platform defaults for a new org", () => {
    renderWizard(config());

    expect(screen.getByRole("combobox", { name: "Model" })).toHaveTextContent(
      "Claude Sonnet 5",
    );
    expect(screen.getByRole("radio", { name: /Claude Code/ })).toBeChecked();
    expect(screen.getByText(/Paste your organization's Anthropic API key/)).toBeInTheDocument();
    expect(screen.queryByText(/was disconnected/)).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Disconnect" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Save" })).not.toBeInTheDocument();
  });

  it("shows the card's contents without its frame, header or the key row's explanation", () => {
    renderWizard(config());

    expect(screen.queryByRole("heading", { name: "AI agents" })).not.toBeInTheDocument();
    expect(screen.queryByText("no API key")).not.toBeInTheDocument();
    expect(screen.queryByText(/There is no platform fallback/)).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Anthropic API key" })).toBeInTheDocument();
  });

  it("says the key was disconnected when the org had one", () => {
    renderWizard(config({ llmDisconnectedAt: "2026-09-20T08:00:00Z" }));

    expect(screen.getByText("Your Anthropic key was disconnected")).toBeInTheDocument();
    expect(screen.getByLabelText("API key")).toBeInTheDocument();
  });

  it("requires the API key: a model change alone cannot continue", () => {
    renderWizard(config());
    expect(continueButton()).toBeDisabled();

    fireEvent.mouseDown(screen.getByRole("combobox", { name: "Model" }));
    fireEvent.click(screen.getByRole("option", { name: "Claude Haiku 4.5" }));
    expect(continueButton()).toBeDisabled();
  });

  it("saves only the key when the defaults are kept, through the settings card's save", () => {
    const c = config();
    renderWizard(c);

    pasteKey("sk-ant-api03-new-key-1234");
    fireEvent.click(continueButton());

    expect(mutate).toHaveBeenCalledTimes(1);
    const patch = mutate.mock.calls[0]?.[0] as unknown;
    expect(patch).toEqual(expectedPatch(c, { apiKey: "sk-ant-api03-new-key-1234" }));
    // Nothing but the key moved, so the org keeps the platform defaults the
    // server already holds rather than restating them.
    expect(JSON.stringify(patch)).toContain("sk-ant-api03-new-key-1234");
    expect(JSON.stringify(patch)).not.toContain("claude-sonnet-5");
  });

  it("saves a changed model with the key in the same save", () => {
    const c = config();
    renderWizard(c);

    fireEvent.mouseDown(screen.getByRole("combobox", { name: "Model" }));
    fireEvent.click(screen.getByRole("option", { name: "Claude Haiku 4.5" }));
    pasteKey("sk-ant-api03-new-key-1234");
    fireEvent.click(continueButton());

    expect(mutate).toHaveBeenCalledTimes(1);
    const patch = mutate.mock.calls[0]?.[0] as unknown;
    expect(patch).toEqual(
      expectedPatch(c, { apiKey: "sk-ant-api03-new-key-1234", model: "claude-haiku-4-5" }),
    );
    expect(JSON.stringify(patch)).toContain("claude-haiku-4-5");
    expect(JSON.stringify(patch)).toContain("sk-ant-api03-new-key-1234");
  });
});
