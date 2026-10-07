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

import type { ReactNode } from "react";
import { createLink, useNavigate } from "@tanstack/react-router";
import { Alert, Box, Button, Link, Skeleton, Typography } from "@wso2/oxygen-ui";
import { GitHub } from "@wso2/oxygen-ui-icons-react";
import { EmptyState } from "../../../components/EmptyState";
import { stamp } from "../../../lib/stamp";
import { chatStore } from "../../agent-chat/useProjectChat";
import { CardOverlay } from "../../projects/components/CardOverlay";
import { repoLabel } from "../../projects/repo";
import { useStartBuild, useStartFix } from "../api/builds";
import { isTerminalRun, useCancelRun } from "../api/runs";
import { useBuildCard, type BuildCardData } from "../hooks/useBuildCard";
import { useBuildOutcome, type BuildOutcome } from "../hooks/useBuildOutcome";
import { useNextInterview } from "../hooks/useNextInterview";
import { fixedBy, isBuilding } from "../model/ledger";
import { buildCardSteps, nextSteps, startedNote } from "../model/nextSteps";
import { runStatus } from "../model/phases";
import { isAgentStreaming, mergedCycle, settledLabel } from "../model/run";
import { rolloutLine, validationLine } from "../model/summary";
import { taskTally } from "../model/taskRow";
import { AgentLog } from "./AgentLog";
import { BuildLogs } from "./BuildLogs";
import { BuildTasks } from "./BuildTasks";
import { CardSection } from "./CardSection";
import { ExplanationNotice } from "./ExplanationNotice";
import { NextStepsBar } from "./NextStepsBar";
import { PhaseStrip } from "./PhaseStrip";

// A version's Build card, over build history: the old console's build page
// (features/builds/components/BuildDetailPage.tsx). Its summary, the strip of
// where the run is, why it is stuck or failed when it is, then its tasks with
// their status lines and logs, the coding agent's log and the component build
// logs; then a line to its validation (a card of its own), and what a
// finished build offers next.

const LinkButton = createLink(Button);
const RouterLink = createLink(Link);

const TONE_COLOUR = { primary: "primary.main", success: "success.main", warning: "warning.main", error: "error.main" } as const;

/** Cancel the run while it is live; Retry a build that failed or was cancelled; the version's milestone on GitHub. */
function BuildActions({
  data,
  cancel,
  retry,
}: {
  data: BuildCardData;
  cancel: ReturnType<typeof useCancelRun>;
  retry: { run: () => void; pending: boolean };
}) {
  const { current, runState, build, summary } = data;
  const repo = repoLabel(data.repoUrl);
  const settledBadly = runState === "failed" || runState === "cancelled" || runState === "blocked";
  return (
    <>
      {current && data.live && (
        <Button size="small" color="error" loading={cancel.isPending} onClick={() => cancel.mutate(current.id)}>
          Cancel run
        </Button>
      )}
      {build && settledBadly && (
        <Button size="small" variant="outlined" loading={retry.pending} onClick={retry.run}>
          Retry
        </Button>
      )}
      {repo && (
        <Button
          size="small"
          href={summary ? `${repo.href}/milestone/${summary.milestoneNumber}` : `${repo.href}/milestones`}
          target="_blank"
          rel="noreferrer"
          startIcon={<GitHub size={14} />}
        >
          GitHub
        </Button>
      )}
    </>
  );
}

function Summary({ data, version, outcome }: { data: BuildCardData; version: string; outcome: BuildOutcome["outcome"] }) {
  const { build, summary, steps } = data;
  const status = data.park ? { text: "Waiting for configuration", tone: "warning" as const } : runStatus(data.runState, steps, outcome);
  const tally = taskTally(data.tasks.data ?? [], data.claims);
  const facts = [
    data.tasks.data ? `${tally.done} of ${tally.total} tasks done` : null,
    summary ? `started ${stamp(summary.startedAt)}` : null,
    build?.features.map((f) => f.name).join(", ") || null,
  ].filter(Boolean);
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 0.75 }}>
      <Typography variant="caption" sx={{ letterSpacing: "0.08em", textTransform: "uppercase", fontWeight: 600, color: "text.secondary" }}>
        {build?.fixes ? `Build · fixes ${build.fixes}` : "Build"}
      </Typography>
      <Box sx={{ display: "flex", alignItems: "baseline", gap: 1.5, flexWrap: "wrap" }}>
        <Typography component="h2" sx={{ fontFamily: "monospace", fontSize: "1.375rem", fontWeight: 600, lineHeight: 1.2 }}>
          {version}
        </Typography>
        <Typography role="status" variant="body2" sx={{ fontWeight: 600, color: status.tone ? TONE_COLOUR[status.tone] : "text.secondary" }}>
          {status.text}
        </Typography>
      </Box>
      {facts.length > 0 && (
        <Typography variant="body2" color="text.secondary">
          {facts.join(" · ")}
        </Typography>
      )}
      <Typography variant="body2">{rolloutLine(version, data.deploy, data.park !== null)}</Typography>
    </Box>
  );
}

