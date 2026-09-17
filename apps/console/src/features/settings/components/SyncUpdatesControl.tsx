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

import { Button, Chip, CircularProgress, Tooltip } from "@wso2/oxygen-ui";
import { RefreshCw } from "@wso2/oxygen-ui-icons-react";
import { useHasPermission } from "../../../auth/permissions";

// Sync is all-or-nothing: the BE's POST /skills/sync takes no body and
// reconciles every embedded skill in one commit (`Reconcile`). So this is a
// count chip plus a single button — no per-skill selection (issue #143).
export function SyncUpdatesControl({
  count,
  pending,
  onSync,
}: {
  count: number;
  pending: boolean;
  onSync: () => void;
}) {
  const hasSkillConfig = useHasPermission("ae:skill-config");

  if (count === 0) return null;

  return (
    <>
      <Chip
        size="small"
        color="warning"
        label={`${count} org skill update${count === 1 ? "" : "s"} available`}
      />
      <Tooltip
        title={
          hasSkillConfig ? "" : "You don't have permission to configure skills."
        }
      >
        <span>
          <Button
            variant="outlined"
            startIcon={
              pending ? <CircularProgress size={16} /> : <RefreshCw size={18} />
            }
            onClick={onSync}
            disabled={pending || !hasSkillConfig}
          >
            {pending ? "Syncing…" : "Sync org skills"}
          </Button>
        </span>
      </Tooltip>
    </>
  );
}
