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

import type { components } from "../../../generated/aep-api";
import { useCycleBuilds } from "../api/queries";
import { sessionStages } from "../lib/sessionSpine";
import type { SpineStage } from "../lib/stage";

type RunCycleView = components["schemas"]["RunCycleView"];
type TaskView = components["schemas"]["TaskView"];

/**
 * One build session's stages (ADR-0014), with the one read they need.
 *
 * `sessionStages` is pure and takes the session's component builds as an
 * argument, and those come from a CLUSTER-derived read — so every surface that
 * wants the rail has to make the same query with the same enabling rule. Two
 * surfaces already did it inline, and a third was about to: the build page's
 * header names the stage the run is on now. One hook, so the enabling rule
 * ("only a cycle that has merged can have built anything") is stated once.
 *
 * No cycle means no session, and an empty rail rather than a fabricated one —
 * a run in its planning phase has nothing to draw and nothing to name.
 */
export function useSessionStages(
  projectName: string,
  tag: string,
  cycle: RunCycleView | undefined,
  work: TaskView[],
): SpineStage[] {
  // Cached per cycle, so a page asking for the same session twice — the header
  // and the Build logs section below it — spends one read.
  const { data: builds } = useCycleBuilds(
    projectName,
    tag,
    cycle?.id ?? "",
    Boolean(cycle?.mergeSha),
  );
  return cycle ? sessionStages({ cycle, work, builds }) : [];
}
