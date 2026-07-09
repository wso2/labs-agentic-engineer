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
  Card,
  CardActionArea,
  CardContent,
  Chip,
  Grid,
  Skeleton,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import { FileText, Hammer, Rocket } from "@wso2/oxygen-ui-icons-react";
import { useNavigate } from "@tanstack/react-router";
import type { components } from "../../../generated/aep-api";
import { bucketTasks } from "../api/taskBuckets";

type ProjectStatus = components["schemas"]["ProjectStatus"];
type TaskView = components["schemas"]["TaskView"];
type TagList = components["schemas"]["TagList"];

type Tone = "default" | "info" | "success" | "warning" | "error";

interface CardState {
  headline: string;
  detail: string;
  tone: Tone;
  chip?: string;
  emphasize?: boolean;
}

// The three pipeline cards always render — a fresh project shows the whole
// journey with the Spec card as the call-to-action (issue #77 decision).

function specCardState(
  status: ProjectStatus,
  tags: TagList | null | undefined,
): CardState {
  // Version + dirty state come from the tag resource (#117): latest = newest
  // user-tagged spec version, specDirty = specs/ changed on GitHub since.
  if (tags?.latest) {
    return tags.specDirty
      ? {
          headline: `${tags.latest} published`,
          detail: "The spec has changed since — a new draft is in progress.",
          tone: "warning",
          chip: "draft changes",
        }
      : {
          headline: `${tags.latest} published`,
          detail: "Spec and design are agreed and versioned.",
          tone: "success",
        };
  }
  switch (status.specStatus) {
    case "draft":
    case "in_progress":
      return {
        headline: "In collaboration",
        detail: "You and the agents are shaping the spec and design.",
        tone: "info",
      };
    case "ready":
      return {
        headline: "Awaiting your review",
        detail: "The derived spec and design are ready for approval.",
        tone: "warning",
      };
    case "failed":
      return {
        headline: "Derivation failed",
        detail: "Spec derivation hit a problem — open Spec for details.",
        tone: "error",
      };
    default:
      return status.phase === "prompt"
        ? {
            headline: "Deriving from your prompt…",
            detail: "Agents are turning your requirement into a spec.",
            tone: "info",
          }
        : {
            headline: "Start here",
            detail: "Work out what to build together with the agents.",
            tone: "default",
            emphasize: true,
          };
  }
}

function buildCardState(
  status: ProjectStatus,
  tasks: TaskView[] | undefined,
): CardState {
  if (!status.hasTasks || !tasks) {
    return {
      headline: "Waiting on spec",
      detail: "Agents start building once the spec is published.",
      tone: "default",
    };
  }
  const { counts, total, firstFailed, firstInProgress } = bucketTasks(tasks);
  if (counts.failed > 0) {
    return {
      headline: `${counts.failed} task${counts.failed > 1 ? "s" : ""} failed`,
      detail: firstFailed?.title ?? "A coding task needs attention.",
      tone: "error",
      chip: `${counts.done}/${total} done`,
    };
  }
  if (counts.inProgress > 0) {
    return {
      headline: `${counts.inProgress} in progress`,
      detail: firstInProgress?.title ?? "Agents are coding.",
      tone: "info",
      chip: `${counts.done}/${total} done`,
    };
  }
  if (total > 0 && counts.done === total) {
    return {
      headline: "All tasks done",
      detail: "Every coding task for this version has landed.",
      tone: "success",
    };
  }
  return {
    headline: "No tasks yet",
    detail: "Tasks appear once the published spec is broken down.",
    tone: "default",
  };
}

// Deployment state has no project-level source in the proposed contract
// (only per-component /deployments) — the card holds a placeholder until a
// deployments slice aggregates it (#113 rework decision).
function deployCardState(status: ProjectStatus): CardState {
  return status.hasTasks
    ? {
        headline: "Nothing deployed yet",
        detail: "Finished builds deploy to dev automatically.",
        tone: "default",
      }
    : {
        headline: "Waiting on build",
        detail: "Published builds deploy to dev automatically.",
        tone: "default",
      };
}

function StatusCard({
  icon,
  title,
  state,
  to,
  projectName,
}: {
  icon: ReactNode;
  title: string;
  state: CardState;
  to: string;
  projectName: string;
}) {
  const navigate = useNavigate();
  return (
    <Card
      variant="outlined"
      sx={{
        height: "100%",
        ...(state.emphasize && { borderColor: "primary.main", borderWidth: 2 }),
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
            {state.chip && <Chip size="small" label={state.chip} />}
          </Stack>
          <Typography
            variant="h6"
            color={state.tone === "default" ? "text.primary" : `${state.tone}.main`}
            gutterBottom
          >
            {state.headline}
          </Typography>
          <Typography variant="body2" color="text.secondary">
            {state.detail}
          </Typography>
        </CardContent>
      </CardActionArea>
    </Card>
  );
}

function LoadingCard({ title }: { title: string }) {
  return (
    <Card variant="outlined" sx={{ height: "100%" }}>
      <CardContent>
        <Typography variant="subtitle2" color="text.secondary" gutterBottom>
          {title}
        </Typography>
        <Skeleton width="60%" height={32} />
        <Skeleton width="90%" />
      </CardContent>
    </Card>
  );
}

export function StatusCards({
  projectName,
  status,
  tasks,
  tags,
}: {
  projectName: string;
  status: ProjectStatus | undefined;
  tasks: TaskView[] | undefined;
  tags: TagList | null | undefined;
}) {
  const cards = status
    ? [
        {
          title: "Spec",
          node: (
            <StatusCard
              icon={<FileText size={18} />}
              title="Spec"
              state={specCardState(status, tags)}
              to="spec"
              projectName={projectName}
            />
          ),
        },
        {
          title: "Build",
          node: (
            <StatusCard
              icon={<Hammer size={18} />}
              title="Build"
              state={buildCardState(status, tasks)}
              to="builds"
              projectName={projectName}
            />
          ),
        },
        {
          title: "Deployment",
          node: (
            <StatusCard
              icon={<Rocket size={18} />}
              title="Deployment"
              state={deployCardState(status)}
              to="deployments"
              projectName={projectName}
            />
          ),
        },
      ]
    : (["Spec", "Build", "Deployment"] as const).map((title) => ({
        title,
        node: <LoadingCard title={title} />,
      }));

  return (
    <>
      {cards.map(({ title, node }) => (
        <Grid key={title} size={{ xs: 12, md: 4 }}>
          {node}
        </Grid>
      ))}
    </>
  );
}
