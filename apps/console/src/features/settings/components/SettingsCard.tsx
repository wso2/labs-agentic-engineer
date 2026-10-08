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

import { useCallback } from "react";
import { createLink, useNavigate } from "@tanstack/react-router";
import { Box, ButtonBase, NativeSelect, Skeleton, Typography } from "@wso2/oxygen-ui";
import { CardFrame } from "../../shell/components/CardFrame";
import { PHONE } from "../../shell/layout";
import { soft } from "../../spec/components/Tag";
import { UsageSection } from "../../usage/components/UsageSection";
import { useConfig } from "../api/queries";
import { SETTINGS_SECTIONS, type SettingsSection } from "../settingsSection";
import { AiAgentsCard } from "./AiAgentsCard";
import { GitHubSection } from "./GitHubSection";
import { SettingsPane, PANE_MAX_WIDTH } from "./SettingsPane";

const MenuLink = createLink(ButtonBase);

/**
 * The org's Settings card, over the Dashboard (`/settings`), laid out like
 * the Spec card: a menu of sections on the left, the open one (`section`,
 * from the address) on the right. At phone width the menu gives way to a
 * picker above the section. Closing goes back to the Dashboard. It sets no
 * Turn scope: the org's chat has nothing to be about here yet.
 */
export function SettingsCard({ section }: { section: SettingsSection }) {
  const navigate = useNavigate();
  const close = useCallback(() => void navigate({ to: "/" }), [navigate]);

  return (
    <CardFrame
      name="Settings"
      heading={
        <Typography component="h2" sx={{ fontSize: "1rem", fontWeight: 600, flex: 1 }}>
          Settings
        </Typography>
      }
      closeHint="Back to Dashboard"
      onClose={close}
      fill
    >
      <Box sx={{ display: "flex", height: "100%", minHeight: 0 }}>
        <SectionMenu current={section} />
        <Box sx={{ flex: 1, minWidth: 0, overflowY: "auto", px: 4, pt: 3, pb: 11, [PHONE]: { px: 2, pb: 17.5 } }}>
          <SectionPicker current={section} />
          <SectionBody section={section} />
        </Box>
      </Box>
    </CardFrame>
  );
}

function SectionBody({ section }: { section: SettingsSection }) {
  const config = useConfig();
  if (section === "usage") {
    return (
      <SettingsPane title="Usage" intro="What the agents have spent on each project, in dollars and in tokens.">
        <UsageSection />
      </SettingsPane>
    );
  }
  // The onboarding gate has loaded the config before any route draws, so
  // this waits only on a refetch that dropped it.
  if (!config.data) return <Skeleton variant="rounded" height={240} />;
  // The AI agents card carries its own title and readiness, so it is the
  // section by itself rather than under a second "AI agents" heading.
  if (section === "ai") {
    return (
      <Box sx={{ maxWidth: PANE_MAX_WIDTH }}>
        <AiAgentsCard config={config.data} />
      </Box>
    );
  }
  return <GitHubSection gitProvider={config.data.gitProvider} />;
}

/** The sections, as the Spec card lists its files. */
function SectionMenu({ current }: { current: SettingsSection }) {
  return (
    <Box
      component="nav"
      aria-label="Settings sections"
      sx={{
        width: 250,
        flexShrink: 0,
        borderRight: 1,
        borderColor: "divider",
        overflowY: "auto",
        px: 1,
        pt: 1.75,
        pb: 10,
        display: "flex",
        flexDirection: "column",
        gap: 0.125,
        [PHONE]: { display: "none" },
      }}
    >
      <Typography
        component="h3"
        sx={{
          px: 1.25,
          pt: 1.25,
          pb: 0.5,
          fontSize: "0.6875rem",
          letterSpacing: "0.08em",
          textTransform: "uppercase",
          fontWeight: 600,
          color: "text.secondary",
        }}
      >
        Organization
      </Typography>
      {SETTINGS_SECTIONS.map(({ key, label }) => {
        const selected = key === current;
        return (
          <MenuLink
            key={key}
            to="/settings"
            search={{ section: key }}
            aria-current={selected ? "page" : undefined}
            sx={{
              width: "100%",
              justifyContent: "flex-start",
              py: 0.75,
              px: 1.25,
              borderRadius: 1.5,
              fontSize: "0.8125rem",
              textAlign: "start",
              color: selected ? "primary.main" : "text.primary",
              fontWeight: selected ? 600 : 400,
              bgcolor: selected ? soft("primary") : "transparent",
              "&:hover": { bgcolor: selected ? soft("primary") : "action.hover" },
            }}
          >
            {label}
          </MenuLink>
        );
      })}
    </Box>
  );
}

/** At phone width the menu gives way to this: the same sections, in a picker. */
function SectionPicker({ current }: { current: SettingsSection }) {
  const navigate = useNavigate();
  return (
    <NativeSelect
      value={current}
      onChange={(e) => void navigate({ to: "/settings", search: { section: e.target.value as SettingsSection } })}
      inputProps={{ "aria-label": "Settings section" }}
      sx={{ display: "none", mb: 2.5, width: "100%", [PHONE]: { display: "inline-flex" } }}
    >
      {SETTINGS_SECTIONS.map(({ key, label }) => (
        <option key={key} value={key}>
          {label}
        </option>
      ))}
    </NativeSelect>
  );
}
