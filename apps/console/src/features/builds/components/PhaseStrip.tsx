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

import { Box, Skeleton, Tooltip } from "@wso2/oxygen-ui";
import { phaseStripLabel, type PhaseState, type RunSteps } from "../model/phases";

// Where the run is, in one line under the Build card's summary: Plan, each
// entry of the coding agent's own plan, then Validate, each with its lamp.
// The one part of the earlier run view the card keeps.

const LAMP: Record<PhaseState, string> = {
  done: "success.main",
  live: "primary.main",
  failed: "error.main",
  queued: "text.disabled",
};

export function PhaseStrip({ steps, loading }: { steps: RunSteps; loading: boolean }) {
  if (loading) return <Skeleton variant="rounded" height={28} aria-label="Loading the run's phases" />;
  return (
    <Box component="ol" aria-label="Phases" sx={{ display: "flex", flexWrap: "wrap", gap: 0.5, m: 0, p: 0, listStyle: "none" }}>
      {steps.phases.map((phase) => {
        const label = phaseStripLabel(phase);
        return (
          <Tooltip key={phase.key} title={label}>
            <Box
              component="li"
              aria-label={label}
              aria-current={phase.state === "live" ? "step" : undefined}
              sx={{
                display: "flex",
                alignItems: "center",
                gap: 0.75,
                px: 1.25,
                py: 0.5,
                borderRadius: 999,
                border: 1,
                borderColor: phase.state === "live" ? "primary.main" : "divider",
                fontSize: "0.75rem",
                color: phase.state === "queued" ? "text.secondary" : "text.primary",
              }}
            >
              <Box component="span" aria-hidden sx={{ width: 8, height: 8, borderRadius: "50%", bgcolor: LAMP[phase.state], flexShrink: 0 }} />
              {phase.label}
            </Box>
          </Tooltip>
        );
      })}
    </Box>
  );
}
