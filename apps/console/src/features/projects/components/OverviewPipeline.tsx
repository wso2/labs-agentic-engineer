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

import { Fragment, type ReactNode } from "react";
import {
  Box,
  Button,
  Card,
  CardActionArea,
  CardContent,
  Chip,
  Skeleton,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import {
  ChevronRight,
  FileText,
  ListChecks,
  RefreshCw,
  Rocket,
  Sparkles,
} from "@wso2/oxygen-ui-icons-react";
import { useNavigate } from "@tanstack/react-router";
import type { components } from "../../../generated/aep-api";
import { useSession } from "../../../auth/SessionContext";
import { useAgentEngaged } from "../../agent-chat/useAgentEngaged";
import {
  useSpecInterview,
  type SpecInterviewState,
} from "../../agent-chat/useSpecInterview";
import { useRetrySpecKickoff } from "../api/queries";
import {
  buildStageView,
  CHIP_COLOR,
  deployStageView,
  specStageView,
  type StageView,
} from "../lib/pipeline";

type ProjectStatus = components["schemas"]["ProjectStatus"];
type SpecKickoffState = components["schemas"]["SpecKickoffState"];

function StageCard({
  icon,
  title,
  view,
  to,
  projectName,
}: {
  icon: ReactNode;
  title: string;
  view: StageView;
  to: string;
  projectName: string;
}) {
  const navigate = useNavigate();
  const ghost = view.tone === "ghost";
  return (
    <Card
      variant="outlined"
      sx={{
        flex: 1,
        minWidth: 0,
        ...(ghost && { opacity: 0.6 }),
        ...(view.tone === "error" && { borderColor: "error.main" }),
      }}
    >
      <CardActionArea
        sx={{ height: "100%", alignItems: "stretch" }}
        onClick={() =>
          void navigate({
            to: `/projects/$projectName/${to}`,
            params: { projectName },
          })
        }
      >
        <CardContent>
          <Stack direction="row" spacing={1} sx={{ alignItems: "center", mb: 1.5 }}>
            {icon}
            <Typography variant="subtitle2" color="text.secondary">
              {title}
            </Typography>
            <Box sx={{ flexGrow: 1 }} />
            <Chip
              size="small"
              label={view.version || "—"}
              color={CHIP_COLOR[view.tone]}
              variant={view.version ? "filled" : "outlined"}
            />
          </Stack>
          <Typography
            variant="body2"
            color={
              view.tone === "error"
                ? "error.main"
                : ghost
                  ? "text.disabled"
                  : "text.secondary"
            }
          >
            {view.line}
          </Typography>
        </CardContent>
      </CardActionArea>
    </Card>
  );
}

// The spec stage as a call-to-action: the way into the interview the BACKEND
// started, showing what it is doing on the way.
//
// This card NEVER starts a turn. Every new project's `/start` is fired
// server-side at create (#485), so the console has nothing to inject — and
// what it used to inject (`?generate=requirements` → a composed `/start` from
// the chat panel) was a SECOND turn racing the backend's first. The
// one-active-turn guard rejected it, and the user read "An agent turn is
// already running for this project" on a button that promised a spec. The
// param is gone; this is now navigation and nothing else.
//
// The label tracks state only so the button tells the truth about where it
// leads: an exchange to continue, or a spec view to open. "Edit spec" would be
// the wrong third word, since a spec the user could edit is exactly what an
// open exchange has not produced yet.
function SpecActionStage({
  projectName,
  view,
  engaged,
  interview,
  kickoff,
}: {
  projectName: string;
  view: StageView;
  engaged: boolean;
  interview: SpecInterviewState;
  /** What became of the backend's `/start` (#485) — the only signal that can
   *  tell "starting…" from "never started". */
  kickoff: SpecKickoffState;
}) {
  const navigate = useNavigate();
  const retry = useRetrySpecKickoff(projectName);
  // The card speaks as the agent (#485 live-testing round 3), tracking the
  // turn's ACTUAL stage: reading the idea, waiting on answers, or writing the
  // document.
  //
  // "your idea" throughout (round 5): the card narrates ABOUT the agent and
  // the chat is the agent speaking, but both are about the one thing the user
  // typed — so the card's two first-run lines and the chat's two
  // (START_READING_LINE, the question handoff) are the same sentence in two
  // voices. The count stays here and nowhere else.
  const interviewLine =
    interview.questionsWaiting > 0
      ? `Agent has ${interview.questionsWaiting} question${
          interview.questionsWaiting === 1 ? "" : "s"
        } about your idea`
      : interview.running
        ? interview.drafting
          ? "Agent is drafting the PRD…"
          : "Agent is looking at your idea"
        : kickoff.status === "pending"
          ? // The claim exists and its turn does not yet: the interview IS
            // starting, and for those seconds this is the only signal that says
            // so — the turn, the thread and the spec files are all still empty.
            "Agent is starting the interview"
          : null;
  // A kickoff that FAILED is the one state this card must not paper over: the
  // project sits with no interview and no way in, and the platform will not
  // retry on its own (an agents service that is down has nothing to retry
  // into). Name what failed, and put the retry on the card.
  const failed = kickoff.status === "failed";
  // `started` rather than the momentary signals: the label must not flip back
  // in the gaps between the poll's intervals, or between one first-run turn
  // ending and the next attaching.
  const open = engaged || interview.started || interviewLine !== null;
  return (
    <Card
      variant="outlined"
      sx={{ flex: 1, minWidth: 0, borderColor: "primary.main", borderWidth: 2 }}
    >
      <CardContent>
        <Stack direction="row" spacing={1} sx={{ alignItems: "center", mb: 1.5 }}>
          <FileText size={18} />
          <Typography variant="subtitle2" color="text.secondary">
            Spec
          </Typography>
          {/* An amendment runs against a spec that already has a version —
              keep its chip, so continuing doesn't look like starting over. */}
          {view.version && (
            <>
              <Box sx={{ flexGrow: 1 }} />
              <Chip
                size="small"
                label={view.version}
                color={CHIP_COLOR[view.tone]}
                variant="filled"
              />
            </>
          )}
        </Stack>
        {/* The live interview state wins the line; otherwise the state line
            the plain stage card would have shown. An amendment replaces that
            card, and the spec's status ("published", "draft changes") is true
            throughout — losing it would make an open exchange look like a
            project with no spec at all. Empty on the cold-start CTA, where
            there is no spec to have a status. */}
        {(failed || interviewLine || view.line) && (
          <Typography
            variant="body2"
            color={
              failed
                ? "error.main"
                : interviewLine
                  ? "primary.main"
                  : view.tone === "error"
                    ? "error.main"
                    : "text.secondary"
            }
            sx={{ mb: 1.5 }}
          >
            {failed
              ? // The reason comes from the attempt itself, so the card says
                // what actually went wrong rather than "something went wrong".
                (kickoff.reason || "The spec interview could not be started.")
              : (interviewLine ?? view.line)}
          </Typography>
        )}
        <Stack direction="row" spacing={1} sx={{ alignItems: "center" }}>
          {failed && (
            <Button
              variant="contained"
              size="small"
              startIcon={<RefreshCw size={16} />}
              // Asks the BACKEND to start the interview again — never a chat
              // message. Disabled while one attempt is in flight; a second
              // click on a kickoff already running would be a no-op anyway
              // (the endpoint is idempotent by state), but a button that does
              // nothing visible invites a third.
              disabled={retry.isPending}
              onClick={() => retry.mutate()}
            >
              {retry.isPending ? "Retrying…" : "Retry"}
            </Button>
          )}
          <Button
            variant={failed ? "outlined" : "contained"}
            size="small"
            startIcon={<Sparkles size={16} />}
            onClick={() =>
              void navigate({
                to: "/projects/$projectName/spec",
                params: { projectName },
              })
            }
          >
            {open ? "Continue spec" : "Open spec"}
          </Button>
        </Stack>
        {/* The retry itself failing to REACH the backend is a different
            failure from the kickoff failing, and the card would otherwise look
            unchanged after the click. */}
        {retry.isError && (
          <Typography variant="body2" color="error.main" sx={{ mt: 1.5 }}>
            {retry.error instanceof Error
              ? retry.error.message
              : "Failed to retry the spec interview."}
          </Typography>
        )}
      </CardContent>
    </Card>
  );
}

// The three stages, in journey order — shared by the live pipeline and its
// loading state so both always name the same steps.
const STAGES = [
  { key: "spec", title: "Spec", icon: <FileText size={18} /> },
  { key: "build", title: "Build", icon: <ListChecks size={18} /> },
  { key: "deploy", title: "Deploy", icon: <Rocket size={18} /> },
] as const;

function StageArrow() {
  return (
    <ChevronRight
      size={20}
      style={{ flexShrink: 0, alignSelf: "center", opacity: 0.4 }}
    />
  );
}

/**
 * The pipeline while the project's status is still in flight (#485
 * live-testing round 2 — it used to be one blank grey slab for the first few
 * seconds of every fresh project). The stage frames, icons and titles are
 * structure, known without asking the server; everything the status decides —
 * the version chip and the state line — is a skeleton, so nothing is claimed
 * before it is known.
 */
export function OverviewPipelineSkeleton() {
  return (
    <Stack
      data-testid="overview-pipeline-skeleton"
      aria-busy="true"
      aria-label="Loading the project pipeline"
      direction={{ xs: "column", md: "row" }}
      spacing={1}
      sx={{ alignItems: { xs: "stretch", md: "center" } }}
    >
      {STAGES.map((stage, i) => (
        <Fragment key={stage.key}>
          {i > 0 && <StageArrow />}
          <Card variant="outlined" sx={{ flex: 1, minWidth: 0 }}>
            <CardContent>
              <Stack
                direction="row"
                spacing={1}
                sx={{ alignItems: "center", mb: 1.5 }}
              >
                {stage.icon}
                <Typography variant="subtitle2" color="text.secondary">
                  {stage.title}
                </Typography>
                <Box sx={{ flexGrow: 1 }} />
                <Skeleton variant="rounded" width={48} height={24} />
              </Stack>
              <Skeleton variant="text" width="70%" />
            </CardContent>
          </Card>
        </Fragment>
      ))}
    </Stack>
  );
}

// The overview's centerpiece (#183): one connected journey, spec → build →
// deploy, each stage stamped with its version and linking to its section.
export function OverviewPipeline({
  projectName,
  status,
}: {
  projectName: string;
  status: ProjectStatus;
}) {
  const spec = specStageView(status);
  const build = buildStageView(status);
  const deploy = deployStageView(status);
  // An open exchange turns the spec stage back into an action, whether or not a
  // spec exists: `/start` on an existing PRD is an amendment interview, which
  // asks questions the same way and is skipped by a stray start the same way —
  // and the overview otherwise gives no sign one is open.
  const { orgHandle } = useSession();
  const engaged = useAgentEngaged(orgHandle ?? "default", projectName);
  // The BE-started interview (#485), server-sourced so it shows on a fresh
  // landing where no chat log exists yet. Enabled only pre-spec — once a spec
  // exists, `engaged` (the live chat log) covers amendment interviews without
  // this hook's polling.
  const interview = useSpecInterview(
    orgHandle ?? "default",
    projectName,
    spec.cta === true,
  );

  return (
    <Stack
      direction={{ xs: "column", md: "row" }}
      spacing={1}
      sx={{ alignItems: { xs: "stretch", md: "center" } }}
    >
      {spec.cta || engaged ? (
        <SpecActionStage
          projectName={projectName}
          view={spec}
          engaged={engaged}
          interview={interview}
          kickoff={status.spec.kickoff}
        />
      ) : (
        <StageCard
          icon={<FileText size={18} />}
          title="Spec"
          view={spec}
          to="spec"
          projectName={projectName}
        />
      )}
      <StageArrow />
      <StageCard
        icon={<ListChecks size={18} />}
        title="Build"
        view={build}
        to="builds"
        projectName={projectName}
      />
      <StageArrow />
      <StageCard
        icon={<Rocket size={18} />}
        title="Deploy"
        view={deploy}
        to="deployments"
        projectName={projectName}
      />
    </Stack>
  );
}
