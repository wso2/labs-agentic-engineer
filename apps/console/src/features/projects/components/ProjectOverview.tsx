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
  Link as MuiLink,
  Skeleton,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import { Link as LinkIcon } from "@wso2/oxygen-ui-icons-react";
import { Link } from "@tanstack/react-router";
import { PageHeader } from "../../../components/PageHeader";
import { SectionTitle } from "../../../components/SectionTitle";
import { useProject, useProjectComponents, useProjectStatus } from "../api/queries";
import { ComponentsList } from "./ComponentsList";
import { OverviewTrack } from "./OverviewTrack";
import { OverviewArchitecture, OVERVIEW_SPLIT_PX } from "./OverviewArchitecture";
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
// powers the whole track. The components list has no interval of its own —
// it refetches when the poll shows a build/deploy transition (the only times
// components change). The architecture diagram adds no interval either: it is
// a one-shot read of the committed design.cell, which only a design turn
// changes, and a design turn is something the user watched happen in the spec
// view.
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
              {/* A block, not the inline span it was while a chip sat beside
                  it in a row Stack — inline, the repo link below reflowed onto
                  the same line as the name. */}
              <Typography variant="h4" component="div">
                {displayName}
              </Typography>
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
          <OverviewTrack projectName={projectName} status={status.data} />
        )}

      {/* Index left, architecture right: the lists are the half you click
          (a component opens its contract, a dependency opens the catalog),
          and the diagram is the half you read.

          A project with nothing in it gets this same body, not a substitute.
          An earlier pass swapped it for an explainer of the three stages, on
          the theory that three panels each saying "nothing here yet" is worse
          than one page that teaches. It is not: each panel's empty state
          already names what belongs there and when it turns up, so the
          explainer was a fourth surface teaching the same thing in worse
          words, and it hid the shape of the page from the one reader who has
          never seen it.

          THE SPLIT IS A CONTAINER QUERY, NOT A BREAKPOINT. The two halves are
          not equally squeezable: the lists reflow to any width, while the
          diagram is a fixed-aspect drawing that can only scale down, and below
          about 600px of column it stops shrinking and starts being CROPPED
          (`DIAGRAM_MIN_HEIGHT` explains the clamp). A viewport breakpoint gets
          this wrong every time, because the viewport is not what the page gets:
          the nav rail takes ~280px of it, and the agent chat panel can take
          half of what is left. At `md` the diagram was cropped on a 1440px
          screen — a picture you cannot read sitting next to a list you can,
          which is the worst of both halves.

          Measured against the page's own width instead, the two columns appear
          exactly when there is room for both and not a pixel sooner. Under the
          threshold the diagram goes below the lists at FULL width, where it
          gets more room than the split ever gave it. */}
        <Box sx={{ containerType: "inline-size" }}>
          <Box
            sx={{
              display: "grid",
              gap: 4,
              gridTemplateColumns: "1fr",
              [`@container (min-width: ${OVERVIEW_SPLIT_PX}px)`]: {
                gridTemplateColumns: "5fr 7fr",
              },
            }}
          >
            <Box sx={{ minWidth: 0 }}>
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
            </Box>
            <Box sx={{ minWidth: 0 }}>
              <OverviewArchitecture projectName={projectName} />
            </Box>
          </Box>
        </Box>
      </Stack>
    </>
  );
}
