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
  X,
} from "@wso2/oxygen-ui-icons-react";
import { createLink, Link } from "@tanstack/react-router";
import { EmptyState } from "../../../components/EmptyState";
import { LogSection } from "../../../components/LogSection";
import { PageHeader } from "../../../components/PageHeader";
import type { components } from "../../../generated/aep-api";
import { useAllTasks } from "../../tasks/api/queries";
import { useProjectStatus } from "../../projects/api/queries";
import { useBuildRuns, useBuilds, useCancelRun } from "../api/queries";
import { runStamp } from "../lib/format";
import {
  buildDuration,
  countTasks,
  isDeployable,
  isDurationOpen,
  isLedgerLive,
  ledgerStatus,
  milestoneLabel,
  taskBreakdown,
} from "../lib/ledger";
import { anyTaskRunning, runClaims, taskTally, type RunClaims } from "../lib/taskRow";
import {
  externalValuesPark,
  isAgentStreaming,
  isDeliveryRun,
  isTerminalRun,
  mergedCycle,
} from "../lib/runView";
import { settledLabel } from "../lib/feedTail";
import { AgentPulse } from "./AgentPulse";
import { BuildTaskList } from "./BuildTaskList";
import { CycleBuilds } from "./CycleBuilds";
import { EXTERNAL_RESOURCES_ANCHOR, ExternalResources } from "./ExternalResources";
import { RunFeed } from "./RunFeed";
import { useCycleBuilds } from "../api/queries";
import { useTicker } from "../hooks/useTicker";

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
  // Agent progress lives on the RUN, not on the task: aep-api leaves
  // `TaskView.executions` empty for agent work ("its pull request lives on the
  // run's cycle record instead"). Without this every open task read `Pending`.
  const claims = runClaims(runList);

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
  // The deploy gate's park (ADR-0023), read from the RUN. `ledgerStatus`
  // already knows a parked version is parked — `BuildSummary.waitingReason`
  // carries it — but only the run names the dependencies the notice below
  // lists, and this page has already made that read.
  const park = externalValuesPark(current);

  return (
    <>
      <PageHeader
        title={`Build ${build.tag}`}
        status={
          park
            ? { label: "Waiting for configuration", tone: "warning", variant: "filled" }
            : { label: status.label, tone: status.tone, variant: "filled" }
        }
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
          claims={claims}
          runs={runList}
          park={park}
          {...(projectStatus.data?.deploy ? { deploy: projectStatus.data.deploy } : {})}
        />

        <LogSection
          title="Tasks"
          disablePadding
          meta={<TasksMeta tasks={tasks} claims={claims} loading={issues.isPending} />}
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
            <BuildTaskList tasks={tasks} claims={claims} />
          )}
        </LogSection>

        {/* Directly after Tasks, and before the two log sections. ADR-0021 §4
            settled the shape of this page: a hold is not a stage of its own, it
            is a row that needs you, rendered like every other row that needs
            you. An external dependency short of a value is outstanding work a
            PERSON must do before this version deploys — a peer of a task, not
            of a log. The logs below it are a record of what happened; this is a
            request, so it reads before them. */}
        <ExternalResources projectName={projectName} />

        {/* TWO ways the log can be claiming a stream that is not there, and
            both clauses are load-bearing:
              - the agent's cycle has ENDED. A run stays in flight through the
                merge, the component builds and the deployment, long after its
                agent stopped, so the build's own state cannot answer this.
              - the run is PARKED at the deploy gate. `waiting` is not terminal
                and the park is a run-level fact, so no cycle has to close for
                it to be true. */}
        <AgentLogSection
          projectName={projectName}
          runId={current?.id}
          streaming={isAgentStreaming(runList) && park === null}
          runState={current?.state}
        />

        {/* The cycle that MERGED, not the newest one: the cluster read answers
            empty for a cycle with no merge SHA, and the newest cycle is
            routinely a validation cycle or a coding cycle that never merged. */}
        <BuildLogsSection projectName={projectName} tag={tag} cycleId={mergedCycle(runList)?.id} />
      </Stack>
    </>
  );
}

function TasksMeta({
  tasks,
  claims,
  loading,
}: {
  tasks: components["schemas"]["TaskView"][];
  claims: RunClaims;
  loading: boolean;
}) {
  if (loading) return null;
  const tally = taskTally(tasks, claims);
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
      {/* Keyed on a task actually executing, NOT on the run being open — a
          settled build must not look like it is still working. */}
      {anyTaskRunning(tasks, claims) && <AgentPulse />}
    </Stack>
  );
}

