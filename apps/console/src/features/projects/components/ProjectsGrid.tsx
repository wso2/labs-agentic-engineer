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
import {
  Box,
  Button,
  Card,
  CardActionArea,
  CircularProgress,
  SearchBar,
  Typography,
} from "@wso2/oxygen-ui";
import { FolderOpen, Plus } from "@wso2/oxygen-ui-icons-react";
import { useSession } from "../../../auth/SessionContext";
import { EmptyState } from "../../../components/EmptyState";
import { useDebouncedValue } from "../../../lib/useDebouncedValue";
import { BasePage } from "../../shell/components/BasePage";
import { projectLabel, useProjectPages, type Project } from "../api/queries";
import { GRID_PAGE_SIZE, gridView, type GridView } from "../model/grid";
import { repoLabel } from "../repo";

const CardLink = createLink(CardActionArea);
const ButtonLink = createLink(Button);

function NewProjectButton() {
  return (
    <ButtonLink to="/projects/new" variant="contained" startIcon={<Plus size={16} />}>
      New project
    </ButtonLink>
  );
}

function ProjectCard({ project }: { project: Project }) {
  const repo = repoLabel(project.repoUrl);
  return (
    <Card
      variant="outlined"
      sx={{
        borderRadius: 3,
        transition: (t) => t.transitions.create("border-color"),
        "&:hover": { borderColor: "primary.main" },
      }}
    >
      <CardLink
        to="/projects/$projectName"
        params={{ projectName: project.name }}
        sx={{ p: 2, minHeight: 140, display: "flex", flexDirection: "column", alignItems: "stretch", justifyContent: "flex-start", gap: 1.25 }}
      >
        <Box>
          <Typography sx={{ fontWeight: 600, fontSize: "0.9375rem" }}>{projectLabel(project)}</Typography>
          {repo && (
            <Typography variant="caption" color="text.secondary" sx={{ fontFamily: "monospace" }}>
              {repo.short}
            </Typography>
          )}
        </Box>
        {project.description && (
          <Typography variant="body2" color="text.secondary">
            {project.description}
          </Typography>
        )}
      </CardLink>
    </Card>
  );
}

function Grid({ view, loadingMore, onMore }: { view: Extract<GridView, { kind: "list" }>; loadingMore: boolean; onMore: () => void }) {
  return (
    <>
      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: "repeat(auto-fill, minmax(260px, 1fr))",
          gap: 1.75,
        }}
      >
        {view.projects.map((p) => (
          <ProjectCard key={p.name} project={p} />
        ))}
      </Box>
      {view.more && (
        <Box sx={{ display: "flex", justifyContent: "center", mt: 3 }}>
          <Button variant="outlined" onClick={onMore} disabled={loadingMore}>
            {loadingMore ? "Loading…" : "View more"}
          </Button>
        </Box>
      )}
    </>
  );
}

/**
 * The org's Projects Page: every project as a card, a page at a time (View
 * more), narrowed by a search on the name; a card opens its overview.
 */
export function ProjectsGrid() {
  const { orgHandle } = useSession();
  const [typed, setTyped] = useState("");
  const search = useDebouncedValue(typed.trim());
  const pages = useProjectPages(search, GRID_PAGE_SIZE);
  const view = pages.data ? gridView(pages.data.pages, search, pages.hasNextPage) : null;
  const count = view?.kind === "list" ? view.count : view?.kind === "empty" ? 0 : null;

  let body;
  if (pages.isError && !pages.data) {
    body = (
      <EmptyState
        title="Couldn't load projects"
        description={pages.error.message}
        action={
          <Button variant="outlined" onClick={() => void pages.refetch()}>
            Try again
          </Button>
        }
      />
    );
  } else if (!view) {
    body = (
      <Box sx={{ display: "grid", placeItems: "center", py: 8 }}>
        <CircularProgress size={24} aria-label="Loading projects" />
      </Box>
    );
  } else if (view.kind === "empty") {
    body = (
      <EmptyState
        icon={<FolderOpen size={48} />}
        title="No projects yet"
        description="Describe what you want to build, and the agent guides it from there."
        action={<NewProjectButton />}
      />
    );
  } else if (view.kind === "no-match") {
    body = (
      <Typography variant="body2" color="text.secondary" sx={{ py: 4 }}>
        No projects match &ldquo;{view.search}&rdquo;.
      </Typography>
    );
  } else {
    body = <Grid view={view} loadingMore={pages.isFetchingNextPage} onMore={() => void pages.fetchNextPage()} />;
  }

  return (
    <BasePage>
      <Box sx={{ display: "flex", alignItems: "flex-start", gap: 2, mb: 2.75 }}>
        <Box sx={{ flex: 1, minWidth: 0 }}>
          <Typography component="h1" variant="h4" sx={{ fontWeight: 600 }}>
            Projects
          </Typography>
          <Typography variant="body2" color="text.secondary">
            {orgHandle ?? "Your organization"}
            {count !== null && ` · ${count === 1 ? "1 project" : `${count} projects`}`}
          </Typography>
        </Box>
        {view?.kind !== "empty" && <NewProjectButton />}
      </Box>
      {view?.kind !== "empty" && (
        <Box sx={{ maxWidth: 420, mb: 2.5 }}>
          <SearchBar
            size="small"
            fullWidth
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            placeholder="Search projects"
            slotProps={{ htmlInput: { "aria-label": "Search projects" } }}
          />
        </Box>
      )}
      {body}
    </BasePage>
  );
}
