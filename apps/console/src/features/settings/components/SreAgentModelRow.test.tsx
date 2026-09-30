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
import { llmConnectedFixture, openaiOrgFixture, sreAgentProjectionFixture, sreLlmFixture } from "../../../mocks/fixtures/settings";

type ConfigProjection = components["schemas"]["ConfigProjection"];

const saveMutate = vi.fn();
const clearMutate = vi.fn();
const saveState: { isPending: boolean; isError: boolean; error: Error | null } = {
  isPending: false,
  isError: false,
  error: null,
};
const clearState: { isPending: boolean; isError: boolean; error: Error | null } = {
  isPending: false,
  isError: false,
  error: null,
};

vi.mock("../api/queries", () => ({
  useSaveSreModel: () => ({ mutate: saveMutate, reset: vi.fn(), ...saveState }),
  useClearSreModel: () => ({ mutate: clearMutate, reset: vi.fn(), ...clearState }),
}));

const { SreAgentModelRow } = await import("./SreAgentModelRow");

function config(over: Partial<ConfigProjection> = {}): ConfigProjection {
  return {
    llm: openaiOrgFixture,
    llmFormats: [],
    agents: {
      runtime: "opencode",
      availableRuntimes: ["claude-code", "opencode"],
      subscription: null,
      updatedAt: null,
      updatedBy: null,
    },
    gitProvider: null,
    idp: {
      kind: "platform",
      issuer: "https://idp.aep.local",
      jwksUrl: "https://idp.aep.local/.well-known/jwks.json",
      hasClientSecret: false,
      publisherClientId: "aep-console",
    },
    sreLlm: null,
    sreAgent: sreAgentProjectionFixture(),
    ...over,
  };
}

function renderRow(c: ConfigProjection) {
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <SreAgentModelRow config={c} />
    </OxygenUIThemeProvider>,
  );
}

const field = (label: string) => screen.getByLabelText(label);
const type = (label: string, value: string) => fireEvent.change(field(label), { target: { value } });
const lastSave = () => saveMutate.mock.calls.at(-1)?.[0] as unknown;

function refused(code: string, message: string) {
  return new ApiRequestError({ code, message, details: [{ field: "body.sreLlm", message }] }, "x");
}

beforeEach(() => {
  saveMutate.mockReset();
  clearMutate.mockReset();
  Object.assign(saveState, { isPending: false, isError: false, error: null });
  Object.assign(clearState, { isPending: false, isError: false, error: null });
});
afterEach(cleanup);

describe("inherited from the org connection", () => {
  it("renders the org connection and an Override button", () => {
    renderRow(config({ sreAgent: sreAgentProjectionFixture({ source: "organization", model: openaiOrgFixture.model, host: "api.openai.com", status: "running" }) }));
    expect(
      screen.getByText("Uses the organization's model connection (gpt-5.4 @ api.openai.com)"),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Override" })).toBeInTheDocument();
  });

  it("opens a form with Base URL, API key and Model, and saves it", () => {
    renderRow(config({ sreAgent: sreAgentProjectionFixture({ source: "organization", model: openaiOrgFixture.model, host: "api.openai.com", status: "running" }) }));
    fireEvent.click(screen.getByRole("button", { name: "Override" }));

    type("Base URL", "https://api.mistral.ai/v1");
    type("API key", "mistral-key-0123456789");
    type("Model", "mistral-large");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(lastSave()).toEqual({
      baseURL: "https://api.mistral.ai/v1",
      apiKey: "mistral-key-0123456789",
      model: "mistral-large",
    });
  });
});

describe("hidden", () => {
  it("renders nothing when the server has no SRE agent", () => {
    const { container } = render(
      <OxygenUIThemeProvider theme={OxygenTheme}>
        <SreAgentModelRow config={config({ sreAgent: null })} />
      </OxygenUIThemeProvider>,
    );
    expect(container).toBeEmptyDOMElement();
  });
});

describe("override", () => {
  it("shows Edit and Remove; Remove confirms then clears", () => {
    renderRow(config({ sreLlm: sreLlmFixture, sreAgent: sreAgentProjectionFixture({ source: "override", model: sreLlmFixture.model, host: sreLlmFixture.host, status: "running" }) }));

    expect(screen.getByText("sk-p…abcd")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Edit" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Remove" }));

    const dialog = screen.getByRole("dialog");
    expect(clearMutate).not.toHaveBeenCalled();
    fireEvent.click(within(dialog).getByRole("button", { name: "Remove" }));
    expect(clearMutate).toHaveBeenCalled();
  });
});

describe("unavailable", () => {
  it("shows the reason and a Set SRE model button", () => {
    renderRow(config({ llm: llmConnectedFixture }));
    expect(screen.getByText(/needs an OpenAI-compatible endpoint/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Set SRE model" })).toBeInTheDocument();
  });
});

describe("status chip", () => {
  it("shows Failed: ... when the agent status is failed", () => {
    renderRow(config({ sreAgent: sreAgentProjectionFixture({ source: "organization", model: openaiOrgFixture.model, host: "api.openai.com", status: "failed", reason: "exited (code 3)" }) }));
    expect(screen.getByText("Failed: exited (code 3)")).toBeInTheDocument();
  });
});

describe("a refused save", () => {
  it("shows the error on the key field", () => {
    saveState.isError = true;
    saveState.error = refused("llm_key_rejected", "api.openai.com answered 401: the key was rejected.");
    renderRow(config({ sreAgent: sreAgentProjectionFixture({ source: "organization", model: openaiOrgFixture.model, host: "api.openai.com", status: "running" }) }));
    fireEvent.click(screen.getByRole("button", { name: "Override" }));
    expect(screen.getByText("api.openai.com answered 401: the key was rejected.")).toBeInTheDocument();
  });
});
