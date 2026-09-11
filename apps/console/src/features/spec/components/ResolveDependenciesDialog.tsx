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

/**
 * ResolveDependenciesDialog — the answer Build gives when a dependency has no
 * provider, no interface on file, or names an org-service this project cannot
 * see (ADR-0029).
 *
 * It LISTS. One row per dependency, a name and who waits on it, and one action
 * — **Resolve** — which runs the guided flow over all of them at once. There is
 * no per-row button: a second way in is a second thing to learn, and the rail
 * already reaches every definition for a user who would rather do one by hand.
 */

import {
  Box,
  Button,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import type { BlockingDependency } from "../lib/blockingDependencies";

/** The scroll bound: about six rows, then the list scrolls under a fixed frame. */
const LIST_MAX_HEIGHT = 320;

export function ResolveDependenciesDialog({
  open,
  version,
  dependencies,
  onClose,
  onResolve,
}: {
  open: boolean;
  /** The version this build would have cut — what the wait is costing. */
  version: string;
  dependencies: BlockingDependency[];
  onClose: () => void;
  /** Runs the guided flow over every listed dependency. */
  onResolve: () => void;
}) {
  return (
    <Dialog
      data-testid="resolve-dependencies-dialog"
      open={open}
      onClose={onClose}
      maxWidth="xs"
      fullWidth
    >
      <DialogTitle>Resolve dependencies</DialogTitle>
      <DialogContent>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          {version
            ? `These need a provider and an interface before ${version} can be cut.`
            : "These need a provider and an interface before a version can be cut."}
        </Typography>
        <Box sx={{ maxHeight: LIST_MAX_HEIGHT, overflowY: "auto" }}>
          <Stack spacing={1.5}>
            {dependencies.map((dep) => (
              <Stack key={dep.name} spacing={0.5}>
                <Typography variant="subtitle2">{dep.name}</Typography>
                <Typography variant="body2" color="text.secondary">
                  {dep.description}
                </Typography>
                {dep.usedBy.length > 1 && (
                  <Stack
                    direction="row"
                    spacing={0.5}
                    alignItems="center"
                    flexWrap="wrap"
                  >
                    <Typography variant="caption" color="text.secondary">
                      Used by:
                    </Typography>
                    {dep.usedBy.map((component) => (
                      <Chip
                        key={component}
                        size="small"
                        variant="outlined"
                        label={component}
                      />
                    ))}
                  </Stack>
                )}
              </Stack>
            ))}
          </Stack>
        </Box>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button variant="contained" onClick={onResolve}>
          Resolve
        </Button>
      </DialogActions>
    </Dialog>
  );
}
