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

import { Alert, AlertTitle, Box, CircularProgress, Typography } from "@wso2/oxygen-ui";
import { useConfig } from "../../settings/api/queries";
import { AiAgentsCard } from "../../settings/components/AiAgentsCard";
import { useHasPermission } from "../../../auth/permissions";
import { connectionWasDisconnected } from "../keyDisconnected";

/**
 * The wizard's "Connect a model" step: the settings card itself, unframed,
 * with an intro. Continue saves the connection (the save probes it), and the
 * wizard advances once `llm` is non-null.
 *
 * The full projection is read HERE, not handed down from the wizard: the
 * wizard runs on GET /config/status, which needs no AE permission, precisely
 * so the gate works before a first admin's grants are provisioned. This step
 * is the one part of it that genuinely needs ae:model-config — it writes the
 * `llm` section — so it is also the right place for GET /config's own gate to
 * bite, and for a caller without the permission to be told why rather than
 * shown a card whose every Save would 403.
 */
export function AiAgentsStep() {
  const canConfigureModel = useHasPermission("ae:model-config");
  const { data: config, isPending, isError, error } = useConfig(canConfigureModel);

  if (!canConfigureModel) {
    return (
      <Alert severity="info">
        <AlertTitle>You don&apos;t have access to the model connection</AlertTitle>
        Connecting the model your agents run on needs the model-configuration
        permission. Ask an organization admin to finish this step, or to grant it
        to you.
      </Alert>
    );
  }

  if (isPending) {
    return (
      <Box sx={{ display: "flex", justifyContent: "center", py: 4 }}>
        <CircularProgress size={28} />
      </Box>
    );
  }

  if (isError) {
    return (
      <Alert severity="error">
        <AlertTitle>Couldn&apos;t load your model settings</AlertTitle>
        {error.message}
      </Alert>
    );
  }

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
      {connectionWasDisconnected(config) ? (
        <Alert severity="warning">
          <AlertTitle>Your model connection was disconnected</AlertTitle>
          Agents cannot run until a connection is saved.
        </Alert>
      ) : (
        <Typography variant="body2" color="text.secondary">
          Connect the model your agents will use. Anthropic&apos;s API is filled
          in; switch the format or change the URL for any other provider or your
          own endpoint. Continue checks the connection and saves it; Test
          connection is optional. You can change this later in Settings.
        </Typography>
      )}
      <AiAgentsCard config={config} onboarding />
    </Box>
  );
}
