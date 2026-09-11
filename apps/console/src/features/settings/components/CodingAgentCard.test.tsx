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

const setMutate = vi.fn();
const saveState: { isPending: boolean; isError: boolean; error: Error | null } =
  { isPending: false, isError: false, error: null };

vi.mock("../api/queries", () => ({
  useSetCodingAgent: () => ({ mutate: setMutate, ...saveState }),
  // The card mounts the coding-agent KEY section when the org key is
  // connected; these keep that subtree inert for these tests.
  useConnectCodingAnthropic: () => ({
    mutate: vi.fn(),
    isPending: false,
    isError: false,
    error: null,
  }),
  useRemoveCodingAnthropic: () => ({
    mutate: vi.fn(),
    isPending: false,
    isError: false,
    error: null,
  }),
}));

const { CodingAgentCard } = await import("./CodingAgentCard");

type Props = Parameters<typeof CodingAgentCard>[0];
type CodingAgent = Props["codingAgent"];

const platformDefaults: CodingAgent = {
  runtime: "claude-code",
  model: "claude-sonnet-5",
  updatedAt: null,
  updatedBy: null,
};

function renderCard(codingAgent: CodingAgent = platformDefaults) {
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <CodingAgentCard
        codingAgent={codingAgent}
        codingLlm={null}
        llmConnected={false}
      />
    </OxygenUIThemeProvider>,
  );
}

const runtimeSelect = () => screen.getByRole("combobox", { name: "Runtime" });
const modelSelect = () => screen.getByRole("combobox", { name: "Model" });

beforeEach(() => {
  setMutate.mockClear();
  saveState.isPending = false;
  saveState.isError = false;
  saveState.error = null;
});
afterEach(cleanup);

describe("CodingAgentCard", () => {
  // updatedBy null means NOBODY has chosen — the card has to say that rather
  // than render the same two values as if someone had picked them.
  it("names the platform-defaults state when nobody has set it", () => {
    renderCard();

    expect(screen.getByText("platform defaults")).toBeInTheDocument();
    expect(
      screen.getByText(/Nobody has changed this yet/),
    ).toBeInTheDocument();
    expect(runtimeSelect()).toHaveTextContent("Claude Code");
    expect(modelSelect()).toHaveTextContent("Claude Sonnet 5");
  });

  it("attributes the choice once somebody has made one", () => {
    renderCard({
      runtime: "claude-code",
      model: "claude-haiku-4-5",
      updatedAt: "2026-09-01T10:00:00Z",
      updatedBy: "dev@acme.example",
    });

    expect(screen.queryByText("platform defaults")).not.toBeInTheDocument();
    expect(screen.getByText(/dev@acme.example/)).toBeInTheDocument();
    expect(modelSelect()).toHaveTextContent("Claude Haiku 4.5");
  });

  it("states that a change only takes effect from the next cycle", () => {
    renderCard();
    expect(screen.getByText(/next cycle/)).toBeInTheDocument();
  });

  // Membership of the enum is not availability. The option is shown so the
  // reader knows it exists, disabled with the reason, and cannot be chosen —
  // choosing it would only earn a rejection from the API.
  it("lists OpenCode with a reason but will not let it be picked", () => {
    renderCard();
    fireEvent.mouseDown(runtimeSelect());

    const opencode = screen.getByRole("option", { name: /OpenCode/ });
    expect(opencode).toHaveAttribute("aria-disabled", "true");
    expect(opencode).toHaveTextContent("no adapter on this platform yet");

    fireEvent.click(opencode);
    expect(setMutate).not.toHaveBeenCalled();
  });

  it("offers only the two models the platform can price", () => {
    renderCard();
    fireEvent.mouseDown(modelSelect());

    expect(screen.getAllByRole("option")).toHaveLength(2);
    expect(
      screen.getByText(/Only models the platform holds a cost rate for/),
    ).toBeInTheDocument();
  });

  // The patch's two fields are independently optional, so a model change sends
  // the model alone — restating the runtime would let a stale read overwrite a
  // change made elsewhere.
  it("patches only the model when the model changes", () => {
    renderCard();
    fireEvent.mouseDown(modelSelect());
    fireEvent.click(screen.getByRole("option", { name: "Claude Haiku 4.5" }));

    expect(setMutate).toHaveBeenCalledTimes(1);
    expect(setMutate).toHaveBeenCalledWith({ model: "claude-haiku-4-5" });
  });

  it("surfaces the reason the API gave for rejecting the change", () => {
    saveState.isError = true;
    saveState.error = new Error(
      'coding agent: runtime "opencode" is not available on this platform — no adapter is installed for it',
    );
    renderCard();

    expect(screen.getByRole("alert")).toHaveTextContent(
      "no adapter is installed for it",
    );
  });

  it("disables both selects while a change is in flight", () => {
    saveState.isPending = true;
    renderCard();

    expect(runtimeSelect()).toHaveAttribute("aria-disabled", "true");
    expect(modelSelect()).toHaveAttribute("aria-disabled", "true");
  });
});
