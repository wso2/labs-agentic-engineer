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

import { useId, useMemo, useState, type ReactNode } from "react";
import { Alert, Box, Tab, Tabs, Typography } from "@wso2/oxygen-ui";

import { ConfigurationTab } from "./ConfigurationTab.js";
import { InstructionsTab } from "./InstructionsTab.js";
import { isParseError, parseAgentAfm } from "./parse.js";
import { countUnresolved, ToolsTab } from "./ToolsTab.js";

/**
 * Read-time resolution status for ONE `x-aep.tools.openapi[].allow` entry,
 * keyed `"<component>:<operation>"` in AgentViewProps.toolStatus. This is the
 * ONLY source of `status`/`reason`: they are computed server-side by
 * spec.ComputeAgentToolStatus on every design read and never authored into
 * agent.afm.md, so parse.ts deliberately does not derive them (see its
 * file-header comment). Kept as raw strings rather than a closed union so an
 * unrecognized value still renders instead of pinning this package to the
 * server's exact enum.
 */
export interface AgentToolStatusInfo {
  /** "resolved" | "unresolved" | "unchecked". */
  status: string;
  /** Empty on resolved; human-readable otherwise. */
  reason?: string | undefined;
}

/**
 * The organisation's model connection, as the console's Settings holds it.
 * Every agent runs on it, which is why it comes from the caller and not from
 * agent.afm.md: the AFM's `model:` block is `${env:}` placeholders the deploy
 * fills in from this same connection.
 */
export interface AgentModelConnection {
  /** Model ID, e.g. "claude-sonnet-5". */
  model: string;
  /** Display label for the message format, e.g. "Anthropic Messages". */
  format: string;
  /** Host of the connection's base URL, e.g. "api.anthropic.com". */
  host: string;
}

export interface AgentViewProps {
  /** Raw `agent.afm.md` text. */
  spec: string;
  /**
   * Persist an edited prompt body. Given, the Instructions card becomes
   * editable; omitted, the whole view stays read-only.
   *
   * ONLY the body is ever editable. The front matter is platform-wiring —
   * `${env:}` values the deploy fills in — and `x-aep.tools.openapi[].allow`
   * is the security boundary: an operation not listed is never generated as a
   * tool, so a text box over it would be a way to grant one. The prose is the
   * part a person tunes, and the only part that cannot break the wiring.
   *
   * Receives the body ALONE. Reassembling the document is the caller's job,
   * because only the caller can read the front matter that is live at save
   * time — see SpecView, which keeps the agent's own front-matter edits.
   */
  onSaveBehaviour?: ((body: string) => Promise<void>) | undefined;
  /**
   * Render the prompt body as markdown. A render prop rather than a dependency:
   * this package carries no markdown renderer, so the console passes the ONE it
   * already uses for the PRD and alerts and everything stays a single product.
   * Without it the body renders as plain pre-wrapped text — which is also
   * exactly what the model receives.
   */
  renderMarkdown?: ((markdown: string) => ReactNode) | undefined;
  /**
   * OPTIONAL per-operation resolution status, keyed `"<component>:<operation>"`,
   * from the design read model's `Dependency.operations`. Optional and keyed
   * defensively — a missing entry simply renders without a chip — so a caller
   * that does not fetch dependencies is unaffected, mirroring DesignView's
   * `dependencyStatus`.
   */
  toolStatus?: Record<string, AgentToolStatusInfo> | undefined;
  /**
   * The organisation's model connection for the Configuration tab's Model
   * panel: `"loading"` while the caller fetches it, `null` when the
   * organisation has none. Omitted, the panel is left out.
   */
  modelConnection?: AgentModelConnection | null | "loading" | undefined;
  /** Where the connection is changed (a link to Settings), shown beside it. */
  settingsLink?: ReactNode;
  /**
   * What the last deploy did with each declared guardrail, keyed by policy,
   * one entry per environment — from the deployments read
   * (`Deployment.guardrails`). Omitted, the Guardrails panel lists what the
   * spec declares without a status; given but missing a policy, that
   * guardrail has not been deployed yet.
   */
  guardrailStatus?: Record<string, AgentGuardrailStatus[]> | undefined;
}

