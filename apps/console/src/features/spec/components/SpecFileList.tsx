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
  Box,
  IconButton,
  List,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  Tooltip,
  Typography,
} from "@wso2/oxygen-ui";
import { FileText, Plus } from "@wso2/oxygen-ui-icons-react";
import type { SpecFileEntry, SpecGroup } from "../api/mapping";

const GROUPS: { id: SpecGroup; title: string }[] = [
  { id: "requirements", title: "Requirements" },
  { id: "designs", title: "Designs" },
  { id: "validation", title: "Validation" },
];

function basename(path: string): string {
  return path.split("/").at(-1) ?? path;
}

export function SpecFileList({
  files,
  selectedPath,
  onSelect,
  onAddArtifact,
  deriving,
  failed,
}: {
  files: SpecFileEntry[];
  selectedPath: string | null;
  onSelect: (path: string) => void;
  onAddArtifact: () => void;
  /** Agents are still shaping the spec — empty groups say so. */
  deriving: boolean;
  /** Derivation failed — empty groups say that instead. */
  failed: boolean;
}) {
  return (
    <Box component="nav" aria-label="Spec files" sx={{ py: 1 }}>
      {GROUPS.map((group) => {
        const groupFiles = files.filter((f) => f.group === group.id);
        return (
          <Box key={group.id} sx={{ mb: 1 }}>
            <Box
              sx={{
                px: 2,
                py: 0.5,
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
              }}
            >
              <Typography variant="overline" color="text.secondary">
                {group.title}
              </Typography>
              {group.id === "requirements" && (
                <Tooltip title="Add requirement artifact">
                  <IconButton
                    size="small"
                    aria-label="Add requirement artifact"
                    onClick={onAddArtifact}
                  >
                    <Plus size={16} />
                  </IconButton>
                </Tooltip>
              )}
            </Box>
            {groupFiles.length > 0 ? (
              <List dense disablePadding>
                {groupFiles.map((file) => (
                  <ListItemButton
                    key={file.path}
                    selected={file.path === selectedPath}
                    onClick={() => onSelect(file.path)}
                    sx={{ px: 2 }}
                  >
                    <ListItemIcon sx={{ minWidth: 32 }}>
                      <FileText size={16} />
                    </ListItemIcon>
                    <ListItemText
                      primary={basename(file.path)}
                      slotProps={{ primary: { noWrap: true } }}
                    />
                  </ListItemButton>
                ))}
              </List>
            ) : (
              <Typography
                variant="body2"
                color="text.secondary"
                sx={{ px: 2, py: 0.5, fontStyle: "italic" }}
              >
                {failed
                  ? "Derivation failed"
                  : deriving
                    ? "Being derived…"
                    : "No files yet"}
              </Typography>
            )}
          </Box>
        );
      })}
    </Box>
  );
}
