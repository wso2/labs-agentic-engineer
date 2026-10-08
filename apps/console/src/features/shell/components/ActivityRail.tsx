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

import type { ReactNode } from "react";
import { createLink } from "@tanstack/react-router";
import { Badge, Box, IconButton, Link, Tooltip, type SxProps, type Theme } from "@wso2/oxygen-ui";
import {
  Boxes,
  CircleDot,
  ClipboardCheck,
  FileText,
  Hammer,
  House,
  Layers,
  LayoutGrid,
  MessageSquare,
  Plus,
  Rocket,
  Settings,
  Sparkles,
} from "@wso2/oxygen-ui-icons-react";
import { useAlerts } from "../../issues/useAlerts";
import type { ShellScope } from "../scope";
import { RAIL_WIDTH } from "../layout";
import { RailUserMenu } from "./RailUserMenu";

// MUI's polymorphic `component={Link}` does not typecheck against the router's
// typed `to`/`params`; createLink is the adapter (as in the old console).
const RailLink = createLink(IconButton);
const LogoLink = createLink(Link);

function railButtonSx(active: boolean): SxProps<Theme> {
  return {
    width: 40,
    height: 40,
    borderRadius: 2,
    position: "relative",
    color: active ? "var(--aep-shell-rail-active)" : "var(--aep-shell-rail-text)",
    "&:hover": {
      bgcolor: "var(--aep-shell-rail-hover)",
      color: "var(--aep-shell-rail-active)",
    },
    // The active item's marker: a short bar on the rail's leading edge.
    ...(active && {
      "&::before": {
        content: '""',
        position: "absolute",
        left: -6,
        top: 8,
        bottom: 8,
        width: 2,
        borderRadius: 1,
        bgcolor: "var(--aep-shell-rail-active)",
      },
    }),
  };
}

function RailTip({ label, children }: { label: string; children: ReactNode }) {
  return (
    <Tooltip title={label} placement="right">
      {/* Tooltip needs a child that holds a ref; the span keeps it off the
          router's link component. */}
      <Box component="span" sx={{ display: "inline-flex" }}>
        {children}
      </Box>
    </Tooltip>
  );
}

/**
 * The dark activity rail: the logo (home: the Dashboard, with a count of the
 * Alerts that need a person), New project, Projects, Skills, Resources; inside a project also
 * its Overview, Spec, Design, Builds, Validation, Deploy and Issues; then the
 * chat toggle, Settings (the org's card, over
 * the Dashboard) and the user menu at the bottom. The rail takes you to Pages; a Card opens from what a Page
 * shows, save the few the rail names.
 */
