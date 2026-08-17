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

import { useSession } from "../../../auth/SessionContext";
import { useInterviewState } from "../../agent-chat/interviewState";
import { useProjectStatus } from "../api/queries";
import { specFirstRunView, type SpecFirstRunView } from "../lib/specFirstRun";

/**
 * A project's first run (#485), for surfaces outside the overview: the
 * backend's kickoff report joined with what the live chat log already knows.
 *
 * `enabled` gates the status read, so a caller mounted on every project route
 * (the app shell) pays for it only where it renders something.
 */
export function useSpecFirstRun(
  projectName: string | undefined,
  enabled = true,
): SpecFirstRunView {
  const { orgHandle } = useSession();
  const status = useProjectStatus(projectName ?? "", Boolean(projectName) && enabled);
  const interview = useInterviewState(orgHandle ?? "default", projectName);
  return specFirstRunView(status.data, interview);
}
