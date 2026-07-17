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

import { useState } from "react";
import { Chip, Tooltip } from "@wso2/oxygen-ui";
import { Coins } from "@wso2/oxygen-ui-icons-react";
import { formatTokens, formatUsd, totalTokens, type Usage } from "../lib/format";
import { ModelComparisonPopover } from "./ModelComparisonPopover";
import { UsageBreakdown } from "./UsageBreakdown";

// The folded cost figure (#245): one USD number + total tokens, breakdown on
// hover, model comparator on click. costUsd null (no catalog entry) degrades
// to tokens-only. Renders nothing for an all-zero aggregate.
export function UsageChip({
  usage,
  label,
}: {
  usage: Usage;
  label?: string;
}) {
  const [anchorEl, setAnchorEl] = useState<HTMLElement | null>(null);
  const tokens = totalTokens(usage);
  if (tokens === 0) return null;

  const figures =
    usage.costUsd !== null
      ? `${formatUsd(usage.costUsd)} · ${formatTokens(tokens)} tok`
      : `${formatTokens(tokens)} tok`;

  return (
    <>
      <Tooltip title={<UsageBreakdown usage={usage} />}>
        <Chip
          size="small"
          variant="outlined"
          icon={<Coins size={14} />}
          label={label ? `${label} ${figures}` : figures}
          onClick={(e) => {
            e.stopPropagation();
            setAnchorEl(e.currentTarget);
          }}
        />
      </Tooltip>
      <ModelComparisonPopover
        anchorEl={anchorEl}
        onClose={() => setAnchorEl(null)}
        tokens={usage}
      />
    </>
  );
}
