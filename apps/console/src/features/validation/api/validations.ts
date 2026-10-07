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

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { client } from "../../../api/client";
import { apiErrorMessage } from "../../../api/errors";
import { runKeys } from "../../builds/api/runs";
import { validationIsLive } from "../model/attempts";

// The validation reads, copied from the old console (features/validation/api/
// queries.ts): the ledger of every version's validation, one version's
// attempts, and asking a version's scenarios again. An attempt's report is
// the builds feature's useValidationSnapshot, which the Build card's outcome
// reads too. All DB-only on the server, so a close poll while something moves
// is affordable; when nothing does they slow rather than stop, because the
// platform starts a validation of its own once a build deploys.

const LIVE_POLL_MS = 5_000;
const IDLE_POLL_MS = 30_000;

const validationKeys = {
  all: (projectName: string) => ["projects", projectName, "validations"] as const,
  detail: (projectName: string, tag: string) => [...validationKeys.all(projectName), tag] as const,
};

/** Every version's validation, newest version first, including versions never validated. */
export function useValidations(projectName: string) {
  return useQuery({
    queryKey: validationKeys.all(projectName),
    queryFn: async () => {
      const { data, error } = await client.GET("/projects/{projectName}/validations", {
        params: { path: { projectName } },
      });
      if (error || data === undefined) throw new Error(apiErrorMessage(error, "Failed to load the validations"));
      return data.validations ?? [];
    },
    refetchInterval: (query) =>
      query.state.data?.some((v) => validationIsLive(v.state)) ? LIVE_POLL_MS : IDLE_POLL_MS,
  });
}

/**
 * One version's validation: the runs that attempted it, newest first, each
 * with its validation cycles only, and whether a run is working it now
 * (`live`) and it is the deployed version (`deployed`), which gate Revalidate.
 * `poll` is off for a ledger row, which only counts the attempts.
 */
export function useValidation(projectName: string, tag: string, poll = true) {
  return useQuery({
    queryKey: validationKeys.detail(projectName, tag),
    queryFn: async () => {
      const { data, error } = await client.GET("/projects/{projectName}/validations/{tag}", {
        params: { path: { projectName, tag } },
      });
      if (error || data === undefined) throw new Error(apiErrorMessage(error, "Failed to load this version's validation"));
      return data;
    },
    refetchInterval: poll ? (query) => (query.state.data?.live ? LIVE_POLL_MS : IDLE_POLL_MS) : false,
  });
}

/**
 * Validate a version again, against what is deployed. 202 means a run
 * started, not that it has a verdict, so success reads the validation, the
 * ledger and the version's runs again and lets the poll take over.
 */
export function useRevalidate(projectName: string, tag: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      const { error } = await client.POST("/projects/{projectName}/builds/{tag}/revalidate", {
        params: { path: { projectName, tag } },
        body: {},
      });
      if (error) throw new Error(apiErrorMessage(error, "Failed to start validation"));
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: validationKeys.all(projectName) });
      void queryClient.invalidateQueries({ queryKey: runKeys.runs(projectName, tag) });
    },
  });
}
