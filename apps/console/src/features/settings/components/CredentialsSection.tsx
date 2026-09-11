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

import { Alert, Box, CircularProgress } from "@wso2/oxygen-ui";
import { useConfig } from "../api/queries";
import { AnthropicCredentialCard } from "./AnthropicCredentialCard";
import { CodingAgentCard } from "./CodingAgentCard";
import { GitHubCredentialCard } from "./GitHubCredentialCard";

export function CredentialsSection() {
  const { data, isLoading, isError, error } = useConfig();

  if (isLoading) {
    return (
      <Box sx={{ display: "flex", justifyContent: "center", py: 6 }}>
        <CircularProgress />
      </Box>
    );
  }

  if (isError || !data) {
    return <Alert severity="error">{error?.message ?? "Failed to load configuration"}</Alert>;
  }

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 3 }}>
      <GitHubCredentialCard gitProvider={data.gitProvider} />
      <AnthropicCredentialCard llm={data.llm} />
      {/* Runtime, model, and the key coding runs bill are one setting group,
          so they render as one card — the org key above is what the agent
          falls back to, not part of the group. */}
      <CodingAgentCard
        codingAgent={data.codingAgent}
        codingLlm={data.codingLlm}
        llmConnected={data.llm !== null}
      />
    </Box>
  );
}
