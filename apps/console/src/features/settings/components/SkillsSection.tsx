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
import { Link } from "@tanstack/react-router";
import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Divider,
  Pagination,
  SearchBar,
  Switch,
  Tooltip,
  Typography,
} from "@wso2/oxygen-ui";
import {
  Eye,
  FolderGit2,
  RefreshCw,
  TriangleAlert,
  Upload,
} from "@wso2/oxygen-ui-icons-react";
import { useHasPermission } from "../../../auth/permissions";
import { StatusChip } from "../../../components/StatusChip";
import {
  useConfig,
  useDeleteSkill,
  useSetSkillEnabled,
  useSkillUpdates,
  useSkills,
  useSyncSkills,
} from "../api/queries";
import { kindBlurb, kindChipTone, kindLabel, normalizeKind } from "../skillKind";
import { paginateSkills } from "../skillsList";
import { EditSkillDialog } from "./EditSkillDialog";
import { ImportSkillDialog } from "./ImportSkillDialog";
import { SkillViewerDialog } from "./SkillViewerDialog";
import { SyncUpdatesControl } from "./SyncUpdatesControl";

const PAGE_SIZE = 10;

// Per-row platform-update status. Both states are framed as "a platform update
// is available" — the only difference is whether the org customized this skill,
// which is why one needs a review before adopting. Deliberately NOT called a
// "conflict": a plain platform update is a new version on offer, not a fault.
// The status is shown as a coloured left stripe on the row plus this inline
// line — kept off the name line so it never competes with the kind chip.
const STATUS_META = {
  // Clean copy, platform moved → Sync brings it up to date. Informational.
  update: {
    icon: RefreshCw,
    color: "info.main",
    label: "Platform update available",
    tooltip:
      "The platform shipped a newer version of this skill. Your copy is unchanged, so syncing brings it up to date.",
  },
  // Org customized it AND the platform moved → adopting would overwrite the
  // org's edits, so it needs a look first (the #298 review flow). Not synced.
  review: {
    icon: TriangleAlert,
    color: "warning.main",
    label: "Platform update — review your changes",
    tooltip:
      "The platform shipped a newer version of this skill after you customized it. Your version is kept; review to decide whether to adopt the platform's.",
  },
} as const;

