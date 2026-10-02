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

import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "../../../generated/aep-api";

type AeStudio = components["schemas"]["AeStudio"];

const mutate = vi.fn();
vi.mock("../../settings/api/queries", () => ({
  useSyncSkills: () => ({ mutate, isPending: false, isError: false, isSuccess: false }),
}));

// The step's read of AE Studio, replaced: each test sets what it answers.
let studio: { data: AeStudio | undefined; isPending: boolean; isError: boolean };
const refetch = vi.fn();
vi.mock("../../ae-studio/api/queries", () => ({
  useAeStudio: () => ({ ...studio, refetch }),
}));

const { SkillsBootstrapStep } = await import("./SkillsBootstrapStep");

function answer(state: AeStudio["state"] | "pending") {
  studio =
    state === "pending"
      ? { data: undefined, isPending: true, isError: false }
      : { data: { state }, isPending: false, isError: false };
}

const onComplete = vi.fn();

function renderStep() {
  return render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <SkillsBootstrapStep onComplete={onComplete} />
    </OxygenUIThemeProvider>,
  );
}

// The bootstrap fires from a deferred one-shot; let it run.
const flushDeferred = () => act(() => new Promise((r) => setTimeout(r, 0)));

beforeEach(() => {
  vi.clearAllMocks();
});
afterEach(cleanup);

describe("SkillsBootstrapStep while AE Studio starts", () => {
  it.each(["provisioning", "pending"] as const)(
    "waits inline while %s, without starting the bootstrap",
    async (state) => {
      answer(state);
      renderStep();
      await flushDeferred();
      expect(screen.getByText("Getting AE Studio ready…")).toBeInTheDocument();
      expect(screen.queryByText(/Setting up your skills catalogue/)).not.toBeInTheDocument();
      expect(mutate).not.toHaveBeenCalled();
    },
  );

  it("proceeds as today once AE Studio is ready", async () => {
    answer("provisioning");
    const view = renderStep();
    answer("ready");
    view.rerender(
      <OxygenUIThemeProvider theme={OxygenTheme}>
        <SkillsBootstrapStep onComplete={onComplete} />
      </OxygenUIThemeProvider>,
    );
    await flushDeferred();
    expect(screen.queryByText("Getting AE Studio ready…")).not.toBeInTheDocument();
    expect(screen.getByText(/Setting up your skills catalogue/)).toBeInTheDocument();
    expect(mutate).toHaveBeenCalledTimes(1);
  });

  it("shows the step's error area when AE Studio failed, with Retry and Continue anyway", async () => {
    answer("failed");
    renderStep();
    await flushDeferred();
    expect(screen.getByText("AE Studio couldn't start")).toBeInTheDocument();
    expect(mutate).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(refetch).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: "Continue anyway" }));
    expect(onComplete).toHaveBeenCalledTimes(1);
  });
});
