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

// Landing from project creation (#562). The platform has already fired
// `/start`, so the arrival's job is to raise the chat — and then to forget it
// ever did, because the panel's open/closed state belongs to the user from the
// next moment on.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render as rtlRender, screen } from "@testing-library/react";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";

const PROJECT = "expense-approval";

let mockPathname = `/projects/${PROJECT}`;
let mockSearch: Record<string, unknown> = {};
const mockNavigate = vi.fn();

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children?: React.ReactNode }) => <a>{children}</a>,
  Outlet: () => <div data-testid="outlet" />,
  useNavigate: () => mockNavigate,
  useParams: () => ({ projectName: PROJECT }),
  useRouterState: ({ select }: { select: (s: unknown) => unknown }) =>
    select({ location: { pathname: mockPathname } }),
  useSearch: () => mockSearch,
}));

vi.mock("../auth/SessionContext", () => ({
  useSession: () => ({
    user: { name: "Test User", email: "test@example.com" },
    orgHandle: "acme",
    signOut: vi.fn(),
  }),
}));

// The switchers and the bell each open their own queries; neither is what this
// file is about.
vi.mock("./HeaderSwitchers", () => ({
  OrgSwitcher: () => null,
  ProjectSwitcher: () => null,
}));
vi.mock("./NotificationBell", () => ({
  AlertsNotificationPanel: () => null,
  NotificationButton: () => null,
}));

// The panel's own behaviour is covered by AgentChatPanel.test.tsx; here it only
// has to be present or absent.
vi.mock("../features/agent-chat/components/AgentChatPanel", () => ({
  AgentChatPanel: () => <div data-testid="agent-chat-panel" />,
}));
vi.mock("../features/agent-chat/useHasPendingSeed", () => ({
  useHasPendingSeed: () => false,
}));

import { AppLayout } from "./AppLayout";

// AppShell reads breakpoints off the theme, so the shell needs a provider —
// nothing here is about theming, it is simply the minimum that renders.
const render = () =>
  rtlRender(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <AppLayout />
    </OxygenUIThemeProvider>,
  );

beforeEach(() => {
  vi.clearAllMocks();
  mockPathname = `/projects/${PROJECT}`;
  mockSearch = {};
});
afterEach(cleanup);

describe("AppLayout — landing from project creation", () => {
  it("opens the agent chat on arrival, and strips the signal", () => {
    mockSearch = { chat: "open" };
    render();

    expect(screen.getByTestId("agent-chat-panel")).toBeInTheDocument();
    // Stripped with `replace`, so this describes the ARRIVAL and not the URL —
    // a refresh or a Back must not reopen a panel the user closed.
    expect(mockNavigate).toHaveBeenCalledWith({
      to: "/projects/$projectName",
      params: { projectName: PROJECT },
      search: {},
      replace: true,
    });
  });

  it("leaves the panel closed without the signal", () => {
    render();

    expect(screen.queryByTestId("agent-chat-panel")).not.toBeInTheDocument();
    expect(mockNavigate).not.toHaveBeenCalled();
  });

  // The strip navigates to the overview, so honouring the param anywhere else
  // would MOVE the user — which nothing on this journey does.
  it("ignores the signal on a sibling project route", () => {
    mockPathname = `/projects/${PROJECT}/builds`;
    mockSearch = { chat: "open" };
    render();

    expect(screen.queryByTestId("agent-chat-panel")).not.toBeInTheDocument();
    expect(mockNavigate).not.toHaveBeenCalled();
  });
});