export function SkillsSection() {
  const hasSkillView = useHasPermission("ae:skill-view");
  const {
    data: config,
    isLoading: configLoading,
    isError: configIsError,
    error: configError,
    refetch: refetchConfig,
  } = useConfig();
  const { data, isLoading, isError, error, refetch } = useSkills(hasSkillView);
  const { data: updates } = useSkillUpdates();
  const hasSkillConfig = useHasPermission("ae:skill-config");

  const [importOpen, setImportOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [requestedPage, setRequestedPage] = useState(1);
  const [viewTarget, setViewTarget] = useState<string | null>(null);
  const [editTarget, setEditTarget] = useState<string | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);

  const deleteSkill = useDeleteSkill();
  const syncSkills = useSyncSkills();
  const setSkillEnabled = useSetSkillEnabled();

  // Checked before the config/skills loading states below: without
  // ae:skill-view there is nothing here to load — the skills query itself
  // never fires (useSkills(hasSkillView)), so falling through would just
  // hang on an eternal loading spinner.
  if (!hasSkillView) {
    return (
      <Alert severity="warning">
        You don't have permission to view skills.
      </Alert>
    );
  }

  if (configLoading) {
    return (
      <Box sx={{ display: "flex", justifyContent: "center", py: 6 }}>
        <CircularProgress />
      </Box>
    );
  }

  // A failed GET /config leaves `config` undefined, which would otherwise fall
  // through to the not-connected branch below and blame the user for a server
  // error. Distinguish the two.
  if (configIsError) {
    return (
      <Alert
        severity="error"
        action={<Button onClick={() => void refetchConfig()}>Retry</Button>}
      >
        {configError?.message ?? "Failed to load the organization configuration"}
      </Alert>
    );
  }

  if (!config?.gitProvider) {
    return (
      <Alert
        severity="info"
        icon={<FolderGit2 size={20} />}
        action={
          <Button component={Link} to="/settings/credentials">
            Connect GitHub
          </Button>
        }
      >
        The skills catalogue lives in the org's GitHub repo — connect GitHub
        first.
      </Alert>
    );
  }

  if (isLoading) {
    return (
      <Box sx={{ display: "flex", justifyContent: "center", py: 6 }}>
        <CircularProgress />
      </Box>
    );
  }

  if (isError || !data) {
    return (
      <Alert
        severity="error"
        action={<Button onClick={() => void refetch()}>Retry</Button>}
      >
        {error?.message ?? "Failed to load skills"}
      </Alert>
    );
  }

  const { skills, repoUrl } = data;
  // Three-way update states drive the per-row status stripe. Only "update"
  // rows are sync-appliable, so only they feed the badge and the sync count.
  // "overridden" (org customized, platform unchanged) has nothing to apply and
  // stays quiet. "conflict" (platform moved a skill the org customized) is NOT
  // synced — it surfaces as the amber "review your changes" status until the
  // review flow lands (#298). See STATUS_META for the wording rationale.
  const applicable = (updates ?? []).filter((u) => u.state === "update");
  const conflicted = (updates ?? []).filter((u) => u.state === "conflict");
  const updatable = new Set(applicable.map((u) => u.name));
  const reviewNames = new Set(conflicted.map((u) => u.name));
  const syncedCount = syncSkills.data?.updated ?? 0;

  // Filter → sort → clamp → slice as one pure derivation: a list that shrinks
  // underneath the current page (delete, sync) lands on the nearest still-
  // valid page without any effect re-syncing state.
  const query = search.trim();
  const { rows, page, pageCount, total } = paginateSkills(
    skills,
    query,
    requestedPage,
    PAGE_SIZE,
  );

  const confirmDelete = () => {
    if (!deleteTarget) return;
    deleteSkill.mutate(deleteTarget, {
      onSuccess: () => setDeleteTarget(null),
    });
  };

  return (
    <Box>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
        Skills shape what the platform's agents emit — code patterns,
        conventions, and project layout. Org and platform skills ship with the
        platform and are read-only; import AgentSkills from the ecosystem to
        extend them.
      </Typography>

      <Box
        sx={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          flexWrap: "wrap",
          gap: 1,
          mb: 2,
        }}
      >
        <Box sx={{ width: { xs: "100%", sm: 320 } }}>
          <SearchBar
            value={search}
            onChange={(e) => {
              setSearch(e.target.value);
              // A new query re-filters from scratch — page 1 is the only
              // page that is meaningful for it.
              setRequestedPage(1);
            }}
            placeholder="Search skills..."
          />
        </Box>
        <Box
          sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}
        >
          <SyncUpdatesControl
            count={applicable.length}
            pending={syncSkills.isPending}
            onSync={() => syncSkills.mutate()}
          />
          <Tooltip
            title={
              hasSkillConfig ? "" : "You don't have permission to configure skills."
            }
          >
            <span>
              <Button
                variant="contained"
                startIcon={<Upload size={18} />}
                onClick={() => setImportOpen(true)}
                disabled={!hasSkillConfig}
              >
                Import
              </Button>
            </span>
          </Tooltip>
        </Box>
      </Box>

      {syncSkills.isSuccess && (
        <Alert
          severity="info"
          sx={{ mb: 2 }}
          onClose={() => syncSkills.reset()}
        >
          {syncedCount > 0
            ? `Synced ${syncedCount} org skill${syncedCount === 1 ? "" : "s"} to the latest content.`
            : "Org skills are already up to date."}
        </Alert>
      )}
      {syncSkills.isError && (
        <Alert severity="error" sx={{ mb: 2 }} onClose={() => syncSkills.reset()}>
          {syncSkills.error.message}
        </Alert>
      )}
      {setSkillEnabled.isError && (
        <Alert
          severity="error"
          sx={{ mb: 2 }}
          onClose={() => setSkillEnabled.reset()}
        >
          {setSkillEnabled.error.message}
        </Alert>
      )}

      {total === 0 ? (
        <Typography
          variant="body2"
          color="text.secondary"
          sx={{ fontStyle: "italic" }}
        >
          {query ? "No matches." : "None yet."}
        </Typography>
      ) : (
        <>
          <Card variant="outlined">
            <CardContent sx={{ p: 0, "&:last-child": { pb: 0 } }}>
              {rows.map((skill, idx) => {
                const kind = normalizeKind(skill.kind);
                // "update" wins over "review" only in that they're mutually
                // exclusive per the three-way states; a row has at most one.
                const status = updatable.has(skill.name)
                  ? STATUS_META.update
                  : reviewNames.has(skill.name)
                    ? STATUS_META.review
                    : null;
                const StatusIcon = status?.icon;
                const isTogglingThisRow =
                  setSkillEnabled.isPending &&
                  setSkillEnabled.variables?.name === skill.name;
                return (
                  <Box key={skill.name}>
                    {idx > 0 && <Divider />}
                    <Box
                      sx={{
                        display: "flex",
                        alignItems: "center",
                        gap: 1.5,
                        px: 2,
                        py: 1.5,
                        // A coloured left stripe carries the status; a
                        // transparent border on statusless rows keeps every
                        // row's content aligned to the same left edge.
                        borderLeft: "3px solid",
                        borderLeftColor: status ? status.color : "transparent",
                        "&:hover": { bgcolor: "action.hover" },
                      }}
                    >
                      <Box sx={{ flexGrow: 1, minWidth: 0 }}>
                        <Box
                          sx={{
                            display: "flex",
                            alignItems: "center",
                            gap: 1,
                            flexWrap: "wrap",
                          }}
                        >
                          <Typography
                            variant="body1"
                            fontWeight={600}
                            sx={{ color: skill.enabled ? "text.primary" : "text.disabled" }}
                          >
                            {skill.name}
                          </Typography>
                          {/* The kind chip is the flat list's only kind
                              signal; its tooltip carries the kind's blurb,
                              including read-only-ness — there is no separate
                              read-only chip. Status lives on its own line
                              below, never competing with the kind here. */}
                          <Tooltip title={kindBlurb(kind)}>
                            {/* Box holds the ref Tooltip needs — StatusChip
                                doesn't forward one. */}
                            <Box sx={{ display: "inline-flex" }}>
                              <StatusChip
                                label={kindLabel(kind)}
                                tone={kindChipTone(kind)}
                              />
                            </Box>
                          </Tooltip>
                        </Box>
                        {status && StatusIcon && (
                          <Tooltip title={status.tooltip}>
                            <Box
                              sx={{
                                display: "inline-flex",
                                alignItems: "center",
                                gap: 0.5,
                                mt: 0.5,
                                color: status.color,
                              }}
                            >
                              <StatusIcon size={14} />
                              <Typography
                                variant="caption"
                                sx={{ fontWeight: 600, color: "inherit" }}
                              >
                                {status.label}
                              </Typography>
                            </Box>
                          </Tooltip>
                        )}
                        <Typography
                          variant="body2"
                          color={skill.enabled ? "text.secondary" : "text.disabled"}
                          sx={{ mt: 0.5 }}
                        >
                          {skill.description}
                        </Typography>
                      </Box>
                      {/* Availability (this switch) and platform-update state
                          (the stripe/status line above) are independent axes —
                          disabling a skill withholds it from the platform's
                          agents without touching its content, so neither the
                          kind chip nor the update status is hidden here. */}
                      <Box sx={{ flexShrink: 0, display: "flex", alignItems: "center", gap: 1 }}>
                        <Tooltip
                          title={
                            // Permission comes first: it's the one reason
                            // that has nothing to do with this particular
                            // skill's own state, so it takes precedence over
                            // `required`/`enabled` explanations below.
                            !hasSkillConfig
                              ? "You don't have permission to configure skills."
                              : // `required` is the server's call, not a name match
                                // here: the coding runner reads this skill on every
                                // run and refuses to start without it, so the PATCH
                                // would 409. A toggle that can only fail is worse
                                // than one that says why it is unavailable.
                                skill.required
                                ? "This skill carries the coding run's workflow, so it can't be turned off — every build in your organization needs it."
                                : skill.enabled
                                  ? "Disable this skill to withhold it from the platform's agents. It stays in your org's skills repo and can be switched back on anytime."
                                  : "Enable this skill to make it available to the platform's agents again."
                          }
                        >
                          <span>
                            <Switch
                              size="small"
                              checked={skill.enabled}
                              disabled={isTogglingThisRow || skill.required || !hasSkillConfig}
                              onChange={(e) =>
                                setSkillEnabled.mutate({
                                  name: skill.name,
                                  enabled: e.target.checked,
                                })
                              }
                              slotProps={{
                                input: {
                                  role: "switch",
                                  "aria-label": `${skill.enabled ? "Disable" : "Enable"} ${skill.name}`,
                                },
                              }}
                            />
                          </span>
                        </Tooltip>
                        <Button
                          size="small"
                          startIcon={<Eye size={16} />}
                          onClick={() => setViewTarget(skill.name)}
                        >
                          View
                        </Button>
                      </Box>
                    </Box>
                  </Box>
                );
              })}
            </CardContent>
          </Card>
          {/* Hidden on a single page — it would be dead UI. */}
          {pageCount > 1 && (
            <Box sx={{ display: "flex", justifyContent: "center", mt: 2 }}>
              <Pagination
                count={pageCount}
                page={page}
                onChange={(_, next) => setRequestedPage(next)}
              />
            </Box>
          )}
        </>
      )}

      <ImportSkillDialog
        open={importOpen}
        onClose={() => setImportOpen(false)}
        repoUrl={repoUrl}
      />
      <SkillViewerDialog
        name={viewTarget}
        onClose={() => setViewTarget(null)}
        onEdit={() => {
          // Hand off from viewing to editing: open the editor on the same
          // skill, then close the viewer behind it.
          setEditTarget(viewTarget);
          setViewTarget(null);
        }}
        onDelete={() => {
          setDeleteTarget(viewTarget);
          setViewTarget(null);
        }}
      />
      <EditSkillDialog name={editTarget} onClose={() => setEditTarget(null)} />

      <Dialog
        open={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        maxWidth="xs"
        fullWidth
      >
        <DialogTitle>Delete {deleteTarget}?</DialogTitle>
        <DialogContent>
          <DialogContentText>
            This removes the skill from your organization's skills repo. Agents
            read skills at the repo's latest commit, so in-flight tasks simply
            stop seeing it.
          </DialogContentText>
          {deleteSkill.isError && (
            <Alert severity="error" sx={{ mt: 2 }}>
              {deleteSkill.error.message}
            </Alert>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDeleteTarget(null)}>Cancel</Button>
          <Button
            color="error"
            variant="contained"
            onClick={confirmDelete}
            disabled={deleteSkill.isPending}
          >
            Delete
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}
