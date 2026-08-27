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
  Box,
  Button,
  Chip,
  LinearProgress,
  Link as MuiLink,
  Stack,
  Typography,
  alpha,
} from "@wso2/oxygen-ui";
import {
  ArrowUpRight,
  Box as BoxIcon,
  ChevronRight,
  CircleAlert,
  CircleCheck,
  CircleDashed,
  GitHub,
  LoaderCircle,
  Plug,
} from "@wso2/oxygen-ui-icons-react";
import { createLink } from "@tanstack/react-router";
import type { components } from "../../../generated/aep-api";
import { runStamp } from "../lib/format";
import { buildDuration } from "../lib/ledger";
import {
  taskElapsedFrom,
  taskRowChip,
  taskRowNote,
  taskRowState,
  taskSettledAt,
  type TaskRowState,
} from "../lib/taskRow";

type TaskView = components["schemas"]["TaskView"];

// MUI polymorphism does not carry the router's typed `to`/`params`;
// createLink is the console's established adapter.
const RouterLink = createLink(MuiLink);
const LinkButton = createLink(Button);

/**
 * The build page's task list — the design's arrangement 2b (ADR-0021 §3).
 *
 * One row per task, gates included: a connection to configure and a feature to
 * write are peers here, each with its own way out (§4). That is what replaced
 * the stage rail's separate provisioning section.
 */

const STATE_ICON: Record<
  TaskRowState,
  { Icon: typeof CircleCheck; palette: "success" | "info" | "warning" | "grey" }
> = {
  done: { Icon: CircleCheck, palette: "success" },
  in_progress: { Icon: LoaderCircle, palette: "info" },
  blocked: { Icon: CircleAlert, palette: "warning" },
  in_review: { Icon: CircleAlert, palette: "warning" },
  pending: { Icon: CircleDashed, palette: "grey" },
};

function StateTile({ state }: { state: TaskRowState }) {
  const { Icon, palette } = STATE_ICON[state];
  return (
    <Box
      sx={{
        width: 34,
        height: 34,
        borderRadius: 1.25,
        flexShrink: 0,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        bgcolor: (t) =>
          palette === "grey"
            ? t.palette.action.hover
            : alpha(t.palette[palette].main, 0.12),
        color: palette === "grey" ? "text.disabled" : `${palette}.main`,
        // The spinner is the one thing on a row that may move: it is the
        // difference between "the agent is on this" and "this is just open".
        ...(state === "in_progress" && {
          "@keyframes taskSpin": { to: { transform: "rotate(360deg)" } },
          "& svg": { animation: "taskSpin 1s linear infinite" },
        }),
      }}
    >
      <Icon size={18} aria-hidden />
    </Box>
  );
}

/** The component (or dependency) a task belongs to. */
function ComponentChip({ task }: { task: TaskView }) {
  if (!task.component) return null;
  const isGate = task.executorClass === "provision";
  const Icon = isGate ? Plug : BoxIcon;
  return (
    <Chip
      size="small"
      icon={<Icon size={12} />}
      label={task.component}
      sx={{ height: 22, flexShrink: 0, fontSize: "0.75rem" }}
    />
  );
}

export function BuildTaskRow({
  projectName,
  task,
}: {
  projectName: string;
  task: TaskView;
}) {
  const state = taskRowState(task);
  const chip = taskRowChip(state);
  const note = taskRowNote(task);
  const elapsedFrom = taskElapsedFrom(task);
  const settledAt = taskSettledAt(task);

  const tint =
    state === "in_progress"
      ? "info"
      : state === "blocked" || state === "in_review"
        ? "warning"
        : null;

  return (
    <Box
      sx={{
        borderBottom: 1,
        borderColor: "divider",
        "&:last-of-type": { borderBottom: 0 },
        ...(tint && {
          bgcolor: (t) => alpha(t.palette[tint].main, 0.05),
        }),
      }}
    >
      <Stack
        direction="row"
        spacing={1.75}
        sx={{ alignItems: "flex-start", px: 2.25, py: 1.875 }}
      >
        <StateTile state={state} />

        <Box sx={{ flex: 1, minWidth: 0 }}>
          <Stack direction="row" spacing={1.25} sx={{ alignItems: "center", minWidth: 0 }}>
            {/* The title links to the task's own page — the row's destination,
                and the one the chevron on the right stands for. */}
            <RouterLink
              to="/projects/$projectName/tasks/$issueNumber"
              params={{ projectName, issueNumber: task.issueNumber }}
              underline="hover"
              color="text.primary"
              sx={{
                fontSize: "0.90625rem",
                fontWeight: 500,
                whiteSpace: "nowrap",
                overflow: "hidden",
                textOverflow: "ellipsis",
              }}
              title={task.title}
            >
              {task.title}
            </RouterLink>
            {/* Straight to GitHub, deliberately NOT to the task page: ADR-0013
                §5's one surviving idea is that an issue chip means the issue. */}
            <Chip
              size="small"
              component="a"
              href={task.issueUrl}
              target="_blank"
              rel="noreferrer"
              clickable
              icon={<GitHub size={13} />}
              label={`#${task.issueNumber}`}
              sx={{
                height: 22,
                flexShrink: 0,
                fontFamily: "monospace",
                fontSize: "0.75rem",
              }}
            />
          </Stack>

          <Stack
            direction="row"
            spacing={1.125}
            sx={{ alignItems: "center", mt: 0.875, minWidth: 0 }}
          >
            <ComponentChip task={task} />
            {note && (
              <Typography
                variant="caption"
                sx={{
                  flex: 1,
                  minWidth: 0,
                  lineHeight: 1.55,
                  color:
                    state === "in_progress"
                      ? "info.main"
                      : state === "blocked" || state === "in_review"
                        ? "warning.main"
                        : "text.secondary",
                  display: "-webkit-box",
                  WebkitLineClamp: 2,
                  WebkitBoxOrient: "vertical",
                  overflow: "hidden",
                }}
              >
                {note}
              </Typography>
            )}
          </Stack>
        </Box>

        <Stack
          direction="row"
          spacing={1.5}
          sx={{ alignItems: "center", flexShrink: 0, pt: 0.625 }}
        >
          {state === "blocked" ? (
            // The way out, on the row that needs it (ADR-0021 §4).
            <LinkButton
              to="/resources"
              size="small"
              variant="outlined"
              endIcon={<ArrowUpRight size={13} />}
              sx={{ borderRadius: 999, height: 28 }}
            >
              Configure in Resources
            </LinkButton>
          ) : elapsedFrom ? (
            <Typography
              variant="caption"
              color="info.main"
              sx={{ fontVariantNumeric: "tabular-nums" }}
            >
              {buildDuration(elapsedFrom)}
            </Typography>
          ) : (
            <Typography variant="caption" color="text.secondary">
              {settledAt ? runStamp(settledAt) : chip.label}
            </Typography>
          )}
          {state !== "blocked" && (
            <ChevronRight size={15} aria-hidden style={{ opacity: 0.5 }} />
          )}
        </Stack>
      </Stack>

      {/* The running row's own progress bar. Indeterminate on purpose — the
          platform reports no percentage for a task, and a determinate bar would
          be inventing one. */}
      {state === "in_progress" && (
        <LinearProgress color="info" sx={{ height: 2 }} />
      )}
    </Box>
  );
}

export function BuildTaskList({
  projectName,
  tasks,
}: {
  projectName: string;
  tasks: TaskView[];
}) {
  return (
    <Box>
      {tasks.map((task) => (
        <BuildTaskRow key={task.issueNumber} projectName={projectName} task={task} />
      ))}
    </Box>
  );
}
