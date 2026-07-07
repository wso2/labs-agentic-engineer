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
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
} from "@wso2/oxygen-ui";
import type { components } from "../../../generated/aep-api";
import { useDeleteProject } from "../api/queries";

type Project = components["schemas"]["Project"];

// "https://github.com/acme/shop.git" → "acme/shop", for the warning copy.
// Anything unparseable falls back to the raw URL — still better than silence.
function repoDisplayName(repoUrl: string): string {
  const match = /github\.com[/:]([^/]+\/[^/]+?)(?:\.git)?$/.exec(repoUrl);
  return match?.[1] ?? repoUrl;
}

interface DeleteProjectDialogProps {
  // The project being confirmed for deletion; null keeps the dialog closed.
  project: Project | null;
  onClose: () => void;
}

// Confirmation dialog for deleting a project from the listing (#107).
// Names the GitHub repository that the cascade destroys when the list
// payload carries one; projects without a repo get generic copy.
export function DeleteProjectDialog({
  project,
  onClose,
}: DeleteProjectDialogProps) {
  const deleteProject = useDeleteProject();
  const busy = deleteProject.isPending;

  const close = () => {
    if (busy) return;
    deleteProject.reset();
    onClose();
  };

  const confirm = () => {
    if (!project) return;
    deleteProject.mutate(project.name, { onSuccess: onClose });
  };

  const displayName = project?.displayName ?? project?.name;

  return (
    <Dialog open={project !== null} onClose={close} maxWidth="xs" fullWidth>
      <DialogTitle>Delete {displayName}?</DialogTitle>
      <DialogContent>
        <DialogContentText>
          This permanently deletes the project, its deployments, and{" "}
          {project?.repoUrl ? (
            <>
              the GitHub repository <strong>{repoDisplayName(project.repoUrl)}</strong>
            </>
          ) : (
            "its GitHub repository"
          )}{" "}
          — including all specs and code.
        </DialogContentText>
        {deleteProject.isError && (
          <Alert severity="error" sx={{ mt: 2 }}>
            {deleteProject.error instanceof Error && deleteProject.error.message
              ? deleteProject.error.message
              : "Failed to delete project"}
          </Alert>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={close} disabled={busy}>
          Cancel
        </Button>
        <Button
          onClick={confirm}
          variant="contained"
          color="error"
          loading={busy}
        >
          Delete project
        </Button>
      </DialogActions>
    </Dialog>
  );
}
