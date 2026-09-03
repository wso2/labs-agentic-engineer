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
import { cleanup, fireEvent, render as rtlRender, screen, act } from "@testing-library/react";
import { chatKeyFor, requestChatOpen } from "../features/agent-chat/chatStore";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";

const PROJECT = "expense-approval";

let mockPathname = `/projects/${PROJECT}`;
let mockSearch: Record<string, unknown> = {};
let mockParams: { projectName?: string } = { projectName: PROJECT };
const mockNavigate = vi.fn();

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children?: React.ReactNode }) => <a>{children}</a>,
  Outlet: () => <div data-testid="outlet" />,
  useNavigate: () => mockNavigate,
  useParams: () => mockParams,
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

// The switchers, the status badge and the bell each open their own queries;
// none of them is what this file is about.
vi.mock("./HeaderSwitchers", () => ({
  OrgSwitcher: () => null,
  ProjectSwitcher: () => null,
}));
vi.mock("./ProjectStatusBadge", () => ({
  ProjectStatusBadge: () => null,
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
  mockParams = { projectName: PROJECT };
  // The panel's open state persists (#666); without this a test that opened
  // it would leak an open panel into every test after it.
  localStorage.removeItem("aep.chat.panelOpen");
});
afterEach(cleanup);

function sidebarItem(name: string) {
  return screen.getByRole("button", { name });
}

describe("AppLayout — org sidebar", () => {
  beforeEach(() => {
    mockParams = {};
  });

  it("lists Projects, Resources, Endpoints, Alerts in that order, with Settings in the footer", () => {
    mockPathname = "/";
    render();

    const projects = sidebarItem("Projects");
    const resources = sidebarItem("Resources");
    const endpoints = sidebarItem("Endpoints");
    const alerts = sidebarItem("Alerts");
    expect(
      projects.compareDocumentPosition(resources) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(
      resources.compareDocumentPosition(endpoints) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(
      endpoints.compareDocumentPosition(alerts) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(sidebarItem("Settings")).toBeInTheDocument();
  });

  it("selects Resources on /resources", () => {
    mockPathname = "/resources";
    render();

    expect(sidebarItem("Resources")).toHaveClass("Mui-selected");
    expect(sidebarItem("Projects")).not.toHaveClass("Mui-selected");
  });

  it("selects Endpoints on /endpoints", () => {
    mockPathname = "/endpoints";
    render();

    expect(sidebarItem("Endpoints")).toHaveClass("Mui-selected");
    expect(sidebarItem("Projects")).not.toHaveClass("Mui-selected");
  });

  it("does not show Resources or Endpoints inside a project", () => {
    mockPathname = `/projects/${PROJECT}`;
    mockParams = { projectName: PROJECT };
    render();

    expect(sidebarItem("Overview")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Resources" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Endpoints" })).not.toBeInTheDocument();
  });
});

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

// The panel's open state survives a reload (#666): a reader who works with the
// chat beside the spec should not have to reopen it every time the page comes
// back. localStorage, per browser — a convenience, never state.
describe("AppLayout — the panel remembers whether it was open", () => {
  it("comes back open after a reload when it was open", () => {
    mockSearch = { chat: "open" };
    const first = render();
    expect(screen.getByTestId("agent-chat-panel")).toBeInTheDocument();
    first.unmount();

    // The reload: a fresh mount with no signal.
    mockSearch = {};
    render();
    expect(screen.getByTestId("agent-chat-panel")).toBeInTheDocument();
  });

  it("comes back closed after the user closed it", () => {
    mockSearch = { chat: "open" };
    const first = render();
    expect(screen.getByTestId("agent-chat-panel")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Toggle agent chat" }));
    // The Collapse unmounts the panel when its exit transition ends, which
    // jsdom never runs — so the closed state is asserted on what persisted,
    // and the reload below is what proves it was honoured.
    expect(localStorage.getItem("aep.chat.panelOpen")).toBe("false");
    first.unmount();

    mockSearch = {};
    render();
    expect(screen.queryByTestId("agent-chat-panel")).not.toBeInTheDocument();
  });
});

// A Discuss request count lives in the store for the life of the page, so the
// layout reacts only to INCREMENTS: leaving a project and coming back must not
// reopen the panel off a request from minutes ago (CodeRabbit on #670).
describe("AppLayout — a stale chat-open request does not replay", () => {
  // The session mock's org — the layout keys the chat by `orgHandle ?? "default"`.
  const CHAT_KEY = chatKeyFor("acme", PROJECT);

  it("ignores a count that predates the mount", () => {
    requestChatOpen(CHAT_KEY);
    render();
    expect(screen.queryByTestId("agent-chat-panel")).not.toBeInTheDocument();
  });

  it("opens on a request made after mount", () => {
    render();
    expect(screen.queryByTestId("agent-chat-panel")).not.toBeInTheDocument();
    act(() => requestChatOpen(CHAT_KEY));
    expect(screen.getByTestId("agent-chat-panel")).toBeInTheDocument();
  });
});

describe("AppLayout — org nav", () => {
  it("shows Endpoints in the org sidebar on /endpoints", () => {
    mockPathname = "/endpoints";
    mockParams = {};
    render();

    expect(screen.getByText("Endpoints")).toBeInTheDocument();
  });
});
