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
  Alert,
  Autocomplete,
  Box,
  Button,
  Card,
  CardContent,
  Chip,
  CircularProgress,
  Stack,
  TextField,
  Typography,
  type TextFieldProps,
} from "@wso2/oxygen-ui";
import type { components } from "../../../generated/aep-api";
import { TasksList } from "../../tasks/components/TasksList";
import { UsageChip } from "../../usage/components/UsageChip";
import { useBuilds } from "../api/queries";

type BuildSummary = components["schemas"]["BuildSummary"];

// Read-only current-build view (#185): the selected build's summary + its
// tag-scoped task list, with an autocomplete over built tags for history.
// Builds are triggered from the Spec view — no actions here.
export function BuildsPage({
  projectName,
  tag,
  onTagChange,
}: {
  projectName: string;
  tag: string | undefined;
  onTagChange: (tag: string | undefined) => void;
}) {
  const builds = useBuilds(projectName);

  if (builds.isPending) {
    return (
      <Box sx={{ display: "flex", justifyContent: "center", p: 6 }}>
        <CircularProgress aria-label="Loading builds" />
      </Box>
    );
  }

  if (builds.isError) {
    return (
      <Alert
        severity="error"
        action={<Button onClick={() => void builds.refetch()}>Retry</Button>}
      >
        Failed to load builds
        {builds.error instanceof Error && builds.error.message
          ? `: ${builds.error.message}`
          : ""}
      </Alert>
    );
  }

  // An unknown/absent ?tag falls back to the newest build (the list is
  // newest-first), so a stale shared link degrades to "latest" not a 404.
  const newest = builds.data[0];
  const selected = builds.data.find((b) => b.tag === tag) ?? newest;
  if (!newest || !selected) {
    return (
      <Typography variant="body2" color="text.secondary" sx={{ py: 3 }}>
        No builds yet — publish your spec and click Build in the spec view to
        start the first one.
      </Typography>
    );
  }

  return (
    <>
      <Stack
        direction="row"
        spacing={2}
        sx={{ alignItems: "flex-start", mb: 2 }}
      >
        <BuildSummaryCard build={selected} />
        <Autocomplete
          options={builds.data.map((b) => b.tag)}
          value={selected.tag}
          onChange={(_, value) =>
            // Selecting the newest build clears ?tag — the default view.
            onTagChange(value && value !== newest.tag ? value : undefined)
          }
          disableClearable
          size="small"
          sx={{ width: 180, flexShrink: 0 }}
          renderInput={(params) => (
            // MUI's render params don't declare `| undefined` on their
            // optional props, which exactOptionalPropertyTypes rejects — the
            // cast is the documented escape hatch for this spread.
            <TextField {...(params as TextFieldProps)} label="Version" />
          )}
        />
      </Stack>
      <TasksList projectName={projectName} tag={selected.tag} />
    </>
  );
}

// Status chip vocabulary mirrors the overview's build stage (#183); a list
// read has no live query, so "started" barely occurs (treated as running).
function statusChip(status: BuildSummary["status"]): {
  label: string;
  color: "info" | "success" | "error";
} {
  switch (status) {
    case "completed":
      return { label: "Succeeded", color: "success" };
    case "failed":
      return { label: "Failed", color: "error" };
    default: // started / in_progress
      return { label: "Running", color: "info" };
  }
}

function BuildSummaryCard({ build }: { build: BuildSummary }) {
  const chip = statusChip(build.status);
  const { total, done, failed } = build.tasks;
  // total is written once the plan step finishes, so a running build with no
  // tasks is still planning — say so instead of "0/0 tasks done".
  const planning = chip.label === "Running" && total === 0;
  const started = new Date(build.startedAt).toLocaleString();
  const completed = build.completedAt
    ? new Date(build.completedAt).toLocaleString()
    : null;

  return (
    <Card variant="outlined" sx={{ flex: 1, minWidth: 0 }}>
      <CardContent>
        <Stack
          direction="row"
          spacing={1.5}
          sx={{ alignItems: "center", mb: 1 }}
        >
          <Typography variant="h6">{build.tag}</Typography>
          <Chip size="small" label={chip.label} color={chip.color} />
          {/* Actual build cost (#245): the run's aggregate coding-execution
              usage — accrues on the existing poll while the build runs.
              Absent for runs that predate usage capture. */}
          {build.usage && <UsageChip usage={build.usage} />}
          <Box sx={{ flexGrow: 1 }} />
          {planning ? (
            // Subtle "planning" signal for the selected tag: the run has started
            // but hasn't emitted its task plan yet (total === 0 while Running).
            <Stack
              direction="row"
              spacing={0.75}
              sx={{ alignItems: "center" }}
            >
              <CircularProgress
                size={12}
                thickness={5}
                aria-label="Generating tasks"
              />
              <Typography variant="body2" color="text.secondary">
                Generating tasks…
              </Typography>
            </Stack>
          ) : (
            <Typography variant="body2" color="text.secondary">
              {`${done}/${total} tasks done`}
            </Typography>
          )}
          {failed > 0 && (
            <Typography variant="body2" color="error.main">
              {failed} failed
            </Typography>
          )}
        </Stack>
        {build.status === "failed" && build.reason && (
          // Surface WHY a build failed (the devflow's recorded error) instead of
          // a bare "Failed" badge — otherwise the reason is buried in Temporal.
          <Typography
            variant="caption"
            color="error.main"
            sx={{ display: "block", mb: 0.5, whiteSpace: "pre-wrap" }}
          >
            {build.reason}
          </Typography>
        )}
        <Typography variant="caption" color="text.secondary">
          Started {started}
          {completed ? ` · Finished ${completed}` : ""}
        </Typography>
      </CardContent>
    </Card>
  );
}
