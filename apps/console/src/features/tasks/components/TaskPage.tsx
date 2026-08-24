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

import { useEffect, useState } from "react";
import {
  Alert,
  Box,
  Button,
  CircularProgress,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import { Link } from "@tanstack/react-router";
import { PageHeader } from "../../../components/PageHeader";
import { StatusChip } from "../../../components/StatusChip";
import { useTask } from "../api/queries";
import { issueStateChip } from "../api/status";
import { useTaskLog } from "../hooks/useTaskLog";
import { GitHubRefChip } from "../../../components/GitHubRefChip";
import { TaskLogView } from "./TaskLogView";

// Seconds elapsed since resetKey last changed, while `active`. Used to age the
// waiting-state tail so a long, silent runner bootstrap (cold-start image pull
// can take a minute) reads as "still working" rather than a stall. The clock
// restarts whenever a new line arrives or the stream (re)connects, and stops
// ticking (no wasted 1s re-renders) once there is nothing to wait on.
function useSecondsSince(resetKey: string, active: boolean): number {
  const [seconds, setSeconds] = useState(0);
  useEffect(() => {
    setSeconds(0);
    if (!active) return;
    const started = Date.now();
    const id = setInterval(
      () => setSeconds(Math.floor((Date.now() - started) / 1000)),
      1000,
    );
    return () => clearInterval(id);
  }, [resetKey, active]);
  return seconds;
}

const EXEC_ACTIVE = new Set(["queued", "running"]);

// The per-ISSUE console: slim header (title, status chip, GitHub icon-link)
// over the flat streaming log. get-task provides the initial state; the SSE
// stream owns everything after that.
//
// Reached by URL, not from a row. The version's story — cycles, budgets, the
// per-cycle agent feed — is the Builds page, because after the flip an agent
// works a whole milestone rather than one issue and its log belongs to a CYCLE,
// not to an issue. What still has a per-issue log is an issue the PLATFORM ran
// something for: a provisioning gate. This page is that view.
export function TaskPage({
  projectName,
  issueNumber,
}: {
  projectName: string;
  issueNumber: number;
}) {
  const detail = useTask(projectName, issueNumber);
  const log = useTaskLog(projectName, issueNumber);
  // An attempt is still queued/running — used to reassure during long, silent
  // stretches (the runner bootstrap emits synthetic phase lines, but between
  // them the feed can be quiet for a while on a cold-start image pull).
  const anyRunning = log.executions.some((e) => EXEC_ACTIVE.has(e.status));
  // Restart the idle clock on every new line and on (re)connect; only tick while
  // something is actually being waited on. Called before the early returns below
  // so the hook order stays stable (rules of hooks).
  const idleSeconds = useSecondsSince(
    `${log.phase}:${log.lines.length}`,
    log.phase !== "ended" && (log.lines.length === 0 || anyRunning),
  );

  // One back-link style (Task 5): a router Link, wrapped by PageHeader's
  // shared PageTitle.BackButton — this replaces the old icon-only arrow.
  const backTo = {
    link: (
      <Link to="/projects/$projectName/builds" params={{ projectName }} />
    ),
    label: "Back to Builds",
  };

  if (detail.isPending) {
    return (
      <>
        <PageHeader title={`Task #${issueNumber}`} backTo={backTo} />
        <Box sx={{ display: "flex", justifyContent: "center", p: 6 }}>
          <CircularProgress aria-label="Loading task" />
        </Box>
      </>
    );
  }

  if (detail.isError) {
    return (
      <>
        <PageHeader title={`Task #${issueNumber}`} backTo={backTo} />
        <Alert
          severity="error"
          action={<Button onClick={() => void detail.refetch()}>Retry</Button>}
        >
          Failed to load the task
          {detail.error instanceof Error && detail.error.message
            ? `: ${detail.error.message}`
            : ""}
        </Alert>
      </>
    );
  }

  // The stream's view of the task is fresher than the initial fetch.
  const derivedStatus =
    log.settledStatus ?? log.task?.derivedStatus ?? detail.data.derivedStatus;
  const chip = issueStateChip(derivedStatus);
  const title = log.task?.title ?? detail.data.title;
  const issueUrl = log.task?.issueUrl ?? detail.data.issueUrl;
  let tail: string | undefined;
  if (log.phase === "reconnecting") {
    tail = "· connection lost — reconnecting…";
  } else if (log.phase === "connecting") {
    tail = "· attaching to the task log…";
  } else if (log.phase === "ended") {
    tail = `· task settled — ${derivedStatus}`;
  } else if (log.lines.length === 0) {
    // Live, no timeline yet: the coding attempt is being prepared (dispatch /
    // scheduling) before the runner's first line lands.
    tail =
      `· preparing the coding agent…${idleSeconds >= 20 ? " (a cold start can take up to a minute)" : ""}` +
      ` · ${idleSeconds}s`;
  } else if (anyRunning && idleSeconds >= 5) {
    // Timeline has content but nothing new for a bit and an attempt is live —
    // reassure rather than leave the last line looking stuck.
    tail = `· still working… · ${idleSeconds}s since last update`;
  }

  return (
    <>
      <PageHeader
        title={
          <Stack direction="row" spacing={1.5} sx={{ alignItems: "center" }}>
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{ fontVariantNumeric: "tabular-nums" }}
            >
              #{issueNumber}
            </Typography>
            <span>{title}</span>
            <StatusChip label={chip.label} tone={chip.tone} appearance="soft" />
          </Stack>
        }
        backTo={backTo}
        actions={
          <GitHubRefChip kind="issue" number={issueNumber} url={issueUrl} />
        }
      />
      <Box
        sx={{
          display: "flex",
          flexDirection: "column",
          // Fill the remaining page height so the log gets a real scroll area.
          minHeight: 480,
          height: "calc(100vh - 320px)",
        }}
      >
        <TaskLogView lines={log.lines} {...(tail ? { tail } : {})} />
      </Box>
    </>
  );
}
