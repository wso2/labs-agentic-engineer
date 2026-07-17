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
import { Chip, Skeleton, Tooltip } from "@wso2/oxygen-ui";
import { useProjectEstimate } from "../api/queries";
import { formatTokens, formatUsd } from "../lib/format";
import { ModelComparisonPopover } from "./ModelComparisonPopover";

// Inline pre-action estimate (#245 decisions 1 & 9): a historical-average
// range beside the Build / Generate-design CTA — no confirm dialog, the
// one-click flow stays. Cold start renders an honest "no estimate yet";
// a load failure renders nothing (the estimate is advisory, never blocking).
export function EstimateChip({
  projectName,
  action,
}: {
  projectName: string;
  action: "design" | "build";
}) {
  const [anchorEl, setAnchorEl] = useState<HTMLElement | null>(null);
  const estimateQ = useProjectEstimate(projectName, action);

  if (estimateQ.isPending) {
    return <Skeleton variant="rounded" width={120} height={24} />;
  }
  if (estimateQ.isError) return null;

  const estimate = estimateQ.data.estimate;
  if (estimate === null) {
    return (
      <Tooltip
        title={`Estimates appear once a few ${action} runs have been recorded — none yet.`}
      >
        <Chip size="small" variant="outlined" label="no estimate yet" />
      </Tooltip>
    );
  }

  return (
    <>
      <Tooltip
        title={`Estimated from ${estimate.sampleSize} previous ${action} runs · ${formatTokens(estimate.tokensMin)}–${formatTokens(estimate.tokensMax)} tok, priced on ${estimate.model}`}
      >
        <Chip
          size="small"
          variant="outlined"
          label={`est. ${formatUsd(estimate.costUsdMin)}–${formatUsd(estimate.costUsdMax)}`}
          onClick={(e) => setAnchorEl(e.currentTarget)}
        />
      </Tooltip>
      <ModelComparisonPopover
        anchorEl={anchorEl}
        onClose={() => setAnchorEl(null)}
        tokens={estimate.average}
      />
    </>
  );
}
