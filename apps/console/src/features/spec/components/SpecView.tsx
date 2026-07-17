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

import { useEffect, useMemo, useState } from "react";
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
import { ArrowLeft, Hammer, Sparkles } from "@wso2/oxygen-ui-icons-react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import type { components } from "../../../generated/aep-api";
import {
  useBuildPreflight,
  useBuildProject,
  useProject,
  useProjectStatus,
  useProjectTags,
} from "../../projects/api/queries";
import { useSpecFileContent, useSpecFiles } from "../api/queries";
import { useProjectUsage } from "../../usage/api/queries";
import { totalTokens } from "../../usage/lib/format";
import { EstimateChip } from "../../usage/components/EstimateChip";
import { UsageChip } from "../../usage/components/UsageChip";
import { toSpecEntry } from "../api/mapping";
import { useCollabSpec } from "../collab/useCollabSpec";
import { CollabTextArea } from "../collab/CollabTextArea";
import { SpecMdEditor } from "../collab/SpecMdEditor";
import { useYTextString } from "../collab/useYTextString";
import { AddArtifactDialog } from "./AddArtifactDialog";
import { BuildDependencyDrawer } from "./BuildDependencyDrawer";
import { SpecFileList } from "./SpecFileList";
import { CellDiagramPanel } from "./CellDiagramPanel";
import { WireframePanel } from "./WireframePanel";
import { OpenApiView } from "@aep/ui-openapi-view";
import { DesignView } from "@aep/ui-design-view";
import { ValidationView } from "@aep/ui-validation-view";
import type { SpecSelection } from "../api/designTree";
import { DESIGN_CELL_PATH } from "../api/designTree";
import { useSession } from "../../../auth/SessionContext";

