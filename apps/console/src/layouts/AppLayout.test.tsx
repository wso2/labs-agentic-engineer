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

// The shell's two first-run rules (#485), which are about where the user is,
// not about what the agent is doing:
//
//   1. The rail opens itself on the SPEC VIEW during a first run, and nowhere
//      else — that view is otherwise dead air, while the overview says the same
//      thing in one line on the Spec card.
//   2. The rail is never unmounted. It is the chat log's only live writer, so
//      unmounting it on close froze the thread — with the rail shut the log
//      went stale, and every surface reading it (the Spec card, the spec view's
//      waiting states) went blind with it.
//
// Nothing here NAVIGATES: the user reaches the spec view by clicking.

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import type { ReactNode } from "react";
import { AppLayout } from "./AppLayout";

const ORG = "acme";
const PROJECT = "proj1";

let mockPathname = `/projects/${PROJECT}`;
const mockNavigate = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children?: ReactNode }) => <a href="#">{children}</a>,
  Outlet: () => <div data-testid="page-body" />,
  useNavigate: () => mockNavigate,
  useParams: () => ({ projectName: PROJECT }),
  useRouterState: ({ select }: { select: (s: unknown) => unknown }) =>
    select({ location: { pathname: mockPathname } }),
  useSearch: () => ({}),
}));

vi.mock("../auth/SessionContext", () => ({
  useSession: () => ({
    user: { name: "Test User", email: "test@example.com" },
    orgHandle: ORG,
    signOut: vi.fn(),
  }),
}));

// Header/notification widgets run their own queries — out of scope here.
vi.mock("./HeaderSwitchers", () => ({
  OrgSwitcher: () => null,
  ProjectSwitcher: () => null,
}));
vi.mock("./NotificationBell", () => ({
  AlertsNotificationPanel: () => null,
  NotificationButton: () => null,
}));

// The panel itself is covered by its own suite; here it only has to be
// identifiable, and to prove whether it is mounted at all.
vi.mock("../features/agent-chat/components/AgentChatPanel", () => ({
  AgentChatPanel: () => <div data-testid="agent-chat-panel" />,
}));

// The first-run report the shell reads.
let mockFirstRunOpen = false;
vi.mock("../features/projects/hooks/useSpecFirstRun", () => ({
  useSpecFirstRun: () => ({
    stage: mockFirstRunOpen ? "reading" : "none",
    open: mockFirstRunOpen,
    questions: 0,
    line: mockFirstRunOpen ? "Agent is looking at your idea" : "",
    reason: "",
  }),
}));

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false, enabled: false } },
});

function renderLayout() {
  return render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <QueryClientProvider client={queryClient}>
        <AppLayout />
      </QueryClientProvider>
    </OxygenUIThemeProvider>,
  );
}

// The Collapse wrapper is what opens and closes; the panel inside it stays
// mounted either way, which is the point of rule 2 above.
function railIsOpen(): boolean {
  const panel = screen.getByTestId("agent-chat-panel");
  const collapse = panel.closest(".MuiCollapse-root");
  return collapse !== null && !collapse.classList.contains("MuiCollapse-hidden");
}

describe("AppLayout — the agent rail through a first run", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockPathname = `/projects/${PROJECT}`;
    mockFirstRunOpen = false;
  });

  it("keeps the panel mounted while the rail is closed", () => {
    renderLayout();

    expect(screen.getByTestId("agent-chat-panel")).toBeInTheDocument();
    expect(railIsOpen()).toBe(false);
  });

  it("does not open the rail on the overview during a first run", () => {
    mockFirstRunOpen = true;
    renderLayout();

    expect(railIsOpen()).toBe(false);
  });

  it("opens the rail on the spec view during a first run", () => {
    mockPathname = `/projects/${PROJECT}/spec`;
    mockFirstRunOpen = true;
    renderLayout();

    expect(railIsOpen()).toBe(true);
  });

  it("leaves the rail alone on the spec view once the first run is over", () => {
    mockPathname = `/projects/${PROJECT}/spec`;
    renderLayout();

    expect(railIsOpen()).toBe(false);
  });

  it("never navigates on its own", () => {
    mockPathname = `/projects/${PROJECT}/spec`;
    mockFirstRunOpen = true;
    renderLayout();

    expect(mockNavigate).not.toHaveBeenCalled();
  });
});