function TasksSection({ projectName, data }: { projectName: string; data: BuildCardData }) {
  const { tasks, claims } = data;
  const tally = taskTally(tasks.data ?? [], claims);
  const meta = tasks.data
    ? [`${tally.done} of ${tally.total} done`, ...(tally.attention ? [`${tally.attention} need${tally.attention === 1 ? "s" : ""} you`] : [])].join(" · ")
    : undefined;
  let body: ReactNode;
  if (tasks.isError) {
    body = (
      <Alert severity="error" sx={{ m: 1.5 }} action={<Button onClick={() => void tasks.refetch()}>Retry</Button>}>
        {tasks.error.message}
      </Alert>
    );
  } else if (!tasks.data) {
    body = <Skeleton variant="rounded" height={96} sx={{ m: 1.5 }} aria-label="Loading the tasks" />;
  } else if (tasks.data.length === 0) {
    body = (
      <Typography variant="body2" color="text.secondary" sx={{ px: 1.75, py: 1.25 }}>
        No tasks yet: they appear as the version is planned.
      </Typography>
    );
  } else {
    body = <BuildTasks projectName={projectName} tasks={tasks.data} claims={claims} />;
  }
  return (
    <CardSection title="Tasks" meta={meta} defaultOpen>
      {body}
    </CardSection>
  );
}

/** The line to the version's validation, and what a finished build offers next. */
function ValidationAndNext({
  projectName,
  data,
  version,
  result,
}: {
  projectName: string;
  data: BuildCardData;
  version: string;
  result: BuildOutcome;
}) {
  const nextInterview = useNextInterview(projectName);
  const settled = data.runState !== undefined && isTerminalRun(data.runState);
  const fix = fixedBy(data.rows, version);
  const next =
    settled && result.outcome
      ? buildCardSteps(
          nextSteps({
            version,
            outcome: result.outcome,
            fixedBy: fix ? { version: fix.version, building: isBuilding(fix.status) } : null,
            latest: data.rows[0]?.version ?? version,
            nextInterview,
          }),
          version,
        )
      : null;
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5, borderTop: 1, borderColor: "divider", pt: 1.75 }}>
      <Box sx={{ display: "flex", alignItems: "baseline", gap: 1, flexWrap: "wrap" }}>
        <Typography variant="body2" color="text.secondary">
          {validationLine(version, result.outcome, Boolean(result.validation) && !result.outcome)}
        </Typography>
        <RouterLink to="/projects/$projectName/validations/$version" params={{ projectName, version }} variant="body2">
          Open {version}&apos;s validation
        </RouterLink>
      </Box>
      {next && next.steps.length > 0 && <NextStepsBar projectName={projectName} next={next} />}
    </Box>
  );
}

function BuildBody({ projectName, version, data }: { projectName: string; version: string; data: BuildCardData }) {
  // The stream's validation cycle is fresher than the run poll's, while the card watches one.
  const liveValidation = data.progress.cycles.filter((c) => c.cycle.kind === "validation").at(-1)?.cycle;
  const result = useBuildOutcome(projectName, version, liveValidation);
  if (data.buildsError) {
    return (
      <Alert severity="error" action={<Button onClick={data.retryBuilds}>Retry</Button>}>
        Couldn't load {version}'s build: {data.buildsError.message}
      </Alert>
    );
  }
  if (data.build === undefined) return <Skeleton variant="rounded" height={160} sx={{ maxWidth: 860 }} />;
  if (data.build === null) {
    return (
      <EmptyState
        title={`No build ${version}`}
        description="That version was never built, or its tag has been removed."
        action={
          <LinkButton variant="contained" to="/projects/$projectName/builds" params={{ projectName }}>
            Build history
          </LinkButton>
        }
      />
    );
  }
  const streaming = isAgentStreaming(data.runs) && data.park === null;
  const agentMeta = streaming ? "writing now" : data.runState && isTerminalRun(data.runState) ? settledLabel(data.runState) : undefined;
  return (
    <Box sx={{ maxWidth: 860, display: "flex", flexDirection: "column", gap: 2 }}>
      <Summary data={data} version={version} outcome={result.outcome} />
      <PhaseStrip steps={data.steps} loading={data.replaying} />
      {data.explanation && <ExplanationNotice projectName={projectName} explanation={data.explanation} />}
      <TasksSection projectName={projectName} data={data} />
      <CardSection title="Coding agent's log" meta={agentMeta ?? "newest first"} defaultOpen={streaming}>
        <AgentLog projectName={projectName} runs={data.runs} progress={data.progress} />
      </CardSection>
      <CardSection title="Build logs">
        <BuildLogs projectName={projectName} version={version} cycleId={mergedCycle(data.runs)?.id} />
      </CardSection>
      <ValidationAndNext projectName={projectName} data={data} version={version} result={result} />
    </Box>
  );
}

export function BuildCard({ projectName, version }: { projectName: string; version: string }) {
  const data = useBuildCard(projectName, version);
  const navigate = useNavigate();
  const cancel = useCancelRun(projectName, version);
  const start = useStartBuild(projectName);
  const fix = useStartFix(projectName);
  const build = data.build;

  // Retry builds the version's features again (a repair, what it fixed): with
  // the spec unchanged the platform reuses the version rather than cutting one.
  const retry = () => {
    if (!build) return;
    const onSuccess = (tag: string) => {
      const next = tag || version;
      void navigate({ to: "/projects/$projectName/builds/$version", params: { projectName, version: next } });
      const note = startedNote(next, build.features.map((f) => f.name), null);
      chatStore.post(projectName, note.text, note.actions);
    };
    if (build.fixes) fix.mutate(build.fixes, { onSuccess });
    else start.mutate({ features: build.features.map((f) => f.id), productWide: [] }, { onSuccess });
  };
  const error = cancel.error ?? start.error ?? fix.error;

  return (
    <CardOverlay
      card="build"
      title={`Build ${version}`}
      actions={<BuildActions data={data} cancel={cancel} retry={{ run: retry, pending: start.isPending || fix.isPending }} />}
    >
      {error && (
        <Alert severity="error" sx={{ mb: 2, maxWidth: 860 }}>
          {error.message}
        </Alert>
      )}
      <BuildBody projectName={projectName} version={version} data={data} />
    </CardOverlay>
  );
}
