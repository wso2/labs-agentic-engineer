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

import { Fragment, useState } from "react";
import { Alert, Box, Button, Skeleton, Typography } from "@wso2/oxygen-ui";
import { ArrowRight } from "@wso2/oxygen-ui-icons-react";
import { EmptyState } from "../../../components/EmptyState";
import { projectLabel, useProject } from "../../projects/api/queries";
import { PHONE } from "../../shell/layout";
import type { EnvironmentColumn } from "../model/pipeline";
import { promoteView } from "../model/promote";
import { useDeployBoard } from "../useDeployBoard";
import { DeployHistory } from "./DeployHistory";
import { EnvironmentCard } from "./EnvironmentCard";
import { TryItPanel } from "./TryItPanel";

/**
 * A project's Deploy Page: the pipeline board, the project's environments
 * side by side in promotion order, each with what runs there, its
 * dependencies' values, Try it, Configure and Promote; then the history of
 * what was deployed where. Each environment's Configure card opens over it.
 */
export function DeployPage({ projectName }: { projectName: string }) {
  const project = useProject(projectName);
  const board = useDeployBoard(projectName);
  // Try it is a Panel: no address, and the chat stays as it was.
  // By name, so the panel follows the board as it refreshes.
  const [tryIn, setTryIn] = useState<string | null>(null);
  const tryColumn = board.columns?.find((c) => c.name === tryIn);

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 3 }}>
      <Box>
        <Typography variant="caption" color="text.secondary">
          {project.data ? projectLabel(project.data) : projectName}
        </Typography>
        <Typography component="h1" variant="h4" sx={{ fontWeight: 600 }}>
          Deploy
        </Typography>
      </Box>
      {board.error ? (
        <Alert
          severity="error"
          action={
            <Button color="inherit" size="small" onClick={board.retry}>
              Try again
            </Button>
          }
        >
          {board.error}
        </Alert>
      ) : !board.columns ? (
        <BoardSkeleton />
      ) : board.columns.length === 0 ? (
        <EmptyState
          compact
          bordered
          description="This organization has no deployment environments yet, so there is nowhere to deploy to."
        />
      ) : (
        <>
          {board.unreadComponents > 0 && (
            <Alert severity="warning">
              The deployments of {board.unreadComponents} component{board.unreadComponents === 1 ? "" : "s"} could not
              be read; the board shows the rest.
            </Alert>
          )}
          <Board columns={board.columns} projectName={projectName} onTry={(c) => setTryIn(c.name)} />
          <DeployHistory rows={board.history} />
        </>
      )}
      {tryColumn && (
        <TryItPanel
          projectName={projectName}
          column={tryColumn}
          entryLabel={board.columns?.[0]?.label ?? ""}
          onClose={() => setTryIn(null)}
        />
      )}
    </Box>
  );
}

function Board({
  columns,
  projectName,
  onTry,
}: {
  columns: EnvironmentColumn[];
  projectName: string;
  onTry: (column: EnvironmentColumn) => void;
}) {
  const entry = columns[0];
  return (
    <Box component="section" aria-label="Pipeline" sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
      {entry && (
        <Typography variant="body2" color="text.secondary">
          A build deploys to {entry.label} by itself. Each environment promotes what it runs to the next.
        </Typography>
      )}
      <Box sx={{ display: "flex", alignItems: "stretch", gap: 1, [PHONE]: { flexDirection: "column" } }}>
        {columns.map((column) => (
          <Fragment key={column.name}>
            <EnvironmentCard
              projectName={projectName}
              column={column}
              promote={promoteView(column, columns.find((c) => c.name === column.next?.name))}
              onTry={() => onTry(column)}
            />
            {column.next && (
              <Box
                aria-hidden
                sx={{
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "center",
                  color: "text.secondary",
                  flexShrink: 0,
                  [PHONE]: { transform: "rotate(90deg)" },
                }}
              >
                <ArrowRight size={20} />
              </Box>
            )}
          </Fragment>
        ))}
      </Box>
    </Box>
  );
}

function BoardSkeleton() {
  return (
    <Box sx={{ display: "flex", gap: 2, [PHONE]: { flexDirection: "column" } }}>
      {[1, 2, 3].map((n) => (
        <Skeleton key={n} variant="rounded" height={260} sx={{ flex: 1 }} />
      ))}
    </Box>
  );
}
