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

import { Link, Outlet, useLocation } from "@tanstack/react-router";
import {
  Box,
  Card,
  CardContent,
  PageContent,
  Tab,
  Tabs,
  Tooltip,
  useMediaQuery,
  useTheme,
} from "@wso2/oxygen-ui";
import { Coins, KeyRound, Sparkles } from "@wso2/oxygen-ui-icons-react";
import { PageHeader } from "../../../components/PageHeader";
import { usePermissions, type Permissions } from "../../../auth/permissions";
import { DENIED } from "../../../auth/denialCopy";

// Each section is its own route (issue #143): deep-linkable and back/forward
// correct, matching the legacy console's settings/<section> URLs. Every
// section here has nothing at all to show a caller holding none of its
// permissions, so every one of them gets its tab disabled in that case —
// SkillsSection's own internal ae:skill-view/-config check is a second,
// belt-and-suspenders gate for direct URL access, not a substitute for this.
//
// Each section carries its own gate as a predicate over the caller's
// permissions rather than a resolved boolean, which is what lets the check
// live in the row it decides instead of being hoisted above the map. The
// order is also the landing priority for bare /settings — settings.index
// walks this list rather than repeating either the gates or the order.
export const SECTIONS = [
  {
    path: "/settings/credentials",
    label: "Credentials",
    Icon: KeyRound,
    // Either permission makes Credentials worth landing on:
    // GitHubCredentialCard and AnthropicCredentialCard each gate their own
    // half independently, because the BFF redacts each card separately.
    allowed: (can: Permissions) =>
      can.hasAny(["ae:github-config", "ae:model-config"]),
    deniedTooltip: DENIED.viewCredentials,
  },
  {
    // Exact-match ae:skill-view, NOT OR'd with ae:skill-config — entry to a
    // section is gated on the view permission exactly.
    path: "/settings/skills",
    label: "Skills",
    Icon: Sparkles,
    allowed: (can: Permissions) => can.has("ae:skill-view"),
    deniedTooltip: DENIED.viewSkills,
  },
  {
    path: "/settings/usage",
    label: "Usage",
    Icon: Coins,
    allowed: (can: Permissions) => can.has("ae:usage-view"),
    deniedTooltip: DENIED.viewUsage,
  },
] as const;

/** A settings section's route, e.g. "/settings/skills". */
export type SettingsSectionPath = (typeof SECTIONS)[number]["path"];

// v1 note (issue #96): no role gate here — any authenticated org member who
// reaches /settings gets full access. Architect/SRE is the intended owner,
// not an enforced restriction (no server-side RBAC on /config or /skills*
// today, and no reliable client-side role signal either).
export function SettingsLayout() {
  const theme = useTheme();
  const isMobile = useMediaQuery(theme.breakpoints.down("sm"));
  const { pathname } = useLocation();
  const can = usePermissions();

  const active =
    SECTIONS.find((s) => pathname.startsWith(s.path))?.path ?? SECTIONS[0].path;

  return (
    <PageContent>
      <PageHeader
        title="Settings"
        subtitle="Org-level GitHub and model credentials, the skills catalogue, and per-project agent usage"
      />

      <Box
        sx={{
          display: "flex",
          flexDirection: { xs: "column", sm: "row" },
          gap: 3,
        }}
      >
        {/* flexShrink:0 — the skills table's intrinsic width would otherwise
            squeeze this rail and clip the tabs. */}
        <Card
          variant="outlined"
          sx={{
            width: { xs: "100%", sm: 220 },
            flexShrink: 0,
            height: "fit-content",
          }}
        >
          <CardContent sx={{ p: 2 }}>
            <Tabs
              orientation={isMobile ? "horizontal" : "vertical"}
              variant={isMobile ? "fullWidth" : "standard"}
              value={active}
            >
              {SECTIONS.map(({ path, label, Icon, allowed, deniedTooltip }) => {
                const denied = !allowed(can);
                const tab = (
                  <Tab
                    key={path}
                    value={path}
                    component={Link}
                    to={path}
                    icon={<Icon size={18} />}
                    iconPosition="start"
                    label={label}
                    disabled={denied}
                  />
                );
                // Tooltip needs a real DOM node ref even while disabled, so
                // it wraps the Tab rather than replacing it — MUI's disabled
                // Tab already blocks the click/navigation via pointer-events.
                return denied ? (
                  <Tooltip key={path} title={deniedTooltip}>
                    <span>{tab}</span>
                  </Tooltip>
                ) : (
                  tab
                );
              })}
            </Tabs>
          </CardContent>
        </Card>

        <Box sx={{ flexGrow: 1, minWidth: 0 }}>
          <Outlet />
        </Box>
      </Box>
    </PageContent>
  );
}
