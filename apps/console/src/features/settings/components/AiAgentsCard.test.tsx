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
type LLMCheck = components["schemas"]["LLMCheck"];
type SubscriptionProjection = components["schemas"]["SubscriptionProjection"];

const mutate = vi.fn();
const testMutate = vi.fn();
const saveState: { isPending: boolean; isError: boolean; error: Error | null } = {
  isPending: false,
  isError: false,
  error: null,
};
const testState: { isPending: boolean; isError: boolean; error: Error | null } = {
  isPending: false,
  isError: false,
  error: null,
};

vi.mock("../api/queries", () => ({
  useSaveAiSettings: () => ({ mutate, reset: vi.fn(), ...saveState }),
  useTestConnection: () => ({ mutate: testMutate, reset: vi.fn(), ...testState }),
  // The SRE agent model row mounts unconditionally (it decides its own
  // hidden state from `config.sreAgent`); every fixture here leaves it null.
  useSaveSreModel: () => ({ mutate: vi.fn(), reset: vi.fn(), isPending: false, isError: false, error: null }),
  useClearSreModel: () => ({ mutate: vi.fn(), reset: vi.fn(), isPending: false, isError: false, error: null }),
}));

const { AiAgentsCard } = await import("./AiAgentsCard");

const anthropic: LLMProjection = {
  kind: "anthropic",
  baseURL: "https://api.anthropic.com/v1",
  model: "claude-sonnet-5",
  keyPreview: "sk-a…wxyz",
  connectedAt: "2026-06-01T12:05:00Z",
  updatedAt: "2026-09-25T13:53:00Z",
  updatedBy: "dev@acme.example",
  priced: true,
  capabilities: {
    claudeSubscription: true,
    webSearch: "anthropic-server-tool",
    imageInput: "yes",
    nativePdf: true,
    generatedAgents: true,
    sreAgent: false,
  },
};

const ollama: LLMProjection = {
  kind: "openai-compatible",
  baseURL: "https://ollama.com/v1",
  model: "glm-5.3",
  keyPreview: "3f9a…c2d1",
  connectedAt: "2026-09-25T13:50:00Z",
  updatedAt: "2026-09-25T13:53:00Z",
  updatedBy: "dev@acme.example",
  priced: false,
  capabilities: {
    claudeSubscription: false,
    webSearch: "ollama-api",
    imageInput: "no",
    nativePdf: false,
    generatedAgents: true,
    sreAgent: false,
  },
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
  availableRuntimes: ["claude-code", "opencode"],
  subscription: null,
  updatedAt: null,
  updatedBy: null,
};

const formats: ConfigProjection["llmFormats"] = [
  {
    kind: "anthropic",
    defaultBaseURL: "https://api.anthropic.com/v1",
    defaultModel: "claude-sonnet-5",
    runtimes: ["claude-code", "opencode"],
  },
  { kind: "openai-compatible", defaultBaseURL: null, defaultModel: "glm-5.3", runtimes: ["opencode"] },
];

function config(over: Partial<ConfigProjection> = {}): ConfigProjection {
  return {
    llm: anthropic,
    llmFormats: formats,
    agents: defaultAgents,
    gitProvider: null,
    idp: {
      kind: "platform",
      issuer: "https://idp.aep.local",
      jwksUrl: "https://idp.aep.local/.well-known/jwks.json",
      hasClientSecret: false,
      publisherClientId: "aep-console",
    },
    sreLlm: null,
    sreAgent: null,
    ...over,
  };
}

const onOllama = () =>
  config({ llm: ollama, agents: { ...defaultAgents, runtime: "opencode" } });

function renderCard(c: ConfigProjection = config()) {
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <AiAgentsCard config={c} />
    </OxygenUIThemeProvider>,
  );
}

