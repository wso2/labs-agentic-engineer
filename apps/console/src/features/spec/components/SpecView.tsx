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

import { useEffect, useState } from "react";
import {
  Alert,
  Avatar,
  AvatarGroup,
  Box,
  Button,
  Chip,
  CircularProgress,
  Divider,
  IconButton,
  PageContent,
  TextField,
  Tooltip,
  Typography,
  useAppShell,
} from "@wso2/oxygen-ui";
import { ArrowLeft, Hammer } from "@wso2/oxygen-ui-icons-react";
import { useNavigate } from "@tanstack/react-router";
import type { components } from "../../../generated/aep-api";
import {
  useProject,
  useProjectStatus,
  useProjectTags,
} from "../../projects/api/queries";
import { useSpecFileContent, useSpecFiles } from "../api/queries";
import { useCollabSpec } from "../collab/useCollabSpec";
import { CollabTextArea } from "../collab/CollabTextArea";
import { SpecMdEditor } from "../collab/SpecMdEditor";
import { AddArtifactDialog } from "./AddArtifactDialog";
import { SpecFileList } from "./SpecFileList";
import { useSession } from "../../../auth/SessionContext";

type ProjectStatus = components["schemas"]["ProjectStatus"];

// specStatus → header chip, same language as the overview's spec card.
function specChip(status: ProjectStatus): {
  label: string;
  color: "default" | "info" | "success" | "warning" | "error";
} | null {
  switch (status.specStatus) {
    case "draft":
    case "in_progress":
      return { label: "In collaboration", color: "info" };
    case "ready":
      return { label: "Awaiting your review", color: "warning" };
    case "approved":
      return { label: "Approved", color: "success" };
    case "failed":
      return { label: "Derivation failed", color: "error" };
    default:
      return null;
  }
}

