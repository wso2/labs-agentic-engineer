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

import { useEffect, useRef } from "react";
import {
  Alert,
  Button,
  Skeleton,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import { useProjectComponents, useProjectStatus } from "../api/queries";
import { ComponentsList } from "./ComponentsList";
import { OverviewPipeline } from "./OverviewPipeline";

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

// The overview renders from ONE polling read (#183): the status aggregate
// powers the whole pipeline. The components list has no interval of its own —
// it refetches when the poll shows a build/deploy transition (the only times
// components change).
export function ProjectOverview({ projectName }: { projectName: string }) {
  const status = useProjectStatus(projectName);
  const componentsQuery = useProjectComponents(projectName);

  const buildState = status.data?.build.status;
  const deployState = status.data?.deploy.status;
  const prev = useRef<string | undefined>(undefined);
  const refetchComponents = componentsQuery.refetch;
  useEffect(() => {
    if (buildState === undefined) return;
    const key = `${buildState}:${deployState}`;
    if (prev.current !== undefined && prev.current !== key) {
      void refetchComponents();
    }
    prev.current = key;
  }, [buildState, deployState, refetchComponents]);

  return (
    <Stack spacing={4}>
      {status.isError ? (
        <SectionError
          what="project status"
          message={status.error instanceof Error ? status.error.message : undefined}
          onRetry={() => void status.refetch()}
        />
      ) : status.isPending ? (
        <Skeleton variant="rounded" height={96} />
      ) : (
        <OverviewPipeline projectName={projectName} status={status.data} />
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
