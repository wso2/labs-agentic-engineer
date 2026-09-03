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

import { useCallback, useEffect, useState, useRef } from "react";
import {
  AppShell,
  Box,
  Collapse,
  ColorSchemeToggle,
  Divider,
  Footer,
  Header,
  IconButton,
  Sidebar,
  Tooltip,
  UserMenu,
  useAppShell,
} from "@wso2/oxygen-ui";
import {
  Boxes,
  CircleAlert,
  CircleCheck,
  FileText,
  FolderOpen,
  LayoutDashboard,
  ListChecks,
  LogOut,
  Radio,
  Rocket,
  Settings,
  Siren,
  Sparkles,
  User as UserIcon,
  WSO2,
} from "@wso2/oxygen-ui-icons-react";
import {
  Link,
  Outlet,
  useNavigate,
  useParams,
  useRouterState,
  useSearch,
} from "@tanstack/react-router";
import { useSession } from "../auth/SessionContext";
import { OrgSwitcher, ProjectSwitcher } from "./HeaderSwitchers";
import { ProjectStatusBadge } from "./ProjectStatusBadge";
import { AlertsNotificationPanel, NotificationButton } from "./NotificationBell";
import { AgentChatPanel } from "../features/agent-chat/components/AgentChatPanel";
import { useHasPendingSeed } from "../features/agent-chat/useHasPendingSeed";
import { useChatOpenRequest } from "../features/agent-chat/useChatOpenRequest";

// Footer links (grilled 2026-07-12): the repo is the only real destination
// today — /tree/HEAD/docs follows the default branch.
const REPO_URL = "https://github.com/wso2/labs-agentic-engineer";

// Sidebar highlight follows the route; grows one mapping per top-level route
// (global nav) or per project section (project nav, ADR-0010).
function activeItemFor(pathname: string, inProject: boolean): string {
  if (pathname.startsWith("/settings")) return "settings";
  if (pathname.startsWith("/resources")) return "resources";
  if (pathname.startsWith("/endpoints")) return "endpoints";
  if (pathname.startsWith("/alerts")) return "alerts";
  if (!inProject) return "projects";
  const section = pathname.split("/")[3];
  switch (section) {
    case "spec":
    case "builds":
    case "deployments":
    case "validation":
    case "issues":
      return section;
    default:
      return "overview";
  }
}

// Full-screen surfaces keep the sidebar but collapse it on entry (ADR-0010);
// leaving re-expands it. Rendered inside <AppShell>, which provides the
// shell context this consumes.
function SidebarAutoCollapse({ collapsed }: { collapsed: boolean }) {
  const { actions } = useAppShell();
  const setSidebarCollapsed = actions.setSidebarCollapsed;
  useEffect(() => {
    setSidebarCollapsed(collapsed);
  }, [collapsed, setSidebarCollapsed]);
  return null;
}

// App shell per the oxygen-ui skill's canonical AppLayout: Header + Sidebar +
// Main(Outlet) + Footer + NotificationPanel (Alerts, #154/#155).
const CHAT_OPEN_KEY = "aep.chat.panelOpen";

