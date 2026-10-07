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

import { useEffect, useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { components } from "../../../generated/aep-api";
import { useEnvironments, useProjectStatus } from "../../deploy/api/deploy";
import { promotionOrder } from "../../deploy/model/pipeline";
import { refreshBuilds, useBuilds, type ProjectBuild } from "../api/builds";
import { deliveryRuns, isTerminalRun, useBuildRuns, useVersionLedger } from "../api/runs";
import { useVersionTasks } from "../api/tasks";
import { buildExplanation, type Explanation } from "../model/explain";
import { versionRows, type VersionRow } from "../model/ledger";
import { runSteps, type RunSteps } from "../model/phases";
import { externalValuesPark } from "../model/run";
import { runClaims, type RunClaims } from "../model/taskRow";
import { useRunProgress, type RunProgressState } from "./useRunProgress";

type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type BuildSummary = components["schemas"]["BuildSummary"];
type TaskView = components["schemas"]["TaskView"];

/** Everything the Build card reads about one version, from the reads the old console's build page made. */
export interface BuildCardData {
  /** Undefined while the builds load; null once they have and the version is not one. */
  build: ProjectBuild | null | undefined;
  /** The version's ledger row; undefined for a version cut a moment ago, whose run is starting. */
  summary: BuildSummary | undefined;
  /** Every version, newest first: the card finds the build that fixes this one there. */
  rows: VersionRow[];
  buildsError: Error | null;
  retryBuilds: () => void;
  /** The runs that delivered the version, newest first; the first is the one the card speaks for. */
  runs: MilestoneRunView[];
  current: MilestoneRunView | undefined;
  /** The current run's state, the stream's `done` frame ahead of the run poll. */
  runState: MilestoneRunView["state"] | undefined;
  live: boolean;
  /** The current run's progress stream, shared by the phase strip and the coding agent's log. */
  progress: RunProgressState;
  steps: RunSteps;
  /** The stream has cycles to replay before the steps are known. */
  replaying: boolean;
  tasks: ReturnType<typeof useVersionTasks>;
  claims: RunClaims;
  /** The dependencies the run is parked on at the deploy gate; null when it is not parked. */
  park: string[] | null;
  explanation: Explanation | null;
  deploy: components["schemas"]["DeployStage"] | undefined;
  repoUrl: string | undefined;
}

/** The write target: the first environment of the pipeline, where every build lands by itself. */
function writeTarget(environments: components["schemas"]["EnvironmentDTO"][] | undefined) {
  const first = environments ? promotionOrder(environments)[0] : undefined;
  return first ? { name: first.name, label: first.displayName || first.name } : null;
}

export function useBuildCard(projectName: string, version: string): BuildCardData {
  const builds = useBuilds(projectName);
  const ledger = useVersionLedger(projectName);
  const runsQuery = useBuildRuns(projectName, version);
  const runs = useMemo(() => deliveryRuns(runsQuery.data?.runs ?? []), [runsQuery.data]);
  const current = runs[0];
  const progress = useRunProgress(projectName, current?.id);
  const build = builds.data ? (builds.data.find((b) => b.version === version) ?? null) : undefined;
  const runState = (progress.settledState as MilestoneRunView["state"] | undefined) ?? current?.state;
  const live = runState ? !isTerminalRun(runState) : build?.status === "building";
  const tasks = useVersionTasks(projectName, version, live);
  const status = useProjectStatus(projectName);
  const environments = useEnvironments();

  // The stream's end is the run's end: the ledger, the run rows and the builds
  // list (so the track and the picker) are read again at once, not at their next poll.
  const queryClient = useQueryClient();
  useEffect(() => {
    if (progress.phase === "ended" && current && !isTerminalRun(current.state)) refreshBuilds(queryClient, projectName);
  }, [progress.phase, current, queryClient, projectName]);

  const steps = useMemo(() => runSteps(current, progress.cycles), [current, progress.cycles]);
  const claims = useMemo(() => runClaims(runsQuery.data?.runs ?? []), [runsQuery.data]);
  const taskList: TaskView[] = tasks.data ?? [];
  const environment = writeTarget(environments.data);

  return {
    build,
    summary: ledger.data?.find((s) => s.tag === version),
    rows: ledger.data && builds.data ? versionRows(ledger.data, builds.data) : [],
    buildsError: builds.error ?? runsQuery.error ?? null,
    retryBuilds: () => void Promise.all([builds.refetch(), runsQuery.refetch()]),
    runs,
    current,
    runState,
    live,
    progress,
    steps,
    replaying: progress.cycles.length === 0 && (current?.cycles.length ?? 0) > 0,
    tasks,
    claims,
    park: externalValuesPark(current),
    explanation: buildExplanation({ run: current, tasks: taskList, claims, environment }),
    deploy: status.data?.deploy,
    repoUrl: status.data?.repoUrl,
  };
}
