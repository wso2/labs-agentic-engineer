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

import { useEffect, useMemo, useState } from "react";
import { Outlet, useMatches, useNavigate, useRouterState, useSearch } from "@tanstack/react-router";
import { Box } from "@wso2/oxygen-ui";
import { ErrorBoundary } from "../../../components/ErrorBoundary";
import { ChatPanel } from "../../agent-chat/components/ChatPanel";
import { OrgChatPanel } from "../../agent-chat/components/OrgChatPanel";
import { chatStore, useOpenQuestionsWhenAsked, useRefreshOnTurnEnd } from "../../agent-chat/useProjectChat";
import { ChatPanelContext, type ChatPanelControls } from "../chatPanel";
import { shellScope } from "../scope";
import { CHAT_OVERLAY_WIDTH, PHONE, PHONE_QUERY, RAIL_WIDTH } from "../layout";
import { useChatWidth } from "../useChatWidth";
import { ActivityRail } from "./ActivityRail";
import { ChatResizeHandle } from "./ChatResizeHandle";

function atPhoneWidth(): boolean {
  return window.matchMedia(PHONE_QUERY).matches;
}

/**
 * The app's frame: the activity rail, the chat panel, and the main
 * area where the routes draw (a base page, and a card over it).
 *
 * The chat follows the entity in view: inside a project it is the project's
 * conversation; on an org Page it is the organization's, which is not available
 * yet and shows as such. It starts open on a wide screen, beside the page in
 * the golden ratio and resizable (`useChatWidth`), and closed at phone width,
 * where it opens as an overlay beside the rail.
 */
export function Shell() {
  const matches = useMatches();
  const leaf = matches[matches.length - 1];
  const scope = shellScope({
    routeId: leaf?.routeId ?? "",
    params: (leaf?.params ?? {}) as { projectName?: string },
    search: (leaf?.search ?? {}) as { file?: unknown },
  });
  const pathname = useRouterState({ select: (s) => s.location.pathname });

  const [chatOpen, setChatOpen] = useState(() => !atPhoneWidth());
  const project = scope.kind === "project" ? scope : null;
  // New project is itself a prompt (its words become the project's first
  // message), so no second, inert composer sits beside it.
  const onNewProject = scope.kind === "org" && scope.page === "new";
  const chatShown = chatOpen && !onNewProject;

  const chatWidth = useChatWidth();
  const chatControls = useMemo<ChatPanelControls>(() => ({ open: () => setChatOpen(true) }), []);
  useRefreshOnTurnEnd();
  useOpenQuestionsWhenAsked();

  // Arriving from New project (`?chat=open` on the overview): the kickoff is
  // already running, so the chat opens, at phone width too, to show it. The
  // param is stripped at once, with `replace`, so it is the arrival's
  // behaviour and not the URL's. Overview only: stripping navigates there, so
  // honouring it under a card would move the user.
  const navigate = useNavigate();
  const chatParam = useSearch({ from: "/projects/$projectName", shouldThrow: false })?.chat;
  const arrivingProject =
    project && project.page === "overview" && project.card === null && chatParam === "open" ? project.projectName : null;
  useEffect(() => {
    if (!arrivingProject) return;
    setChatOpen(true);
    // This browser created the project, so its kickoff is the user's own
    // turn: the questions it asks open the Questions card.
    chatStore.claimKickoff(arrivingProject);
    void navigate({
      to: "/projects/$projectName",
      params: { projectName: arrivingProject },
      search: {},
      replace: true,
    });
  }, [arrivingProject, navigate]);

  return (
    <ChatPanelContext.Provider value={chatControls}>
      {/* The frame is the viewport: only the areas inside it scroll. Being a
          containing block makes its `overflow: hidden` hold for absolutely
          positioned descendants too, which would otherwise size the page. */}
      <Box sx={{ display: "flex", height: "100vh", overflow: "hidden", position: "relative", bgcolor: "background.default" }}>
        <ActivityRail
          scope={scope}
          chatOpen={onNewProject ? null : chatOpen}
          onToggleChat={() => setChatOpen((v) => !v)}
        />
        {chatShown && (
          <Box
            sx={{
              width: chatWidth.width,
              flexShrink: 0,
              height: "100%",
              position: "relative",
              bgcolor: "var(--aep-shell-chat)",
              borderRight: 1,
              borderColor: "divider",
              [PHONE]: {
                position: "fixed",
                left: RAIL_WIDTH,
                top: 0,
                bottom: 0,
                width: `min(${CHAT_OVERLAY_WIDTH}px, calc(100vw - ${RAIL_WIDTH}px))`,
                zIndex: (t) => t.zIndex.drawer,
                boxShadow: "var(--aep-shell-card-shadow)",
              },
            }}
          >
            {/* Beside the main outlet, outside its boundary, so it has its own:
                a throw here leaves the page usable. A new project resets it. */}
            <ErrorBoundary
              label="The chat panel"
              resetKey={project?.projectName ?? "org"}
              fill
              fallbackSx={{ height: "100%" }}
            >
              {project ? (
                <ChatPanel
                  projectName={project.projectName}
                  page={project.page}
                  card={project.card}
                  specFile={project.specFile}
                  onClose={() => setChatOpen(false)}
                />
              ) : (
                <OrgChatPanel onClose={() => setChatOpen(false)} />
              )}
            </ErrorBoundary>
            <ChatResizeHandle width={chatWidth.width} onResize={chatWidth.resizeTo} onReset={chatWidth.reset} />
          </Box>
        )}
        <Box
          component="main"
          sx={{ flex: 1, minWidth: 0, position: "relative", overflow: "hidden" }}
        >
          {/* A page that throws is contained here: the rail and the chat stay
              up. Routes carry no error component of their own, so every route's
              throw lands in this one; navigating clears it. */}
          <ErrorBoundary label="This page" resetKey={pathname} fill fallbackSx={{ height: "100%" }}>
            <Outlet />
          </ErrorBoundary>
        </Box>
      </Box>
    </ChatPanelContext.Provider>
  );
}
