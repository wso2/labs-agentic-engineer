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

import { Box, Stack, Typography } from "@wso2/oxygen-ui";
import { formatTokens, type Usage } from "../lib/format";

// Tooltip body behind every folded cost figure (#245 decision 7): the
// input/output/cache split that explains why agentic token counts look large.
export function UsageBreakdown({ usage }: { usage: Usage }) {
  const rows: [string, number][] = [
    ["Input", usage.inputTokens],
    ["Output", usage.outputTokens],
    ["Cache read", usage.cacheReadTokens],
    ["Cache write", usage.cacheCreationTokens],
  ];
  return (
    <Stack spacing={0.25} sx={{ py: 0.25 }}>
      {rows.map(([label, tokens]) => (
        <Box
          key={label}
          sx={{ display: "flex", justifyContent: "space-between", gap: 2 }}
        >
          <Typography variant="caption">{label}</Typography>
          <Typography
            variant="caption"
            sx={{ fontVariantNumeric: "tabular-nums" }}
          >
            {formatTokens(tokens)} tok
          </Typography>
        </Box>
      ))}
      {usage.model && (
        <Typography variant="caption" color="inherit" sx={{ opacity: 0.7 }}>
          {usage.model}
        </Typography>
      )}
      {usage.costUsd === null && (
        <Typography variant="caption" sx={{ opacity: 0.7 }}>
          No pricing for this model — tokens only.
        </Typography>
      )}
    </Stack>
  );
}
