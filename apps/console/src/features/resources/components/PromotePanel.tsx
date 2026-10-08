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
import { useNavigate } from "@tanstack/react-router";
import {
  Alert,
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Skeleton,
  TextField,
  Typography,
} from "@wso2/oxygen-ui";
import type { components } from "../../../generated/aep-api";
import { useEnvironments } from "../../deploy/api/deploy";
import { usePromoteExternalResource } from "../api/resources";
import { cellKey, promoteProblems, promoteRequest, type PromoteDraft } from "../model/resourceDraft";
import { useProjectLabel } from "./ResourcesPage";

type ExternalResourceDTO = components["schemas"]["ExternalResourceDTO"];

/**
 * Promote to organization, a Panel over a project's resource: the
 * organization takes the project's record (name, provider, keys, description,
 * contract document) and adds how projects should use it and a value for
 * every key in every environment. A value left blank is carried over from
 * the project. The project's dependency then reuses the organization's
 * record, and the card opens on it, registered and editable.
 */
export function PromotePanel({
  project,
  resource,
  onClose,
}: {
  project: string;
  resource: ExternalResourceDTO;
  onClose: () => void;
}) {
  const navigate = useNavigate();
  const label = useProjectLabel();
  const environments = useEnvironments();
  const promote = usePromoteExternalResource(project, resource.name);
  const [draft, setDraft] = useState<PromoteDraft>({ consumptionInstructions: "", values: {} });
  const [tried, setTried] = useState(false);
  const keys = resource.config ?? [];
  const envs = environments.data ?? [];
  const envNames = envs.map((e) => e.name);
  const problems = promoteProblems(draft, { keys, environments: envNames });
  const shown = tried ? problems : null;

  const submit = () => {
    setTried(true);
    if (problems || promote.isPending || !environments.data) return;
    promote.mutate(promoteRequest(draft, { keys, environments: envNames }), {
      onSuccess: (record) => {
        onClose();
        void navigate({ to: "/resources/$name", params: { name: record.name }, search: {}, replace: true });
      },
    });
  };

  return (
    <Dialog open onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>Promote {resource.name} to the organization</DialogTitle>
      <DialogContent sx={{ display: "flex", flexDirection: "column", gap: 2.5 }}>
        <DialogContentText>
          The organization takes {label(project)}&apos;s {resource.name} as its own record, and holds its values. Every
          project that needs {resource.provider || "it"} can then reuse it; {label(project)} already does.
        </DialogContentText>
        <TextField
          label="Consumption instructions"
          required
          multiline
          minRows={3}
          value={draft.consumptionInstructions}
          onChange={(e) => setDraft((d) => ({ ...d, consumptionInstructions: e.target.value }))}
          error={Boolean(shown?.consumptionInstructions)}
          helperText={shown?.consumptionInstructions ?? "How a project should use it, beyond what it is. The coding agent reads this."}
        />
        <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
          <Typography sx={{ fontSize: "0.8125rem", fontWeight: 600 }}>Values</Typography>
          <Typography variant="caption" color="text.secondary">
            Leave a value blank to carry over {label(project)}&apos;s own. An environment the project has no value for
            needs one here.
          </Typography>
          {environments.isError ? (
            <Alert severity="error">{environments.error.message}</Alert>
          ) : !environments.data ? (
            <Skeleton variant="rounded" height={80} />
          ) : (
            envs.map((env) => (
              <Box key={env.name} sx={{ display: "flex", flexDirection: "column", gap: 0.75 }}>
                <Typography variant="caption" sx={{ fontWeight: 600 }}>
                  {env.displayName}
                </Typography>
                {keys.map((k) => {
                  const cell = cellKey(env.name, k.key);
                  return (
                    <TextField
                      key={cell}
                      size="small"
                      label={k.key}
                      type={k.secret ? "password" : "text"}
                      autoComplete="off"
                      value={draft.values[cell] ?? ""}
                      placeholder="Carried over"
                      onChange={(e) => setDraft((d) => ({ ...d, values: { ...d.values, [cell]: e.target.value } }))}
                      slotProps={{ inputLabel: { shrink: true } }}
                    />
                  );
                })}
                {shown?.environments[env.name] && (
                  <Typography variant="caption" color="error">
                    {shown.environments[env.name]}
                  </Typography>
                )}
              </Box>
            ))
          )}
        </Box>
        {promote.isError && <Alert severity="error">{promote.error.message}</Alert>}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button variant="contained" disabled={promote.isPending} onClick={submit}>
          {promote.isPending ? "Promoting…" : "Promote"}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