const saveButton = () => screen.getByRole("button", { name: "Save" });
const testButton = () => screen.getByRole("button", { name: "Test connection" });
const radio = (name: RegExp) => screen.getByRole("radio", { name });
const format = (name: string) => screen.getByRole("button", { name });
const field = (label: string) => screen.getByLabelText(label);
const type = (label: string, value: string) => fireEvent.change(field(label), { target: { value } });
const info = () => within(screen.getByRole("list", { name: "What this connection means" }));
const lastPatch = () => mutate.mock.calls.at(-1)?.[0] as unknown;

function refused(code: string, section: "llm" | "agents", message: string) {
  return new ApiRequestError({ code, message, details: [{ field: `body.${section}`, message }] }, "x");
}

beforeEach(() => {
  mutate.mockReset();
  testMutate.mockReset();
  Object.assign(saveState, { isPending: false, isError: false, error: null });
  Object.assign(testState, { isPending: false, isError: false, error: null });
});
afterEach(cleanup);

describe("AiAgentsCard on Anthropic's API", () => {
  it("renders the connection, then the coding agent", () => {
    renderCard();

    expect(screen.getByRole("heading", { name: "AI agents" })).toBeInTheDocument();
    expect(screen.getByText("ready")).toBeInTheDocument();
    expect(screen.getAllByRole("heading", { level: 3 }).map((h) => h.textContent)).toEqual([
      "Model connection",
      "Coding agent",
    ]);
    expect(format("Anthropic Messages")).toHaveAttribute("aria-pressed", "true");
    expect(field("Base URL")).toHaveValue("https://api.anthropic.com/v1");
    expect(field("Model")).toHaveValue("claude-sonnet-5");
    expect(screen.getByText("sk-a…wxyz")).toBeInTheDocument();
    expect(radio(/Claude Code/)).toBeChecked();
  });

  it("draws the info box from the saved capabilities", () => {
    renderCard();
    expect(info().getByText("api.anthropic.com").tagName).toBe("STRONG");
    expect(info().getByText("Usage is priced in USD: claude-sonnet-5 has a rate.")).toBeInTheDocument();
    expect(info().getByText("Chat reads attached images and PDFs.")).toBeInTheDocument();
  });

  it("offers the Claude subscription as an optional token field inside the Claude Code tile", () => {
    renderCard();
    expect(screen.getByText(/Claude subscription token/)).toBeInTheDocument();
    expect(field("Subscription token")).toHaveValue("");
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  });

  it("says who last changed the card", () => {
    renderCard();
    expect(screen.getByText(/^Changed .* by dev@acme.example$/)).toBeInTheDocument();
  });

  it("saves nothing until something changes, then only the model", () => {
    renderCard();
    expect(saveButton()).toBeDisabled();
    type("Model", "claude-haiku-4-5");
    fireEvent.click(saveButton());
    expect(lastPatch()).toEqual({ llm: { model: "claude-haiku-4-5" } });
  });

  it("sends a pasted subscription token", () => {
    renderCard();
    type("Subscription token", "sk-ant-oat01-new-token-abcd");
    fireEvent.click(saveButton());
    expect(lastPatch()).toEqual({ agents: { subscription: { kind: "claude", token: "sk-ant-oat01-new-token-abcd" } } });
  });

  it("shows a stored token masked; Remove deletes it on save", () => {
    renderCard(config({ agents: { ...defaultAgents, subscription } }));
    expect(screen.getByText("sk-ant-oat01-••••••9f2c")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Remove" }));
    expect(screen.getByText(/Saving deletes the stored token/)).toBeInTheDocument();
    fireEvent.click(saveButton());
    expect(lastPatch()).toEqual({ agents: { subscription: null } });
  });

  it("disconnects on its own, naming the subscription it takes along", () => {
    renderCard(config({ agents: { ...defaultAgents, subscription } }));
    fireEvent.click(screen.getByRole("button", { name: "Disconnect" }));
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByText("Disconnect the model connection?")).toBeInTheDocument();
    expect(within(dialog).getByText(/The stored Claude subscription token is deleted as well/)).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Disconnect" }));
    expect(lastPatch()).toEqual({ llm: null });
  });
});

