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

import { Alert, Button } from "@wso2/oxygen-ui";
import type { components } from "../../../generated/aep-api";

type Recording = components["schemas"]["RunCycleView"]["recording"];

// Copy from the lexicon (*Builds › A cycle's log can be missing*). No retention
// number: the platform keeps logs for a few days and says no more than that.
const EXPIRED = "This run's log is no longer kept (logs are kept for a few days)";
const UNAVAILABLE = "Couldn't load this run's log right now";

/**
 * Why a cycle's log area is empty, when the platform knows the reason.
 *
 * `live` and `kept` render nothing: the feed itself is the content. `expired`
 * is final, so it offers no retry; `unavailable` is transient, so it does. The
 * run's outcome and failure explanation sit elsewhere on the page and are not
 * touched by either state.
 */
export function CycleLogState({
  recording,
  onRetry,
}: {
  recording: Recording;
  onRetry: () => void;
}) {
  if (recording === "expired") {
    return <Alert severity="info" sx={{ mb: 1 }}>{EXPIRED}</Alert>;
  }
  if (recording === "unavailable") {
    return (
      <Alert
        severity="warning"
        sx={{ mb: 1 }}
        action={
          <Button color="inherit" size="small" onClick={onRetry}>
            Try again
          </Button>
        }
      >
        {UNAVAILABLE}
      </Alert>
    );
  }
  return null;
}