type ProjectStatus = components["schemas"]["ProjectStatus"];
type PreflightItem = components["schemas"]["PreflightItem"];
type BuildInputItem = components["schemas"]["BuildInputItem"];

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
  // Cost visibility (#245): the header's draft-cycle spend chip — spec/design
  // turn usage since the last published tag, mirroring the version chips.
  const usageQ = useProjectUsage(projectName);
  const [selection, setSelection] = useState<SpecSelection | null>(null);
  const [addArtifactOpen, setAddArtifactOpen] = useState(false);
  // Build (#162): commit-then-build. buildPhase drives the button label /
  // loading; an agent peer in the room means a turn is writing → block Build.
  const build = useBuildProject(projectName);
  // Preflight (#164): checked between commit and build — a project with
  // unresolved dependencies (external config/spec, platform resources, org
  // services) routes through the drawer instead of building blind.
  const preflight = useBuildPreflight(projectName);
  const [buildPhase, setBuildPhase] = useState<
    "committing" | "checking" | "building" | null
  >(null);
  const [buildError, setBuildError] = useState<string | null>(null);
  const [dependencyDrawerOpen, setDependencyDrawerOpen] = useState(false);
  const [preflightItems, setPreflightItems] = useState<PreflightItem[]>([]);

  // Collapse the sidebar while focused on the spec, expand when leaving.
  useEffect(() => {
    actions.collapseSidebar();
    return () => {
      actions.expandSidebar();
    };
  }, [actions]);

  // The spec list is git (one-shot, committed truth + offline fallback)
  // UNIONed with the live collab doc (agent-created files and edits arrive
  // here in real time, before they commit). Deduped by path; the git entry
  // wins when both have it (it carries the real blob sha). Sorted by path so
  // the order is stable as live files appear.
  const files = useMemo(() => {
    const byPath = new Map<string, ReturnType<typeof toSpecEntry>>();
    for (const path of collab.docPaths) {
      const entry = toSpecEntry({ path, sha: "" });
      if (entry) byPath.set(entry.path, entry);
    }
    for (const entry of spec.data ?? []) byPath.set(entry.path, entry);
    return [...byPath.values()]
      .filter((e): e is NonNullable<typeof e> => e !== null)
      .sort((a, b) => a.path.localeCompare(b.path));
  }, [spec.data, collab.docPaths]);
  // A live design turn is signalled by `?generate=design` (the Generate-design
  // CTA) and, more durably, by an agent peer streaming design.cell into the
  // room. In either case the Architecture (cell-diagram) tab is where the user
  // wants to be, so we auto-select it.
  const generate = (
    useSearch({ strict: false }) as { generate?: "requirements" | "design" }
  ).generate;
  const agentInRoom = collab.peers.some((p) => p.kind === "agent");
  const hasDesignCell = files.some((f) => f.path === DESIGN_CELL_PATH);

  // On the Generate-design signal, jump to the Architecture tab immediately
  // (before design.cell even exists) so the empty/streaming cell is shown.
  // AppLayout strips the param right after auto-sending, so this fires once.
  useEffect(() => {
    if (generate === "design") setSelection({ kind: "cell-diagram" });
  }, [generate]);

  // Default selection: while a design turn is actively producing design.cell,
  // default to Architecture (covers a reload mid-turn); otherwise the first
  // requirements file (the seeded PRD). A manual click sets `selection` and
  // always wins over this default.
  const firstRequirements = files.find((f) => f.group === "requirements");
  const effectiveSelection: SpecSelection =
    selection ??
    (agentInRoom && hasDesignCell
      ? { kind: "cell-diagram" }
      : firstRequirements
        ? { kind: "file", path: firstRequirements.path }
        : files[0]
          ? { kind: "file", path: files[0].path }
          : { kind: "file", path: "" });

  // The concrete file entry when the selection is a file (else null: the
  // synthetic cell-diagram / wireframe views render their own panels).
  const selectedFile =
    effectiveSelection.kind === "file"
      ? (files.find((f) => f.path === effectiveSelection.path) ?? null)
      : null;

  // Collab supplies live content when connected; the REST read (lazy, per
  // selected file) is only the solo fallback, so it stays disabled while a
  // collab doc backs the selection. `openapi.yaml` is a fully rendered,
  // read-only API Spec view — like the wireframe .dsl, it never goes through
  // the collab text editor, so it's excluded from both branches below.
  const isOpenApiFile = selectedFile?.path.endsWith("/openapi.yaml") ?? false;
  // A component's design.json renders as a read-only structured Overview —
  // like openapi.yaml, it never goes through the collab text editor.
  const isComponentDesignFile =
    /^specs\/design\/components\/[^/]+\/design\.json$/.test(
      selectedFile?.path ?? "",
    );
  // The validation acceptance oracle renders as a read-only structured view —
  // like design.json, it never goes through the collab text editor.
  const isValidationCriteriaFile =
    /^specs\/validation\/validation-criteria\.json$/.test(
      selectedFile?.path ?? "",
    );
  // The structured files share the read-only render path (no collab editor,
  // sourced from the live doc or the committed fetch).
  const isStructuredFile =
    isOpenApiFile || isComponentDesignFile || isValidationCriteriaFile;
  // Canvas-based views (cell diagram, Excalidraw) need a flex-column,
  // overflow-hidden ancestor so their own `flex: 1` roots get a real
  // measured height to stretch into — a plain overflow:auto block (used for
  // text content below) leaves them at their library-default intrinsic size
  // instead of filling the pane.
  const isDiagramView =
    effectiveSelection.kind === "cell-diagram" ||
    effectiveSelection.kind === "wireframe";
  const selectedIsMd = selectedFile?.path.endsWith(".md") ?? false;
  const fragment =
    selectedFile && selectedIsMd && !isOpenApiFile
      ? collab.getFileFragment(selectedFile.path)
      : null;
  const ytext =
    selectedFile && !selectedIsMd && !isStructuredFile
      ? collab.getFileText(selectedFile.path)
      : null;
  const usesCollab = Boolean((fragment && collab.provider) || ytext);
  // The md editor owns its scrolling (toolbar docked as the frame's header,
  // document area scrolls inside — #206 rework), so its pane must be the
  // same flex-column/overflow-hidden shape the canvas views need.
  const isMdEditorView = Boolean(fragment && collab.provider);
  // The collab doc is the SOURCE for the structured views while collab is up
  // (the design.md rule): rooms are seeded with every committed specs/ file
  // (non-md as Y.Text) and the agents service mirrors each applied write, so
  // the doc always holds the freshest complete, gate-validated content — the
  // committed file between turns, a new file before its commit lands, an
  // EDITED file while the committed copy is stale. The committed fetch below
  // stays for the collab-less base path only.
  const structuredLiveText = useYTextString(
    selectedFile && isStructuredFile
      ? collab.getFileText(selectedFile.path)
      : null,
  );
  const structuredLive =
    typeof structuredLiveText === "string" && structuredLiveText.trim().length > 0
      ? structuredLiveText
      : null;
  const content = useSpecFileContent(
    projectName,
    selectedFile &&
      (isStructuredFile
        ? // Doc has it → no fetch (mirrors usesCollab for md). An agent in
          // the room also suppresses it: the doc WILL deliver the file, and
          // probing git for a not-yet-committed path just sprays 404s.
          !structuredLive && !agentInRoom
        : !usesCollab)
      ? selectedFile
      : null,
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
  // #159: design is derived FROM requirements, so its CTA needs them first.
  const hasRequirementsFiles = files.some((f) => f.group === "requirements");

  // Generate/Re-generate design (#159): open the agent panel and auto-send the
  // design turn via the shared ?generate=design signal (AppLayout + the panel).
  const generateDesign = () =>
    void navigate({
      to: "/projects/$projectName/spec",
      params: { projectName },
      search: { generate: "design" },
    });

  // An agent turn is in flight iff an agent peer is present in the room (#86 d7
  // renders them with kind:"agent"). Building a half-written design is wrong,
  // so Build is disabled — with a tooltip — while one is working (#162).
  const agentBusy = collab.peers.some((p) => p.kind === "agent");

  // Build (#162, #164): commit the room's live edits FIRST (POST /build tags
  // HEAD), then check preflight — a project with unresolved dependencies
  // routes to the drawer instead of building blind; only once preflight says
  // the project is ready does this trigger the build and go watch progress
  // on the overview.
  const onBuild = () => {
    setBuildError(null);
    setBuildPhase("committing");
    void (async () => {
      try {
        await collab.flush(); // no-op when offline
        setBuildPhase("checking");
        const { data, isError, error } = await preflight.refetch();
        if (isError || data === undefined) {
          // TanStack's refetch() resolves rather than throws on error, so a
          // preflight failure (network blip, expired session, BFF hiccup)
          // must be handled explicitly here — otherwise it falls through to
          // building with empty inputs, silently skipping dependency
          // provisioning (the exact #164 symptom this feature fixes).
          setBuildError(
            error instanceof Error
              ? error.message
              : "Failed to check build readiness.",
          );
          return;
        }
        if (data.needsInput) {
          setPreflightItems(data.items ?? []);
          setDependencyDrawerOpen(true);
          return;
        }
        setBuildPhase("building");
        await build.mutateAsync({ inputs: [] });
        void navigate({
          to: "/projects/$projectName",
          params: { projectName },
        });
      } catch (e) {
        setBuildError(
          e instanceof Error ? e.message : "Failed to start the build.",
        );
      } finally {
        setBuildPhase(null);
      }
    })();
  };

  // Drawer Continue (#164): resubmit the build with the resolved dependency
  // inputs. A clean response closes the drawer and moves on to the overview;
  // any inputs the BFF/devflow rejects come back as `failures` — surface the
  // reasons and leave the drawer open so the user can fix them and retry.
  const onContinueBuild = async (inputs: BuildInputItem[]) => {
    setBuildError(null);
    setBuildPhase("building");
    try {
      const res = await build.mutateAsync({ inputs });
      if (res.failures?.length) {
        setBuildError(
          res.failures.map((f) => `${f.dependency}: ${f.reason}`).join("; "),
        );
        return;
      }
      setDependencyDrawerOpen(false);
      void navigate({
        to: "/projects/$projectName",
        params: { projectName },
      });
    } catch (e) {
      setBuildError(
        e instanceof Error ? e.message : "Failed to start the build.",
      );
    } finally {
      setBuildPhase(null);
    }
  };

  const displayName = project.data?.displayName ?? projectName;

  return (
    // oxygen-ui's PageContentInner (the direct parent of these children) has
    // no height/flex of its own — it sizes to its content, breaking the
    // height:100% chain PageContentRoot otherwise correctly establishes
    // (PageContentRoot IS height:100%+flex-column, and correctly excludes
    // the AppShell footer's height via the shell's own flex distribution).
    // `sx` here isn't filtered by PageContent's prop allowlist, so it
    // forwards straight through to PageContentInner and closes that one
    // missing link — this is the supported override, not a guessed
    // viewport value like 100vh (which ignores the footer entirely and
    // overshoots the actual available space).
    <PageContent fullWidth noPadding sx={{ height: "100%", display: "flex", flexDirection: "column" }}>
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
          {/* Draft-cycle spend (#245): what this version-in-progress has cost
              in spec/design turns. Hidden until any spend exists. */}
          {usageQ.data && totalTokens(usageQ.data.draftCycle) > 0 && (
            <UsageChip usage={usageQ.data.draftCycle} label="spec" />
          )}

          <Divider orientation="vertical" flexItem />

          {/* Phase-aware primary CTA (#159): the prominent action is always the
              next pipeline step — Generate design until a design exists, then
              Build. A dead disabled Build hid what to do next. */}
          {hasDesignFiles ? (
            <>
            {/* Inline pre-build estimate (#245 decision 9): informs the click
                without adding a confirm step to the one-click flow (#162). */}
            <EstimateChip projectName={projectName} action="build" />
            <Tooltip
              title={
                agentBusy
                  ? "An agent is still working — Build is available once it finishes"
                  : "Commit your latest changes and start building"
              }
            >
              {/* span so the tooltip works while the button is disabled */}
              <span>
                <Button
                  variant="contained"
                  startIcon={<Hammer size={18} />}
                  disabled={agentBusy || buildPhase !== null}
                  loading={buildPhase !== null}
                  onClick={onBuild}
                >
                  {buildPhase === "committing"
                    ? "Committing…"
                    : buildPhase === "checking"
                      ? "Checking…"
                      : buildPhase === "building"
                        ? "Building…"
                        : "Build"}
                </Button>
              </span>
            </Tooltip>
            </>
          ) : (
            <>
            {/* Estimate only once requirements exist — before that there is
                nothing to derive a design from, so a figure would be noise. */}
            {hasRequirementsFiles && (
              <EstimateChip projectName={projectName} action="design" />
            )}
            <Tooltip
              title={
                agentBusy
                  ? "An agent is still working — Generate design is available once it finishes"
                  : hasRequirementsFiles
                    ? "Derive the component design from your requirements"
                    : "Generate requirements first"
              }
            >
              {/* span so the tooltip works while the button is disabled */}
              <span>
                <Button
                  variant="contained"
                  startIcon={<Sparkles size={18} />}
                  disabled={!hasRequirementsFiles || agentBusy}
                  onClick={generateDesign}
                >
                  Generate design
                </Button>
              </span>
            </Tooltip>
            </>
          )}
        </Box>

        {failed && (
          <Alert severity="error" sx={{ borderRadius: 0 }}>
            Spec derivation hit a problem. Existing files remain browsable;
            the agents' error details will surface here in a follow-up.
          </Alert>
        )}

        {/* Build failed to start (#162): commit or POST /build errored. */}
        {buildError && (
          <Alert
            severity="error"
            sx={{ borderRadius: 0 }}
            onClose={() => setBuildError(null)}
          >
            {buildError}
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
                selection={effectiveSelection}
                onSelect={setSelection}
                onAddArtifact={() => setAddArtifactOpen(true)}
                onRegenerateDesign={generateDesign}
                regenerateDisabled={agentBusy}
                deriving={deriving}
                failed={failed}
              />
            </Box>
            <Box
              sx={
                isDiagramView || isMdEditorView
                  ? {
                      flexGrow: 1,
                      minWidth: 0,
                      minHeight: 0,
                      display: "flex",
                      flexDirection: "column",
                      overflow: "hidden",
                      ...(isMdEditorView && { p: 2 }),
                    }
                  : { flexGrow: 1, minWidth: 0, overflow: "auto", p: 2 }
              }
            >
              {effectiveSelection.kind === "cell-diagram" ? (
                <CellDiagramPanel projectName={projectName} files={files} collab={collab} />
              ) : effectiveSelection.kind === "wireframe" ? (
                <WireframePanel
                  projectName={projectName}
                  dslPath={effectiveSelection.dslPath}
                  files={files}
                  collab={collab}
                />
              ) : selectedFile ? (
                // Per-type renderers (WYSIWYG for markdown, dedicated components
                // for structured files). Collaborative when the collab service
                // is reachable (#86 phase 5); solo-and-unsaved otherwise
                // (#86 decision 10).
                isStructuredFile ? (
                  structuredLive ? (
                    // Fresh from the live collab doc — ahead of (or newer
                    // than) the committed copy.
                    isOpenApiFile ? (
                      <OpenApiView spec={structuredLive} />
                    ) : isValidationCriteriaFile ? (
                      <ValidationView criteria={structuredLive} />
                    ) : (
                      <DesignView design={structuredLive} />
                    )
                  ) : content.data ? (
                    isOpenApiFile ? (
                      <OpenApiView
                        key={content.data.sha}
                        spec={content.data.content}
                      />
                    ) : isValidationCriteriaFile ? (
                      <ValidationView
                        key={content.data.sha}
                        criteria={content.data.content}
                      />
                    ) : (
                      <DesignView
                        key={content.data.sha}
                        design={content.data.content}
                      />
                    )
                  ) : agentBusy ? (
                    // Mid-generation the committed fetch is suppressed (the
                    // doc will deliver the file) — "about to appear",
                    // not a failure.
                    <Box
                      sx={{
                        height: "100%",
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "center",
                      }}
                    >
                      <Typography variant="body2" color="text.secondary">
                        Waiting for the agent to write {selectedFile.path.split("/").at(-1)}…
                      </Typography>
                    </Box>
                  ) : content.isError ? (
                    <Alert
                      severity="error"
                      action={
                        <Button onClick={() => void content.refetch()}>Retry</Button>
                      }
                    >
                      Failed to load {selectedFile.path}
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
                      <CircularProgress aria-label={`Loading ${selectedFile.path}`} />
                    </Box>
                  )
                ) : fragment && collab.provider ? (
                  // Markdown gets the Tiptap editor on the file's
                  // Y.XmlFragment (#86 phase 6).
                  <SpecMdEditor
                    key={`${selectedFile.path}:md`}
                    fragment={fragment}
                    provider={collab.provider}
                    self={collab.self}
                    agentStreaming={agentBusy}
                  />
                ) : ytext ? (
                  <CollabTextArea
                    key={`${selectedFile.path}:collab`}
                    ytext={ytext}
                    path={selectedFile.path}
                    isLocalTransaction={collab.isLocalTransaction}
                  />
                ) : content.data ? (
                  <TextField
                    key={`${selectedFile.path}:${content.data.sha}`}
                    fullWidth
                    multiline
                    minRows={20}
                    defaultValue={content.data.content}
                    aria-label={`Content of ${selectedFile.path}`}
                    helperText={`${selectedFile.path} — edits aren't saved yet; editing lands with the file editors.`}
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
                    Failed to load {selectedFile.path}
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
                      aria-label={`Loading ${selectedFile.path}`}
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

      <BuildDependencyDrawer
        open={dependencyDrawerOpen}
        items={preflightItems}
        submitting={dependencyDrawerOpen && buildPhase === "building"}
        onClose={() => setDependencyDrawerOpen(false)}
        onContinue={(inputs) => void onContinueBuild(inputs)}
      />
    </PageContent>
  );
}