describe("AiAgentsCard on an OpenAI-compatible connection", () => {
  it("disables Claude Code with the reason and draws the open model's info box", () => {
    renderCard(onOllama());

    expect(format("OpenAI-compatible")).toHaveAttribute("aria-pressed", "true");
    expect(radio(/Claude Code/)).toBeDisabled();
    expect(screen.getByText("Needs the Anthropic Messages format.")).toBeInTheDocument();
    expect(radio(/OpenCode/)).toBeChecked();
    expect(info().getByText(/the platform has no rate for glm-5.3/)).toBeInTheDocument();
    expect(info().getByText(/Ollama's search API/)).toBeInTheDocument();
    expect(info().getByText(/Chat does not accept images: glm-5.3/)).toBeInTheDocument();
    expect(screen.queryByText(/Claude subscription token/)).not.toBeInTheDocument();
  });
});

describe("switching the format", () => {
  it("clears Anthropic's URL, fills the format's model, and moves coding to OpenCode", () => {
    renderCard(config({ agents: { ...defaultAgents, subscription } }));
    fireEvent.click(format("OpenAI-compatible"));

    expect(field("Base URL")).toHaveValue("");
    expect(field("Model")).toHaveValue("glm-5.3");
    expect(radio(/OpenCode/)).toBeChecked();
    expect(
      screen.getByText(/Coding moved to OpenCode: Claude Code speaks only the Anthropic Messages format. Saving deletes the stored Claude subscription token./),
    ).toBeInTheDocument();
    expect(saveButton()).toBeDisabled();
  });

  it("asks for a key for the new host, and saves the connection and runtime together", () => {
    renderCard();
    fireEvent.click(format("OpenAI-compatible"));
    type("Base URL", "https://ollama.com/v1");

    expect(screen.getByText("A new host needs its own key: the saved key is never sent to ollama.com.")).toBeInTheDocument();
    expect(saveButton()).toBeDisabled();
    type("API key", "ollama-key-0123456789");
    fireEvent.click(saveButton());
    expect(lastPatch()).toEqual({
      llm: { kind: "openai-compatible", baseURL: "https://ollama.com/v1", model: "glm-5.3", apiKey: "ollama-key-0123456789" },
      agents: { runtime: "opencode" },
    });
  });
});

describe("Test connection", () => {
  const check: LLMCheck = {
    kind: "openai-compatible",
    baseURL: "https://ollama.com/v1",
    model: "glm-5.3",
    modelListed: "yes",
    priced: false,
    capabilities: ollama.capabilities,
  };

  it("probes the draft and draws the result into the status and info box", () => {
    renderCard();
    fireEvent.click(format("OpenAI-compatible"));
    type("Base URL", "https://ollama.com/v1");
    type("API key", "ollama-key-0123456789");
    // Before a test, only where prompts go is known.
    expect(info().getByText(/Test connection shows what it supports/)).toBeInTheDocument();

    testMutate.mockImplementation((_body, opts: { onSuccess: (c: LLMCheck) => void }) => opts.onSuccess(check));
    fireEvent.click(testButton());

    expect(testMutate.mock.calls[0]?.[0]).toEqual({
      kind: "openai-compatible",
      baseURL: "https://ollama.com/v1",
      model: "glm-5.3",
      apiKey: "ollama-key-0123456789",
    });
    expect(screen.getByText("Connected to ollama.com · glm-5.3 is available")).toHaveAttribute("role", "status");
    expect(info().getByText(/Chat does not accept images/)).toBeInTheDocument();
  });

  it("warns on a model the endpoint does not list", () => {
    renderCard();
    type("Model", "claude-future");
    testMutate.mockImplementation((_b, opts: { onSuccess: (c: LLMCheck) => void }) =>
      opts.onSuccess({ ...check, kind: "anthropic", baseURL: anthropic.baseURL, model: "claude-future", modelListed: "no", capabilities: anthropic.capabilities }),
    );
    fireEvent.click(testButton());
    expect(screen.getByRole("status")).toHaveTextContent("claude-future is not in its model list. You can still save");
  });

  it("puts a rejected key on the key field and says Not connected", () => {
    testState.isError = true;
    testState.error = refused("llm_key_rejected", "llm", "ollama.com answered 401: the key was rejected.");
    renderCard(onOllama());
    fireEvent.click(screen.getByRole("button", { name: "Replace" }));
    expect(screen.getByText("ollama.com answered 401: the key was rejected.")).toBeInTheDocument();
    expect(screen.getByText("Not connected")).toBeInTheDocument();
  });

  it("puts a refused private host on the URL field", () => {
    testState.isError = true;
    testState.error = refused("llm_host_refused", "llm", "Refused: llm.internal.acme.example resolves to a private address.");
    renderCard();
    expect(field("Base URL")).toHaveAttribute("aria-invalid", "true");
  });

  it("shows an unreachable endpoint beside the button", () => {
    testState.isError = true;
    testState.error = refused("llm_unreachable", "llm", "could not reach gateway.acme.example");
    renderCard();
    expect(screen.getByRole("alert")).toHaveTextContent("could not reach gateway.acme.example");
  });
});

describe("with no connection", () => {
  it("reads not connected and asks for a key on Anthropic's defaults", () => {
    renderCard(config({ llm: null }));
    expect(screen.getByText("not connected")).toBeInTheDocument();
    expect(field("API key")).toHaveValue("");
    expect(field("Base URL")).toHaveValue("https://api.anthropic.com/v1");
    expect(screen.queryByRole("button", { name: "Disconnect" })).not.toBeInTheDocument();
  });

  it("keeps the key and token out of the browser's password manager", () => {
    renderCard(config({ llm: null }));
    expect(field("API key")).toHaveAttribute("autocomplete", "new-password");
  });
});

describe("on an installation without OpenCode", () => {
  const noOpenCode = config({
    agents: { ...defaultAgents, availableRuntimes: ["claude-code"] },
    llmFormats: [
      { ...formats[0]!, runtimes: ["claude-code"] },
      { ...formats[1]!, runtimes: [] },
    ],
  });

  it("disables the OpenCode tile and says why", () => {
    renderCard(noOpenCode);
    expect(radio(/OpenCode/)).toBeDisabled();
    expect(screen.getByText("Not available on this installation.")).toBeInTheDocument();
  });

  it("holds back a format no coding agent here runs", () => {
    renderCard(noOpenCode);
    fireEvent.click(format("OpenAI-compatible"));
    type("Base URL", "https://ollama.com/v1");
    type("API key", "ollama-key-0123456789");
    expect(screen.getByText("No coding agent on this installation runs the OpenAI-compatible format.")).toBeInTheDocument();
    expect(saveButton()).toBeDisabled();
  });
});

describe("a refused save", () => {
  it("shows a runtime refusal beside the coding agents", () => {
    saveState.isError = true;
    saveState.error = refused("agents_runtime_requires_anthropic_format", "agents", "Claude Code speaks only the Anthropic Messages format");
    renderCard();
    expect(screen.getByText("Claude Code speaks only the Anthropic Messages format")).toBeInTheDocument();
  });

  it("shows a subscription refusal on the token field", () => {
    saveState.isError = true;
    saveState.error = refused("agents_subscription_requires_anthropic_host", "agents", "a Claude subscription bills only against Anthropic's own API");
    renderCard();
    expect(field("Subscription token")).toHaveAttribute("aria-invalid", "true");
  });

  it("disables the controls while a save is in flight", () => {
    saveState.isPending = true;
    renderCard();
    expect(screen.getByRole("button", { name: "Saving…" })).toBeDisabled();
    expect(field("Model")).toBeDisabled();
  });
});