function BuildSummaryCard({
  projectName,
  build,
  tasks,
  claims,
  runs,
  park,
  deploy,
}: {
  projectName: string;
  build: BuildSummary;
  tasks: components["schemas"]["TaskView"][];
  claims: RunClaims;
  runs: components["schemas"]["MilestoneRunView"][];
  /** The external dependencies this version's run is parked on at the deploy
   *  gate, or null when it is not parked. Empty means parked, naming nothing. */
  park: string[] | null;
  deploy?: components["schemas"]["DeployStage"] | undefined;
}) {
  const live = isLedgerLive(build);
  // The duration counts against `Date.now()` until the build ends, so this card
  // has to re-render every second for it to move at all.
  const counting = isDurationOpen(build);
  useTicker(counting);
  const duration = buildDuration(build.startedAt, build.completedAt);
  // Derived from the tasks this page already holds — the same TAG-SCOPED read
  // the Tasks section below renders.
  const breakdown = taskBreakdown(countTasks(tasks, claims));
  const deployable = isDeployable(build, runs, deploy);

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
          {counting && (
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

      {/* THE PARK (ADR-0023). A run held at the deploy gate is unbounded and
          only a person can end it, so `waiting` with nothing beside it reads as
          a hang. This card is what now says why a version is or is not moving,
          so the explanation belongs on it — naming the dependencies, because
          "something is missing" leaves the reader hunting, and pointing at the
          External resources section on THIS page rather than a route: two ways
          into one configuration surface would be two things to keep in step. */}
      {park && (
        <Alert
          severity="warning"
          sx={{ mt: 2 }}
          action={
            <Button
              size="small"
              color="inherit"
              href={`#${EXTERNAL_RESOURCES_ANCHOR}`}
            >
              Add configuration
            </Button>
          }
        >
          <Typography variant="body2" sx={{ fontWeight: 600 }}>
            {parkTitle(park)}
          </Typography>
          <Typography variant="body2">
            Everything built. This version is not deployed until every external
            resource holds its development configuration — add it under External
            resources below and the run resumes and deploys on its own, with
            nothing to restart.
          </Typography>
        </Alert>
      )}

      <Divider sx={{ my: 2 }} />

      <Stack direction="row" spacing={1.25} sx={{ alignItems: "center", flexWrap: "wrap" }}>
        {/* Only once the version's work has actually merged — see
            `isDeployable`. Until then the note below says what has to happen,
            and a link to a board with nothing on it would contradict it. */}
        {deployable && (
          <RouterLink
            to="/projects/$projectName/deployments"
            params={{ projectName }}
            underline="hover"
            sx={{ fontSize: "0.8125rem", fontWeight: 500, display: "inline-flex", alignItems: "center", gap: 0.5 }}
          >
            Go to Deployments <ArrowRight size={14} />
          </RouterLink>
        )}
        <Typography variant="caption" color="text.secondary">
          {park
            ? `${build.tag} is built and waiting for its external configuration.`
            : deploymentNote(build.tag, deploy)}
        </Typography>
      </Stack>
    </Card>
  );
}

/**
 * The park's headline. It NAMES the dependencies when the run row carried them;
 * an older row (or a lost write) carries none, and that park still has to be
 * explainable, so the nameless case gets its own sentence rather than an empty
 * list rendered as punctuation.
 */
function parkTitle(dependencies: string[]): string {
  if (dependencies.length === 0) return "Waiting for external configuration";
  return `Waiting for configuration: ${dependencies.join(", ")}`;
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
  streaming,
  runState,
}: {
  projectName: string;
  runId: string | undefined;
  streaming: boolean;
  /** Forwarded to `AgentLogMeta`, which is where it is explained. */
  runState: string | undefined;
}) {
  return (
    <LogSection
      title="Coding agent log"
      meta={<AgentLogMeta streaming={streaming} runState={runState} />}
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

/**
 * The coding agent log's header note — the Tasks header's treatment, applied to
 * a different fact.
 *
 * Secondary caption text beside the title, with `AgentPulse` for "working right
 * now", exactly as `TasksMeta` renders its counts. It was a `StatusChip` before,
 * which made two sections one card apart label themselves in two different
 * shapes.
 */
function AgentLogMeta({
  streaming,
  runState,
}: {
  streaming: boolean;
  /** The run's own state, for the settled half. Both halves read the run list so
   *  they cannot disagree — see `settledLabel`. */
  runState: string | undefined;
}) {
  // Live beats settled: `streaming` is the stronger claim and the one a reader
  // is watching for. A run that is neither streaming nor terminal (parked at the
  // deploy gate, or between cycles) is labelled by neither — the summary card
  // above says what it is waiting on, and a note here would only compete.
  if (!streaming && (!runState || !isTerminalRun(runState))) return null;
  return (
    <Stack direction="row" spacing={1.25} sx={{ alignItems: "center" }}>
      <Typography variant="caption" color="text.secondary">
        {streaming ? "Streaming" : settledLabel(runState)}
      </Typography>
      {streaming && <AgentPulse />}
    </Stack>
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
          description="Build logs appear once a build session's pull request has merged and the components rebuild."
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
