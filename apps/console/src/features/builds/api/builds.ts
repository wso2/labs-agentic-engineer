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

import { useMemo } from "react";
import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { client } from "../../../api/client";
import { apiErrorMessage } from "../../../api/errors";
import type { components } from "../../../generated/aep-api";
import { useSpecFlush } from "../../spec/collab/specDoc";
import { buildBody, fixBody, type BuildSelection } from "../buildSelection";
import { runKeys, useVersionLedger } from "./runs";

// The builds so far, as the picker, the track and the Builds card read them:
// each version, what it built (list-project-versions: the features it
// carried with their lines at its tag, and its product-wide items), and how
// its run went (the version ledger, list-project-builds). What a version
// built never changes, so it is read again only when a build starts or ends;
// the ledger polls while a version is moving.

type SpecVersion = components["schemas"]["SpecVersion"];
type BuildSummary = components["schemas"]["BuildSummary"];

/** A line of a feature's spec as it was built: its own ID when it has one, and its words. */
export interface BuiltLine {
  id: string | null;
  words: string;
}

export interface BuiltFeature {
  id: string;
  name: string;
  /** The feature's spec as built (model/changes.ts). */
  lines: BuiltLine[];
}

export type BuildStatus = "building" | "built" | "failed";

export interface ProjectBuild {
  /** "v1": the version's name, the tag the build cut. */
  version: string;
  status: BuildStatus;
  features: BuiltFeature[];
  /** Product-wide items it carried ("P1"). */
  productWide: string[];
  /** A repair build names the version it fixes ("v1.1" fixes "v1"); it builds the same features. */
  fixes?: string;
}

export function buildsKey(projectName: string) {
  return ["projects", projectName, "versions"] as const;
}

/**
 * A version's state, from its ledger row. A version with no row yet was cut
 * a moment ago and its run is starting, so it is building.
 */
function statusOf(row: BuildSummary | undefined): BuildStatus {
  switch (row?.status) {
    case "completed":
      return "built";
    case "failed":
    case "cancelled":
      return "failed";
    default:
      return "building";
  }
}

/** The project's builds, oldest first: what each version built, with its ledger state. */
export function projectBuilds(versions: SpecVersion[], ledger: BuildSummary[]): ProjectBuild[] {
  return versions.map((v) => ({
    version: v.name,
    status: statusOf(ledger.find((row) => row.tag === v.name)),
    features: v.features.map((f) => ({
      id: f.id,
      name: f.name,
      lines: f.lines.map((l) => ({ id: l.id ?? null, words: l.words })),
    })),
    productWide: v.productWide,
    ...(v.fixes ? { fixes: v.fixes } : {}),
  }));
}

/** The project's builds, oldest first. The ledger half polls while one is building, so its end reaches the track and the chat. */
export function useBuilds(projectName: string) {
  const versions = useQuery({
    queryKey: buildsKey(projectName),
    queryFn: async (): Promise<SpecVersion[]> => {
      const { data, error } = await client.GET("/projects/{projectName}/versions", {
        params: { path: { projectName } },
      });
      if (error || data === undefined) throw new Error(apiErrorMessage(error, "Couldn't load the builds"));
      return data.versions;
    },
    staleTime: Infinity,
  });
  const ledger = useVersionLedger(projectName);
  const data = useMemo(
    () => (versions.data && ledger.data ? projectBuilds(versions.data, ledger.data) : undefined),
    [versions.data, ledger.data],
  );
  return {
    data,
    isError: versions.isError || ledger.isError,
    error: versions.error ?? ledger.error,
    refetch: () => Promise.all([versions.refetch(), ledger.refetch()]),
  };
}

/** One unmet condition of the build gate's 422 refusal (the old console's #372). */
export interface GateProblem {
  field?: string;
  message: string;
}

/** Why a build did not start: the message, and the gate's checklist when it refused one. */
export class BuildRefusedError extends Error {
  readonly problems: GateProblem[];

  constructor(message: string, problems: GateProblem[]) {
    super(message);
    this.name = "BuildRefusedError";
    this.problems = problems;
  }
}

/**
 * A failed build start, as the old console reads it (useBuildProject): the
 * envelope's message, and the gate's 422 detail rows ({field, message}) so
 * the refusal renders as a checklist instead of one flattened string.
 */
export function buildRefusal(error: unknown): BuildRefusedError {
  const details = (error as { details?: unknown } | undefined)?.details;
  const problems = Array.isArray(details)
    ? details.flatMap((d: unknown): GateProblem[] => {
        const row = d as { field?: unknown; message?: unknown } | null;
        if (typeof row?.message !== "string") return [];
        return [typeof row.field === "string" ? { field: row.field, message: row.message } : { message: row.message }];
      })
    : [];
  return new BuildRefusedError(apiErrorMessage(error, "Failed to start the build"), problems);
}

/** Everything the Builds card and the track read about builds: invalidated when a build starts or ends. */
export function refreshBuilds(queryClient: QueryClient, projectName: string): void {
  void queryClient.invalidateQueries({ queryKey: buildsKey(projectName) });
  void queryClient.invalidateQueries({ queryKey: runKeys.all(projectName) });
}

/**
 * Start a build of the selection; resolves with the version it cut ("v1").
 * The room's pending edits are committed first: the build tags HEAD, and an
 * edit still in the room would not be in the version.
 */
export function useStartBuild(projectName: string) {
  const queryClient = useQueryClient();
  const flush = useSpecFlush(projectName);
  return useMutation({
    mutationFn: async (selection: BuildSelection): Promise<string> => {
      await flush();
      const { data, error } = await client.POST("/projects/{projectName}/build", {
        params: { path: { projectName } },
        body: buildBody(selection),
      });
      if (error || data === undefined) throw buildRefusal(error);
      return data.tag ?? "";
    },
    onSuccess: () => refreshBuilds(queryClient, projectName),
  });
}

/**
 * Fix what failed in a version: a repair build of the same features, which
 * re-runs every scenario. Resolves with the version it cut ("v1.1").
 */
export function useStartFix(projectName: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (of: string): Promise<string> => {
      const { data, error } = await client.POST("/projects/{projectName}/build", {
        params: { path: { projectName } },
        body: fixBody(of),
      });
      if (error || data === undefined) throw buildRefusal(error);
      return data.tag ?? "";
    },
    onSuccess: () => refreshBuilds(queryClient, projectName),
  });
}
