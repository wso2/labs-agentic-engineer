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

import { useVersionLedger } from "../builds/api/runs";
import {
  useComponentsDeployments,
  useEnvironments,
  useProjectComponents,
  useProjectStatus,
  useReadinessByEnvironment,
} from "./api/deploy";
import { deployHistory, type HistoryRow } from "./model/history";
import { environmentColumns, type EnvironmentColumn } from "./model/pipeline";

export interface DeployBoard {
  /** Null until the environments and the components (and their deployments) have answered. */
  columns: EnvironmentColumn[] | null;
  /** Null until the version ledger has answered. */
  history: HistoryRow[] | null;
  /** Why the board cannot be drawn: the environments or the components could not be read. */
  error: string | null;
  /** How many components' deployments could not be read; the board shows the rest. */
  unreadComponents: number;
  retry: () => void;
}

interface Columns {
  columns: EnvironmentColumn[] | null;
  error: string | null;
  unreadComponents: number;
  retry: () => void;
}

/**
 * The board's columns, from the environments, the components and every
 * component's deployments; with each environment's dependency readiness when
 * `withReadiness` (the Deploy Page shows it, the overview does not).
 */
function useColumns(projectName: string, withReadiness: boolean): Columns {
  const environments = useEnvironments();
  const components = useProjectComponents(projectName);
  const names = (components.data ?? []).map((c) => c.name);
  const deployments = useComponentsDeployments(projectName, names);
  const status = useProjectStatus(projectName);
  const readiness = useReadinessByEnvironment(
    projectName,
    withReadiness ? (environments.data ?? []).map((e) => e.name) : [],
  );

  const ready = environments.data && components.data && !(names.length > 0 && deployments.isPending);
  const columns = ready
    ? environmentColumns({
        environments: environments.data,
        components: components.data,
        deployments: deployments.deployments,
        deploy: status.data?.deploy,
        readiness,
      })
    : null;
  const failed = [environments, components].filter((q) => q.isError);
  return {
    columns,
    error: failed[0]?.error?.message ?? null,
    unreadComponents: deployments.failedCount,
    retry: () => {
      for (const q of failed) void q.refetch();
    },
  };
}

/** The Deploy Page's board and history, from its reads. */
export function useDeployBoard(projectName: string): DeployBoard {
  const board = useColumns(projectName, true);
  const ledger = useVersionLedger(projectName);
  return {
    ...board,
    history: board.columns && ledger.data ? deployHistory(board.columns, ledger.data) : null,
  };
}

/** What runs where, for the overview's components: the board's columns without the dependencies' values. */
export function useEnvironmentColumns(projectName: string): Columns {
  return useColumns(projectName, false);
}
