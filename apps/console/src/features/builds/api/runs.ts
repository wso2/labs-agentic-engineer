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
import type { components } from "../../../generated/aep-api";

// Today's build reads, copied from the old console (features/builds/api/queries.ts
// and features/validation/api/queries.ts): the version ledger, one version's
// runs, the component builds a merge fanned out to, one validation attempt's
// report, and cancelling a run. Same calls, same polling; a run's live events
// arrive over its progress stream (hooks/useRunProgress.ts).

type MilestoneRunView = components["schemas"]["MilestoneRunView"];

// Both run reads are DB-only on the server (run rows and cycle records fed by
// webhooks, no GitHub, no cluster), which is what makes a 5s poll affordable.
const RUNS_POLL_MS = 5_000;

export const runKeys = {
  all: (projectName: string) => ["projects", projectName, "builds"] as const,
  /** The version ledger: one row per built spec version tag. */
  ledger: (projectName: string) => [...runKeys.all(projectName), "ledger"] as const,
  /** One version's whole run story: its milestone runs and their cycles. */
  runs: (projectName: string, tag: string) => [...runKeys.all(projectName), "runs", tag] as const,
  /** The component builds one cycle's merge fanned out to: a cluster read, priced apart. */
  cycleBuilds: (projectName: string, tag: string, cycleId: string) =>
    [...runKeys.runs(projectName, tag), "cycles", cycleId, "builds"] as const,
  /** One validation attempt's report and criteria, read at one commit. */
  snapshot: (projectName: string, tag: string, cycleId: string, settled: boolean) =>
    [...runKeys.all(projectName), "snapshot", tag, cycleId, settled] as const,
};

const TERMINAL_RUN_STATES = new Set(["succeeded", "failed", "cancelled", "blocked"]);

export function isTerminalRun(state: string): boolean {
  return TERMINAL_RUN_STATES.has(state);
}

/** Newest first, and only the newest can be live. */
function versionIsLive(runs: MilestoneRunView[]): boolean {
  const newest = runs[0];
  return newest !== undefined && !isTerminalRun(newest.state);
}

/**
 * The runs that delivered the version, newest first: every run that built
 * something. A run that only re-judged it has no build to show; a validation
 * run that went on to repair what it found has, and is kept.
 */
export function deliveryRuns(runs: MilestoneRunView[]): MilestoneRunView[] {
  return runs.filter((r) => r.kind !== "validation" || r.cycles.some((c) => c.kind !== "validation"));
}

/** The newest run that delivered the version: the one its build speaks for. */
export function deliveryRun(runs: MilestoneRunView[]): MilestoneRunView | undefined {
  return deliveryRuns(runs)[0];
}

/** The version ledger, newest first. Polls while a version is moving. */
export function useVersionLedger(projectName: string) {
  return useQuery({
    queryKey: runKeys.ledger(projectName),
    queryFn: async () => {
      const { data, error } = await client.GET("/projects/{projectName}/builds", {
        params: { path: { projectName } },
      });
      if (error || data === undefined) throw new Error(apiErrorMessage(error, "Failed to load builds"));
      return data.builds ?? [];
    },
    refetchInterval: (query) => {
      const builds = query.state.data;
      if (!builds) return RUNS_POLL_MS; // no data yet (or errored): keep trying
      return builds.some((b) => b.status === "in_progress" || b.status === "started") ? RUNS_POLL_MS : false;
    },
  });
}

/**
 * One version's whole run story: every milestone run that has worked it,
 * newest first, each with its cycle records in dispatch order. Polling stops
 * the moment the newest run is terminal.
 */
export function useBuildRuns(projectName: string, tag: string | undefined) {
  return useQuery({
    queryKey: runKeys.runs(projectName, tag ?? ""),
    enabled: Boolean(tag),
    queryFn: async () => {
      const { data, error } = await client.GET("/projects/{projectName}/builds/{tag}/runs", {
        params: { path: { projectName, tag: tag ?? "" } },
      });
      if (error || data === undefined) throw new Error(apiErrorMessage(error, "Failed to load the version's runs"));
      return data;
    },
    refetchInterval: (query) => {
      const list = query.state.data;
      if (!list) return RUNS_POLL_MS;
      return versionIsLive(list.runs) ? RUNS_POLL_MS : false;
    },
  });
}

/**
 * Cancel a run, abandoning the increment. 202 only means the signal was sent;
 * the run row turns `cancelled` when the platform acts on it, so success reads
 * the version's runs and the ledger again rather than writing either.
 */
export function useCancelRun(projectName: string, tag: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (runId: string) => {
      const { error } = await client.POST("/projects/{projectName}/runs/{runId}/cancel", {
        params: { path: { projectName, runId } },
      });
      if (error) throw new Error(apiErrorMessage(error, "Failed to cancel the run"));
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: runKeys.runs(projectName, tag) });
      void queryClient.invalidateQueries({ queryKey: runKeys.ledger(projectName) });
    },
  });
}

// A cycle's component builds are read from the cluster, not the database, so
// they poll slower, and only while one is still moving.
const CYCLE_BUILDS_POLL_MS = 10_000;

/**
 * The component builds one cycle's merge fanned out to. Read only for a cycle
 * that merged: before the merge there is nothing to have built. An empty list
 * on a merged cycle means the fan-out has not reached the cluster yet.
 */
export function useCycleBuilds(projectName: string, tag: string, cycleId: string | undefined) {
  return useQuery({
    queryKey: runKeys.cycleBuilds(projectName, tag, cycleId ?? ""),
    enabled: Boolean(tag) && Boolean(cycleId),
    queryFn: async () => {
      const { data, error } = await client.GET("/projects/{projectName}/builds/{tag}/cycles/{cycleId}/builds", {
        params: { path: { projectName, tag, cycleId: cycleId ?? "" } },
      });
      if (error || data === undefined) throw new Error(apiErrorMessage(error, "Failed to load the component builds"));
      return data.items ?? [];
    },
    refetchInterval: (query) => {
      const builds = query.state.data;
      if (!builds || builds.length === 0) return CYCLE_BUILDS_POLL_MS;
      return builds.every((b) => b.completed) ? false : CYCLE_BUILDS_POLL_MS;
    },
  });
}

/**
 * One validation attempt's report and the acceptance criteria it was judged
 * against, both at the same commit. A settled attempt is pinned to its merge
 * commit and never changes; a running one is read at HEAD and has no report
 * yet, which is why `settled` is in the key as well as in the stale time.
 */
export function useValidationSnapshot(
  projectName: string,
  tag: string,
  cycleId: string,
  enabled: boolean,
  settled: boolean,
) {
  return useQuery({
    queryKey: runKeys.snapshot(projectName, tag, cycleId, settled),
    enabled: enabled && Boolean(tag) && Boolean(cycleId),
    // A missing snapshot is a deterministic answer, not a transient failure.
    retry: false,
    staleTime: settled ? Infinity : 30_000,
    queryFn: async () => {
      const { data, error } = await client.GET("/projects/{projectName}/validations/{tag}/cycles/{cycleId}/report", {
        params: { path: { projectName, tag, cycleId } },
      });
      if (error || data === undefined) throw new Error(apiErrorMessage(error, "Failed to load this attempt's report"));
      return data;
    },
  });
}
