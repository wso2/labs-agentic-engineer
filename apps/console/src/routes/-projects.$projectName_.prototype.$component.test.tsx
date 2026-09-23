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

import { render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { PrototypeViewRequest } from "../features/prototype/model/viewState";

// `createFileRoute` hands back its options plus stubbed hooks, so the real `validateSearch` and
// the real route component run without a router.
const mockUseParams = vi.fn();
const mockUseSearch = vi.fn();
const mockNavigate = vi.fn();

vi.mock("@tanstack/react-router", () => ({
  createFileRoute: () => (options: Record<string, unknown>) => ({
    ...options,
    useParams: () => mockUseParams(),
    useSearch: () => mockUseSearch(),
    useNavigate: () => mockNavigate,
  }),
}));

let captured: {
  projectName?: string;
  component?: string;
  search?: PrototypeViewRequest;
  onSearchChange?: (next: PrototypeViewRequest) => void;
} = {};

vi.mock("../features/prototype/components/ComponentPrototypePage", () => ({
  ComponentPrototypePage: (props: typeof captured) => {
    captured = props;
    return <div data-testid="prototype-page" />;
  },
}));

import { Route as RouteUnderTest } from "./projects.$projectName_.prototype.$component";

const Route = RouteUnderTest as unknown as {
  validateSearch: (search: Record<string, unknown>) => PrototypeViewRequest;
  component: React.ComponentType;
};
const PrototypeRoute = Route.component;

const FULL = {
  screen: "screen.detail",
  flow: "flow.approve",
  state: "state.failed",
  mode: "annotate",
  role: "approver",
} as const;

beforeEach(() => {
  mockUseParams.mockReset().mockReturnValue({ projectName: "p", component: "storefront" });
  mockUseSearch.mockReset();
  mockNavigate.mockReset();
  captured = {};
});

function lastNavigation() {
  expect(mockNavigate).toHaveBeenCalledTimes(1);
  return mockNavigate.mock.calls[0]![0] as { replace: boolean; search: (prev: unknown) => unknown };
}

describe("prototype route", () => {
  describe("validateSearch", () => {
    it("keeps screen, flow, state, mode and role", () => {
      expect(Route.validateSearch({ ...FULL })).toEqual(FULL);
    });

    it("drops an invalid mode and keeps the rest", () => {
      expect(Route.validateSearch({ ...FULL, mode: "edit" })).toEqual({
        screen: "screen.detail",
        flow: "flow.approve",
        state: "state.failed",
        role: "approver",
      });
      expect(Route.validateSearch({ mode: "preview" })).toEqual({ mode: "preview" });
    });

    it("drops empty, non-string and unknown params", () => {
      expect(Route.validateSearch({ screen: "", flow: 42, state: null, variant: "D" })).toEqual({});
    });

    it("drops an invalid role and keeps the rest", () => {
      expect(Route.validateSearch({ screen: "screen.detail", role: "" })).toEqual({ screen: "screen.detail" });
      expect(Route.validateSearch({ screen: "screen.detail", role: ["approver"] })).toEqual({ screen: "screen.detail" });
    });
  });

  describe("route component", () => {
    it("hands the page its project, component and search", () => {
      mockUseSearch.mockReturnValue(FULL);
      render(<PrototypeRoute />);
      expect(captured).toMatchObject({ projectName: "p", component: "storefront", search: FULL });
    });

    it.each<[string, PrototypeViewRequest, PrototypeViewRequest]>([
      ["a screen change", { ...FULL, screen: "screen.queue" }, { ...FULL, screen: "screen.queue" }],
      ["a flow change", { ...FULL, flow: "flow.month-end" }, { ...FULL, flow: "flow.month-end" }],
      ["a display-state change", { ...FULL, state: "state.empty" }, { ...FULL, state: "state.empty" }],
      ["a role change", { screen: FULL.screen, state: FULL.state, mode: FULL.mode, role: "finance" }, { screen: FULL.screen, state: FULL.state, mode: FULL.mode, role: "finance" }],
      ["a mode change", { screen: FULL.screen, flow: FULL.flow, state: FULL.state, role: FULL.role }, { screen: FULL.screen, flow: FULL.flow, state: FULL.state, role: FULL.role }],
      ["leaving a flow", { screen: FULL.screen, state: FULL.state, mode: "annotate", role: FULL.role }, { screen: FULL.screen, state: FULL.state, mode: "annotate", role: FULL.role }],
    ])("writes %s with replace navigation, keeping every other param", (_, next, expected) => {
      mockUseSearch.mockReturnValue(FULL);
      render(<PrototypeRoute />);

      captured.onSearchChange!(next);

      const call = lastNavigation();
      expect(call.replace).toBe(true);
      expect(call.search({ ...FULL })).toEqual(expected);
    });
  });
});
