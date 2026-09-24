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
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import type { components } from "../../../generated/aep-api";
import { ApiRequestError } from "../../../api/errors";

type ConfigProjection = components["schemas"]["ConfigProjection"];
type LLMProjection = components["schemas"]["LLMProjection"];
type SubscriptionProjection = components["schemas"]["SubscriptionProjection"];

const mutate = vi.fn();
const saveState: { isPending: boolean; isError: boolean; error: Error | null } = {
  isPending: false,
  isError: false,
  error: null,
};

vi.mock("../api/queries", () => ({
  useSaveAiSettings: () => ({ mutate, reset: vi.fn(), ...saveState }),
}));

const { AiAgentsCard } = await import("./AiAgentsCard");

const apiKey: LLMProjection = {
  kind: "anthropic",
  credentialKind: "api_key",
  status: "connected",
  keyPrefix: "sk-ant-api03-",
  keyLast4: "wxyz",
  connectedAt: "2026-06-01T12:05:00Z",
};

const subscription: SubscriptionProjection = {
  kind: "claude",
  status: "connected",
  keyPrefix: "sk-ant-oat01-",
  keyLast4: "9f2c",
  connectedAt: "2026-09-01T10:00:00Z",
};

const defaultAgents: ConfigProjection["agents"] = {
  runtime: "claude-code",
  model: "claude-sonnet-5",
  subscription: null,
  updatedAt: null,
  updatedBy: null,
};

/** An org on the defaults with a stored Claude subscription. */
const withSubscription = { agents: { ...defaultAgents, subscription } };

function config(over: Partial<ConfigProjection> = {}): ConfigProjection {
  return {
    llm: apiKey,
    agents: defaultAgents,
    gitProvider: null,
    idp: {
      kind: "platform",
      issuer: "https://idp.aep.local",
      jwksUrl: "https://idp.aep.local/.well-known/jwks.json",
      hasClientSecret: false,
      publisherClientId: "aep-console",
    },
    ...over,
  };
}

function renderCard(c: ConfigProjection = config()) {
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <AiAgentsCard config={c} />
    </OxygenUIThemeProvider>,
  );
}

const saveButton = () => screen.getByRole("button", { name: "Save" });
const subscriptionSwitch = () =>
  screen.getByLabelText("Bill coding to a Claude subscription");
const radio = (name: RegExp) => screen.getByRole("radio", { name });
const lastPatch = () => mutate.mock.calls.at(-1)?.[0] as unknown;

function chooseModel(label: string) {
  fireEvent.mouseDown(screen.getByRole("combobox", { name: "Model" }));
  fireEvent.click(screen.getByRole("option", { name: label }));
}

beforeEach(() => {
  mutate.mockReset();
  saveState.isPending = false;
  saveState.isError = false;
  saveState.error = null;
});
afterEach(cleanup);

