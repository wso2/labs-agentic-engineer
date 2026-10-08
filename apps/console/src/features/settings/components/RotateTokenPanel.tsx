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

import { useState } from "react";
import {
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  TextField,
} from "@wso2/oxygen-ui";
import type { components } from "../../../generated/aep-api";
import { useConnectGitHubPat } from "../api/queries";
import { SecretField } from "./CredentialField";
import { GitHubPatScopeGuide } from "./GitHubPatScopeGuide";

type GitProviderProjection = components["schemas"]["GitProviderProjection"];

/**
 * The Rotate token Panel: a new personal access token for the same GitHub
 * organization, through the same connect onboarding uses (the server checks
 * the token against GitHub before it keeps it). The organization is the
 * connection's, so rotating asks only for the token; a connection that does
 * not name one asks for it too.
 */
export function RotateTokenPanel({
  gitProvider,
  onClose,
  onRotated,
}: {
  gitProvider: GitProviderProjection;
  onClose: () => void;
  onRotated: () => void;
}) {
  const knownOrg = gitProvider.githubLogin ?? "";
  const [pat, setPat] = useState("");
  const [githubLogin, setGithubLogin] = useState(knownOrg);
  const connect = useConnectGitHubPat();
  const org = githubLogin.trim();
  const ready = pat.trim() !== "" && org !== "" && !connect.isPending;

  const submit = () => {
    if (!ready) return;
    connect.mutate({ pat: pat.trim(), githubLogin: org }, { onSuccess: onRotated });
  };

  return (
    <Dialog open onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>Rotate the GitHub token</DialogTitle>
      <DialogContent>
        <Box
          component="form"
          id="rotate-github-token"
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
          sx={{ display: "flex", flexDirection: "column", gap: 2, pt: 1 }}
        >
          {!knownOrg && (
            <TextField
              required
              label="GitHub organization name"
              placeholder="octocat"
              value={githubLogin}
              onChange={(e) => setGithubLogin(e.target.value)}
              helperText="The GitHub organization the platform reads and writes repos in."
              fullWidth
            />
          )}
          <SecretField
            label="New personal access token"
            placeholder="github_pat_…"
            value={pat}
            onChange={setPat}
            disabled={connect.isPending}
            error={connect.isError ? connect.error.message : undefined}
            helperText={
              knownOrg
                ? `It replaces the current token for ${knownOrg}, for every project.`
                : "It replaces the current token for every project."
            }
            noun="token"
          />
          <GitHubPatScopeGuide />
        </Box>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button type="submit" form="rotate-github-token" variant="contained" disabled={!ready}>
          {connect.isPending ? "Validating…" : "Save token"}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
