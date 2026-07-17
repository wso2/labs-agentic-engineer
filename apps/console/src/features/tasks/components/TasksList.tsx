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
  Alert,
  Box,
  Button,
  Chip,
  CircularProgress,
  IconButton,
  ListingTable,
  Tooltip,
  Typography,
} from "@wso2/oxygen-ui";
import { GitHub } from "@wso2/oxygen-ui-icons-react";
import { useNavigate } from "@tanstack/react-router";
import { useAllTasks } from "../api/queries";
import { UsageBreakdown } from "../../usage/components/UsageBreakdown";
import { formatTokens, formatUsd, totalTokens } from "../../usage/lib/format";
import { TaskStatusChip } from "./TaskStatusChip";

// The flat task list (#173): one row per task, status chip inline — the user
// watches chips go green. Card-variant listing per the components list /
// oxygen-ui sample precedent. `tag` scopes the list to one build's lineage
// (the builds page, #185); omitted = every version's tasks.
export function TasksList({
  projectName,
  tag,
}: {
  projectName: string;
  tag?: string;
}) {
  const tasks = useAllTasks(projectName, tag);
  const navigate = useNavigate();

  if (tasks.isPending) {
    return (
      <Box sx={{ display: "flex", justifyContent: "center", p: 6 }}>
        <CircularProgress aria-label="Loading tasks" />
      </Box>
    );
  }

  if (tasks.isError) {
    return (
      <Alert
        severity="error"
        action={<Button onClick={() => void tasks.refetch()}>Retry</Button>}
      >
        Failed to load tasks
        {tasks.error instanceof Error && tasks.error.message
          ? `: ${tasks.error.message}`
          : ""}
      </Alert>
    );
  }

  // The project's validation task is surfaced on the deployments board (its
  // status rides deploy.validation), not as a coding-task row here — it targets
  // the whole project, not a component.
  const visibleTasks = tasks.data.filter((t) => t.executorClass !== "validation");

  if (visibleTasks.length === 0) {
    return (
      <Typography variant="body2" color="text.secondary" sx={{ py: 3 }}>
        {tag
          ? `No tasks for ${tag} yet — the build plans its coding-agent tasks right after it starts.`
          : "No tasks yet — publish a design and start a build; coding-agent tasks show up here as the build plans them."}
      </Typography>
    );
  }

  return (
    <ListingTable.Container sx={{ width: "100%" }} disablePaper>
      <ListingTable variant="card" density="standard">
        <ListingTable.Head>
          <ListingTable.Row>
            <ListingTable.Cell>Task</ListingTable.Cell>
            <ListingTable.Cell sx={{ maxWidth: 160 }}>
              Component
            </ListingTable.Cell>
            <ListingTable.Cell sx={{ maxWidth: 120 }}>Status</ListingTable.Cell>
            <ListingTable.Cell sx={{ maxWidth: 64 }} aria-label="Links" />
          </ListingTable.Row>
        </ListingTable.Head>
        <ListingTable.Body>
          {visibleTasks.map((t) => {
            // Provision/config gates are platform-driven (approved in the Build
            // drawer, resolved out-of-band): there is no task page to open, so the
            // row is non-clickable — you watch its status and open the GitHub issue.
            // Its Component column shows "—" (a gate names a dependency, not a
            // component).
            const isGate = t.executorClass === "provision";
            return (
            <ListingTable.Row
              key={t.issueNumber}
              variant="card"
              hover={!isGate}
              onClick={
                isGate
                  ? undefined
                  : () =>
                      void navigate({
                        to: "/projects/$projectName/builds/$issueNumber",
                        params: { projectName, issueNumber: t.issueNumber },
                      })
              }
              sx={{ cursor: isGate ? "default" : "pointer" }}
            >
              <ListingTable.Cell>
                <ListingTable.CellIcon
                  icon={
                    <Typography
                      variant="caption"
                      color="text.secondary"
                      sx={{ fontVariantNumeric: "tabular-nums" }}
                    >
                      #{t.issueNumber}
                    </Typography>
                  }
                  primary={t.title}
                  // Per-task cost (#245): deliberately quiet — a small caption
                  // under the title, never a column of highlighted figures.
                  // USD only (tokens live in the hover breakdown); absent
                  // until an execution has run.
                  secondary={
                    t.usage && totalTokens(t.usage) > 0 ? (
                      <Tooltip title={<UsageBreakdown usage={t.usage} />}>
                        <Typography
                          component="span"
                          variant="caption"
                          color="text.secondary"
                          sx={{ fontVariantNumeric: "tabular-nums" }}
                        >
                          {t.usage.costUsd !== null
                            ? formatUsd(t.usage.costUsd)
                            : `${formatTokens(totalTokens(t.usage))} tok`}
                        </Typography>
                      </Tooltip>
                    ) : undefined
                  }
                />
              </ListingTable.Cell>
              <ListingTable.Cell sx={{ maxWidth: 160 }}>
                {isGate || !t.component ? (
                  <Typography variant="caption" color="text.secondary">
                    —
                  </Typography>
                ) : (
                  <Chip label={t.component} size="small" variant="outlined" />
                )}
              </ListingTable.Cell>
              <ListingTable.Cell sx={{ maxWidth: 120 }}>
                {t.derivedStatus === "on_hold" && t.blockedBy?.length ? (
                  <Tooltip title={`Waiting for ${t.blockedBy.join(", ")}`}>
                    {/* Box holds the ref Tooltip needs; hovering the pill shows the reason. */}
                    <Box sx={{ display: "inline-flex" }}>
                      <TaskStatusChip derivedStatus={t.derivedStatus} />
                    </Box>
                  </Tooltip>
                ) : (
                  <TaskStatusChip derivedStatus={t.derivedStatus} />
                )}
              </ListingTable.Cell>
              <ListingTable.Cell sx={{ maxWidth: 64 }}>
                <Tooltip title="Open the GitHub issue">
                  <IconButton
                    component="a"
                    href={t.issueUrl}
                    target="_blank"
                    rel="noreferrer"
                    aria-label={`GitHub issue #${t.issueNumber}`}
                    onClick={(e) => e.stopPropagation()}
                  >
                    <GitHub size={16} />
                  </IconButton>
                </Tooltip>
              </ListingTable.Cell>
            </ListingTable.Row>
            );
          })}
        </ListingTable.Body>
      </ListingTable>
    </ListingTable.Container>
  );
}
