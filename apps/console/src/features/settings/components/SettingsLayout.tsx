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
  useMediaQuery,
  useTheme,
} from "@wso2/oxygen-ui";
import { Coins, KeyRound, Sparkles } from "@wso2/oxygen-ui-icons-react";
import { PageHeader } from "../../../components/PageHeader";

// Each section is its own route (issue #143): deep-linkable and back/forward
// correct, matching the legacy console's settings/<section> URLs.
const SECTIONS = [
  { path: "/settings/credentials", label: "Credentials", Icon: KeyRound },
  { path: "/settings/skills", label: "Skills", Icon: Sparkles },
  { path: "/settings/usage", label: "Usage", Icon: Coins },
] as const;

// v1 note (issue #96): no role gate here — any authenticated org member who
// reaches /settings gets full access. Architect/SRE is the intended owner,
// not an enforced restriction (no server-side RBAC on /config or /skills*
// today, and no reliable client-side role signal either).
export function SettingsLayout() {
  const theme = useTheme();
  const isMobile = useMediaQuery(theme.breakpoints.down("sm"));
  const { pathname } = useLocation();

  const active =
    SECTIONS.find((s) => pathname.startsWith(s.path))?.path ?? SECTIONS[0].path;

  return (
    <PageContent>
      <PageHeader
        title="Settings"
        subtitle="Org-level GitHub and Anthropic credentials, the skills catalogue, and per-project agent usage"
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
              {SECTIONS.map(({ path, label, Icon }) => (
                <Tab
                  key={path}
                  value={path}
                  component={Link}
                  to={path}
                  icon={<Icon size={18} />}
                  iconPosition="start"
                  label={label}
                />
              ))}
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
