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
import {
  Box,
  Button,
  Card,
  CardActionArea,
  CardContent,
  Chip,
  CircularProgress,
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
import { useInterviewState } from "../../agent-chat/interviewState";
import { useRetrySpecKickoff } from "../api/queries";
import { specFirstRunView, type SpecFirstRunView } from "../lib/specFirstRun";
import {
  buildStageView,
  CHIP_COLOR,
  deployStageView,
  specStageView,
  type StageView,
} from "../lib/pipeline";

type ProjectStatus = components["schemas"]["ProjectStatus"];

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

// The spec stage as a call-to-action, and as the agent's own voice through a
// new project's first run (#485).
//
// The CTA is PURE NAVIGATION. It carries no `?generate`, seeds no message and
// sends nothing: the backend starts every project's `/start` turn at creation,
// and a console that started one too raced it into the one-active-turn 409.
// (The older hazard is still real and is why this is worth saying twice: a
// second `/start` landing on an unanswered question form reads to the start
// skill as the user's skip valve, so the interview is silently replaced by the
// agent's own answers.)
//
// One button, two labels, one destination. "Continue spec" whenever an
// interview is open — "Generate spec" would promise a start that has already
// happened — and "Edit spec" is the wrong third word, since a spec the user
// could edit is exactly what an open exchange has not produced yet.
function SpecActionStage({
  projectName,
  view,
  firstRun,
  engaged,
}: {
  projectName: string;
  view: StageView;
  firstRun: SpecFirstRunView;
  engaged: boolean;
}) {
  const navigate = useNavigate();
  const retry = useRetrySpecKickoff(projectName);
  const failed = firstRun.stage === "failed";
  // The first run speaks for the card: a live interview's state is the only
  // thing worth saying while the document does not exist yet. Otherwise the
  // spec's own status ("published", "draft changes") stays — an amendment
  // replaces the plain card, and losing that line would make an open exchange
  // look like a project with no spec at all.
  const line = firstRun.line || view.line;
  const working = firstRun.stage === "starting" || firstRun.stage === "reading";
  return (
    <Card
      variant="outlined"
      sx={{
        flex: 1,
        minWidth: 0,
        borderWidth: 2,
        borderColor: failed ? "error.main" : "primary.main",
      }}
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
        {line && (
          <Stack
            direction="row"
            spacing={1}
            sx={{ alignItems: "center", mb: 1.5 }}
          >
            {working && <CircularProgress size={14} aria-hidden />}
            <Typography
              variant="body2"
              color={
                failed || view.tone === "error" ? "error.main" : "text.secondary"
              }
            >
              {line}
            </Typography>
          </Stack>
        )}
        {/* The failure says what happened, in the words the backend derived
            from the turn that died. A card that showed an error with nothing
            in it was the state this replaces. */}
        {failed && firstRun.reason && (
          <Typography variant="caption" color="text.secondary" sx={{ display: "block", mb: 1.5 }}>
            {firstRun.reason}
          </Typography>
        )}
        {failed ? (
          <Stack direction="row" spacing={1} sx={{ alignItems: "center" }}>
            <Button
              variant="contained"
              size="small"
              color="error"
              startIcon={<RefreshCw size={16} />}
              loading={retry.isPending}
              disabled={retry.isPending}
              onClick={() => retry.mutate()}
            >
              Retry
            </Button>
            {retry.isError && (
              <Typography variant="caption" color="error.main">
                {retry.error instanceof Error
                  ? retry.error.message
                  : "Failed to retry the spec interview."}
              </Typography>
            )}
          </Stack>
        ) : (
          <Button
            variant="contained"
            size="small"
            startIcon={<Sparkles size={16} />}
            onClick={() =>
              void navigate({
                to: "/projects/$projectName/spec",
                params: { projectName },
              })
            }
          >
            {engaged || firstRun.open ? "Continue spec" : "Generate spec"}
          </Button>
        )}
      </CardContent>
    </Card>
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
  // The first run (#485): the backend's kickoff report, sharpened by whatever
  // the live chat log already knows. The rail does not have to be open — the
  // log is kept fresh for the whole project view (AppLayout).
  const interview = useInterviewState(orgHandle ?? "default", projectName);
  const firstRun = specFirstRunView(status, interview);

  return (
    <Stack
      direction={{ xs: "column", md: "row" }}
      spacing={1}
      sx={{ alignItems: { xs: "stretch", md: "center" } }}
    >
      {spec.cta || engaged || firstRun.stage !== "none" ? (
        <SpecActionStage
          projectName={projectName}
          view={spec}
          firstRun={firstRun}
          engaged={engaged}
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
      <ChevronRight
        size={20}
        style={{ flexShrink: 0, alignSelf: "center", opacity: 0.4 }}
      />
      <StageCard
        icon={<ListChecks size={18} />}
        title="Build"
        view={build}
        to="builds"
        projectName={projectName}
      />
      <ChevronRight
        size={20}
        style={{ flexShrink: 0, alignSelf: "center", opacity: 0.4 }}
      />
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
