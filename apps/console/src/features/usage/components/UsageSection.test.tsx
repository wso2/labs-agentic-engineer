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

type ProjectUsageList = components["schemas"]["ProjectUsageList"];
type Usage = components["schemas"]["Usage"];
type PhaseUsage = components["schemas"]["PhaseUsage"];

// --- Usage query: replaced wholesale so the test needs neither a
// QueryClientProvider nor MSW — only the rendering under test is real. ----
let mockResult: {
  data?: ProjectUsageList;
  isPending: boolean;
  isError: boolean;
  error?: Error;
} = { isPending: false, isError: false };
vi.mock("../api/queries", () => ({
  useProjectUsageList: () => mockResult,
}));

function usage(costUsd: number | null): Usage {
  return {
    inputTokens: 100_000,
    outputTokens: 50_000,
    cacheReadTokens: 1_000_000,
    cacheCreationTokens: 200_000,
    model: "claude-fable-5",
    costUsd,
  };
}

// A trivial phase split for the required `phases` field — the total lands in
// the build phase, spec/validation zero. These tests assert on the total, not
// the phase rows.
function phasesOf(u: Usage): PhaseUsage {
  const z: Usage = {
    inputTokens: 0,
    outputTokens: 0,
    cacheReadTokens: 0,
    cacheCreationTokens: 0,
    model: "",
    costUsd: 0,
  };
  return { spec: z, build: u, validation: z };
}

