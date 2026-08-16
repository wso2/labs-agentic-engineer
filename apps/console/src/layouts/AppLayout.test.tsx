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

// The header's "Toggle agent chat" (#485 live-testing round 2). It opens and
// closes the chat rail — nothing else. It used to change route: the rail is
// mounted with `unmountOnExit`, so opening it MOUNTS the chat panel, and the
// panel navigated to the spec view for any question the thread still had
// pending. Clicking the toggle on the project overview silently left the page.
//
// The real AgentChatPanel is rendered here (only its network hooks are
// stubbed) — the defect lived in the seam between the layout's open state and
// the panel's mount effects, so a stubbed panel would prove nothing.

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import type { ReactNode } from "react";
import { AppLayout } from "./AppLayout";
import { chatKeyFor, replaceMessages } from "../features/agent-chat/chatStore";

const ORG = "acme";
const PROJECT = "proj1";
const KEY = chatKeyFor(ORG, PROJECT);

// --- Router: the project OVERVIEW route, with a navigate spy. -------------
const mockNavigate = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children?: ReactNode }) => <a href="#">{children}</a>,
  Outlet: () => <div data-testid="page-body" />,
  useNavigate: () => mockNavigate,
  useParams: () => ({ projectName: PROJECT }),
  useRouterState: ({ select }: { select: (s: unknown) => unknown }) =>
    select({ location: { pathname: `/projects/${PROJECT}` } }),
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

// --- The panel's data edges: the chat thread comes from the store so the
// test can stage a pending question the way a rehydrate would. -------------
let mockMessages: unknown[] = [];
const mockSend = vi.fn();
vi.mock("../features/agent-chat/useAgentChat", () => ({
  useAgentChat: () => ({
    messages: mockMessages,
    isSending: false,
    activeTurnId: undefined,
    conversationReady: true,
    conversationError: false,
    send: mockSend,
    newConversation: vi.fn(),
  }),
}));
vi.mock("../features/spec/api/queries", () => ({
  useSpecFiles: () => ({ data: [] }),
}));
vi.mock("../features/agent-chat/useSpecInterview", () => ({
  useSpecInterview: () => ({ running: false, questionsWaiting: 1, drafting: false }),
}));
vi.mock("../features/agent-chat/useTurnEndDependencyRefresh", () => ({
  useTurnEndDependencyRefresh: vi.fn(),
}));
vi.mock("use-stick-to-bottom", () => ({
  useStickToBottom: () => ({
    scrollRef: { current: null },
    contentRef: { current: null },
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

describe("AppLayout — the agent-chat toggle", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    replaceMessages(KEY, []);
    // A question the thread is waiting on, exactly as a rehydrate leaves it.
    mockMessages = [
      {
        id: "q1",
        role: "question",
        turnId: "t1",
        toolCallId: "tc-pending-on-overview",
        questions: [{ question: "Who signs in?", options: [{ label: "Anyone" }] }],
      },
    ];
  });

  it("opens the rail on the overview with a question pending, and navigates nowhere", () => {
    renderLayout();
    expect(screen.queryByLabelText("Close agent chat")).not.toBeInTheDocument();

    fireEvent.click(screen.getByLabelText("Toggle agent chat"));

    // The rail is open (the panel's own close button is the proof it mounted)…
    expect(screen.getByLabelText("Close agent chat")).toBeInTheDocument();
    // …and the user is still on the overview.
    expect(mockNavigate).not.toHaveBeenCalled();
  });

  it("closes the rail again without navigating", () => {
    renderLayout();
    fireEvent.click(screen.getByLabelText("Toggle agent chat"));
    fireEvent.click(screen.getByLabelText("Toggle agent chat"));

    expect(mockNavigate).not.toHaveBeenCalled();
  });
});
