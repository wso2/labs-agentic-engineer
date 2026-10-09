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

import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import type { components } from "../../../generated/aep-api";
import { client } from "../../../api/client";
import { configKeys } from "./keys";
import { aeStudioKeys } from "../../ae-studio/api/queries";
import { ApiRequestError, apiErrorMessage } from "../../../api/errors";

type ConfigProjection = components["schemas"]["ConfigProjection"];
type ConfigPatch = components["schemas"]["ConfigPatch"];
type LLMPatch = components["schemas"]["LLMPatch"];

function errorMessage(error: unknown, fallback: string): string {
  return apiErrorMessage(error, fallback);
}

// Every org config write rolls the org's AE Studio (its pod reads the GitHub
// token and the model connection), so the cached state is stale the moment
// one succeeds: re-read it, which is what lets the gate show the restart and
// keeps onboarding's skills step from starting on a `ready` from before.
function invalidateAeStudio(queryClient: QueryClient) {
  void queryClient.invalidateQueries({ queryKey: aeStudioKeys.all });
}

// --- Org config: GitHub + the model connection --------------------------

export function useConfig() {
  return useQuery({
    queryKey: configKeys.all,
    queryFn: async () => {
      const { data, error } = await client.GET("/config");
      if (error) {
        throw new Error(errorMessage(error, "Failed to load configuration"));
      }
      return data;
    },
    staleTime: 30_000,
  });
}

// Refusals that arrive after the save was committed: the server wrote the
// settings and then failed a follow-up step, so the cached config is stale.
const COMMITTED_SAVE_CODES = new Set(["agent_manager_not_updated"]);

// The AI agents card's one Save: sends the patch aiSettingsPatch built.
export function useSaveAiSettings() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (patch: ConfigPatch) => {
      const { data, error } = await client.PATCH("/config", { body: patch });
      if (error) {
        // ApiRequestError keeps `details[].field`, which names the section the
        // server refused, so the card can show it on the field it came from.
        throw new ApiRequestError(error, "Failed to save the AI settings");
      }
      return data;
    },
    onSuccess: (data: ConfigProjection) => {
      queryClient.setQueryData(configKeys.all, data);
      invalidateAeStudio(queryClient);
    },
    onError: (error) => {
      if (error instanceof ApiRequestError && error.code !== undefined && COMMITTED_SAVE_CODES.has(error.code)) {
        void queryClient.invalidateQueries({ queryKey: configKeys.all });
        invalidateAeStudio(queryClient);
      }
    },
  });
}

// Test connection: probes the draft's connection without saving it. The
// result (or refusal, whose `details[].field` and `code` name the field) is the
// card's to draw; nothing is cached, since a test changes no server state.
export function useTestConnection() {
  return useMutation({
    mutationFn: async (body: LLMPatch) => {
      const { data, error } = await client.POST("/config/llm/test", { body });
      if (error) throw new ApiRequestError(error, "Failed to test the connection");
      return data;
    },
  });
}

export function useConnectGitHubPat() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (input: { pat: string; githubLogin?: string }) => {
      const { data, error } = await client.PATCH("/config", {
        body: {
          gitProvider: {
            kind: "github",
            mode: "pat",
            pat: input.pat,
            ...(input.githubLogin ? { githubLogin: input.githubLogin } : {}),
          },
        },
      });
      if (error) {
        throw new Error(errorMessage(error, "Failed to connect GitHub"));
      }
      return data;
    },
    onSuccess: (data: ConfigProjection) => {
      queryClient.setQueryData(configKeys.all, data);
      invalidateAeStudio(queryClient);
    },
  });
}

// Disconnect: drops the org's GitHub connection (the platform no longer
// installs or uninstalls a GitHub App). The config refetch then finds no
// connection, and onboarding takes over.
export function useDisconnectGitProvider() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      const { error } = await client.POST("/config/git-provider/disconnect");
      if (error) {
        throw new Error(errorMessage(error, "Failed to disconnect GitHub"));
      }
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: configKeys.all });
      invalidateAeStudio(queryClient);
    },
  });
}
