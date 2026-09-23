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

import { Box, Paper, Stack, Typography } from "@wso2/oxygen-ui";
import type { GenUiPropsOf, GenUiRenderProps } from "@aep/ui-genui";

// Oxygen has no timeline or Gantt component, so this one is composed from
// Oxygen primitives; every colour and radius is a theme token.
export function GenUiAgentTimeline({
  props,
}: GenUiRenderProps<GenUiPropsOf<"AgentTimeline">>) {
  return (
    <Paper variant="outlined">
      <Stack spacing={1.5} sx={{ p: 2 }}>
        <Stack direction="row" sx={{ justifyContent: "space-between" }}>
          <Typography variant="caption" color="text.secondary">
            {props.total}
          </Typography>
          <Typography variant="caption" color="text.secondary">
            solid · working &nbsp; faded · waiting on another agent
          </Typography>
        </Stack>
        {props.lanes.map((lane, index) => (
          <Box
            key={`${index}-${lane.name}`}
            sx={{
              display: "grid",
              gridTemplateColumns: "minmax(0, 16rem) 1fr 4.5rem",
              gap: 2,
              alignItems: "center",
            }}
          >
            <Typography
              variant="body2"
              noWrap
              title={lane.name}
              sx={{ pl: (lane.depth ?? 0) * 1.5 }}
            >
              {lane.name}
            </Typography>
            <Box sx={{ position: "relative", height: 8 }}>
              <Box
                sx={{
                  position: "absolute",
                  top: 0,
                  bottom: 0,
                  left: `${lane.start}%`,
                  width: `${Math.max(lane.end - lane.start, 1)}%`,
                  borderRadius: 1,
                  bgcolor: "success.main",
                  opacity: lane.state === "waiting" ? 0.4 : 1,
                }}
              />
            </Box>
            <Typography variant="caption" color="text.secondary" align="right">
              {lane.duration}
            </Typography>
          </Box>
        ))}
      </Stack>
    </Paper>
  );
}
