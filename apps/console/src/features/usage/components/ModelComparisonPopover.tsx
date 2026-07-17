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

import {
  Box,
  Chip,
  Popover,
  Skeleton,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import { useModelPricing } from "../api/queries";
import { formatUsd, repriceUsd, type Usage } from "../lib/format";

// The model comparator (#245 decision 2): the SAME token mix re-priced at
// every catalog model's rates, primary model first. Deliberately naive —
// the disclaimer below is part of the design, not a nice-to-have.
export function ModelComparisonPopover({
  anchorEl,
  onClose,
  tokens,
}: {
  anchorEl: HTMLElement | null;
  onClose: () => void;
  tokens: Usage;
}) {
  const pricing = useModelPricing();
  const open = anchorEl !== null;

  // The model this usage actually ran on leads; catalog order for the rest.
  const models = pricing.data
    ? [...pricing.data.models].sort((a, b) => {
        const primary = tokens.model || pricing.data.defaultModel;
        return (b.model === primary ? 1 : 0) - (a.model === primary ? 1 : 0);
      })
    : [];
  const primaryModel = tokens.model || pricing.data?.defaultModel;

  return (
    <Popover
      open={open}
      anchorEl={anchorEl}
      onClose={onClose}
      anchorOrigin={{ vertical: "bottom", horizontal: "left" }}
    >
      <Box sx={{ p: 2, width: 280 }}>
        <Typography variant="subtitle2" gutterBottom>
          Cost by model
        </Typography>
        {pricing.isPending ? (
          <Stack spacing={0.75}>
            <Skeleton height={22} />
            <Skeleton height={22} />
            <Skeleton height={22} />
          </Stack>
        ) : pricing.isError ? (
          <Typography variant="body2" color="text.secondary">
            Pricing catalog unavailable — token figures still apply.
          </Typography>
        ) : (
          <Stack spacing={0.5}>
            {models.map((m) => {
              const isPrimary = m.model === primaryModel;
              return (
                <Box
                  key={m.model}
                  sx={{
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "space-between",
                    gap: 2,
                  }}
                >
                  <Stack direction="row" spacing={0.75} alignItems="center">
                    <Typography
                      variant="body2"
                      sx={{ fontWeight: isPrimary ? 600 : 400 }}
                    >
                      {m.displayName}
                    </Typography>
                    {isPrimary && <Chip size="small" label="current" />}
                  </Stack>
                  <Typography
                    variant="body2"
                    sx={{
                      fontVariantNumeric: "tabular-nums",
                      fontWeight: isPrimary ? 600 : 400,
                    }}
                  >
                    {formatUsd(repriceUsd(tokens, m))}
                  </Typography>
                </Box>
              );
            })}
          </Stack>
        )}
        <Typography
          variant="caption"
          color="text.secondary"
          sx={{ display: "block", mt: 1.5 }}
        >
          Approximate — assumes the same token usage on every model; tokenizers
          and turn counts differ in practice.
        </Typography>
      </Box>
    </Popover>
  );
}
