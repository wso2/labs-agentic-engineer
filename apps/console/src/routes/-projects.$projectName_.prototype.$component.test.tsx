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

// `PrototypeRoute` calls `Route.useParams()` / `Route.useSearch()` /
// `Route.useNavigate()`, all of which TanStack Router attaches to the
// object `createFileRoute(path)(options)` returns. Mocking `createFileRoute`
// to return the options verbatim (plus stubbed hooks) lets us exercise the
// real `validateSearch` and the real `PrototypeRoute` component without a
// full router.
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

// The heavy page is irrelevant here — record the callbacks PrototypeRoute
// wires up so the tests can invoke them directly.
let captured: {
  onScreenChange?: (screen: string) => void;
  onFlowChange?: (flow: string) => void;
} = {};

vi.mock("../features/spec/components/PrototypePage", () => ({
  PrototypePage: (props: {
    onScreenChange: (screen: string) => void;
    onFlowChange: (flow: string) => void;
  }) => {
    captured = props;
    return <div data-testid="prototype-page" />;
  },
}));

import { Route as RouteUnderTest } from "./projects.$projectName_.prototype.$component";

// `createFileRoute` is mocked above to hand back its options object as-is
// (plus stubbed hooks), so at runtime `Route` also carries `component` and
// `validateSearch` — properties the real generated `Route` type doesn't
// expose. Cast through this narrower shape to access them from the tests.
const Route = RouteUnderTest as unknown as {
  validateSearch: (search: Record<string, unknown>) => { screen?: string; flow?: string };
  component: React.ComponentType;
};
const PrototypeRoute = Route.component;

beforeEach(() => {
  mockUseParams.mockReset();
  mockUseSearch.mockReset();
  mockNavigate.mockReset();
  captured = {};
});

describe("prototype route", () => {
  describe("validateSearch", () => {
    it("keeps both screen and flow when present", () => {
      expect(Route.validateSearch({ screen: "Login", flow: "Admin path" })).toEqual({
        screen: "Login",
        flow: "Admin path",
      });
    });

    it("drops non-string and empty values", () => {
      expect(Route.validateSearch({ screen: "", flow: 42 })).toEqual({});
      expect(Route.validateSearch({})).toEqual({});
    });
  });

  describe("PrototypeRoute", () => {
    beforeEach(() => {
      mockUseParams.mockReturnValue({ projectName: "p", component: "shop" });
    });

    it("preserves flow when onScreenChange navigates (fails if the object form of search regresses)", () => {
      mockUseSearch.mockReturnValue({ screen: "Login", flow: "Admin path" });
      render(<PrototypeRoute />);

      captured.onScreenChange!("Orders");

      expect(mockNavigate).toHaveBeenCalledTimes(1);
      const call = mockNavigate.mock.calls[0]![0] as { replace: boolean; search: (prev: unknown) => unknown };
      expect(call.replace).toBe(true);
      expect(typeof call.search).toBe("function");
      expect(call.search({ flow: "Admin path", screen: "Login" })).toEqual({
        flow: "Admin path",
        screen: "Orders",
      });
    });

    it("preserves screen when onFlowChange navigates", () => {
      mockUseSearch.mockReturnValue({ screen: "Login", flow: "Admin path" });
      render(<PrototypeRoute />);

      captured.onFlowChange!("Customer path");

      expect(mockNavigate).toHaveBeenCalledTimes(1);
      const call = mockNavigate.mock.calls[0]![0] as { replace: boolean; search: (prev: unknown) => unknown };
      expect(call.replace).toBe(true);
      expect(typeof call.search).toBe("function");
      expect(call.search({ flow: "Admin path", screen: "Login" })).toEqual({
        flow: "Customer path",
        screen: "Login",
      });
    });
  });
});