describe("UsageSection", () => {
  it("renders one card per project with the USD figure as a plain value", () => {
    mockResult = {
      isPending: false,
      isError: false,
      data: {
        projects: [
          {
            projectName: "storefront-webapp",
            displayName: "Storefront Webapp",
            deleted: false,
            usage: usage(12.34),
            phases: phasesOf(usage(12.34)),
          },
        ],
      },
    };
    render(<UsageSection />);
    expect(screen.getByText("Storefront Webapp")).toBeTruthy();
    expect(screen.getByText("storefront-webapp · claude-fable-5")).toBeTruthy();
    expect(screen.getByText("$12.34")).toBeTruthy();
  });

  it("names the host that billed a tokens-only figure", () => {
    mockResult = {
      isPending: false,
      isError: false,
      data: {
        projects: [
          {
            projectName: "order-events",
            displayName: "Order Events",
            deleted: false,
            usage: { ...usage(null), model: "kimi-k3", host: "ollama.com" },
            phases: phasesOf(usage(null)),
          },
        ],
      },
    };
    render(<UsageSection />);
    expect(screen.getByText("order-events · kimi-k3 via ollama.com")).toBeTruthy();
    expect(screen.getByText("1.4M tok")).toBeTruthy();
    expect(screen.getByText("not priced · billed by ollama.com")).toBeTruthy();
  });

  it("labels the project-less usage key as the marketplace, not a deleted project", () => {
    // Marketplace register turns belong to no project; the API reports them
    // under the empty key, which it also reads as a deleted slug.
    mockResult = {
      isPending: false,
      isError: false,
      data: {
        projects: [
          {
            projectName: "",
            displayName: "",
            deleted: true,
            usage: usage(2.5),
            phases: phasesOf(usage(2.5)),
          },
        ],
      },
    };
    render(<UsageSection />);
    expect(screen.getByText("Marketplace")).toBeTruthy();
    expect(screen.getByText("Resource registration · claude-fable-5")).toBeTruthy();
    expect(screen.queryByText("Deleted project")).toBeNull();
    expect(screen.getByText("$2.50")).toBeTruthy();
  });

  it("marks deleted projects and falls back to tokens when no cost is stamped", () => {
    mockResult = {
      isPending: false,
      isError: false,
      data: {
        projects: [
          {
            projectName: "legacy-crm-poc",
            displayName: "legacy-crm-poc",
            deleted: true,
            usage: usage(3.5),
            phases: phasesOf(usage(3.5)),
          },
          {
            projectName: "spike",
            displayName: "Spike",
            deleted: false,
            usage: usage(null), // pre-stamping rows: tokens only, no USD
            phases: phasesOf(usage(null)),
          },
        ],
      },
    };
    render(<UsageSection />);
    expect(screen.getByText("Deleted project")).toBeTruthy();
    // 1.35M total tokens, abbreviated by the shared formatter.
    expect(screen.getByText("1.4M tok")).toBeTruthy();
  });

  it("reveals the token breakdown on hover, not as inline text", async () => {
    mockResult = {
      isPending: false,
      isError: false,
      data: {
        projects: [
          {
            projectName: "storefront-webapp",
            displayName: "Storefront Webapp",
            deleted: false,
            usage: usage(12.34),
            phases: phasesOf(usage(12.34)),
          },
        ],
      },
    };
    const { container } = render(<UsageSection />);

    const figure = screen.getByText("$12.34");
    // A plain value, not a chip — the review asked for the frame to go (#300).
    expect(container.querySelector(".MuiChip-root")).toBeNull();
    // The detail stays hidden until hover.
    expect(screen.queryByText("Cache read")).toBeNull();

    fireEvent.mouseOver(figure);
    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip).toHaveTextContent("Agent spend — Storefront Webapp");
    expect(tooltip).toHaveTextContent("Cache read");
    expect(tooltip).toHaveTextContent("1.0M tok");
  });

  it("keeps the figure focusable so the breakdown is not hover-only", () => {
    mockResult = {
      isPending: false,
      isError: false,
      data: {
        projects: [
          {
            projectName: "storefront-webapp",
            displayName: "Storefront Webapp",
            deleted: false,
            usage: usage(12.34),
            phases: phasesOf(usage(12.34)),
          },
        ],
      },
    };
    render(<UsageSection />);
    // Hover-only detail would strand keyboard users. We own the tab stop;
    // MUI opens the tooltip itself once that element takes :focus-visible.
    expect(screen.getByText("$12.34").getAttribute("tabindex")).toBe("0");
  });

  it("shows the per-phase cost split in the hover breakdown", async () => {
    mockResult = {
      isPending: false,
      isError: false,
      data: {
        projects: [
          {
            projectName: "storefront-webapp",
            displayName: "Storefront Webapp",
            deleted: false,
            usage: usage(12.34),
            phases: {
              spec: usage(2.0),
              build: usage(9.0),
              validation: usage(1.34),
            },
          },
        ],
      },
    };
    render(<UsageSection />);
    fireEvent.mouseOver(screen.getByText("$12.34"));
    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip).toHaveTextContent("Cost by phase");
    expect(tooltip).toHaveTextContent("Spec / design");
    expect(tooltip).toHaveTextContent("$2");
    expect(tooltip).toHaveTextContent("Build");
    expect(tooltip).toHaveTextContent("$9");
    expect(tooltip).toHaveTextContent("Validation");
    expect(tooltip).toHaveTextContent("$1.34");
  });

  it("renders an idle live project as a $0 card (not hidden)", () => {
    mockResult = {
      isPending: false,
      isError: false,
      data: {
        projects: [
          {
            projectName: "fresh-idea",
            displayName: "Fresh Idea",
            deleted: false,
            usage: {
              inputTokens: 0,
              outputTokens: 0,
              cacheReadTokens: 0,
              cacheCreationTokens: 0,
              model: "",
              costUsd: 0, // idle: real $0, not a null unpriced cost
            },
            phases: phasesOf(usage(0)),
          },
        ],
      },
    };
    render(<UsageSection />);
    expect(screen.getByText("Fresh Idea")).toBeTruthy();
    expect(screen.getByText("$0")).toBeTruthy();
  });

  it("shows the empty state when no project has usage", () => {
    mockResult = {
      isPending: false,
      isError: false,
      data: { projects: [] },
    };
    render(<UsageSection />);
    expect(screen.getByText(/No agent usage yet/)).toBeTruthy();
  });

  it("shows the error state with the server detail", () => {
    mockResult = {
      isPending: false,
      isError: true,
      error: new Error("usage roll-up unavailable"),
    };
    render(<UsageSection />);
    expect(
      screen.getByText(/Failed to load usage: usage roll-up unavailable/),
    ).toBeTruthy();
  });
});
