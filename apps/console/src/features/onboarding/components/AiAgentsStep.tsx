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

import { Alert, AlertTitle, Box, Typography } from "@wso2/oxygen-ui";
import type { components } from "../../../generated/aep-api";
import { AiAgentsCard } from "../../settings/components/AiAgentsCard";
import { anthropicKeyWasDisconnected } from "../keyDisconnected";

type ConfigProjection = components["schemas"]["ConfigProjection"];

/** The wizard's AI step: the settings card itself, unframed, with an intro. */
export function AiAgentsStep({ config }: { config: ConfigProjection }) {
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
      {anthropicKeyWasDisconnected(config) ? (
        <Alert severity="warning">
          <AlertTitle>Your Anthropic key was disconnected</AlertTitle>
          Agents cannot run until a new key is saved.
        </Alert>
      ) : (
        <Typography variant="body2" color="text.secondary">
          Paste your organization&apos;s Anthropic API key to continue. The
          model and coding agent start on the platform&apos;s defaults and can
          be changed here or later in Settings.
        </Typography>
      )}
      <AiAgentsCard config={config} onboarding />
    </Box>
  );
}
