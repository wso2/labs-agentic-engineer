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
import { AnthropicCredentialCard } from "./AnthropicCredentialCard";

type LLMProjection = components["schemas"]["LLMProjection"];

// Every existing test in this file assumes the card is otherwise operable —
// only the dedicated "no permission" test below flips this to false.
const modelConfigPermission = vi.hoisted(() => ({ current: true }));
vi.mock("../../../auth/permissions", () => ({
  useHasPermission: () => modelConfigPermission.current,
}));

vi.mock("../api/queries", () => ({
  useConnectAnthropic: () => ({ mutate: vi.fn(), isPending: false, isError: false, error: null }),
  useDisconnectAnthropic: () => ({ mutate: vi.fn(), isPending: false, isError: false, error: null }),
  useConnectCodingAnthropic: () => ({ mutate: vi.fn(), isPending: false, isError: false, error: null }),
  useRemoveCodingAnthropic: () => ({ mutate: vi.fn(), isPending: false, isError: false, error: null }),
}));

const connectedLlm: LLMProjection = {
  kind: "anthropic",
  credentialKind: "api_key",
  status: "connected",
  keyPrefix: "sk-ant-api03-ABC",
  keyLast4: "wxyz",
  connectedAt: "2026-08-06T09:41:00Z",
};

function renderCard(llm: LLMProjection | null) {
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <AnthropicCredentialCard llm={llm} codingLlm={null} />
    </OxygenUIThemeProvider>,
  );
}

beforeEach(() => {
  modelConfigPermission.current = true;
});

describe("AnthropicCredentialCard — permission gate", () => {
  it("renders an operable form when the user holds ae:model-config", () => {
    renderCard(null);

    expect(screen.getByLabelText("API key")).toBeEnabled();
    expect(screen.getByLabelText("show key")).toBeEnabled();
    expect(screen.getByRole("button", { name: "Connect" })).toBeDisabled(); // empty input, not permission
  });

  it("disables the key field, reveal toggle, and connect/disconnect actions without ae:model-config", () => {
    modelConfigPermission.current = false;
    renderCard(connectedLlm);

    expect(screen.getByLabelText("Replace API key")).toBeDisabled();
    expect(screen.getByLabelText("show key")).toBeDisabled();
    expect(screen.getByRole("button", { name: "Replace key" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Disconnect" })).toBeDisabled();
  });

  it("shows no permission banner when the user holds ae:model-config", () => {
    renderCard(connectedLlm);
    expect(
      screen.queryByText("You don't have permission to change these settings."),
    ).not.toBeInTheDocument();
  });

  it("shows a permission banner when the user lacks ae:model-config", () => {
    modelConfigPermission.current = false;
    renderCard(connectedLlm);
    expect(
      screen.getByText("You don't have permission to change these settings."),
    ).toBeInTheDocument();
  });
});
