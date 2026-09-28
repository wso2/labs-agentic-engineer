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

import { Box, Tooltip, Typography } from "@wso2/oxygen-ui";
import {
  formatTokens,
  formatUsd,
  totalTokens,
  unpricedHostLine,
  type PhaseUsage,
  type Usage,
} from "../lib/format";
import { UsageBreakdown } from "./UsageBreakdown";

// The folded cost figure (#245/#291): USD is the ONLY primary value — the token
// detail is a technical view that lives behind the hover, never beside the
// figure. A stamped costUsd (incl. $0 for an idle project) shows as USD; a null
// cost (captured usage the platform could not price) degrades to tokens. On the
// Usage page every project cards, idle ones as $0, so the figure never hides.
//
// Rendered as a PLAIN value rather than a chip (PR #300 review): a chip frames
// a value as one tag among several, and a card carries exactly one figure here
// — the frame bought no distinction and only added weight. Reach for a chip
// again only if this row ever lists more than one value.
export function UsageFigure({
  usage,
  phases,
  context,
}: {
  usage: Usage;
  /** The per-phase split (#291), shown as a small section in the hover detail. */
  phases?: PhaseUsage;
  /** Names what the figure covers in the hover detail, e.g. "Agent spend — build v1". */
  context?: string;
}) {
  const tokens = totalTokens(usage);

  const figure =
    usage.costUsd !== null
      ? formatUsd(usage.costUsd)
      : `${formatTokens(tokens)} tok`;
  const billedBy = unpricedHostLine(usage);

  const tooltip = (
    <Tooltip
      title={<UsageBreakdown usage={usage} phases={phases} context={context} />}
      slotProps={{
        tooltip: {
          sx: {
            maxWidth: 320,
            // MUI's default tooltip sits at 92% alpha, which is fine for a
            // one-line hint but lets the card underneath ghost through a
            // dense figure table. Same hue, fully opaque.
            bgcolor: (theme) => theme.palette.grey[700],
          },
        },
      }}
    >
      {/* tabIndex keeps the breakdown reachable without a pointer — MUI opens
          the tooltip on focus as well as hover. */}
      <Typography
        component="span"
        variant="body2"
        tabIndex={0}
        sx={{ fontVariantNumeric: "tabular-nums", whiteSpace: "nowrap" }}
      >
        {figure}
      </Typography>
    </Tooltip>
  );

  if (!billedBy) return tooltip;
  // A tokens-only figure says why there is no dollar figure: the platform
  // holds no rate for the host that billed the work.
  return (
    <Box sx={{ display: "flex", flexDirection: "column", alignItems: "flex-end" }}>
      {tooltip}
      <Typography variant="caption" color="text.secondary" sx={{ whiteSpace: "nowrap" }}>
        {billedBy}
      </Typography>
    </Box>
  );
}
