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

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import type { components } from "../../../generated/aep-api";

type ConfigProjection = components["schemas"]["ConfigProjection"];

vi.mock("../api/queries", () => ({
  useSaveAiSettings: () => ({ mutate: vi.fn(), reset: vi.fn(), isPending: false, isError: false, error: null }),
  useTestConnection: () => ({ mutate: vi.fn(), reset: vi.fn(), isPending: false, isError: false, error: null }),
}));

const { useAiSettings } = await import("../hooks/useAiSettings");
const { ModelConnectionRow } = await import("./ModelConnectionRow");

const connected: NonNullable<ConfigProjection["llm"]> = {
  kind: "anthropic",
  baseURL: "https://api.anthropic.com/v1",
  model: "claude-sonnet-5",
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
  },
};

function config(llm: ConfigProjection["llm"]): ConfigProjection {
  return {
    llm,
    llmFormats: [
      {
        kind: "anthropic",
        defaultBaseURL: "https://api.anthropic.com/v1",
        defaultModel: "claude-sonnet-5",
        runtimes: ["claude-code", "opencode"],
      },
    ],
    agents: {
      runtime: "claude-code",
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
      hasClientSecret: true,
      publisherClientId: "aep-console",
    },
  };
}

function Row({ c }: { c: ConfigProjection }) {
  const ai = useAiSettings(c);
  return <ModelConnectionRow ai={ai} onboarding={false} />;
}

function renderRow(c: ConfigProjection) {
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <Row c={c} />
    </OxygenUIThemeProvider>,
  );
}

afterEach(cleanup);

describe("ModelConnectionRow", () => {
  it("reads Set for a connected key, with no characters of it and no rotate button", () => {
    renderRow(config(connected));
    expect(screen.getByText("Set ••••••••")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Replace" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /rotate/i })).not.toBeInTheDocument();
  });

  it("asks for a key, not a masked field, when nothing is connected", () => {
    renderRow(config(null));
    expect(screen.queryByText("Set ••••••••")).not.toBeInTheDocument();
    expect(screen.getByLabelText("API key")).toHaveValue("");
  });

  it("Replace swaps the Set field for the key input", () => {
    renderRow(config(connected));
    fireEvent.click(screen.getByRole("button", { name: "Replace" }));
    expect(screen.queryByText("Set ••••••••")).not.toBeInTheDocument();
    expect(screen.getByLabelText(/API key/)).toHaveValue("");
  });
});
