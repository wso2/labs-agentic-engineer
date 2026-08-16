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
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import {
  ChevronRight,
  FileText,
  ListChecks,
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

// The spec stage as a call-to-action: when no spec exists yet (#150 behavior
// preserved) Generate spec opens the Spec view and auto-sends the first
// requirements turn, and when the agent is mid-exchange the same stage offers
// the way back into it.
//
// A fresh prompt-ful project is usually ALREADY interviewing — the BE starts
// `/start` at create (#485) — so the card shows the live state
// ("interviewing…", "interviewing — N questions waiting") instead of offering
// to start what is running. Generate spec survives only as the prompt-less
// project's manual fallback.
//
// "Continue spec" NEVER carries `?generate`. Injecting a second `/start` into
// an open exchange is the whole bug: landing on an unanswered question form, it
// reads to the start skill as the user's skip valve, so the interview is
// silently replaced by the agent's own answers (see `agentEngaged`). The chat
// panel still opens on arrival — SpecView requests it while the first-run turn
// is active (live-testing round) — but side by side, so the question form
// keeps the spec body.
//
// One button, two labels, one destination: "Edit spec" would be the wrong third
// word, since a spec the user could edit is exactly what an open exchange has
// not produced yet.
function SpecActionStage({
  projectName,
  view,
  engaged,
  interview,
}: {
  projectName: string;
  view: StageView;
  engaged: boolean;
  interview: SpecInterviewState;
}) {
  const navigate = useNavigate();
  // Wording aligned with the spec view's working-state stage lines (#485
  // live-testing round): the card tracks the turn's ACTUAL stage — preparing
  // questions, parked on questions, or drafting the document.
  const interviewLine =
    interview.questionsWaiting > 0
      ? `interviewing — ${interview.questionsWaiting} question${
          interview.questionsWaiting === 1 ? "" : "s"
        } waiting`
      : interview.running
        ? interview.drafting
          ? "Agent is drafting the PRD…"
          : "Agent is preparing your questions…"
        : null;
  // The interview state is an open exchange by definition — same injection
  // guard as `engaged`, sourced server-side so it holds before the chat log
  // ever loaded in this browser.
  const open = engaged || interviewLine !== null;
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
        {(interviewLine ?? view.line) && (
          <Typography
            variant="body2"
            color={
              interviewLine
                ? "primary.main"
                : view.tone === "error"
                  ? "error.main"
                  : "text.secondary"
            }
            sx={{ mb: 1.5 }}
          >
            {interviewLine ?? view.line}
          </Typography>
        )}
        <Button
          variant="contained"
          size="small"
          startIcon={<Sparkles size={16} />}
          onClick={() =>
            void navigate({
              to: "/projects/$projectName/spec",
              params: { projectName },
              ...(open ? {} : { search: { generate: "requirements" as const } }),
            })
          }
        >
          {open ? "Continue spec" : "Generate spec"}
        </Button>
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
