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

import { useState, type ReactNode } from "react";
import { Alert, Box, Button, Typography } from "@wso2/oxygen-ui";
import type { components } from "../../../generated/aep-api";
import { DisconnectGitHubPanel } from "./DisconnectGitHubPanel";
import { RotateTokenPanel } from "./RotateTokenPanel";
import { SettingsPane } from "./SettingsPane";

type GitProviderProjection = components["schemas"]["GitProviderProjection"];

function when(iso: string | undefined): string | null {
  if (!iso) return null;
  const at = new Date(iso);
  return Number.isNaN(at.getTime()) ? null : at.toLocaleString();
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <Typography component="dt" variant="body2" color="text.secondary">
        {label}
      </Typography>
      <Typography component="dd" variant="body2" sx={{ m: 0, minWidth: 0, overflowWrap: "anywhere" }}>
        {children}
      </Typography>
    </>
  );
}

/**
 * Settings, GitHub: the org's connection as it stands (which organization,
 * whose token, since when, when it was last checked, its status), with
 * Rotate token and Disconnect, each a Panel. The org always has a connection
 * here: without one, onboarding holds the console until it is made.
 */
export function GitHubSection({ gitProvider }: { gitProvider: GitProviderProjection | null }) {
  const [panel, setPanel] = useState<"rotate" | "disconnect" | null>(null);
  const [rotated, setRotated] = useState(false);
  const close = () => setPanel(null);

  if (!gitProvider) {
    return (
      <SettingsPane title="GitHub" intro="Where the agents read and write every project's repository.">
        <Typography variant="body2" color="text.secondary">
          Not connected.
        </Typography>
      </SettingsPane>
    );
  }

  const org = gitProvider.githubLogin ?? gitProvider.identityLogin;
  const who = gitProvider.identityName ?? gitProvider.identityLogin ?? gitProvider.githubLogin;
  const checked = when(gitProvider.lastValidatedAt);

  return (
    <SettingsPane
      title="GitHub"
      intro="Where the agents read and write every project's repository."
      action={
        <Box sx={{ display: "flex", gap: 1 }}>
          <Button size="small" variant="outlined" onClick={() => setPanel("rotate")}>
            Rotate token
          </Button>
          <Button size="small" color="error" onClick={() => setPanel("disconnect")}>
            Disconnect
          </Button>
        </Box>
      }
    >
      {rotated && (
        <Alert severity="success" role="status" onClose={() => setRotated(false)}>
          Token rotated. Agents use the new one from their next step.
        </Alert>
      )}
      <Box
        component="dl"
        sx={{ display: "grid", gridTemplateColumns: "max-content 1fr", columnGap: 3, rowGap: 1, m: 0 }}
      >
        {org && <Fact label="Organization">{org}</Fact>}
        {who && (
          <Fact label="Connected as">
            {who}
            {gitProvider.identityEmail ? ` (${gitProvider.identityEmail})` : ""}
            {gitProvider.mode === "app" ? ", through the GitHub App" : ", with a personal access token"}
          </Fact>
        )}
        <Fact label="Since">{when(gitProvider.connectedAt) ?? "unknown"}</Fact>
        <Fact label="Last checked">{checked ?? "not yet"}</Fact>
        <Fact label="Status">{gitProvider.status}</Fact>
      </Box>

      {panel === "rotate" && (
        <RotateTokenPanel
          gitProvider={gitProvider}
          onClose={close}
          onRotated={() => {
            close();
            setRotated(true);
          }}
        />
      )}
      {panel === "disconnect" && <DisconnectGitHubPanel appInstalled={gitProvider.mode === "app"} onClose={close} />}
    </SettingsPane>
  );
}
