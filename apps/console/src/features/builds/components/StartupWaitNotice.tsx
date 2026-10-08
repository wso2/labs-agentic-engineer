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

import { Alert, AlertTitle, Typography } from "@wso2/oxygen-ui";
import type { components } from "../../../generated/aep-api";
import { startupWaitNotice } from "../model/agentStart";

type RunCycleView = components["schemas"]["RunCycleView"];

/**
 * An open cycle whose agent the cluster has not started: why, and when the run
 * gives up. Shared by the Build card and the Validation card, because a coding
 * and a validation agent wait for the cluster the same way.
 *
 * Warning, not info: the run fails at the deadline unless the cause clears,
 * and freeing room in the cluster is something a person can do about it.
 * Nothing renders while the agent runs or once the cycle has ended; the
 * failure card says what happened after that.
 */
export function StartupWaitNotice({ cycle }: { cycle: RunCycleView | undefined }) {
  const notice = startupWaitNotice(cycle);
  if (!notice) return null;
  return (
    <Alert severity="warning" role="status">
      <AlertTitle>{notice.title}</AlertTitle>
      <Typography variant="body2">{notice.body}</Typography>
    </Alert>
  );
}
