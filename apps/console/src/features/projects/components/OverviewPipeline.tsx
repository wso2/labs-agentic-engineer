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
} from "@wso2/oxygen-ui-icons-react";
import { useNavigate } from "@tanstack/react-router";
import type { components } from "../../../generated/aep-api";
import { useSession } from "../../../auth/SessionContext";
import { useAgentEngaged } from "../../agent-chat/useAgentEngaged";
import { useConversationLog } from "../../agent-chat/useConversationLog";
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

// The spec stage: one line that always says something, and one button that
// never changes (#562 retest).
//
// The button was three buttons — Generate spec, then Open spec, then Continue
// spec — and it walked all three during a single kickoff with no input from the
// user, because each state was inferred from a different signal that moved on
// its own. A control that renames itself while you read it cannot be learned.
// The destination never actually varied, so neither does the caption now.
//
// STARTING moved out with it. The card no longer fires anything: a project
// whose kickoff never ran is started from the spec view's empty state, which is
// exactly where this button lands, so the one control here can stay a
// destination rather than sometimes being a destination and sometimes a send.
function SpecActionStage({
  projectName,
  view,
}: {
  projectName: string;
  view: StageView;
}) {
  const navigate = useNavigate();
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
          {/* The version is a separate fact from what is happening right now —
              an amendment interview on v2 still reads as v2. */}
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
        <Typography
          variant="body2"
          color={view.tone === "error" ? "error.main" : "text.secondary"}
          sx={{ mb: 1.5 }}
        >
          {view.line}
        </Typography>
        <Button
          variant="contained"
          size="small"
          startIcon={<ChevronRight size={16} />}
          onClick={() =>
            void navigate({
              to: "/projects/$projectName/spec",
              params: { projectName },
            })
          }
        >
          Open spec
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
  // "The agent is waiting on you" has no server-side source: `spec.agent` folds
  // a completed turn to "", and a turn that ends ON a question is exactly that.
  // The local chat log is the only thing that knows.
  const org = useSession().orgHandle ?? "default";
  // ...so this card must make sure the log EXISTS (#606). It used to be filled
  // only while the chat panel was mounted, which is why a teammate opening the
  // overview in a fresh browser read "nothing has started" over a thread holding
  // someone else's unanswered question. One shared cache entry with the panel
  // and the spec workspace, so mounting it here costs no extra request.
  useConversationLog(org, projectName);
  const engaged = useAgentEngaged(org, projectName);
  const spec = specStageView(status, engaged);
  const build = buildStageView(status);
  const deploy = deployStageView(status);

  return (
    <Stack
      direction={{ xs: "column", md: "row" }}
      spacing={1}
      sx={{ alignItems: { xs: "stretch", md: "center" } }}
    >
      <SpecActionStage projectName={projectName} view={spec} />
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