/** One environment's outcome for a declared guardrail. */
export interface AgentGuardrailStatus {
  environment: string;
  /** "applied" | "partial" | "unavailable" | "invalid" | "conflict" | "failed" | "unsupported". */
  status: string;
  reason?: string | undefined;
}

const AGENT_BADGE_COLOR = "#7c3aed";

type TabKey = "instructions" | "tools" | "configuration";

function SolidBadge({ label, color }: { label: string; color: string }) {
  return (
    <Box
      component="span"
      sx={(theme) => ({
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        px: 1,
        py: 0.5,
        borderRadius: 1,
        flexShrink: 0,
        fontFamily: "monospace",
        fontSize: "0.6875rem",
        fontWeight: 700,
        letterSpacing: "0.06em",
        textTransform: "uppercase",
        bgcolor: color,
        color: theme.palette.getContrastText(color),
      })}
    >
      {label}
    </Box>
  );
}

/**
 * An agent's spec in three tabs: Instructions (the prompt, which IS the agent),
 * Tools (the allow-list, the security boundary) and Configuration (how it is
 * reached, what it remembers, the model it runs on). The name and description
 * sit above the tabs because they belong to all three.
 */
export function AgentView({
  spec,
  toolStatus,
  onSaveBehaviour,
  renderMarkdown,
  modelConnection,
  settingsLink,
  guardrailStatus,
}: AgentViewProps) {
  const attempt = useMemo(() => parseAgentAfm(spec), [spec]);
  const [tab, setTab] = useState<TabKey>("instructions");
  const idPrefix = useId();

  if (isParseError(attempt)) {
    return (
      <Box sx={{ p: 3 }}>
        <Alert severity="warning">{attempt.error}</Alert>
      </Box>
    );
  }

  // A broken tool is the one thing on a hidden tab a reviewer must not miss,
  // so its count rides on the tab label.
  const unresolved = countUnresolved(attempt.tools, toolStatus);
  const tabId = (key: TabKey) => `${idPrefix}-tab-${key}`;
  const panelId = (key: TabKey) => `${idPrefix}-panel-${key}`;
  const tabProps = (key: TabKey) => ({ value: key, id: tabId(key), "aria-controls": panelId(key) });

  return (
    <Box sx={{ p: 3, overflow: "auto", height: "100%" }}>
      <Box sx={{ maxWidth: 960, mx: "auto" }}>
        <Box sx={{ display: "flex", alignItems: "center", gap: 1, mb: 1 }}>
          <SolidBadge label="ai-agent" color={AGENT_BADGE_COLOR} />
        </Box>
        <Typography variant="h4" sx={{ fontWeight: 700, lineHeight: 1.2 }}>
          {attempt.name || "agent"}
        </Typography>
        {attempt.description ? (
          <Typography variant="body1" color="text.secondary" sx={{ mt: 1 }}>
            {attempt.description}
          </Typography>
        ) : null}

        <Tabs
          value={tab}
          onChange={(_, value: TabKey) => setTab(value)}
          sx={{ mt: 3, mb: 2.5, borderBottom: 1, borderColor: "divider" }}
        >
          <Tab {...tabProps("instructions")} label="Instructions" />
          <Tab
            {...tabProps("tools")}
            label={
              unresolved > 0 ? (
                <Box component="span" sx={{ display: "inline-flex", gap: 0.75, alignItems: "center" }}>
                  Tools
                  <Box component="span" sx={{ color: "error.main", fontWeight: 700 }}>
                    · {unresolved} unresolved
                  </Box>
                </Box>
              ) : (
                "Tools"
              )
            }
          />
          <Tab {...tabProps("configuration")} label="Configuration" />
        </Tabs>

        <Box role="tabpanel" id={panelId(tab)} aria-labelledby={tabId(tab)}>
          {tab === "instructions" && (
            <InstructionsTab
              body={attempt.body}
              onSaveBehaviour={onSaveBehaviour}
              renderMarkdown={renderMarkdown}
            />
          )}
          {tab === "tools" && <ToolsTab tools={attempt.tools} toolStatus={toolStatus} />}
          {tab === "configuration" && (
            <ConfigurationTab
              spec={attempt}
              modelConnection={modelConnection}
              settingsLink={settingsLink}
              guardrailStatus={guardrailStatus}
            />
          )}
        </Box>
      </Box>
    </Box>
  );
}
