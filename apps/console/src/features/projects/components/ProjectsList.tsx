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
import {
  Alert,
  Box,
  Button,
  Card,
  CardActionArea,
  CardContent,
  CircularProgress,
  Grid,
  IconButton,
  ListItemIcon,
  ListItemText,
  Menu,
  MenuItem,
  PageContent,
  SearchBar,
  Typography,
  useMediaQuery,
  useTheme,
} from "@wso2/oxygen-ui";
import {
  EllipsisVertical,
  Folder,
  Plus,
  Trash2,
} from "@wso2/oxygen-ui-icons-react";
import { Link, useNavigate } from "@tanstack/react-router";
import { EmptyState } from "../../../components/EmptyState";
import { PageHeader } from "../../../components/PageHeader";
import type { components } from "../../../generated/aep-api";
import { useProjectsList } from "../api/queries";
import { useDebouncedValue } from "../hooks/useDebouncedValue";
import { DeleteProjectDialog } from "./DeleteProjectDialog";

type Project = components["schemas"]["Project"];

function formatCreatedAt(createdAt?: string): string | null {
  if (!createdAt) return null;
  const date = new Date(createdAt);
  if (Number.isNaN(date.getTime())) return null;
  return `Created ${date.toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
  })}`;
}

function ProjectCard({
  project,
  onDelete,
}: {
  project: Project;
  onDelete: (project: Project) => void;
}) {
  const navigate = useNavigate();
  const created = formatCreatedAt(project.createdAt);
  // Overflow (⋮) menu anchor — a sibling of CardActionArea (never nested
  // inside it: button-in-button), absolutely positioned over the corner.
  const [menuAnchor, setMenuAnchor] = useState<HTMLElement | null>(null);

  return (
    <Card variant="outlined" sx={{ height: "100%", position: "relative" }}>
      <CardActionArea
        sx={{ height: "100%", alignItems: "stretch" }}
        onClick={() =>
          void navigate({
            to: "/projects/$projectName",
            params: { projectName: project.name },
          })
        }
      >
        <CardContent
          sx={{ display: "flex", flexDirection: "column", height: "100%" }}
        >
          <Typography variant="h6" gutterBottom sx={{ pr: 4 }}>
            {project.displayName ?? project.name}
          </Typography>
          <Typography
            variant="body2"
            color="text.secondary"
            sx={{
              flexGrow: 1,
              display: "-webkit-box",
              WebkitLineClamp: 2,
              WebkitBoxOrient: "vertical",
              overflow: "hidden",
            }}
          >
            {project.description}
          </Typography>
          {created && (
            <Typography variant="caption" color="text.secondary" sx={{ mt: 1.5 }}>
              {created}
            </Typography>
          )}
        </CardContent>
      </CardActionArea>
      <IconButton
        aria-label={`Actions for ${project.displayName ?? project.name}`}
        size="small"
        onClick={(e) => setMenuAnchor(e.currentTarget)}
        sx={{ position: "absolute", top: 8, right: 8 }}
      >
        <EllipsisVertical size={18} />
      </IconButton>
      <Menu
        anchorEl={menuAnchor}
        open={menuAnchor !== null}
        onClose={() => setMenuAnchor(null)}
      >
        <MenuItem
          onClick={() => {
            setMenuAnchor(null);
            onDelete(project);
          }}
        >
          <ListItemIcon>
            <Trash2 size={18} />
          </ListItemIcon>
          <ListItemText>Delete project</ListItemText>
        </MenuItem>
      </Menu>
    </Card>
  );
}

// Grid columns per breakpoint — must mirror the card Grid size props below.
function useGridColumns(): number {
  const theme = useTheme();
  const lg = useMediaQuery(theme.breakpoints.up("lg"));
  const md = useMediaQuery(theme.breakpoints.up("md"));
  const sm = useMediaQuery(theme.breakpoints.up("sm"));
  if (lg) return 4;
  if (md) return 3;
  if (sm) return 2;
  return 1;
}

// Rows requested per page: the page size is columns × rows, so View more
// only ever appears under a completely filled last row (#71 feedback).
const GRID_ROWS_PER_PAGE = 3;

export function ProjectsList() {
  const [search, setSearch] = useState("");
  // The project awaiting delete confirmation; one dialog serves the grid.
  const [deleteTarget, setDeleteTarget] = useState<Project | null>(null);
  const debouncedSearch = useDebouncedValue(search.trim());
  const columns = useGridColumns();
  const {
    data,
    isPending,
    isError,
    error,
    refetch,
    hasNextPage,
    fetchNextPage,
    isFetchingNextPage,
  } = useProjectsList(debouncedSearch, columns * GRID_ROWS_PER_PAGE);

  const items = data?.pages.flatMap((page) => page.items ?? []) ?? [];
  // True-empty (no projects at all, not a fruitless search) hides the page
  // action and centers the create button instead — issue #71 decision.
  const isTrueEmpty = !isPending && !isError && items.length === 0 && !debouncedSearch;

  return (
    <PageContent>
      <PageHeader
        title="Projects"
        subtitle="Everything Agentic Engineer is building for you, one project per app."
        {...(!isTrueEmpty && {
          actions: (
            <Button
              variant="contained"
              startIcon={<Plus size={20} />}
              component={Link}
              to="/projects/new"
            >
              Create project
            </Button>
          ),
        })}
      />

      {isPending ? (
        <Box sx={{ display: "flex", justifyContent: "center", p: 6 }}>
          <CircularProgress aria-label="Loading projects" />
        </Box>
      ) : isError ? (
        <Alert
          severity="error"
          action={<Button onClick={() => void refetch()}>Retry</Button>}
        >
          Failed to load projects
          {error instanceof Error && error.message ? `: ${error.message}` : ""}
        </Alert>
      ) : isTrueEmpty ? (
        <EmptyState
          icon={<Folder size={48} />}
          title="No projects yet"
          description="Tell Agentic Engineer what you want to build and it becomes your first project."
          action={
            <Button
              variant="contained"
              startIcon={<Plus size={20} />}
              component={Link}
              to="/projects/new"
            >
              Create project
            </Button>
          }
        />
      ) : (
        <>
          <Box sx={{ maxWidth: 420, mb: 3 }}>
            <SearchBar
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search projects..."
            />
          </Box>
          {items.length === 0 ? (
            <Typography variant="body2" color="text.secondary" sx={{ py: 4 }}>
              No projects match “{debouncedSearch}”.
            </Typography>
          ) : (
            <>
              <Grid container spacing={3}>
                {items.map((project) => (
                  <Grid key={project.name} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
                    <ProjectCard project={project} onDelete={setDeleteTarget} />
                  </Grid>
                ))}
              </Grid>
              {hasNextPage && (
                <Box sx={{ display: "flex", justifyContent: "center", mt: 4 }}>
                  <Button
                    variant="outlined"
                    onClick={() => void fetchNextPage()}
                    loading={isFetchingNextPage}
                  >
                    View more
                  </Button>
                </Box>
              )}
            </>
          )}
        </>
      )}
      <DeleteProjectDialog
        project={deleteTarget}
        onClose={() => setDeleteTarget(null)}
      />
    </PageContent>
  );
}
