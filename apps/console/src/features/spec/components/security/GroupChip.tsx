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

/**
 * Whether an org group is already on the directory — two words and a tooltip.
 *
 * It is the ONE place that vocabulary is decided. The Groups list is the only
 * surface that renders it today, but it was previously stated inside a role
 * card in a third wording, and the two drifted; keeping the words here means a
 * later reader of the same fact cannot invent a fourth.
 *
 * Two states, not three. A group the platform did not create reads `Existing`
 * like any other — because that is the answer to "will Build create this?" —
 * and the fact that Build will leave its membership alone is the tooltip's, not
 * a third chip's. A directory that could not be read gets NO chip: absent is
 * honest, a guess is not.
 */

import { Chip, Tooltip } from "@wso2/oxygen-ui";

import type { GroupRow } from "../../lib/groupRows";

/** How many people are in it today, as a tooltip clause or nothing at all. */
function membersClause(row: GroupRow): string {
  if (row.memberCount === null) return "";
  return ` ${row.memberCount} ${row.memberCount === 1 ? "member" : "members"} today.`;
}

export function GroupChip({ row }: { row: GroupRow }) {
  if (row.status === null) return null;
  if (row.status === "new") {
    return (
      <Tooltip
        title={`${row.name} is not on the identity provider yet — Build creates it.`}
      >
        <Chip size="small" variant="outlined" color="primary" label="New" />
      </Tooltip>
    );
  }
  return (
    <Tooltip
      title={
        row.platformCreated
          ? `${row.name} is already on the identity provider, created by the platform.${membersClause(row)}`
          : `${row.name} already exists and the platform did not create it, so Build will leave it alone.${membersClause(row)}`
      }
    >
      <Chip size="small" variant="outlined" color="success" label="Existing" />
    </Tooltip>
  );
}
