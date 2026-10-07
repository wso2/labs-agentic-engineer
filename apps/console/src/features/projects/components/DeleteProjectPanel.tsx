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

import { useNavigate } from "@tanstack/react-router";
import { Alert, Button, Dialog, DialogActions, DialogContent, DialogContentText, DialogTitle } from "@wso2/oxygen-ui";
import { projectLabel, useDeleteProject, type Project } from "../api/queries";
import { repoLabel } from "../repo";

// Delete project, a Panel opened from the overview's menu, after the old
// console's DeleteProjectDialog. It asks first and says what the delete
// does: the platform drops the project, its deployments and its build
// history, and the GitHub repository is KEPT (aep-api's DeleteProject leaves
// the remote standing). It names that repository, because it keeps its name
// taken: a new project cannot be created under it while it exists.

export function DeleteProjectPanel({
  project,
  repoUrl,
  onClose,
}: {
  project: Project;
  repoUrl: string | undefined;
  onClose: () => void;
}) {
  const remove = useDeleteProject();
  const navigate = useNavigate();
  const repo = repoLabel(repoUrl);
  const name = projectLabel(project);
  const busy = remove.isPending;

  const close = () => {
    if (!busy) onClose();
  };

  return (
    <Dialog open onClose={close} maxWidth="xs" fullWidth>
      <DialogTitle>Delete {name}?</DialogTitle>
      <DialogContent>
        <DialogContentText>This deletes the project, its deployments and its build history. It cannot be undone.</DialogContentText>
        <DialogContentText sx={{ mt: 2 }}>
          {repo ? (
            <>
              The GitHub repository <strong>{repo.short}</strong> is kept, with the spec, the code and the issues. Delete
              it on GitHub to free its name: a new project cannot use it while it exists.
            </>
          ) : (
            "The project's GitHub repository is kept, with the spec, the code and the issues."
          )}
        </DialogContentText>
        {remove.isError && (
          <Alert severity="error" sx={{ mt: 2 }}>
            {remove.error.message}
          </Alert>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={close} disabled={busy}>
          Cancel
        </Button>
        <Button
          variant="contained"
          color="error"
          disabled={busy}
          onClick={() => remove.mutate(project.name, { onSuccess: () => void navigate({ to: "/projects" }) })}
        >
          {busy ? "Deleting…" : "Delete project"}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
