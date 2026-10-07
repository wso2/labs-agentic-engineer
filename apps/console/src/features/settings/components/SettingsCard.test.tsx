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

import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  useSearch,
} from "@tanstack/react-router";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import type { components } from "../../../generated/aep-api";
import { sectionFromSearch } from "../settingsSection";
import { SettingsCard } from "./SettingsCard";

type ConfigProjection = components["schemas"]["ConfigProjection"];

const config = {
  gitProvider: {
    kind: "github",
    mode: "pat",
    status: "connected",
    githubLogin: "acme-dev",
    identityName: "Acme Dev",
    connectedAt: "2026-09-01T10:00:00Z",
  },
} as unknown as ConfigProjection;

// The sections' data and the AI agents card (tested on its own) are stood in
// for: what is under test is the card's menu and the address it follows.
vi.mock("../api/queries", () => ({
  useConfig: () => ({ data: config }),
  useConnectGitHubPat: () => ({ mutate: vi.fn(), isPending: false, isError: false }),
  useDisconnectGitProvider: () => ({ mutate: vi.fn(), isPending: false, isError: false }),
}));
vi.mock("./AiAgentsCard", () => ({ AiAgentsCard: () => <p>The model connection</p> }));
vi.mock("../../usage/api/queries", () => ({
  useProjectUsageList: () => ({ isPending: false, isError: false, data: { projects: [] } }),
}));

// The Dashboard and the Settings card at their addresses, as the app's route
// tree draws them.
function renderAt(path: string) {
  const root = createRootRoute({ component: () => <Outlet /> });
  const dashboard = createRoute({ getParentRoute: () => root, path: "/", component: () => <h1>Dashboard</h1> });
  const settings = createRoute({
    getParentRoute: () => root,
    path: "/settings",
    validateSearch: (search: Record<string, unknown>) => ({ section: sectionFromSearch(search.section) }),
    component: function Settings() {
      return <SettingsCard section={sectionFromSearch(useSearch({ strict: false }).section)} />;
    },
  });
  const router = createRouter({
    routeTree: root.addChildren([dashboard, settings]),
    history: createMemoryHistory({ initialEntries: [path] }),
  });
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <RouterProvider router={router} />
    </OxygenUIThemeProvider>,
  );
  return router;
}

// The router restores scroll on navigation; jsdom has no scrolling.
beforeAll(() => {
  vi.spyOn(window, "scrollTo").mockImplementation(() => {});
});
afterEach(cleanup);

describe("SettingsCard", () => {
  it("opens on GitHub, and its menu switches the section and the address", async () => {
    const router = renderAt("/settings");
    expect(await screen.findByRole("heading", { name: "GitHub" })).toBeInTheDocument();
    expect(screen.getByText("acme-dev")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "GitHub" })).toHaveAttribute("aria-current", "page");

    fireEvent.click(screen.getByRole("link", { name: "AI agents" }));
    expect(await screen.findByText("The model connection")).toBeInTheDocument();
    expect(router.state.location.search).toEqual({ section: "ai" });
    expect(screen.getByRole("link", { name: "AI agents" })).toHaveAttribute("aria-current", "page");

    fireEvent.click(screen.getByRole("link", { name: "Usage" }));
    expect(await screen.findByText(/No agent usage yet/)).toBeInTheDocument();
    expect(router.state.location.search).toEqual({ section: "usage" });
  });

  it("opens the section the address names", async () => {
    renderAt("/settings?section=usage");
    expect(await screen.findByRole("heading", { name: "Usage" })).toBeInTheDocument();
  });

  it("closes back to the Dashboard on Escape", async () => {
    const router = renderAt("/settings");
    await screen.findByRole("dialog", { name: "Settings" });
    fireEvent.keyDown(document, { key: "Escape" });
    expect(await screen.findByRole("heading", { name: "Dashboard" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/");
  });
});
