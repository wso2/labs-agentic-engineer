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
  Avatar,
  Box,
  Button,
  Grid,
  Link as MuiLink,
  Skeleton,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import { Link as LinkIcon } from "@wso2/oxygen-ui-icons-react";
import { Link } from "@tanstack/react-router";
import { PageHeader } from "../../../components/PageHeader";
import { SectionTitle } from "../../../components/SectionTitle";
import { StatusChip } from "../../../components/StatusChip";
import { useProject, useProjectComponents, useProjectStatus } from "../api/queries";
import { projectChip } from "../lib/projectChip";
import { RecentActivity } from "./RecentActivity";
import { ComponentsList } from "./ComponentsList";
import { OverviewPipeline } from "./OverviewPipeline";
import { OverviewDependencies } from "./OverviewDependencies";

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
  const project = useProject(projectName);
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

  const displayName = project.data?.displayName ?? project.data?.name ?? projectName;
  const initial = (displayName.trim()[0] ?? "P").toUpperCase();

  return (
    <>
      {/* The project identity (Overview-only per Task 5; other sub-pages drop
          it as redundant with the project switcher): a rounded-square avatar
          leads a two-line column — title + phase chip on top, the GitHub repo
          link indented directly beneath the title. No description subtitle —
          that belongs on the project cards. */}
      <PageHeader
        title={
          <Stack direction="row" spacing={2} sx={{ alignItems: "center" }}>
            <Avatar
              variant="rounded"
              sx={{
                bgcolor: "primary.main",
                color: "primary.contrastText",
                width: 52,
                height: 52,
                fontSize: "1.5rem",
              }}
            >
              {initial}
            </Avatar>
            <Box>
              <Stack direction="row" spacing={1.5} sx={{ alignItems: "center" }}>
                <Typography variant="h4" component="span">
                  {displayName}
                </Typography>
                {status.data && (
                  <StatusChip {...projectChip(status.data)} appearance="soft" dot />
                )}
              </Stack>
              {status.data?.repoUrl && (
                <MuiLink
                  href={status.data.repoUrl}
                  target="_blank"
                  rel="noreferrer"
                  variant="body2"
                  sx={{
                    display: "inline-flex",
                    alignItems: "center",
                    gap: 0.5,
                    mt: 0.5,
                  }}
                >
                  <LinkIcon size={14} />
                  {status.data.repoUrl.replace(/^https?:\/\/(www\.)?/, "")}
                </MuiLink>
              )}
            </Box>
          </Stack>
        }
        backTo={{ link: <Link to="/" />, label: "Back to Projects" }}
      />
      <Stack spacing={3} sx={{ mt: 3 }}>
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

        {/* Two-column body: the agent-activity feed (what the agents have
            done) beside the component cards (what they're building). */}
        <Grid container spacing={4}>
          <Grid size={{ xs: 12, md: 6 }}>
            <RecentActivity projectName={projectName} />
          </Grid>
          <Grid size={{ xs: 12, md: 6 }}>
            <SectionTitle>Components</SectionTitle>
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
            <OverviewDependencies projectName={projectName} />
          </Grid>
        </Grid>
      </Stack>
    </>
  );
}
