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

import { Box, Link as MuiLink, Stack, Typography } from "@wso2/oxygen-ui";
import { createLink } from "@tanstack/react-router";
import { StatusChip } from "../../../components/StatusChip";
import type { components } from "../../../generated/aep-api";
import { issueKindChip, issueStateChip } from "../../tasks/api/status";

type TaskView = components["schemas"]["TaskView"];

// Router-typed link (the console's createLink pattern): an issue row opens the
// console's OWN task page — the issue's log view — not GitHub. The task page
// carries the agent's execution log and links out to GitHub itself, so nothing
// is lost and the reader stays in the story they were following.
const TaskLink = createLink(MuiLink);

// The issues a build session worked, as links — the CODING AGENT stage's own
// content.
//
// Issues appear on that stage and on PROVISIONING (which renders its gates
// itself, labelled with who is acting on each rather than with a GitHub state).
// They are deliberately NOT repeated on the pull request, the merge, the builds
// or the deployment: past the point where the set stops changing, the same chips
// again read as duplication rather than as progress — and the merge's matched set
// IS the coding stage's set.
//
// Each row carries the issue's own DURABLE state (open, or done) and nothing
// else — mid-run liveness on an issue row would be a lie, because the platform
// learns issue facts only when GitHub tells it. The progression is expressed by
// which stage the rows sit under, and the caption says how the console knows
// they are this session's.

export function IssueChips({
  projectName,
  issues,
  caption,
  /** Trim long platform-authored titles (gate titles are prose). */
  label,
}: {
  projectName: string;
  issues: TaskView[];
  /** How the console knows these are this stage's issues. */
  caption?: string;
  label?: (issue: TaskView) => string;
}) {
  if (issues.length === 0) return null;
  return (
    <Box>
      {caption && (
        <Typography variant="caption" color="text.disabled" sx={{ display: "block", mb: 0.75 }}>
          {caption}
        </Typography>
      )}
      <Stack spacing={0.75}>
        {issues.map((issue) => {
          const chip = issueStateChip(issue.derivedStatus);
          // The KIND, shown only where it changes how the row should be read.
          // `development` is the majority of a version's list and renders
          // untagged — the untagged row IS planned work — so a chip here means
          // the version picked up something it did not plan: a defect, a pull
          // request waiting on a rebase, or a gate the PLATFORM works.
          const kind = issueKindChip(issue.kind);
          return (
            <Stack
              key={issue.issueNumber}
              direction="row"
              spacing={1}
              sx={{ alignItems: "center", flexWrap: "wrap", rowGap: 0.5 }}
            >
              <Typography
                variant="caption"
                color="text.secondary"
                sx={{ fontVariantNumeric: "tabular-nums", flexShrink: 0 }}
              >
                #{issue.issueNumber}
              </Typography>
              <TaskLink
                to="/projects/$projectName/tasks/$issueNumber"
                params={{
                  projectName,
                  // A number now: `/tasks/$issueNumber` parses its param
                  // (ADR-0021 §7); it used to be a bare string redirect.
                  issueNumber: issue.issueNumber,
                }}
                variant="body2"
                sx={{
                  color: "text.primary",
                  textDecoration: "none",
                  minWidth: 0,
                  "&:hover": { textDecoration: "underline" },
                }}
              >
                {label ? label(issue) : issue.title}
              </TaskLink>
              <StatusChip label={chip.label} tone={chip.tone} appearance="soft" />
              {kind && <StatusChip label={kind.label} tone={kind.tone} appearance="soft" />}
            </Stack>
          );
        })}
      </Stack>
    </Box>
  );
}
