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

import { render, screen, fireEvent } from "@testing-library/react";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
}));

// Only useAppShell needs a stub (it throws outside an <AppShell> provider);
// every other export passes through untouched — mirrors SpecView.test.tsx.
const toggleNotificationPanel = vi.fn();
vi.mock("@wso2/oxygen-ui", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@wso2/oxygen-ui")>();
  return {
    ...actual,
    useAppShell: () => ({ actions: { toggleNotificationPanel } }),
  };
});

// Every test but the dedicated "no permission" ones below holds
// ae:observability-view, so the bell reads as reachable by default.
const hasObservabilityAccess = vi.hoisted(() => ({ current: true }));
vi.mock("../auth/permissions", () => ({
  useHasPermission: () => hasObservabilityAccess.current,
}));

let recentAlertsResult: { data?: unknown[]; isPending?: boolean; isError?: boolean; error?: Error; refetch?: () => void } = { data: [] };
vi.mock("../features/alerts/api/queries", () => ({
  useRecentAlerts: () => recentAlertsResult,
}));

import { NotificationButton } from "./NotificationBell";

const render_ = (ui: React.ReactElement) =>
  render(<OxygenUIThemeProvider theme={OxygenTheme}>{ui}</OxygenUIThemeProvider>);

afterEach(() => {
  hasObservabilityAccess.current = true;
  recentAlertsResult = { data: [] };
  toggleNotificationPanel.mockClear();
});

describe("NotificationButton", () => {
  it("is enabled and opens the panel when the caller holds ae:observability-view", () => {
    render_(<NotificationButton />);
    const bell = screen.getByRole("button", { name: "Alerts" });
    expect(bell).not.toBeDisabled();

    fireEvent.click(bell);
    expect(toggleNotificationPanel).toHaveBeenCalledOnce();
  });

  it("is disabled with an explanatory tooltip without ae:observability-view", async () => {
    hasObservabilityAccess.current = false;
    render_(<NotificationButton />);

    const bell = screen.getByRole("button", { name: "Alerts" });
    expect(bell).toBeDisabled();

    fireEvent.mouseOver(bell.closest("span") ?? bell);
    expect(
      await screen.findByText("You don't have permission to view alerts."),
    ).toBeInTheDocument();
  });
});

// AlertsNotificationPanel shares NotificationButton's exact permission check
// and useRecentAlerts(undefined, hasObservabilityAccess) call, but its body
// renders through Oxygen UI's NotificationPanel, which reads its own
// open/closed state off the SAME useAppShell() hook internally — stubbing
// the hook down to only `actions` (as above) makes the library's own
// component render nothing, so it needs a real <AppShell> tree to test
// meaningfully rather than this hook-level stub. Not covered here for that
// reason; NotificationButton's tests already pin the shared permission logic.
