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
  Button,
  Grid,
  Skeleton,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import {
  useProjectComponents,
  useProjectStatus,
  useProjectTags,
  useProjectTasks,
} from "../api/queries";
import { ComponentsList } from "./ComponentsList";
import { StatusCards } from "./StatusCards";

function SectionError({
  what,
  message,
  onRetry,
}: {
  what: string;
  message?: string | undefined;
  onRetry: () => void;
}) {
  return (
    <Alert severity="error" action={<Button onClick={onRetry}>Retry</Button>}>
      Failed to load {what}
      {message ? `: ${message}` : ""}
    </Alert>
  );
}

// The overview never blocks entirely on one failed read: the cards and the
// components list each degrade independently (issue #77).
export function ProjectOverview({ projectName }: { projectName: string }) {
  const status = useProjectStatus(projectName);
  const tasks = useProjectTasks(projectName);
  const tags = useProjectTags(projectName);
  const componentsQuery = useProjectComponents(projectName);

  return (
    <Stack spacing={4}>
      {status.isError ? (
        <SectionError
          what="project status"
          message={status.error instanceof Error ? status.error.message : undefined}
          onRetry={() => void status.refetch()}
        />
      ) : (
        <Grid container spacing={3}>
          <StatusCards
            projectName={projectName}
            status={status.data}
            tasks={tasks.data ?? undefined}
            tags={tags.data}
          />
        </Grid>
      )}

      <div>
        <Typography variant="h6" gutterBottom>
          Components
        </Typography>
        {componentsQuery.isError ? (
          <SectionError
            what="components"
            message={
              componentsQuery.error instanceof Error
                ? componentsQuery.error.message
                : undefined
            }
            onRetry={() => void componentsQuery.refetch()}
          />
        ) : componentsQuery.isPending ? (
          <Skeleton variant="rounded" height={120} />
        ) : (
          <ComponentsList
            projectName={projectName}
            items={componentsQuery.data.items ?? []}
          />
        )}
      </div>
    </Stack>
  );
}
