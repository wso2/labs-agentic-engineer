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
import { createLink } from "@tanstack/react-router";
import { Box, Button, IconButton, Link, ListItemIcon, ListItemText, Menu, MenuItem, Skeleton, Typography } from "@wso2/oxygen-ui";
import { EllipsisVertical, GitHub, Trash2 } from "@wso2/oxygen-ui-icons-react";
import { EmptyState } from "../../../components/EmptyState";
import { BuildButton } from "../../builds/components/BuildButton";
import { useProjectStatus } from "../../deploy/api/deploy";
import { ProjectFeatures } from "../../spec/components/ProjectFeatures";
import { projectLabel, useProject, type Project } from "../api/queries";
import { repoLabel } from "../repo";
import { DeleteProjectPanel } from "./DeleteProjectPanel";
import { OverviewArchitecture } from "./OverviewArchitecture";
import { OverviewComponents } from "./OverviewComponents";
import { Track } from "./Track";

const ButtonLink = createLink(Button);

/** The overview's menu: what is done to the project as a whole (today, delete it). */
function ProjectMenu({ project, repoUrl }: { project: Project; repoUrl: string | undefined }) {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const [deleting, setDeleting] = useState(false);
  return (
    <>
      <IconButton aria-label="Project actions" size="small" onClick={(e) => setAnchor(e.currentTarget)}>
        <EllipsisVertical size={18} />
      </IconButton>
      <Menu anchorEl={anchor} open={anchor !== null} onClose={() => setAnchor(null)}>
        <MenuItem
          onClick={() => {
            setAnchor(null);
            setDeleting(true);
          }}
        >
          <ListItemIcon>
            <Trash2 size={16} />
          </ListItemIcon>
          <ListItemText>Delete project</ListItemText>
        </MenuItem>
      </Menu>
      {deleting && <DeleteProjectPanel project={project} repoUrl={repoUrl} onClose={() => setDeleting(false)} />}
    </>
  );
}

function SectionHeading({ id, children }: { id: string; children: string }) {
  return (
    <Typography id={id} component="h2" sx={{ fontSize: "0.8125rem", fontWeight: 600, mb: 1 }}>
      {children}
    </Typography>
  );
}

/**
 * A project's base page: its name and repository (with Build v1 once
 * something is designed, and the project's menu), the track, the features
 * (the spec workspace's rows), the architecture and the components, each of
 * which can be tried. Cards open over it.
 */
export function ProjectOverview({ projectName }: { projectName: string }) {
  const project = useProject(projectName);
  // The repository comes with the project's status (the track reads it too),
  // as it did in the classic console: the project itself does not carry it.
  const status = useProjectStatus(projectName);
  const repoUrl = status.data?.repoUrl;

  if (project.isError) {
    return (
      <EmptyState
        title="Couldn't open this project"
        description={project.error.message}
        action={
          <Box sx={{ display: "flex", gap: 1, justifyContent: "center" }}>
            <Button variant="outlined" onClick={() => void project.refetch()}>
              Try again
            </Button>
            <ButtonLink to="/projects" variant="text">
              All projects
            </ButtonLink>
          </Box>
        }
      />
    );
  }

  const repo = repoLabel(repoUrl);
  return (
    <>
      <Box sx={{ mb: 2.75, display: "flex", alignItems: "flex-start", gap: 2 }}>
        <Box sx={{ flex: 1, minWidth: 0 }}>
          <Typography component="h1" variant="h4" sx={{ fontWeight: 600 }}>
            {project.data ? projectLabel(project.data) : <Skeleton width={220} />}
          </Typography>
          {status.isPending ? (
            <Skeleton width={260} />
          ) : (
            repo && (
              <Link
                href={repo.href}
                target="_blank"
                rel="noreferrer"
                variant="caption"
                color="text.secondary"
                underline="hover"
                sx={{ display: "inline-flex", alignItems: "center", gap: 0.5, fontFamily: "monospace" }}
              >
                <GitHub size={14} aria-hidden />
                {repo.short}
              </Link>
            )
          )}
        </Box>
        <BuildButton projectName={projectName} />
        {project.data && <ProjectMenu project={project.data} repoUrl={repoUrl} />}
      </Box>
      <Track projectName={projectName} />
      <Box sx={{ display: "flex", flexDirection: "column", gap: 3.5 }}>
        <Box component="section" aria-labelledby="features-heading">
          <SectionHeading id="features-heading">Features</SectionHeading>
          <ProjectFeatures projectName={projectName} />
        </Box>
        <OverviewArchitecture projectName={projectName} />
        <Box component="section" aria-labelledby="components-heading">
          <SectionHeading id="components-heading">Components</SectionHeading>
          <OverviewComponents projectName={projectName} />
        </Box>
      </Box>
    </>
  );
}