export function AppLayout() {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const { user, signOut, orgHandle } = useSession();

  // Project AI panel (#130): available on every project route — mounted here
  // because the full-screen spec route bypasses ProjectLayout. Same
  // strict:false param read as the header's project switcher.
  const params = useParams({ strict: false }) as { projectName?: string };
  const projectName = params.projectName;
  // Whether the panel is showing survives a reload — a reader who works with
  // the chat beside the spec should not have to reopen it every time the page
  // comes back (#666). Per-browser convenience, never state: localStorage can
  // be absent or throwing (privacy modes), and then the panel simply starts
  // closed, which is the pre-persistence behaviour.
  const [chatOpen, setChatOpen] = useState(() => {
    try {
      return localStorage.getItem(CHAT_OPEN_KEY) === "true";
    } catch {
      return false;
    }
  });
  useEffect(() => {
    try {
      localStorage.setItem(CHAT_OPEN_KEY, String(chatOpen));
    } catch {
      // memory-only
    }
  }, [chatOpen]);

  const activeItem = activeItemFor(pathname, Boolean(projectName));
  // The spec workspace is the console's full-screen surface (#80).
  const isSpecRoute = Boolean(projectName) && activeItem === "spec";

  // "Generate design" CTA (#159): the spec view navigates to itself with
  // ?generate=design. Open the panel and hand the one-shot signal to
  // AgentChatPanel, which sends the design turn; then strip the param so a
  // refresh/back doesn't re-fire it.
  const navigate = useNavigate();
  const search = useSearch({ strict: false }) as {
    generate?: "design";
    chat?: "open";
  };
  const generate = search.generate;
  useEffect(() => {
    if (generate && projectName) setChatOpen(true);
  }, [generate, projectName]);
  const clearGenerate = useCallback(() => {
    if (!projectName) return;
    void navigate({
      to: "/projects/$projectName/spec",
      params: { projectName },
      search: {},
      replace: true,
    });
  }, [navigate, projectName]);

  // Landing from project creation (#562): the platform already fired `/start`,
  // so the panel opens on arrival and the user's first sight of the project is
  // the agent working from their own words. Stripped immediately, and with
  // `replace`, so this is the ARRIVAL's behaviour and not the URL's — a
  // refresh, a Back, or a shared link must not reopen a panel the user closed.
  //
  // Sits beside `generate` rather than folding into it because they are not the
  // same act: that one hands a command across a navigation, this one only
  // raises a surface.
  const chatParam = search.chat;
  useEffect(() => {
    // Overview only — the strip below navigates there, so honouring the param
    // on a sibling project route would MOVE the user, which this journey never
    // does. Only the create flow sends it, and only to the overview.
    if (chatParam !== "open" || !projectName || activeItem !== "overview") return;
    setChatOpen(true);
    void navigate({
      to: "/projects/$projectName",
      params: { projectName },
      search: {},
      replace: true,
    });
  }, [chatParam, projectName, activeItem, navigate]);

  // "Resolve via chat" (#252 Task 5): the dep card / drawer / build drawer
  // (Task 9) seeds a message into chatStore's pendingSeed slot from a
  // subtree that doesn't share this component's chatOpen state — open the
  // panel reactively the moment a seed appears, same shape as the ?generate=
  // effect above. AgentChatPanel (once mounted) consumes the seed exactly
  // once and auto-sends it.
  const hasPendingSeed = useHasPendingSeed(orgHandle ?? "default", projectName);
  useEffect(() => {
    if (hasPendingSeed && projectName) setChatOpen(true);
  }, [hasPendingSeed, projectName]);

  // An anchored Discuss (#666) has already sent its turn, with the anchor
  // attached — it needs the panel shown, not a message seeded. The count is
  // monotonic, so a second Discuss re-opens a panel the user closed in between.
  //
  // Only an INCREMENT opens: the store keeps the count for the life of the
  // page, so reacting to "count > 0" replayed old requests — leave the
  // project, come back, and the panel opened itself off a Discuss from
  // minutes ago. The baseline resets per project, since each has its own
  // count.
  const chatOpenRequest = useChatOpenRequest(orgHandle ?? "default", projectName);
  const seenChatOpenRef = useRef({ key: projectName, seen: chatOpenRequest });
  useEffect(() => {
    if (seenChatOpenRef.current.key !== projectName) {
      seenChatOpenRef.current = { key: projectName, seen: chatOpenRequest };
      return;
    }
    if (chatOpenRequest > seenChatOpenRef.current.seen) {
      seenChatOpenRef.current.seen = chatOpenRequest;
      if (projectName) setChatOpen(true);
    }
  }, [chatOpenRequest, projectName]);

  return (
    <AppShell initialCollapsed={false} collapseOnSelectOnMobile>
      <AppShell.Navbar>
        <Header>
          <Header.Toggle />
          <Header.Brand>
            {/* Logo/title lead home — the projects list (issue #71). */}
            <Link
              to="/"
              style={{
                display: "flex",
                alignItems: "center",
                textDecoration: "none",
                color: "inherit",
              }}
            >
              <Header.BrandLogo>
                <WSO2 size={24} />
              </Header.BrandLogo>
              <Header.BrandTitle>Agentic Engineer</Header.BrandTitle>
            </Link>
          </Header.Brand>
          <Header.Switchers showDivider={false}>
            <OrgSwitcher />
            <ProjectSwitcher />
            {/* Beside the project's name, not beside each page's title: one
                fact, in the one place that is on screen from every page. */}
            <ProjectStatusBadge />
          </Header.Switchers>
          <Header.Spacer />
          <Header.Actions>
            {projectName && (
              <Tooltip title={chatOpen ? "Close agent chat" : "Agent chat"}>
                <IconButton
                  aria-label="Toggle agent chat"
                  color={chatOpen ? "primary" : "default"}
                  onClick={() => setChatOpen((v) => !v)}
                >
                  <Sparkles size={20} />
                </IconButton>
              </Tooltip>
            )}
            <ColorSchemeToggle />
            <NotificationButton />
            <Divider orientation="vertical" flexItem sx={{ mx: 2 }} />
            <UserMenu>
              <UserMenu.Trigger name={user.name} />
              <UserMenu.Header
                name={user.name}
                email={user.email}
                {...(user.role ? { role: user.role } : {})}
              />
              <UserMenu.Item icon={<UserIcon />} label="Profile" />
              <UserMenu.Item icon={<Settings />} label="Settings" />
              <UserMenu.Divider />
              <UserMenu.Logout icon={<LogOut />} label="Sign out" onClick={signOut} />
            </UserMenu>
          </Header.Actions>
        </Header>
      </AppShell.Navbar>

      {/* Must live inside a named AppShell slot: unrecognized direct children
          of AppShell are dropped by its slot extraction. */}
      <AppShell.Sidebar>
        <SidebarAutoCollapse collapsed={isSpecRoute} />
        <Sidebar activeItem={activeItem}>
          <Sidebar.Nav>
            {/* Project-scoped nav (ADR-0010): inside a project the nav fully
                swaps to its sections — no back-item; home is the header brand
                or the project switcher. */}
            {projectName ? (
              <Sidebar.Category>
                <Sidebar.Item
                  id="overview"
                  link={
                    <Link to="/projects/$projectName" params={{ projectName }} />
                  }
                >
                  <Sidebar.ItemIcon>
                    <LayoutDashboard />
                  </Sidebar.ItemIcon>
                  <Sidebar.ItemLabel>Overview</Sidebar.ItemLabel>
                </Sidebar.Item>
                <Sidebar.Item
                  id="spec"
                  link={
                    <Link
                      to="/projects/$projectName/spec"
                      params={{ projectName }}
                    />
                  }
                >
                  <Sidebar.ItemIcon>
                    <FileText />
                  </Sidebar.ItemIcon>
                  <Sidebar.ItemLabel>Spec</Sidebar.ItemLabel>
                </Sidebar.Item>
                <Sidebar.Item
                  id="builds"
                  link={
                    <Link
                      to="/projects/$projectName/builds"
                      params={{ projectName }}
                    />
                  }
                >
                  <Sidebar.ItemIcon>
                    <ListChecks />
                  </Sidebar.ItemIcon>
                  <Sidebar.ItemLabel>Builds</Sidebar.ItemLabel>
                </Sidebar.Item>
                <Sidebar.Item
                  id="deployments"
                  link={
                    <Link
                      to="/projects/$projectName/deployments"
                      params={{ projectName }}
                    />
                  }
                >
                  <Sidebar.ItemIcon>
                    <Rocket />
                  </Sidebar.ItemIcon>
                  <Sidebar.ItemLabel>Deployments</Sidebar.ItemLabel>
                </Sidebar.Item>
                <Sidebar.Item
                  id="validation"
                  link={
                    <Link
                      to="/projects/$projectName/validation"
                      params={{ projectName }}
                    />
                  }
                >
                  <Sidebar.ItemIcon>
                    <CircleCheck />
                  </Sidebar.ItemIcon>
                  <Sidebar.ItemLabel>Validation</Sidebar.ItemLabel>
                </Sidebar.Item>
                <Sidebar.Item
                  id="issues"
                  link={
                    <Link
                      to="/projects/$projectName/issues"
                      params={{ projectName }}
                    />
                  }
                >
                  <Sidebar.ItemIcon>
                    <CircleAlert />
                  </Sidebar.ItemIcon>
                  <Sidebar.ItemLabel>Issues</Sidebar.ItemLabel>
                </Sidebar.Item>
              </Sidebar.Category>
            ) : (
              <Sidebar.Category>
                <Sidebar.Item id="projects" link={<Link to="/" />}>
                  <Sidebar.ItemIcon>
                    <FolderOpen />
                  </Sidebar.ItemIcon>
                  <Sidebar.ItemLabel>Projects</Sidebar.ItemLabel>
                </Sidebar.Item>
                <Sidebar.Item id="resources" link={<Link to="/resources" />}>
                  <Sidebar.ItemIcon>
                    <Boxes />
                  </Sidebar.ItemIcon>
                  <Sidebar.ItemLabel>Resources</Sidebar.ItemLabel>
                </Sidebar.Item>
                <Sidebar.Item id="endpoints" link={<Link to="/endpoints" />}>
                  <Sidebar.ItemIcon>
                    <Radio />
                  </Sidebar.ItemIcon>
                  <Sidebar.ItemLabel>Endpoints</Sidebar.ItemLabel>
                </Sidebar.Item>
                {/* Global Alerts section (#155) — RCA-agent reports across every project. */}
                <Sidebar.Item id="alerts" link={<Link to="/alerts" />}>
                  <Sidebar.ItemIcon>
                    <Siren />
                  </Sidebar.ItemIcon>
                  <Sidebar.ItemLabel>Alerts</Sidebar.ItemLabel>
                </Sidebar.Item>
              </Sidebar.Category>
            )}
          </Sidebar.Nav>
          <Sidebar.Footer>
            <Sidebar.Category>
              {/* Org-level Settings (issue #96) — not the UserMenu's
                  personal-settings stub above, which is untouched. */}
              <Sidebar.Item id="settings" link={<Link to="/settings" />}>
                <Sidebar.ItemIcon>
                  <Settings />
                </Sidebar.ItemIcon>
                <Sidebar.ItemLabel>Settings</Sidebar.ItemLabel>
              </Sidebar.Item>
            </Sidebar.Category>
          </Sidebar.Footer>
        </Sidebar>
      </AppShell.Sidebar>

      <AppShell.Main>
        {/* Content + the project AI panel side by side: the page shrinks
            rather than being overlaid; the panel mounts only while open.
            AppShell.Main is itself a flex container, so this wrapper must
            grow (it's a flex ITEM) or it collapses to content width. */}
        <Box
          sx={{
            display: "flex",
            flexGrow: 1,
            width: "100%",
            minWidth: 0,
            height: "100%",
            minHeight: 0,
          }}
        >
          <Box sx={{ flexGrow: 1, minWidth: 0 }}>
            <Outlet />
          </Box>
          {/* Horizontal Collapse gives the sidebar-style slide; unmountOnExit
              keeps the closed panel out of the tree (no idle polling). */}
          {projectName && (
            <Collapse
              in={chatOpen}
              orientation="horizontal"
              unmountOnExit
              sx={{ height: "100%", flexShrink: 0 }}
            >
              <AgentChatPanel
                org={orgHandle ?? "default"}
                projectName={projectName}
                onClose={() => setChatOpen(false)}
                {...(generate ? { autoGenerate: generate } : {})}
                onAutoGenerated={clearGenerate}
              />
            </Collapse>
          )}
        </Box>
      </AppShell.Main>

      <AppShell.Footer>
        {/* Slim variant: the default footer padding spends ~66px of every
            page on two caption lines; halving it keeps the pane taller. */}
        <Footer sx={{ py: 0.5 }}>
          <Footer.Copyright>
            © {new Date().getFullYear()} WSO2 LLC.
          </Footer.Copyright>
          <Footer.Link
            href={`${REPO_URL}/tree/HEAD/docs`}
            target="_blank"
            rel="noopener"
          >
            Docs
          </Footer.Link>
          <Footer.Link
            href={`${REPO_URL}/issues/new`}
            target="_blank"
            rel="noopener"
          >
            Report an issue
          </Footer.Link>
          <Footer.Link href={REPO_URL} target="_blank" rel="noopener">
            GitHub
          </Footer.Link>
        </Footer>
      </AppShell.Footer>

      <AppShell.NotificationPanel>
        <AlertsNotificationPanel />
      </AppShell.NotificationPanel>
    </AppShell>
  );
}
