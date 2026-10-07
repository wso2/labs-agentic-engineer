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
import { Box, ButtonBase, Link, Typography } from "@wso2/oxygen-ui";
import { ChevronRight } from "@wso2/oxygen-ui-icons-react";
import type { components } from "../../../generated/aep-api";
import { orderedTasks, taskNote, taskState, taskStateLabel, type RunClaims, type TaskState } from "../model/taskRow";
import { TaskLog } from "./TaskLog";

// The Build card's tasks: one row per issue of the version, in the order the
// milestone planned them, each with its state and its status line (the
// issue's newest comment, runner ADR-0010). A row opens the task's live log
// in place, copied from the old console's per-task page.

type TaskView = components["schemas"]["TaskView"];

const LAMP: Record<TaskState, string> = {
  merged: "success.main",
  in_progress: "primary.main",
  blocked: "warning.main",
  pr_sent: "warning.main",
  pending: "text.disabled",
};

const NOTE_COLOUR: Partial<Record<TaskState, string>> = {
  in_progress: "primary.main",
  blocked: "warning.main",
  pr_sent: "warning.main",
};

function TaskRow({
  projectName,
  task,
  claims,
  open,
  onToggle,
}: {
  projectName: string;
  task: TaskView;
  claims: RunClaims;
  open: boolean;
  onToggle: () => void;
}) {
  const state = taskState(task, claims);
  const note = taskNote(task);
  return (
    <Box sx={{ "& + &": { borderTop: 1, borderColor: "divider" } }}>
      <Box sx={{ display: "flex", alignItems: "flex-start", gap: 1 }}>
        <ButtonBase
          onClick={onToggle}
          aria-expanded={open}
          sx={{
            flex: 1,
            minWidth: 0,
            display: "grid",
            gridTemplateColumns: "16px 10px minmax(0, 1fr)",
            columnGap: 1.25,
            alignItems: "baseline",
            textAlign: "start",
            px: 1.75,
            py: 1.125,
            "&:hover": { bgcolor: "action.hover" },
          }}
        >
          <ChevronRight
            size={14}
            aria-hidden
            style={{ transition: "transform 0.15s", transform: open ? "rotate(90deg)" : "none", alignSelf: "center" }}
          />
          <Box
            component="span"
            role="img"
            aria-label={taskStateLabel(state)}
            sx={{ width: 9, height: 9, borderRadius: "50%", bgcolor: LAMP[state], alignSelf: "center" }}
          />
          <Box component="span" sx={{ minWidth: 0, display: "flex", flexDirection: "column", gap: 0.25 }}>
            <Typography component="span" variant="body2" sx={{ fontWeight: 600 }}>
              {task.title}
            </Typography>
            <Typography
              component="span"
              variant="caption"
              sx={{
                color: NOTE_COLOUR[state] ?? "text.secondary",
                display: "-webkit-box",
                WebkitLineClamp: 2,
                WebkitBoxOrient: "vertical",
                overflow: "hidden",
              }}
            >
              {note ?? taskStateLabel(state)}
            </Typography>
          </Box>
        </ButtonBase>
        <Link
          href={task.issueUrl}
          target="_blank"
          rel="noreferrer"
          variant="caption"
          sx={{ fontFamily: "monospace", pt: 1.25, pr: 1.75, flexShrink: 0 }}
          aria-label={`Issue #${task.issueNumber} on GitHub`}
        >
          #{task.issueNumber}
        </Link>
      </Box>
      {open && (
        <Box sx={{ pl: 6, pr: 1.75, pb: 1.25 }}>
          <Box sx={{ border: 1, borderColor: "divider", borderRadius: 2, overflow: "hidden" }}>
            <TaskLog projectName={projectName} issueNumber={task.issueNumber} />
          </Box>
        </Box>
      )}
    </Box>
  );
}

/** The version's tasks; the first that is blocked or running opens on arrival. */
export function BuildTasks({ projectName, tasks, claims }: { projectName: string; tasks: TaskView[]; claims: RunClaims }) {
  const ordered = orderedTasks(tasks);
  const [chosen, setChosen] = useState<number | null | undefined>(undefined);
  const lead = ordered.find((t) => ["blocked", "in_progress"].includes(taskState(t, claims)))?.issueNumber ?? null;
  const openIssue = chosen === undefined ? lead : chosen;
  return (
    <Box>
      {ordered.map((task) => (
        <TaskRow
          key={task.issueNumber}
          projectName={projectName}
          task={task}
          claims={claims}
          open={openIssue === task.issueNumber}
          onToggle={() => setChosen(openIssue === task.issueNumber ? null : task.issueNumber)}
        />
      ))}
    </Box>
  );
}
