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

import { Box, Divider, Stack, Typography } from "@wso2/oxygen-ui";
import { formatTokens, formatUsd, totalTokens, type PhaseUsage, type Usage } from "../format";

// The three SDLC phases, in pipeline order (#291), each with its label.
const PHASES: [keyof PhaseUsage, string][] = [
  ["spec", "Spec / design"],
  ["build", "Build"],
  ["validation", "Validation"],
];

// A phase's figure: its stamped USD, or tokens when it ran on rows the
// platform could not price.
function phaseFigure(u: Usage): string {
  return u.costUsd !== null ? formatUsd(u.costUsd) : `${formatTokens(totalTokens(u))} tok`;
}

const row = { display: "flex", justifyContent: "space-between", gap: 2 } as const;
const figure = { fontVariantNumeric: "tabular-nums" } as const;

/**
 * What a spend figure is made of (#245, #291): the input, output and cache
 * split that explains why agent token counts run large, the model, and the
 * split by phase. `context` names what the figure covers, so no number floats
 * without a scope.
 */
export function UsageBreakdown({ usage, phases, context }: { usage: Usage; phases: PhaseUsage; context: string }) {
  const rows: [string, number][] = [
    ["Input", usage.inputTokens],
    ["Output", usage.outputTokens],
    ["Cache read", usage.cacheReadTokens],
    ["Cache write", usage.cacheCreationTokens],
  ];
  return (
    <Stack spacing={0.25} sx={{ py: 0.25 }}>
      <Typography variant="caption" sx={{ fontWeight: 600, mb: 0.25 }}>
        {context}
      </Typography>
      {rows.map(([label, tokens]) => (
        <Box key={label} sx={row}>
          <Typography variant="caption">{label}</Typography>
          <Typography variant="caption" sx={figure}>
            {formatTokens(tokens)} tok
          </Typography>
        </Box>
      ))}
      {usage.model && (
        <Typography variant="caption" sx={{ opacity: 0.7 }}>
          {usage.model}
        </Typography>
      )}
      {usage.costUsd === null && (
        <Typography variant="caption" sx={{ opacity: 0.7 }}>
          No stamped cost: this usage predates pricing, or its model had no rate.
        </Typography>
      )}
      <Divider sx={{ my: 0.5 }} />
      <Typography variant="caption" sx={{ fontWeight: 600, opacity: 0.7 }}>
        Cost by phase
      </Typography>
      {PHASES.map(([key, label]) => (
        <Box key={key} sx={row}>
          <Typography variant="caption">{label}</Typography>
          <Typography variant="caption" sx={figure}>
            {phaseFigure(phases[key])}
          </Typography>
        </Box>
      ))}
    </Stack>
  );
}
