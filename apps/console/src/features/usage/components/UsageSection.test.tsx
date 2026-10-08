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

import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { components } from "../../../generated/aep-api";
import { UsageSection } from "./UsageSection";

type ProjectUsageCard = components["schemas"]["ProjectUsageCard"];
type Usage = components["schemas"]["Usage"];

// The query is replaced wholesale, so the test needs neither a query client
// nor MSW: only the rendering is under test.
let result: { data?: { projects: ProjectUsageCard[] }; isPending: boolean; isError: boolean; error?: Error } = {
  isPending: false,
  isError: false,
};
vi.mock("../api/queries", () => ({ useProjectUsageList: () => result }));

function usage(costUsd: number | null, over: Partial<Usage> = {}): Usage {
  return {
    inputTokens: 100_000,
    outputTokens: 50_000,
    cacheReadTokens: 1_000_000,
    cacheCreationTokens: 200_000,
    model: "claude-fable-5",
    costUsd,
    ...over,
  };
}

function project(name: string, u: Usage, over: Partial<ProjectUsageCard> = {}): ProjectUsageCard {
  return {
    projectName: name,
    displayName: name.replace(/-/g, " "),
    deleted: false,
    usage: u,
    phases: { spec: usage(2), build: usage(9), validation: usage(1.34) },
    ...over,
  };
}

const show = (projects: ProjectUsageCard[]) => {
  result = { isPending: false, isError: false, data: { projects } };
  render(<UsageSection />);
};

describe("UsageSection", () => {
  it("lists each project's spend in USD and in tokens", () => {
    show([project("storefront-webapp", usage(12.34))]);
    expect(screen.getByText("storefront webapp")).toBeTruthy();
    expect(screen.getByText("storefront-webapp · claude-fable-5")).toBeTruthy();
    expect(screen.getByText("$12.34")).toBeTruthy();
    expect(screen.getByText("1.4M")).toBeTruthy();
  });

  it("says who billed a figure the platform could not price", () => {
    show([project("order-events", usage(null, { model: "kimi-k3", host: "ollama.com" }))]);
    expect(screen.getByText("Not priced")).toBeTruthy();
    expect(screen.getByText("not priced · billed by ollama.com")).toBeTruthy();
    expect(screen.getByText("order-events · kimi-k3 via ollama.com")).toBeTruthy();
  });

  it("keeps a deleted project's row, marked", () => {
    show([project("legacy-crm-poc", usage(3.5), { deleted: true })]);
    expect(screen.getByText("Deleted project")).toBeTruthy();
    expect(screen.getByText("$3.50")).toBeTruthy();
  });

  it("opens the breakdown, with the split by phase, from the dollar figure", async () => {
    show([project("storefront-webapp", usage(12.34))]);
    const figure = screen.getByText("$12.34");
    expect(figure.getAttribute("tabindex")).toBe("0");
    fireEvent.mouseOver(figure);
    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip).toHaveTextContent("Agent spend, storefront webapp");
    expect(tooltip).toHaveTextContent("Cache read");
    expect(tooltip).toHaveTextContent("Cost by phase");
    expect(tooltip).toHaveTextContent("$1.34");
  });

  it("says so when no agent has run yet", () => {
    show([]);
    expect(screen.getByText(/No agent usage yet/)).toBeTruthy();
  });

  it("shows the server's reason when the usage cannot load", () => {
    result = { isPending: false, isError: true, error: new Error("usage roll-up unavailable") };
    render(<UsageSection />);
    expect(screen.getByText(/Failed to load usage: usage roll-up unavailable/)).toBeTruthy();
  });
});