// Full-screen spec workspace (#80), per the oxygen-ui sample's
// LoginEditorView pattern: fullWidth/noPadding page, own header bar,
// sidebar collapsed while the view is open.
export function SpecView({ projectName }: { projectName: string }) {
  const navigate = useNavigate();
  const { actions } = useAppShell();
  const project = useProject(projectName);
  const status = useProjectStatus(projectName);
  const tags = useProjectTags(projectName);
  const spec = useSpecFiles(projectName);
  const { user, orgHandle } = useSession();
  // Rooms are org-scoped (`spec-<org>-<project>`); without an org claim fall
  // back to the collab mock BFF's default org so mock mode keeps working.
  const collab = useCollabSpec(projectName, user, orgHandle ?? "acme");
  const [selectedPath, setSelectedPath] = useState<string | null>(null);
  const [addArtifactOpen, setAddArtifactOpen] = useState(false);

  // Collapse the sidebar while focused on the spec, expand when leaving.
  useEffect(() => {
    actions.collapseSidebar();
    return () => {
      actions.expandSidebar();
    };
  }, [actions]);

  const files = spec.data ?? [];
  // Default selection: the first requirements file (the seeded PRD).
  const selected =
    files.find((f) => f.path === selectedPath) ??
    files.find((f) => f.group === "requirements") ??
    files[0] ??
    null;

  // Collab supplies live content when connected; the REST read (lazy, per
  // selected file) is only the solo fallback, so it stays disabled while a
  // collab doc backs the selection.
  const selectedIsMd = selected?.path.endsWith(".md") ?? false;
  const fragment =
    selected && selectedIsMd ? collab.getFileFragment(selected.path) : null;
  const ytext =
    selected && !selectedIsMd ? collab.getFileText(selected.path) : null;
  const usesCollab = Boolean((fragment && collab.provider) || ytext);
  const content = useSpecFileContent(
    projectName,
    selected && !usesCollab ? selected : null,
  );

  const specStatus = status.data?.specStatus;
  const deriving =
    specStatus === "pending" ||
    specStatus === "draft" ||
    specStatus === "in_progress";
  const failed = specStatus === "failed";
  const chip = status.data ? specChip(status.data) : null;
  // The design gate: Build arms once design files are generated (#80).
  const hasDesignFiles = files.some((f) => f.group === "designs");

  const displayName = project.data?.displayName ?? projectName;

  return (
    <PageContent fullWidth noPadding>
      <Box
        sx={{
          height: "100%",
          minHeight: 0,
          display: "flex",
          flexDirection: "column",
        }}
      >
        {/* Header */}
        <Box
          sx={{
            p: 2,
            borderBottom: 1,
            borderColor: "divider",
            display: "flex",
            alignItems: "center",
            gap: 2,
            bgcolor: "background.paper",
          }}
        >
          <IconButton
            aria-label="Back to project overview"
            onClick={() =>
              void navigate({
                to: "/projects/$projectName",
                params: { projectName },
              })
            }
          >
            <ArrowLeft size={20} />
          </IconButton>
          <Box sx={{ flexGrow: 1, minWidth: 0 }}>
            <Typography variant="h4" noWrap>
              Spec
            </Typography>
            <Typography variant="body2" color="text.secondary" noWrap>
              {displayName}
            </Typography>
          </Box>

          {collab.peers.length > 0 && (
            <AvatarGroup max={5}>
              {collab.peers.map((peer) => (
                <Tooltip
                  key={peer.clientId}
                  title={`${peer.name}${peer.kind === "agent" ? " (agent)" : ""}`}
                >
                  <Avatar
                    sx={{
                      width: 28,
                      height: 28,
                      fontSize: "0.8rem",
                      bgcolor: peer.color,
                      // Agents get a square-ish avatar so presence is honest
                      // about who is human (#86 decision 7).
                      borderRadius: peer.kind === "agent" ? 1 : "50%",
                    }}
                  >
                    {(peer.name.trim()[0] ?? "?").toUpperCase()}
                  </Avatar>
                </Tooltip>
              ))}
            </AvatarGroup>
          )}
          {collab.status === "offline" && (
            <Tooltip title="Collaboration server unreachable — editing solo; edits aren't shared or saved.">
              <Chip size="small" variant="outlined" label="solo" />
            </Tooltip>
          )}

          {/* Version chips from the tag resource (#117): latest user tag +
              whether specs/ moved on GitHub since. */}
          {tags.data?.latest && (
            <Chip
              size="small"
              variant="outlined"
              color="success"
              label={`${tags.data.latest} published`}
            />
          )}
          {tags.data?.specDirty && (
            <Chip size="small" color="warning" label="draft changes" />
          )}
          {chip && <Chip size="small" color={chip.color} label={chip.label} />}

          <Divider orientation="vertical" flexItem />

          <Tooltip
            title={
              hasDesignFiles
                ? "Start building from this spec"
                : "Available once design files are generated"
            }
          >
            {/* span so the tooltip works while the button is disabled */}
            <span>
              <Button
                variant="contained"
                startIcon={<Hammer size={18} />}
                disabled={!hasDesignFiles}
                onClick={() =>
                  void navigate({
                    to: "/projects/$projectName",
                    params: { projectName },
                  })
                }
              >
                Build
              </Button>
            </span>
          </Tooltip>
        </Box>

        {failed && (
          <Alert severity="error" sx={{ borderRadius: 0 }}>
            Spec derivation hit a problem. Existing files remain browsable;
            the agents' error details will surface here in a follow-up.
          </Alert>
        )}

        {/* Body: grouped file list + file content */}
        {spec.isPending ? (
          <Box
            sx={{
              flexGrow: 1,
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
            }}
          >
            <CircularProgress aria-label="Loading spec" />
          </Box>
        ) : spec.isError ? (
          <Box sx={{ p: 3 }}>
            <Alert
              severity="error"
              action={<Button onClick={() => void spec.refetch()}>Retry</Button>}
            >
              Failed to load the spec
              {spec.error instanceof Error && spec.error.message
                ? `: ${spec.error.message}`
                : ""}
            </Alert>
          </Box>
        ) : (
          <Box sx={{ flexGrow: 1, minHeight: 0, display: "flex" }}>
            <Box
              sx={{
                width: 280,
                flexShrink: 0,
                borderRight: 1,
                borderColor: "divider",
                overflow: "auto",
              }}
            >
              <SpecFileList
                files={files}
                selectedPath={selected?.path ?? null}
                onSelect={setSelectedPath}
                onAddArtifact={() => setAddArtifactOpen(true)}
                deriving={deriving}
                failed={failed}
              />
            </Box>
            <Box sx={{ flexGrow: 1, minWidth: 0, overflow: "auto", p: 2 }}>
              {selected ? (
                // Placeholder for the per-type renderers (WYSIWYG for
                // markdown, dedicated components for structured files —
                // each its own feature). Collaborative when the collab
                // service is reachable (#86 phase 5); solo-and-unsaved
                // otherwise (#86 decision 10).
                fragment && collab.provider ? (
                  // Markdown gets the Tiptap editor on the file's
                  // Y.XmlFragment (#86 phase 6).
                  <SpecMdEditor
                    key={`${selected.path}:md`}
                    fragment={fragment}
                    path={selected.path}
                    provider={collab.provider}
                    self={collab.self}
                  />
                ) : ytext ? (
                  <CollabTextArea
                    key={`${selected.path}:collab`}
                    ytext={ytext}
                    path={selected.path}
                    isLocalTransaction={collab.isLocalTransaction}
                  />
                ) : content.data ? (
                  <TextField
                    key={`${selected.path}:${content.data.sha}`}
                    fullWidth
                    multiline
                    minRows={20}
                    defaultValue={content.data.content}
                    aria-label={`Content of ${selected.path}`}
                    helperText={`${selected.path} — edits aren't saved yet; editing lands with the file editors.`}
                    slotProps={{
                      input: {
                        sx: { fontFamily: "monospace", fontSize: "0.875rem" },
                      },
                    }}
                  />
                ) : content.isError ? (
                  <Alert
                    severity="error"
                    action={
                      <Button onClick={() => void content.refetch()}>
                        Retry
                      </Button>
                    }
                  >
                    Failed to load {selected.path}
                    {content.error instanceof Error && content.error.message
                      ? `: ${content.error.message}`
                      : ""}
                  </Alert>
                ) : (
                  <Box
                    sx={{
                      height: "100%",
                      display: "flex",
                      alignItems: "center",
                      justifyContent: "center",
                    }}
                  >
                    <CircularProgress
                      aria-label={`Loading ${selected.path}`}
                    />
                  </Box>
                )
              ) : (
                <Typography variant="body2" color="text.secondary">
                  {deriving
                    ? "The agents are shaping the spec — files appear here as they land."
                    : "Select a file to view its content."}
                </Typography>
              )}
            </Box>
          </Box>
        )}
      </Box>

      <AddArtifactDialog
        open={addArtifactOpen}
        onClose={() => setAddArtifactOpen(false)}
      />
    </PageContent>
  );
}
