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

import { useState } from "react";
import {
  Alert,
  Box,
  Button,
  Card,
  CircularProgress,
  Divider,
  IconButton,
  Link as MuiLink,
  Menu,
  MenuItem,
  Skeleton,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import {
  ArrowRight,
  Compass,
  Copy,
  Ellipsis,
  GitHub,
  RotateCcw,
  Sparkles,
  X,
} from "@wso2/oxygen-ui-icons-react";
import { createLink, Link } from "@tanstack/react-router";
import { EmptyState } from "../../../components/EmptyState";
import { LogSection } from "../../../components/LogSection";
import { PageHeader } from "../../../components/PageHeader";
import { StatusChip } from "../../../components/StatusChip";
import type { components } from "../../../generated/aep-api";
import { useAllTasks } from "../../tasks/api/queries";
import { useProjectStatus } from "../../projects/api/queries";
import { useBuildRuns, useBuilds, useCancelRun } from "../api/queries";
import { runStamp } from "../lib/format";
import {
  buildDuration,
  countTasks,
  isLedgerLive,
  ledgerStatus,
  milestoneLabel,
  taskBreakdown,
} from "../lib/ledger";
import { anyTaskRunning, taskTally } from "../lib/taskRow";
import { isDeliveryRun } from "../lib/runView";
import { BuildTaskList } from "./BuildTaskList";
import { CycleBuilds } from "./CycleBuilds";
import { RunFeed } from "./RunFeed";
import { useCycleBuilds } from "../api/queries";

type BuildSummary = components["schemas"]["BuildSummary"];

// MUI's polymorphic `component={Link}` does not typecheck against the router's
// typed `to`/`params`; createLink is the console's established adapter.
const LinkButton = createLink(Button);
const LinkMenuItem = createLink(MenuItem);
const RouterLink = createLink(MuiLink);

/**
 * One version's build (ADR-0021 §2, §3).
 *
 * A summary card, then three collapsible sections — Tasks, the coding agent's
 * log, and the build logs. This carries what the Builds page used to lead with;
 * the page itself is now the ledger.
 */
export function BuildDetailPage({
  projectName,
  tag,
}: {
  projectName: string;
  tag: string;
}) {
  const builds = useBuilds(projectName);
  const build = builds.data?.find((b) => b.tag === tag);
  const live = build ? isLedgerLive(build) : false;

  const runs = useBuildRuns(projectName, tag);
  const runList = runs.data?.runs ?? [];
  // The runs that DELIVERED this version. A run that only re-judged it has no
  // build session to show — its verdict lives on the Validation board.
  const current = runList.filter(isDeliveryRun)[0];

  const issues = useAllTasks(projectName, tag, { live });
  const tasks = issues.data ?? [];
  // The deploy aggregate names which version reached an environment; the
  // project layout already polls it, so this is served from cache.
  const projectStatus = useProjectStatus(projectName);

  const backTo = {
    link: <Link to="/projects/$projectName/builds" params={{ projectName }} />,
    label: "Back to Builds",
  };

  if (builds.isPending) {
    return (
      <>
        <PageHeader title={`Build ${tag}`} backTo={backTo} />
        <Stack spacing={2} sx={{ mt: 2 }}>
          <Skeleton variant="rounded" height={120} />
          <Skeleton variant="rounded" height={280} />
        </Stack>
      </>
    );
  }

  if (builds.isError) {
    return (
      <>
        <PageHeader title={`Build ${tag}`} backTo={backTo} />
        <Alert
          severity="error"
          action={<Button onClick={() => void builds.refetch()}>Retry</Button>}
        >
          Failed to load builds
          {builds.error instanceof Error && builds.error.message
            ? `: ${builds.error.message}`
            : ""}
        </Alert>
      </>
    );
  }

  if (!build) {
    // An unknown tag is a dead end with a way out, not a blank page with a
    // title on it. The console's own `NotFound` is the router's catch-all and
    // takes no props, so this states the specific thing that is missing.
    return (
      <>
        <PageHeader title={`Build ${tag}`} backTo={backTo} />
        <EmptyState
          icon={<Compass size={48} />}
          title={`No build ${tag}`}
          description="That version was never built, or its tag has been removed."
          action={
            <LinkButton
              variant="contained"
              to="/projects/$projectName/builds"
              params={{ projectName }}
            >
              Back to Builds
            </LinkButton>
          }
        />
      </>
    );
  }

  const status = ledgerStatus(build, projectStatus.data?.deploy);

  return (
    <>
      <PageHeader
        title={`Build ${build.tag}`}
        status={{ label: status.label, tone: status.tone, variant: "filled" }}
        backTo={backTo}
        actions={
          <BuildActions projectName={projectName} tag={tag} runId={current?.id} live={live} />
        }
      />

      <Stack spacing={2}>
        <BuildSummaryCard
          projectName={projectName}
          build={build}
          tasks={tasks}
          {...(projectStatus.data?.deploy ? { deploy: projectStatus.data.deploy } : {})}
        />

        <LogSection
          title="Tasks"
          disablePadding
          meta={<TasksMeta tasks={tasks} loading={issues.isPending} />}
          actions={
            <LinkButton
              size="small"
              variant="outlined"
              color="primary"
              startIcon={<Sparkles size={14} />}
              to="/projects/$projectName"
              params={{ projectName }}
              sx={{ borderRadius: 999, height: 30 }}
            >
              Resolve via chat
            </LinkButton>
          }
        >
          {issues.isPending ? (
            <Box sx={{ p: 3, display: "flex", justifyContent: "center" }}>
              <CircularProgress size={24} aria-label="Loading this build's tasks" />
            </Box>
          ) : issues.isError ? (
            <Alert
              severity="error"
              sx={{ m: 2 }}
              action={<Button onClick={() => void issues.refetch()}>Retry</Button>}
            >
              Failed to load this build&apos;s tasks
              {issues.error instanceof Error && issues.error.message
                ? `: ${issues.error.message}`
                : ""}
            </Alert>
          ) : tasks.length === 0 ? (
            <EmptyState
              compact
              description="This build has no tasks yet — they appear as the milestone is planned."
            />
          ) : (
            <BuildTaskList projectName={projectName} tasks={tasks} />
          )}
        </LogSection>

        <AgentLogSection projectName={projectName} runId={current?.id} live={live} />

        <BuildLogsSection projectName={projectName} tag={tag} cycleId={current?.cycles?.at(-1)?.id} />
      </Stack>
    </>
  );
}

function TasksMeta({
  tasks,
  loading,
}: {
  tasks: components["schemas"]["TaskView"][];
  loading: boolean;
}) {
  if (loading) return null;
  const tally = taskTally(tasks);
  const parts = [`${tally.total} in this build`, `${tally.done} done`];
  if (tally.attention > 0) parts.push(`${tally.attention} need your attention`);
  return (
    <Stack direction="row" spacing={1.25} sx={{ alignItems: "center" }}>
      <Typography
        variant="caption"
        color="text.secondary"
        sx={{ fontVariantNumeric: "tabular-nums" }}
      >
        {parts.join(" · ")}
      </Typography>
      {/* The pulse is keyed on a task actually executing, NOT on the run being
          open — a settled build must not look like it is still working. */}
      {anyTaskRunning(tasks) && (
        <StatusChip label="agent working" tone="info" appearance="soft" dot />
      )}
    </Stack>
  );
}

function BuildSummaryCard({
  projectName,
  build,
  tasks,
  deploy,
}: {
  projectName: string;
  build: BuildSummary;
  tasks: components["schemas"]["TaskView"][];
  deploy?: components["schemas"]["DeployStage"] | undefined;
}) {
  const live = isLedgerLive(build);
  const duration = buildDuration(build.startedAt, build.completedAt);
  // Derived from the tasks this page already holds — the same TAG-SCOPED read
  // the Tasks section below renders.
  const breakdown = taskBreakdown(countTasks(tasks));

  const cells: Array<{ label: string; value: React.ReactNode }> = [
    { label: "Milestone", value: milestoneLabel(build) },
    { label: "Started", value: runStamp(build.startedAt) },
    {
      label: "Duration",
      value: (
        <>
          <Box component="span" sx={{ fontVariantNumeric: "tabular-nums" }}>
            {duration || "—"}
          </Box>
          {live && (
            <Box component="span" sx={{ color: "text.secondary" }}>
              {" "}
              and counting
            </Box>
          )}
        </>
      ),
    },
    { label: "Tasks", value: breakdown || "—" },
  ];

  return (
    <Card
      variant="outlined"
      sx={{
        p: 2.5,
        // The card is bordered in the version's own state colour while it is
        // moving, so the page's most important fact is visible before reading.
        ...(live && { borderColor: "info.main" }),
      }}
    >
      <Box
        sx={{
          display: "grid",
          gap: 2.5,
          gridTemplateColumns: { xs: "1fr", sm: "1fr 1fr", lg: "repeat(4, minmax(0, 1fr))" },
        }}
      >
        {cells.map((c) => (
          <Box key={c.label} sx={{ minWidth: 0 }}>
            <Typography
              variant="overline"
              color="text.secondary"
              sx={{ fontWeight: 700, letterSpacing: "0.07em" }}
            >
              {c.label}
            </Typography>
            <Typography variant="body2" sx={{ mt: 0.5, fontWeight: 500 }}>
              {c.value}
            </Typography>
          </Box>
        ))}
      </Box>

      <Divider sx={{ my: 2 }} />

      <Stack direction="row" spacing={1.25} sx={{ alignItems: "center", flexWrap: "wrap" }}>
        <RouterLink
          to="/projects/$projectName/deployments"
          params={{ projectName }}
          underline="hover"
          sx={{ fontSize: "0.8125rem", fontWeight: 500, display: "inline-flex", alignItems: "center", gap: 0.5 }}
        >
          Go to Deployments <ArrowRight size={14} />
        </RouterLink>
        <Typography variant="caption" color="text.secondary">
          {deploymentNote(build.tag, deploy)}
        </Typography>
      </Stack>
    </Card>
  );
}

/**
 * What the summary card says about this version's rollout.
 *
 * Every state the header pill can show gets its own sentence. The generic
 * "deploys as its tasks merge" line is for a version that has not reached an
 * environment — using it while the header reads "Deploying to development"
 * put two contradictory claims on one card.
 */
function deploymentNote(
  tag: string,
  deploy: components["schemas"]["DeployStage"] | undefined,
): string {
  if (deploy?.version !== tag) return `${tag} deploys as its tasks merge.`;
  switch (deploy.status) {
    case "deployed":
      return `${tag} is live in development.`;
    case "deploying":
      return `${tag} is rolling out to development now.`;
    case "failed":
      return `${tag} failed to deploy to development.`;
    default:
      return `${tag} deploys as its tasks merge.`;
  }
}

/** Cancel / retry / GitHub / copy — the design's overflow menu. */
function BuildActions({
  projectName,
  tag,
  runId,
  live,
}: {
  projectName: string;
  tag: string;
  runId: string | undefined;
  live: boolean;
}) {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const cancel = useCancelRun(projectName, tag);
  const status = useProjectStatus(projectName);
  const repoUrl = status.data?.repoUrl?.replace(/\/+$/, "").replace(/\.git$/, "");
  const close = () => setAnchor(null);

  return (
    <>
      <IconButton
        aria-label="Build actions"
        onClick={(e) => setAnchor(e.currentTarget)}
        sx={{ border: 1, borderColor: "divider" }}
      >
        <Ellipsis size={16} />
      </IconButton>
      <Menu anchorEl={anchor} open={Boolean(anchor)} onClose={close}>
        {/* Cancel is offered only while there is something to cancel — a menu
            item that cannot act is worse than an absent one. */}
        <MenuItem
          disabled={!live || !runId || cancel.isPending}
          onClick={() => {
            if (runId) cancel.mutate(runId);
            close();
          }}
        >
          <X size={15} style={{ marginRight: 10 }} />
          Cancel build
        </MenuItem>
        <LinkMenuItem
          to="/projects/$projectName/spec"
          params={{ projectName }}
          onClick={close}
        >
          <RotateCcw size={15} style={{ marginRight: 10 }} />
          Retry this build
        </LinkMenuItem>
        <Divider />
        <MenuItem
          component="a"
          href={repoUrl ? `${repoUrl}/milestones` : undefined}
          target="_blank"
          rel="noreferrer"
          disabled={!repoUrl}
          onClick={close}
        >
          <GitHub size={15} style={{ marginRight: 10 }} />
          View milestone on GitHub
        </MenuItem>
        <MenuItem
          onClick={() => {
            void navigator.clipboard?.writeText(tag);
            close();
          }}
        >
          <Copy size={15} style={{ marginRight: 10 }} />
          Copy build ID
        </MenuItem>
      </Menu>
      {cancel.isError && (
        <Alert severity="error" sx={{ mt: 1 }}>
          {cancel.error instanceof Error
            ? cancel.error.message
            : "Failed to cancel the build"}
        </Alert>
      )}
    </>
  );
}

function AgentLogSection({
  projectName,
  runId,
  live,
}: {
  projectName: string;
  runId: string | undefined;
  live: boolean;
}) {
  return (
    <LogSection
      title="Coding agent log"
      meta={
        live ? (
          <StatusChip label="streaming" tone="info" appearance="soft" dot />
        ) : undefined
      }
    >
      {runId ? (
        <RunFeed projectName={projectName} runId={runId} />
      ) : (
        <EmptyState
          compact
          description="Nothing has been dispatched for this version yet — the agent's log appears once a build session starts."
        />
      )}
    </LogSection>
  );
}

function BuildLogsSection({
  projectName,
  tag,
  cycleId,
}: {
  projectName: string;
  tag: string;
  cycleId: string | undefined;
}) {
  // Enabled only once there is a build session to have built anything: asking
  // earlier spends a cluster read to be told so.
  const builds = useCycleBuilds(projectName, tag, cycleId ?? "", Boolean(cycleId));
  return (
    <LogSection title="Build logs">
      {!cycleId ? (
        <EmptyState
          compact
          description="Build logs appear once a build session's work has merged and the components rebuild."
        />
      ) : builds.isPending ? (
        // Distinct from the note above: that one states a fact about the
        // build, and while the read is in flight that fact is not known yet.
        <Box sx={{ p: 3, display: "flex", justifyContent: "center" }}>
          <CircularProgress size={24} aria-label="Loading the build logs" />
        </Box>
      ) : builds.isError ? (
        <Alert
          severity="error"
          action={<Button onClick={() => void builds.refetch()}>Retry</Button>}
        >
          Failed to load the build logs
          {builds.error instanceof Error && builds.error.message
            ? `: ${builds.error.message}`
            : ""}
        </Alert>
      ) : (builds.data ?? []).length === 0 ? (
        <EmptyState compact description="No component builds were produced for this version." />
      ) : (
        <CycleBuilds projectName={projectName} builds={builds.data} />
      )}
    </LogSection>
  );
}