describe("AiAgentsCard", () => {
  it("renders one card: model, then the API key, then the coding agent", () => {
    renderCard();

    expect(screen.getByRole("heading", { name: "AI agents" })).toBeInTheDocument();
    const headings = screen
      .getAllByRole("heading", { level: 3 })
      .map((h) => h.textContent);
    expect(headings).toEqual(["Anthropic API key", "Coding agent"]);
    expect(screen.getByRole("combobox", { name: "Model" })).toHaveTextContent(
      "Claude Sonnet 5",
    );
    expect(screen.getByText(/Every agent uses this model/)).toBeInTheDocument();
    expect(radio(/Claude Code/)).toBeChecked();
    expect(radio(/OpenCode/)).not.toBeChecked();
  });

  it("says who last changed the settings, and nothing before anyone has", () => {
    renderCard(
      config({
        agents: { ...defaultAgents, updatedAt: null, updatedBy: "alice@acme.test" },
      }),
    );
    expect(screen.getByText("Changed by alice@acme.test")).toBeInTheDocument();
    cleanup();

    renderCard();
    expect(screen.queryByText(/^Changed/)).not.toBeInTheDocument();
    expect(screen.queryByText(/platform's defaults/)).not.toBeInTheDocument();
  });

  it("offers no separate coding API key", () => {
    renderCard();
    expect(screen.queryByText(/separate key/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/Reuse the organization/)).not.toBeInTheDocument();
  });

  it("saves nothing until something changes", () => {
    renderCard();
    expect(saveButton()).toBeDisabled();
  });

  it("sends only the field that moved", () => {
    renderCard();
    chooseModel("Claude Haiku 4.5");
    fireEvent.click(saveButton());

    expect(mutate).toHaveBeenCalledTimes(1);
    expect(lastPatch()).toEqual({ agents: { model: "claude-haiku-4-5" } });
  });

  it("sends a new key, model and coding agent in one save", () => {
    renderCard();
    chooseModel("Claude Haiku 4.5");
    fireEvent.click(screen.getByRole("button", { name: "Replace" }));
    fireEvent.change(screen.getByLabelText("New API key"), {
      target: { value: "sk-ant-api03-new" },
    });
    fireEvent.click(radio(/OpenCode/));
    fireEvent.click(saveButton());

    expect(mutate).toHaveBeenCalledTimes(1);
    expect(lastPatch()).toEqual({
      llm: { kind: "anthropic", apiKey: "sk-ant-api03-new" },
      agents: { model: "claude-haiku-4-5", runtime: "opencode" },
    });
  });

  describe("with no API key", () => {
    it("asks for the key and holds back the subscription", () => {
      renderCard(config({ llm: null }));

      expect(screen.getByText("no API key")).toBeInTheDocument();
      expect(screen.getByText(/There is no platform fallback/)).toBeInTheDocument();
      expect(screen.getByLabelText("API key")).toBeInTheDocument();
      expect(subscriptionSwitch()).toBeDisabled();
      expect(screen.getByText("Add the Anthropic API key first.")).toBeInTheDocument();
    });

    it("lets a key and a subscription token go out together", () => {
      renderCard(config({ llm: null }));
      fireEvent.change(screen.getByLabelText("API key"), {
        target: { value: "sk-ant-api03-first" },
      });
      fireEvent.click(subscriptionSwitch());
      fireEvent.change(screen.getByLabelText("Subscription token"), {
        target: { value: "sk-ant-oat01-token" },
      });
      fireEvent.click(saveButton());

      expect(lastPatch()).toEqual({
        llm: { kind: "anthropic", apiKey: "sk-ant-api03-first" },
        agents: { subscription: { kind: "claude", token: "sk-ant-oat01-token" } },
      });
    });

    it("keeps the key and token out of the browser's password manager", () => {
      renderCard(config({ llm: null }));
      fireEvent.change(screen.getByLabelText("API key"), {
        target: { value: "sk-ant-api03-first" },
      });
      fireEvent.click(subscriptionSwitch());

      for (const label of ["API key", "Subscription token"]) {
        expect(screen.getByLabelText(label)).toHaveAttribute("autocomplete", "new-password");
      }
    });
  });

  describe("Claude subscription", () => {
    it("needs a token before it can be saved", () => {
      renderCard();
      fireEvent.click(subscriptionSwitch());
      expect(saveButton()).toBeDisabled();
    });

    it("refuses an API key pasted as the token", () => {
      renderCard();
      fireEvent.click(subscriptionSwitch());
      fireEvent.change(screen.getByLabelText("Subscription token"), {
        target: { value: "sk-ant-api03-notatoken" },
      });

      expect(screen.getByText(/not a Claude subscription token/)).toBeInTheDocument();
      expect(saveButton()).toBeDisabled();
    });

    it("sends the token as the subscription", () => {
      renderCard();
      fireEvent.click(subscriptionSwitch());
      fireEvent.change(screen.getByLabelText("Subscription token"), {
        target: { value: " sk-ant-oat01-token " },
      });
      fireEvent.click(saveButton());

      expect(lastPatch()).toEqual({
        agents: { subscription: { kind: "claude", token: "sk-ant-oat01-token" } },
      });
    });

    it("shows a stored token masked, with Replace", () => {
      renderCard(config(withSubscription));

      expect(subscriptionSwitch()).toBeChecked();
      expect(screen.getByText("sk-ant-oat01-•••••••••9f2c")).toBeInTheDocument();
      expect(screen.queryByLabelText(/subscription token/i)).not.toBeInTheDocument();

      const tile = screen.getByText("sk-ant-oat01-•••••••••9f2c").parentElement!;
      fireEvent.click(within(tile).getByRole("button", { name: "Replace" }));
      fireEvent.change(screen.getByLabelText("New subscription token"), {
        target: { value: "sk-ant-oat01-rotated" },
      });
      fireEvent.click(saveButton());

      expect(lastPatch()).toEqual({
        agents: { subscription: { kind: "claude", token: "sk-ant-oat01-rotated" } },
      });
    });

    it("warns, then deletes the token, when the switch is turned off", () => {
      renderCard(config(withSubscription));
      fireEvent.click(subscriptionSwitch());

      expect(
        screen.getByText("Saving deletes the stored Claude subscription token."),
      ).toBeInTheDocument();
      fireEvent.click(saveButton());
      expect(lastPatch()).toEqual({ agents: { subscription: null } });
    });
  });

  describe("choosing OpenCode", () => {
    it("says, before saving, that the stored token will be deleted", () => {
      renderCard(config(withSubscription));
      expect(screen.queryByText(/OpenCode bills coding/)).not.toBeInTheDocument();

      fireEvent.click(radio(/OpenCode/));

      expect(screen.getByText(/Saving deletes the stored Claude subscription token/))
        .toBeInTheDocument();
      expect(screen.queryByLabelText("Bill coding to a Claude subscription"))
        .not.toBeInTheDocument();
    });

    it("deletes the token in the same save as the runtime", () => {
      renderCard(config(withSubscription));
      fireEvent.click(radio(/OpenCode/));
      fireEvent.click(saveButton());

      expect(mutate).toHaveBeenCalledTimes(1);
      expect(lastPatch()).toEqual({
        agents: { runtime: "opencode", subscription: null },
      });
    });

    it("keeps the token when the reader switches back before saving", () => {
      renderCard(config(withSubscription));
      fireEvent.click(radio(/OpenCode/));
      fireEvent.click(radio(/Claude Code/));

      expect(subscriptionSwitch()).toBeChecked();
      expect(saveButton()).toBeDisabled();
    });
  });

  describe("a refused save", () => {
    it("shows a rejected API key on the key field", () => {
      saveState.isError = true;
      saveState.error = new ApiRequestError(
        {
          code: "validation_failed",
          message: "the provided API key was rejected by Anthropic",
          details: [{ field: "body.llm", message: "rejected" }],
        },
        "fallback",
      );
      renderCard(config({ llm: null }));

      expect(screen.getByLabelText("API key")).toHaveAccessibleDescription(
        "the provided API key was rejected by Anthropic",
      );
    });

    it("shows a rejected token on the token field", () => {
      saveState.isError = true;
      saveState.error = new ApiRequestError(
        {
          code: "validation_failed",
          message: "the token was rejected by Anthropic",
          details: [{ field: "body.agents", message: "rejected" }],
        },
        "fallback",
      );
      renderCard();
      fireEvent.click(subscriptionSwitch());

      expect(screen.getByLabelText("Subscription token")).toHaveAccessibleDescription(
        "the token was rejected by Anthropic",
      );
    });

    it("shows anything else on the card", () => {
      saveState.isError = true;
      saveState.error = new Error("Failed to save the AI settings");
      renderCard();
      expect(screen.getByRole("alert")).toHaveTextContent(
        "Failed to save the AI settings",
      );
    });
  });

  it("disconnects the key on its own, naming the subscription it takes along", () => {
    renderCard(config(withSubscription));
    fireEvent.click(screen.getByRole("button", { name: "Disconnect" }));

    const dialog = screen.getByRole("dialog");
    expect(dialog).toHaveTextContent(
      "The stored Claude subscription token is deleted as well.",
    );
    fireEvent.click(within(dialog).getByRole("button", { name: "Disconnect" }));
    expect(lastPatch()).toEqual({ llm: null });
  });

  it("disables the controls while a save is in flight", () => {
    saveState.isPending = true;
    renderCard();

    expect(screen.getByRole("button", { name: "Saving…" })).toBeDisabled();
    expect(radio(/OpenCode/)).toBeDisabled();
  });
});
