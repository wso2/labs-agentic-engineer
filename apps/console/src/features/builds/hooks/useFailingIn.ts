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
import { useBuilds } from "../api/builds";
import { useBuildOutcome } from "./useBuildOutcome";

/**
 * Which features the latest validation found failing, and in which version
 * (B4): `F2 → v2` while v2's validation has a failing F2 scenario. It clears
 * when a later version's validation passes, and changes no feature's stage.
 * Every version re-checks every feature built so far, so the latest built
 * version speaks for all of them.
 */
export function useFailingIn(projectName: string): ReadonlyMap<string, string> {
  const builds = useBuilds(projectName).data;
  const latest = builds?.filter((b) => b.status === "built").at(-1)?.version;
  const outcome = useBuildOutcome(projectName, latest).outcome;
  return useMemo(
    () => new Map(latest && outcome ? outcome.failing.map((f) => [f.featureId, latest] as const) : []),
    [latest, outcome],
  );
}