export function ActivityRail({
  scope,
  chatOpen,
  onToggleChat,
}: {
  scope: ShellScope;
  /** Null where there is no chat to toggle (New project, which is itself a prompt). */
  chatOpen: boolean | null;
  onToggleChat: () => void;
}) {
  const project = scope.kind === "project" ? scope : null;
  // An org Page is current only with no card over it: Settings open over the
  // Dashboard makes Settings the active item, not the logo.
  const onPage = (page: "dashboard" | "projects" | "new") =>
    scope.kind === "org" && scope.page === page && scope.card === null;
  const settingsOpen = scope.kind === "org" && scope.card === "settings";
  // Skills and Resources stay current with one of their cards open over them.
  const onOrgPage = (page: "skills" | "resources") => scope.kind === "org" && scope.page === page;
  const needsYou = useAlerts().needsPerson;
  const home = needsYou > 0 ? `Dashboard, ${needsYou} need${needsYou === 1 ? "s" : ""} you` : "Dashboard";

  return (
    <Box
      component="nav"
      aria-label="Activity rail"
      sx={{
        width: RAIL_WIDTH,
        flexShrink: 0,
        height: "100%",
        bgcolor: "var(--aep-shell-rail)",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        py: 1.25,
        gap: 0.5,
        // Above the phone-width chat overlay, which opens beside it.
        zIndex: (t) => t.zIndex.appBar,
      }}
    >
      {/* The logo is the way home: it opens the Dashboard. */}
      <RailTip label={home}>
        <Badge
          badgeContent={needsYou}
          color="warning"
          max={99}
          overlap="rectangular"
          slotProps={{ badge: { "aria-hidden": true } }}
          sx={{ mb: 1, "& .MuiBadge-badge": { pointerEvents: "none" } }}
        >
          <LogoLink
            to="/"
            aria-label={home}
            aria-current={onPage("dashboard") ? "page" : undefined}
            underline="none"
            sx={{
              width: 32,
              height: 32,
              borderRadius: 2,
              bgcolor: "primary.main",
              color: "primary.contrastText",
              display: "grid",
              placeItems: "center",
              fontFamily: "monospace",
              fontWeight: 600,
              textDecoration: "none",
              "&:focus-visible": { outline: "2px solid var(--aep-shell-rail-active)", outlineOffset: 2 },
            }}
          >
            ae
          </LogoLink>
        </Badge>
      </RailTip>
      <RailTip label="New project">
        <RailLink to="/projects/new" aria-label="New project" sx={railButtonSx(onPage("new"))}>
          <Plus size={20} />
        </RailLink>
      </RailTip>
      <RailTip label="Projects">
        <RailLink to="/projects" aria-label="Projects" sx={railButtonSx(onPage("projects"))}>
          <LayoutGrid size={20} />
        </RailLink>
      </RailTip>
      <RailTip label="Skills">
        <RailLink to="/skills" aria-label="Skills" sx={railButtonSx(onOrgPage("skills"))}>
          <Sparkles size={20} />
        </RailLink>
      </RailTip>
      <RailTip label="Resources">
        <RailLink to="/resources" aria-label="Resources" sx={railButtonSx(onOrgPage("resources"))}>
          <Boxes size={20} />
        </RailLink>
      </RailTip>
      {project && (
        <>
          <RailTip label="Overview">
            <RailLink
              to="/projects/$projectName"
              params={{ projectName: project.projectName }}
              aria-label="Overview"
              sx={railButtonSx(project.page === "overview" && project.card === null)}
            >
              <House size={20} />
            </RailLink>
          </RailTip>
          <RailTip label="Spec">
            <RailLink
              to="/projects/$projectName/spec"
              params={{ projectName: project.projectName }}
              aria-label="Spec"
              sx={railButtonSx(project.card === "spec")}
            >
              <FileText size={20} />
            </RailLink>
          </RailTip>
          <RailTip label="Design">
            <RailLink
              to="/projects/$projectName/design"
              params={{ projectName: project.projectName }}
              aria-label="Design"
              sx={railButtonSx(project.card === "design")}
            >
              <Layers size={20} />
            </RailLink>
          </RailTip>
          <RailTip label="Builds">
            <RailLink
              to="/projects/$projectName/builds"
              params={{ projectName: project.projectName }}
              aria-label="Builds"
              sx={railButtonSx(project.page === "builds")}
            >
              <Hammer size={20} />
            </RailLink>
          </RailTip>
          <RailTip label="Validation">
            <RailLink
              to="/projects/$projectName/validations"
              params={{ projectName: project.projectName }}
              aria-label="Validation"
              sx={railButtonSx(project.page === "validations")}
            >
              <ClipboardCheck size={20} />
            </RailLink>
          </RailTip>
          <RailTip label="Deploy">
            <RailLink
              to="/projects/$projectName/deploy"
              params={{ projectName: project.projectName }}
              aria-label="Deploy"
              sx={railButtonSx(project.page === "deploy")}
            >
              <Rocket size={20} />
            </RailLink>
          </RailTip>
          <RailTip label="Issues">
            <RailLink
              to="/projects/$projectName/issues"
              params={{ projectName: project.projectName }}
              aria-label="Issues"
              sx={railButtonSx(project.page === "issues")}
            >
              <CircleDot size={20} />
            </RailLink>
          </RailTip>
        </>
      )}
      <Box sx={{ flex: 1 }} />
      {chatOpen !== null && (
        <RailTip label={chatOpen ? "Hide agent chat" : "Show agent chat"}>
          <IconButton
            aria-label="Agent chat"
            aria-pressed={chatOpen}
            onClick={onToggleChat}
            sx={railButtonSx(chatOpen)}
          >
            <MessageSquare size={20} />
          </IconButton>
        </RailTip>
      )}
      <RailTip label="Settings">
        <RailLink
          to="/settings"
          aria-label="Settings"
          aria-current={settingsOpen ? "page" : undefined}
          sx={railButtonSx(settingsOpen)}
        >
          <Settings size={20} />
        </RailLink>
      </RailTip>
      <RailUserMenu />
    </Box>
  );
}
