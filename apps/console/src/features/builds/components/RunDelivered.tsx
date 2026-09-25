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

import { Box, Button, CircularProgress, Stack, Typography, alpha } from "@wso2/oxygen-ui";
import { ArrowRight, CircleCheck } from "@wso2/oxygen-ui-icons-react";
import { useNavigate } from "@tanstack/react-router";
import type { components } from "../../../generated/aep-api";
import { runStamp } from "../lib/format";

type RunCycleView = components["schemas"]["RunCycleView"];
type TaskView = components["schemas"]["TaskView"];

/**
 * A run that finished, as a RESULT rather than a rail.
 *
 * Nothing is happening, so there is no "now" to narrate and no log worth
 * tailing — what a reader wants is what it produced and where the thing it
 * produced now lives. The stages stay on screen as the strip above this,
 * all green; this is the sentence under them.
 */
export function RunDelivered({
  projectName,
  milestoneTitle,
  cycles,
  work,
  validating = false,
  endedAt,
}: {
  projectName: string;
  milestoneTitle: string;
  /** The run's build sessions. */
  cycles: RunCycleView[];
  /** The milestone's agent work, open and closed. */
  work: TaskView[];
  /** True while the run holds an OPEN validation cycle: the agent work is
   *  done, and the platform is now validating the deployed system — which is
   *  exactly why the run's chip still says Running. */
  validating?: boolean;
  endedAt?: string | null;
}) {
  const navigate = useNavigate();

  // Closed by merge — the only way an agent issue closes. (`deployed` is the
  // retired ten-value vocabulary; it never occurs on rows any more.)
  const closed = work.filter((t) => t.derivedStatus === "merged").length;
  const merged = cycles.filter((c) => c.mergeSha);
  const prs = merged
    .map((c) => c.prNumber)
    .filter((n): n is number => typeof n === "number");

  const finished = endedAt ? runStamp(endedAt) : "";

  // Only facts this surface actually holds. Build counts are per-session
  // cluster reads the settled view no longer makes, so they are not claimed
  // here — the deployments board is the thing that knows.
  const parts = [
    work.length > 0
      ? `${closed} of ${work.length} issue${work.length === 1 ? "" : "s"} closed`
      : "No issues needed changing",
    ...(merged.length > 0
      ? [
          `${merged.length} pull request${merged.length === 1 ? "" : "s"} merged${
            prs.length > 0 ? ` (${prs.map((n) => `#${n}`).join(" ")})` : ""
          }`,
        ]
      : []),
    ...(finished ? [`succeeded ${finished}`] : []),
  ];

  return (
    <Box
      sx={{
        borderRadius: 1.5,
        p: 2.5,
        border: "1px solid",
        borderColor: (t) => alpha(t.palette.success.main, 0.35),
        bgcolor: (t) => alpha(t.palette.success.main, 0.06),
      }}
    >
      <Stack direction="row" spacing={2} sx={{ alignItems: "flex-start" }}>
        <Box aria-hidden sx={{ display: "flex", flexShrink: 0, color: "success.main", mt: 0.25 }}>
          <CircleCheck size={32} />
        </Box>
        <Box sx={{ minWidth: 0 }}>
          <Typography variant="h6">
            All agent work for {milestoneTitle} is done
          </Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
            {parts.join(" · ")}. Components carry auto-deploy, so the green
            builds roll out on their own — what is running now lives on the
            deployments board.
          </Typography>
        </Box>
      </Stack>

      {validating && (
        <Stack direction="row" spacing={1} sx={{ mt: 1.5, alignItems: "center" }}>
          <CircularProgress size={14} thickness={5} aria-label="Validation running" />
          <Typography variant="body2" color="text.secondary">
            Validation is running against the deployed system — that is what
            keeps this run open. Its verdict lands on the Validation board.
          </Typography>
        </Stack>
      )}

      <Stack direction="row" spacing={1.5} sx={{ mt: 2, flexWrap: "wrap", rowGap: 1 }}>
        <Button
          variant="contained"
          endIcon={<ArrowRight size={16} />}
          onClick={() =>
            void navigate({
              to: "/projects/$projectName/deployments",
              params: { projectName },
            })
          }
        >
          View deployment status
        </Button>
        <Button
          variant="outlined"
          onClick={() =>
            void navigate({
              to: "/projects/$projectName/validations",
              params: { projectName },
            })
          }
        >
          Validations
        </Button>
      </Stack>
    </Box>
  );
}

