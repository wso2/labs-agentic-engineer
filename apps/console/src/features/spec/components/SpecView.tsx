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

import { useEffect, useMemo, useRef, useState } from "react";
import {
  Alert,
  AlertTitle,
  Avatar,
  AvatarGroup,
  Box,
  Button,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  IconButton,
  PageContent,
  Stack,
  Tooltip,
  Typography,
  useAppShell,
} from "@wso2/oxygen-ui";
import { ArrowLeft, Hammer, Sparkles } from "@wso2/oxygen-ui-icons-react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { StatusChip } from "../../../components/StatusChip";
import type { components } from "../../../generated/aep-api";
import {
  useBuildPreflight,
  useBuildProject,
  useProjectStatus,
  useProjectTags,
} from "../../projects/api/queries";
import {
  useDesignDependencies,
  useSpecFileContent,
  useSpecFiles,
} from "../api/queries";
import { PRD_PATH, specGroupOf, toSpecEntry } from "../api/mapping";
import { fileLabel } from "../api/labels";
import { computeDependencyUsedBy } from "../lib/dependencyUsedBy";
import { useCollabSpec } from "../collab/useCollabSpec";
import { SpecQuestionForm } from "./SpecQuestionForm";
import { SecurityPanel } from "./SecurityPanel";
import { useSecurityEntry } from "../hooks/useSecurityEntry";
import { nextVersionLabel, parsePrdStories } from "../lib/buildScope";
import { useRoomQuestion } from "../../agent-chat/useRoomQuestion";
import { CollabTextArea } from "../collab/CollabTextArea";
import { SpecMdEditor } from "../collab/SpecMdEditor";
import { useYTextString } from "../collab/useYTextString";
import { useTurnEndFlush } from "../collab/useTurnEndFlush";
import { START_COMMAND } from "@aep/contracts/commands";
import { fragmentToMarkdown } from "@aep/collab-doc";
import { prdUnsettled } from "../lib/prdUnsettled";
import { useYFragmentVersion } from "../collab/useYFragmentVersion";
import {
  railSections as buildRailSections,
  type RailPlanEntry,
  type SectionReason,
} from "../lib/railSections";
import {
  chatKeyFor,
  setPendingSeed,
  subscribeTurnEnd,
} from "../../agent-chat/chatStore";
import { useConversationLog } from "../../agent-chat/useConversationLog";
import { useLocalTurnActivity } from "../../agent-chat/useLocalTurnActivity";
import { EmptyState } from "../../../components/EmptyState";
import { ProblemsDialog } from "./ProblemsDialog";
import { CommittedFileView } from "./CommittedFileView";
import { useResolveDependencyViaChat } from "../../agent-chat/useResolveDependencyViaChat";
import { useAnchoredTurn } from "../../agent-chat/useAnchoredTurn";
import type { Anchor } from "../lib/anchor";
import type { DependencyResolutionIntent } from "../../projects/lib/dependencyResolutionMessage.js";
import { usePlan } from "../../agent-chat/usePlan";
import { approvalInputsFor } from "../lib/buildInputs";
import { BuildDependencyDrawer } from "./BuildDependencyDrawer";
import { SpecFileList } from "./SpecFileList";
import { CellDiagramPanel } from "./CellDiagramPanel";
import { WireframePanel } from "./WireframePanel";
import { OpenApiView } from "@aep/ui-openapi-view";
import { DesignView } from "@aep/ui-design-view";
import type { DependencyStatusInfo } from "@aep/ui-design-view";
import { ValidationView } from "@aep/ui-validation-view";
import { type SpecSelection } from "../api/designTree";
import { DESIGN_CELL_PATH, componentOf, followSelection } from "../api/designTree";
import { useSession } from "../../../auth/SessionContext";

type PreflightItem = components["schemas"]["PreflightItem"];
type BuildInputItem = components["schemas"]["BuildInputItem"];

// Full-screen spec workspace (#80), per the oxygen-ui sample's
// LoginEditorView pattern: fullWidth/noPadding page, own header bar,
// sidebar collapsed while the view is open.
/**
 * The design warning's one paragraph, in the user's words, and only about what
 * is actually there: a project with assumed decisions and no open questions
 * must not be told the agent "left some questions for you". What the user
 * needs at this click is what the agent did, what happens next, and what
 * being wrong costs.
 */
/**
 * Why firing a turn from the spec would be refused right now, or "" when it is
 * live. One computation for the lenses and the aim box, in significance order.
 *
 * `localTurnActivity` covers the window `agentBusy` cannot see: a dispatch has
 * resolved (or a fold is draining) but the agent peer has not joined the room
 * yet, so a second send in those seconds would be a 409 the user meets as a
 * mysterious refusal (CodeRabbit on #670).
 */
export function specTurnGate(input: {
  agentBusy: boolean;
  localTurnActivity: boolean;
  awaitingAnswers: boolean;
}): string {
  if (input.agentBusy) return "An agent is still working — this is available once it finishes";
  if (input.localTurnActivity) return "Your last message is on its way to the agent — one moment";
  if (input.awaitingAnswers)
    return "The agent is waiting on your answers — finish the questions below first";
  return "";
}

export function designWarningIntro(reasons: ReadonlyArray<{ key: string }>): string {
  const assumed = reasons.some((r) => r.key === "assumptions");
  const questions = reasons.some((r) => r.key === "open-questions");
  const what =
    assumed && questions
      ? "The agent has made some decisions on your behalf — they are marked assumed in the document — and left some questions only you can answer."
      : assumed
        ? "The agent has made some decisions on your behalf — they are marked assumed in the document."
        : "The requirements still hold questions only you can answer.";
  return (
    what +
    " The design will be built on the requirements as they stand; change any of these " +
    "afterwards and the design has to be generated again."
  );
}

export function SpecView({ projectName }: { projectName: string }) {
  const navigate = useNavigate();
  const { actions } = useAppShell();
  const status = useProjectStatus(projectName);
  const tags = useProjectTags(projectName);
  const spec = useSpecFiles(projectName);
  // #252 Task 9: every component's read-time dependency status, for the
  // Architecture/design.json cards below (keyed off specKeys.dependencies —
  // the same key Task 5's turn-end freshness invalidation targets).
  const dependencies = useDesignDependencies(projectName);
  const { user, orgHandle } = useSession();
  // Rooms are org-scoped (`spec-<org>-<project>`); without an org claim fall
  // back to the collab mock BFF's default org so mock mode keeps working.
  const collab = useCollabSpec(projectName, user, orgHandle ?? "acme");
  // Collab question cards spike: a pending agent question (one or many) takes
  // over the body with a full-panel form, shared live with everyone in the
  // room. useRoomQuestion mirrors this client's chat log into the room doc and
  // observes the shared map. chatKey uses the "default" org fallback matching
  // the chat panel, not the collab room's "acme".
  const roomDoc = collab.doc;
  const roomQuestion = useRoomQuestion(
    roomDoc,
    chatKeyFor(orgHandle ?? "default", projectName),
  );
  // Chat-path turn-end flush (#252 Task 5): the chat panel's chatKey uses a
  // DIFFERENT fallback ("default", matching AppLayout/AgentChatPanel) than
  // the collab room's org scoping above ("acme") — these are unrelated
  // conventions and must not be conflated, or this subscribes to a chat key
  // nothing is listening on.
  useTurnEndFlush(
    chatKeyFor(orgHandle ?? "default", projectName),
    projectName,
    collab,
  );
  // "Resolve in chat" (#252 Task 9 seam, Task 5's plumbing): SAME "default"
  // org fallback as the chatKey above — not the collab room's "acme" one.
  const resolveDependencyViaChat = useResolveDependencyViaChat(
    orgHandle ?? "default",
    projectName,
  );
  const [selection, setSelection] = useState<SpecSelection | null>(null);
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
  // The build gate's 422 refusal, rendered as an actionable checklist (#372)
  // rather than one flattened alert string.
  const [gateRefusal, setGateRefusal] = useState<Array<{
    field?: string;
    message: string;
  }> | null>(null);
  /** The warning standing between a design run and unsettled requirements. */
  const [confirmDesign, setConfirmDesign] = useState(false);
  // The "Cut version" ceremony (#369/#372): Build first shows what the click
  // does — the next version, the stories in scope, the milestone —
  // and only a confirm POSTs. The backend cuts the real tag.
  const [cutDialogOpen, setCutDialogOpen] = useState(false);
  const [dependencyDrawerOpen, setDependencyDrawerOpen] = useState(false);
  const [preflightItems, setPreflightItems] = useState<PreflightItem[]>([]);

  // #252 Task 10: keep an OPEN drawer fresh after "Resolve via chat" ends a
  // turn. useTurnEndFlush (above) already invalidates the preflight query's
  // cache entry on turn-end, but useBuildPreflight is a manual `enabled:
  // false` query (its only observer), so that invalidation alone never
  // triggers a background refetch — TanStack only auto-refetches invalidated
  // queries that have at least one ENABLED observer. Explicitly flushing the
  // room (idempotent alongside useTurnEndFlush's own flush) then refetching
  // here is what actually lands the resolved item's disappearance in the
  // still-open drawer's `items`, rather than only on the NEXT Build click.
  // collab/preflight are read via refs (mirroring useTurnEndFlush.ts's own
  // collabRef) rather than closed over directly: both are fresh objects most
  // renders, and the effect must fire on `dependencyDrawerOpen`/chat-key
  // identity only, not on every such reference change.
  const collabRef = useRef(collab);
  collabRef.current = collab;
  const preflightRef = useRef(preflight);
  preflightRef.current = preflight;
  useEffect(() => {
    if (!dependencyDrawerOpen) return;
    const chatKey = chatKeyFor(orgHandle ?? "default", projectName);
    return subscribeTurnEnd(chatKey, () => {
      void collabRef.current
        .flush()
        .catch(() => undefined)
        .then(() => preflightRef.current.refetch())
        .then(({ data }) => {
          if (data) setPreflightItems(data.items ?? []);
        });
    });
  }, [dependencyDrawerOpen, orgHandle, projectName]);

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
  // Which references resolve is decided against this list. `files` is rebuilt
  // on every render (`collab.docPaths` is derived, not memoized), so the editor
  // compares it BY VALUE rather than by identity — see `knownPaths` there.
  const specPaths = useMemo(() => files.map((f) => f.path), [files]);
  // A live design turn is signalled by `?generate=design` (the Generate-design
  // CTA) and, more durably, by an agent peer streaming design.cell into the
  // room. In either case the Architecture (cell-diagram) tab is where the user
  // wants to be, so we auto-select it.
  const search = useSearch({ strict: false }) as {
    generate?: "design";
    view?: "architecture";
  };
  const generate = search.generate;
  const agentInRoom = collab.peers.some((p) => p.kind === "agent");
  const hasDesignCell = files.some((f) => f.path === DESIGN_CELL_PATH);

  // On the Generate-design signal, jump to the Architecture tab immediately
  // (before design.cell even exists) so the empty/streaming cell is shown.
  // AppLayout strips the param right after auto-sending, so this fires once.
  useEffect(() => {
    if (generate === "design") setSelection({ kind: "cell-diagram" });
  }, [generate]);

  // `?view=architecture` — arriving from the overview's architecture panel,
  // which links here precisely because it is drawing a diagram. Runs once on
  // the param, so a rail click afterwards is never undone.
  useEffect(() => {
    if (search.view === "architecture") setSelection({ kind: "cell-diagram" });
  }, [search.view]);

  // Follow the write (#576, ADR-0026): while a turn runs, the editor selects
  // each artifact as its write starts, so the passive watcher — the default
  // posture at turn start — sees the work land in whatever renderer that
  // artifact already has. The FIRST manual selection is a declaration of
  // reading intent and ends the following for the rest of the turn; the rail's
  // pulse on the writing entry stays the one-click way back in. A new turn
  // resets to following. Supersedes the cell's burst navigation, which yanked
  // back even over a manual selection.
  const plan = usePlan(orgHandle ?? "default", projectName);
  const followingRef = useRef(true);
  const planTurnId = plan?.turnActive ? plan.turnId : null;
  useEffect(() => {
    if (planTurnId) followingRef.current = true;
  }, [planTurnId]);
  const writingPath = plan?.turnActive ? plan.writingPath : null;
  // Keyed on the TURN as well as the path: a delta pass re-writes the same
  // artifact the failed turn died on, so its first write can carry the exact
  // path the previous turn left in `writingPath` — same value, new turn, and
  // the follow must still fire.
  useEffect(() => {
    if (!writingPath || !followingRef.current) return;
    setSelection(followSelection(writingPath));
  }, [planTurnId, writingPath]);
  const selectManually = (sel: SpecSelection) => {
    followingRef.current = false;
    setSelection(sel);
  };

  // Default selection: while a DESIGN turn is producing design.cell, default
  // to Architecture (covers a reload mid-turn); otherwise the first
  // requirements file (the seeded PRD). A manual click sets `selection` and
  // always wins over this default.
  //
  // Keyed on the flow, not on an agent being in the room. This default is
  // reactive — it is recomputed on every render — so keyed on presence it
  // swapped the pane the moment ANY agent joined: a reader on the PRD with no
  // click recorded asked the agent a question, the pane became Architecture
  // for the length of the reply, and came back as a fresh editor at the top.
  // Reported as "the PRD scrolls when the agent says something" (#666). The
  // flow token comes from the project's status, so a reload mid-design-turn
  // still lands on Architecture; a chat, settle or aimed turn leaves the
  // reader where they were.
  const firstRequirements = files.find((f) => f.group === "requirements");
  // A fresh project may hold no requirements file yet; fall back to whatever
  // the spec view does list. Named for what it IS — any listed entry, which may
  // be a structured path (`openapi.yaml`, a component `design.json`,
  // `validation-criteria.json`) that renders as a read-only structured view
  // rather than in the editor. That is the right fallback: showing the one
  // artifact a bare project has beats showing an empty pane, and every path
  // reaching here has a renderer.
  //
  // What must never reach it is a REFERENCE — `toSpecEntry` drops those, which
  // is what keeps a v1 project's committed PDF out of the editor pane.
  const firstListed = files[0];
  const designTurnRunning = status.data?.spec.agentFlow === "design" && agentInRoom;
  const effectiveSelection: SpecSelection =
    selection ??
    (designTurnRunning && hasDesignCell
      ? { kind: "cell-diagram" }
      : firstRequirements
        ? { kind: "file", path: firstRequirements.path }
        : firstListed
          ? { kind: "file", path: firstListed.path }
          : { kind: "file", path: "" });

  // The concrete file entry when the selection is a file (else null: the
  // synthetic cell-diagram / wireframe views render their own panels).
  const selectedFile =
    effectiveSelection.kind === "file"
      ? (files.find((f) => f.path === effectiveSelection.path) ?? null)
      : null;

  // #252 Task 9: dependency-status cards only apply to the component
  // design.json view. componentOf() pulls the component name straight from
  // its `specs/design/components/<name>/design.json` path — the same name
  // ComponentDependencies.componentName carries (the design's own `name`
  // field, which the coding agent always sets equal to its directory).
  const selectedComponentName = selectedFile
    ? componentOf(selectedFile.path)
    : null;
  const componentDependencies = useMemo(
    () =>
      dependencies.data?.find((c) => c.componentName === selectedComponentName)
        ?.dependencies ?? [],
    [dependencies.data, selectedComponentName],
  );
  // Keyed by dependency name for DesignView's optional dependencyStatus prop
  // — status/reason are the ONLY fields this map carries. candidates/config
  // are already in the raw design.json DesignView parses itself; see
  // DesignViewProps.dependencyStatus's comment for why status/reason can't
  // join them.
  const dependencyStatus = useMemo<Record<string, DependencyStatusInfo>>(
    () =>
      Object.fromEntries(
        componentDependencies.map((d) => [
          d.name,
          { status: d.status, reason: d.reason },
        ]),
      ),
    [componentDependencies],
  );
  // #252 Task 15: cross-component "Used by" for the selected component's own
  // cards — computed across EVERY component's dependencies (dependencies.data
  // spans the whole project; componentDependencies above is only the
  // selected one), keyed by the selected component's own dependency names.
  // See dependencyUsedBy.ts's file header for why this is the annotation the
  // per-component design.json view gets, rather than the drawer's literal
  // one-card-per-shared-dependency merge (a single DesignView only ever
  // renders one component at a time, so there is nothing to merge here).
  const dependencyUsedBy = useMemo<Record<string, string[]>>(
    () =>
      selectedComponentName
        ? computeDependencyUsedBy(
            dependencies.data ?? [],
            selectedComponentName,
          )
        : {},
    [dependencies.data, selectedComponentName],
  );
  // Fires Task 5's seeded chat message with the dependency's FULL endpoint
  // entry (status/reason/candidates/config included) — never the
  // locally parsed one, which deliberately drops status/reason. `intent`
  // (#252 Task 17) is "resolve" from the design-view card's chat button on a
  // non-resolved dependency, or "reconsider" from its hamburger's "Discuss in
  // chat & modify" on an already-resolved one.
  const handleResolveDependency = (
    dependencyName: string,
    intent: DependencyResolutionIntent,
  ) => {
    if (!selectedComponentName) return;
    const dep = componentDependencies.find((d) => d.name === dependencyName);
    if (!dep) return;
    resolveDependencyViaChat(selectedComponentName, dep, intent);
  };

  // #252 Task 10: the build dependency drawer's "Resolve via chat" — same
  // seeded-message flow as handleResolveDependency above, but keyed off a
  // PreflightItem (component/dependency name) rather than the currently
  // selected component's design.json, since the drawer's items can span
  // ANY of the project's service components, not just the one selected in
  // the file tree. `intent` (#252 Task 17) is "resolve" from a blocker/
  // external-spec panel's chat button, or "reconsider" from an
  // external-config/platform-resource/org-service panel's hamburger.
  //
  // #252 Task 15: also closes the drawer, for BOTH intents. The drawer is a
  // MUI overlay Drawer (unlike the side-by-side chat panel AppLayout mounts —
  // see its own comment above `chatOpen`), so left open it covers the chat
  // panel the seeded message just opened and the user can't see what they're
  // supposed to respond to. Closing only happens here, on the explicit click —
  // NOT on turn-end (the useEffect above deliberately leaves the drawer open
  // and just refreshes its items; re-opening mid-resolution is out of scope,
  // matching Task 10's "do not auto-reopen" decision). The design-view
  // "Resolve in chat" cards (handleResolveDependency above) have no
  // equivalent occlusion: they render in the main content pane, which the
  // chat panel opens BESIDE (Collapse in AppLayout), never over.
  const handleResolveDrawerDependency = (
    item: PreflightItem,
    intent: DependencyResolutionIntent,
  ) => {
    const dep = (
      dependencies.data?.find((c) => c.componentName === item.component)
        ?.dependencies ?? []
    ).find((d) => d.name === item.dependency);
    if (!dep) return;
    resolveDependencyViaChat(item.component, dep, intent);
    setDependencyDrawerOpen(false);
  };

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
    typeof structuredLiveText === "string" &&
    structuredLiveText.trim().length > 0
      ? structuredLiveText
      : null;
  // The Security entry's own wiring lives in its hook — see useSecurityEntry
  // for why this page does not carry it.
  const isSecurityView = effectiveSelection.kind === "security";
  const security = useSecurityEntry({
    projectName,
    active: isSecurityView,
    files,
    collab,
    agentInRoom,
  });

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

  // Whether an agent is working on this project's spec RIGHT NOW, and how the
  // last attempt ended (#562). Read from `spec.agent` — the one status field
  // that is not derived from committed git, which is what makes it the only one
  // that can see a turn before it lands.
  //
  // It replaces a read of the flat `specStatus`, which never answered this: the
  // BFF only ever sets that to ""/draft/approved, so `deriving` meant "spec
  // files exist and none is versioned" and claimed an agent was shaping the
  // spec for every unversioned project on screen — while the one moment work
  // really is in flight, the kickoff, has no files at all and read as idle.
  const specAgent = status.data?.spec.agent;
  const deriving = specAgent === "working";
  // `agent` is PROJECT-wide — the newest turn of any flow — so it says an agent
  // is working, never which document. Only the kickoff can be named: with no
  // requirements file in the project there is nothing else a turn could be
  // writing, and it is the state this workspace has to explain (#562).
  //
  // The failure banner is scoped the same way, and for a sharper reason: it
  // was unreachable before this change (the flat `specStatus` only ever carried
  // ""/draft/approved), so unscoped it would newly pin a red alert across a
  // healthy published spec after any turn failed — a design pass, a chat reply
  // — until some later turn happened to succeed. A kickoff that died is the one
  // failure that leaves the user with nothing and no explanation.
  // The ONE reading of "this project has requirements" — the failure banner,
  // the rail input, and the design CTA (#159: design is derived FROM
  // requirements, so its CTA needs them first) all share it, so they cannot
  // drift into contradicting each other about the same files.
  const hasRequirementsFiles = files.some((f) => f.group === "requirements");
  // The status field cannot see a turn before its row exists (#635): submitted
  // interview answers travel through the chat's seed slot and take the dispatch
  // round-trip — seconds — to become a turn `spec.agent` reports. This browser
  // holds that evidence locally (seed waiting, dispatch in flight, stream being
  // folded), so a send counts as agent work from the moment it leaves the form;
  // otherwise the pane meets the gap with "Nothing written yet" plus a Retry
  // whose `/start` would supersede the interview it cannot see.
  const localTurnActivity = useLocalTurnActivity(
    orgHandle ?? "default",
    projectName,
  );
  // `failed` yields to that same evidence: `spec.agent` keeps reading "failed"
  // until the retry's own turn has a row, so unguarded the banner would sit
  // through the retry's dispatch offering a SECOND Retry against the send it
  // already fired — while the rail beside it pulses working. If the send dies,
  // its claim releases (or the seed's TTL lapses) and the banner returns.
  const failed =
    specAgent === "failed" && !hasRequirementsFiles && !localTurnActivity;
  // Retrying is a SEND, so it goes where every other send goes: the chat's
  // one-shot seed slot, GUARDED — the panel re-decides after it has rehydrated,
  // which is the first moment "is the agent mid-exchange" is knowable here.
  const retryStart = () =>
    setPendingSeed(
      chatKeyFor(orgHandle ?? "default", projectName),
      START_COMMAND,
      true,
    );
  // The design gate: Build arms once design files are generated (#80).
  const hasDesignFiles = files.some((f) => f.group === "designs");
  // The committed PRD, read for the Build drawer's story preview. It used to
  // also feed an Open Questions gate on Generate design (#365/#372); open
  // questions gate nothing now (#539), and the launchers that answered them
  // live on the document itself (#579).
  const prdEntry = files.find((f) => f.path === PRD_PATH) ?? null;
  const prdContent = useSpecFileContent(
    projectName,
    prdEntry ? { path: prdEntry.path, sha: prdEntry.sha } : null,
  );
  const cutPreview = useMemo(() => {
    const stories = prdContent.data
      ? parsePrdStories(prdContent.data.content)
      : [];
    return { stories, nextVersion: nextVersionLabel(tags.data?.latest) };
  }, [prdContent.data, tags.data?.latest]);

  // What the rail says (#575). Derived here rather than inside the rail so the
  // rules stay testable without a workspace — and so the two facts the rail
  // cannot see for itself (whether the requirements have moved since the design
  // was derived, and how much of the document is still unsettled) arrive as
  // plain inputs.
  // Read the LIVE document, not the committed copy. The committed one is a
  // collab flush behind — so deleting an `*assumed*` flag left the alert up
  // until the server next wrote, and on the agent's own edits that lag was long
  // enough to look broken. Falls back to the committed content when the room is
  // offline, which is the only time it is the freshest thing available.
  const prdFragment = collab.getFileFragment(PRD_PATH);
  const prdVersion = useYFragmentVersion(prdFragment);
  const livePrd = useMemo(() => {
    // The fragment is mutated IN PLACE, so its identity never changes and only
    // the counter marks that its content did — reading it here is what makes
    // that a real dependency rather than one the linter can dismiss.
    void prdVersion;
    return prdFragment ? fragmentToMarkdown(prdFragment) : null;
  }, [prdFragment, prdVersion]);
  const unsettled = useMemo(
    () => prdUnsettled(livePrd ?? prdContent.data?.content),
    [livePrd, prdContent.data],
  );
  // The plan's entries sorted into rail sections (#576). `specGroupOf` is the
  // same folder rule the committed files go through, so a planned path and the
  // file it becomes can never disagree about where they belong.
  const planEntries = useMemo<RailPlanEntry[]>(
    () =>
      (plan?.entries ?? []).map((e) => {
        const group = specGroupOf(e.path);
        return {
          path: e.path,
          status: e.status,
          section: group === "designs" ? "design" : group,
        };
      }),
    [plan],
  );
  // The selected path when the plan says a document is coming but the room has
  // not delivered it yet. Any status EXCEPT a failed one counts while the turn
  // runs: a body only reaches the doc when its write executes (and some bodies
  // stream in earlier than others), so `done` can lead the room by a beat. Once
  // the turn ends, a still-missing file is a real absence and the honest
  // "Select a file" below takes over.
  const pendingPlanPath =
    plan?.turnActive &&
    effectiveSelection.kind === "file" &&
    !files.some((f) => f.path === effectiveSelection.path) &&
    plan.entries.some(
      (e) => e.path === effectiveSelection.path && e.status !== "error",
    )
      ? effectiveSelection.path
      : null;

  const railSections = useMemo(
    () =>
      buildRailSections({
        hasRequirements: hasRequirementsFiles,
        hasDesign: files.some((f) => f.group === "designs"),
        hasValidation: files.some((f) => f.group === "validation"),
        agentWorking: deriving || localTurnActivity,
        agentFlow: status.data?.spec.agentFlow ?? "",
        designOutdated: status.data?.spec.designOutdated ?? false,
        assumptions: unsettled.assumptions,
        openQuestions: unsettled.openQuestions,
        planEntries,
        planWreckage: plan?.wreckage ?? false,
      }),
    [
      files,
      hasRequirementsFiles,
      deriving,
      localTurnActivity,
      status.data?.spec.agentFlow,
      status.data?.spec.designOutdated,
      unsettled,
      planEntries,
      plan?.wreckage,
    ],
  );
  // The rail's own answer to "is an agent writing the requirements", reused so
  // the workspace body cannot contradict the rail beside it — and its reasons,
  // reused so the warning before a design run cannot drift from the rail's own
  // account of what is unsettled.
  const requirementsSection = railSections.find(
    (sec) => sec.id === "requirements",
  );
  const requirementsActive = requirementsSection?.state === "active";
  // Written nothing, and nothing on the way. `!deriving` is not redundant with
  // `!requirementsActive`: a KNOWN non-requirements flow on an empty project —
  // `/design` typed into chat before any kickoff landed — marks Design active,
  // not Requirements, and without this guard the empty state would offer Retry
  // against that running turn (#629's failure, through the one flow the rail
  // attributes elsewhere).
  const nothingToShow =
    files.length === 0 && !requirementsActive && !deriving && !failed;

  // A reason row is a pointer to where the work already happens: the settle
  // controls live on the requirements document's own flagged lines, and a stale
  // design is repaired by the same re-derivation the header offers.
  // Going to the document means going to the LINE: the first flagged one,
  // scrolled into view, so "Review them first" is not "here is a long
  // document, find them yourself".
  const [revealUnsettled, setRevealUnsettled] = useState(0);
  const onRailReason = (action: SectionReason["action"]) => {
    if (action === "update-design") {
      generateDesign();
      return;
    }
    selectManually({ kind: "file", path: PRD_PATH });
    setRevealUnsettled((n) => n + 1);
  };

  const seedChat = (message: string) =>
    setPendingSeed(chatKeyFor(orgHandle ?? "default", projectName), message);

  // Generate/Re-generate design (#159): open the agent panel and auto-send the
  // design turn via the shared ?generate=design signal (AppLayout + the panel).
  const runDesign = () =>
    void navigate({
      to: "/projects/$projectName/spec",
      params: { projectName },
      search: { generate: "design" },
    });

  // Deriving a design from requirements the agent is still guessing at is
  // allowed — it is how this product is meant to be used, and gating it was
  // tried and removed (#539). But it has a cost the user cannot see from the
  // button: the design is built on those guesses, and overturning one later
  // means deriving again. So the click WARNS and goes on, rather than asking
  // for permission. The rail already says the same thing at rest; this is the
  // moment it becomes consequential.
  const unsettledReasons = requirementsSection?.reasons ?? [];
  const generateDesign = () => {
    if (unsettledReasons.length === 0) {
      runDesign();
      return;
    }
    setConfirmDesign(true);
  };

  // An agent turn is in flight iff an agent peer is present in the room (#86 d7
  // renders them with kind:"agent"). Building a half-written design is wrong,
  // so Build is disabled — with a tooltip — while one is working (#162).
  const agentBusy = collab.peers.some((p) => p.kind === "agent");

  // Keep this browser's chat log fed WITHOUT the chat panel (#606).
  //
  // The question form is the room's, but the fact that a question is pending
  // reaches the room via `useRoomQuestion`, which reads the chat log — and the
  // log used to be filled only while `AgentChatPanel` was mounted. So a member
  // opening a spec link cold had no log, nothing mirrored, and this workspace
  // showed "Nothing written yet" plus a Retry while the agent stood waiting on
  // their answers. Mounting the log here removes the panel from that path.
  //
  // Same "default" org fallback as the chatKey above, not the room's "acme".
  const { resync: resyncConversation } = useConversationLog(
    orgHandle ?? "default",
    projectName,
  );
  // Turn-end, observed rather than polled. The agent joins the room as a peer
  // while it works and leaves when the turn ends, so its DEPARTURE is the exact
  // moment the thread gained something — a question, or the answer to one.
  // `subscribeTurnEnd` cannot serve this: it fires from the panel's fold, which
  // is precisely what is absent here. Edge-triggered off a ref rather than a
  // state flag: this must fire on the falling edge only, never on mount into an
  // already-idle project, where the query's own mount read has it covered.
  const agentWasInRoom = useRef(false);
  useEffect(() => {
    if (agentBusy) {
      agentWasInRoom.current = true;
      return;
    }
    if (!agentWasInRoom.current) return;
    agentWasInRoom.current = false;
    resyncConversation();
  }, [agentBusy, resyncConversation]);
  // A standing question form owns the turn: the agent's turn ended ON the
  // question, so `agentBusy` is false while the work is very much unfinished.
  // Since the interview is uncapped (#578) the PRD now exists from the first
  // round on, which is what makes the header's requirements-gated launchers
  // reachable mid-interview — and firing one supersedes the live questions,
  // handing the agent's own assumptions back as the user's answers.
  const awaitingAnswers = Boolean(roomQuestion && roomDoc);
  // A lens fired while the agent already holds the turn would be refused by the
  // composer anyway, and firing one mid-interview supersedes the live question
  // form for the whole room — so the lenses go inert for the same two reasons
  // the header's launchers do, and say which one.
  // Aiming the agent at a selection (#666). A turn fired from the DOCUMENT,
  // which the chat panel cannot dispatch for us: it is mounted `unmountOnExit`,
  // so while it is closed — the whole point of a quiet Change — the hook that
  // owns `send` does not exist.
  const anchoredTurn = useAnchoredTurn(orgHandle ?? "default", projectName);
  const aimSend = async (
    instruction: string,
    anchor: Anchor,
    intent: "change" | "discuss",
  ): Promise<boolean> => anchoredTurn.send(instruction, { anchor, intent });

  const lensBusyReason = specTurnGate({ agentBusy, localTurnActivity, awaitingAnswers });

  // Build (#162, #164): commit the room's live edits FIRST (POST /build tags
  // HEAD), then check preflight. Only a RESOLUTION blocker — a dependency
  // whose identity the design cannot settle — routes to the drawer instead of
  // building: there is nothing to cut a version against until it is named.
  // Everything else preflight reports rides along: the platform-resource /
  // org-service approvals go out with the build request, and an external
  // dependency's config VALUES are collected on the Builds page while the
  // coding agent runs, then enforced at the deploy gate. So a project that
  // only needs values goes straight to the cut-version ceremony.
  const onBuild = () => {
    setBuildError(null);
    setBuildPhase("committing");
    void (async () => {
      try {
        await collab.flush(); // no-op when offline
        // The cut drawer previews stories from the COMMITTED PRD; the flush
        // may just have landed edits, so refresh the file listing (whose shas
        // drive the PRD content query) before showing what the click will do.
        await spec.refetch();
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
        // Stashed on EVERY path, not just the drawer's: runBuild derives the
        // build request's approval inputs from these items.
        setPreflightItems(data.items ?? []);
        if (data.needsResolution) {
          setDependencyDrawerOpen(true);
          return;
        }
        setCutDialogOpen(true);
      } catch (e) {
        setBuildError(
          e instanceof Error ? e.message : "Failed to start the build.",
        );
      } finally {
        setBuildPhase(null);
      }
    })();
  };

  // Where a started build lands the reader: the version's own page when the
  // response named a tag, the ledger when it did not (`tag` is optional on
  // BuildResponse, and a build with no tag has no page to land on).
  //
  // The overview used to be the answer and stopped being one twice over. The
  // version's story moved to its own page (console ADR-0021), and ADR-0023 gave
  // that page a job: Build no longer collects an external dependency's values,
  // so the External resources section there is where they are supplied — and
  // the run parks at the deploy gate until they are. Landing on the overview
  // would put a navigation between the reader and the one action their run is
  // waiting on.
  const goToBuild = (tag: string | undefined) => {
    if (tag) {
      void navigate({
        to: "/projects/$projectName/builds/$tag",
        params: { projectName, tag },
      });
      return;
    }
    void navigate({
      to: "/projects/$projectName/builds",
      params: { projectName },
    });
  };

  // The ceremony's confirm: POST the build, carrying the approvals preflight
  // raised (the platform resources it will provision) — the drawer used to
  // submit those and no longer opens for them. A 422 refusal renders as the
  // gate checklist, anything else as the plain build error.
  const runBuild = () => {
    setCutDialogOpen(false);
    setGateRefusal(null);
    setBuildError(null);
    setBuildPhase("building");
    void (async () => {
      try {
        const res = await build.mutateAsync({
          inputs: approvalInputsFor(preflightItems),
        });
        goToBuild(res.tag);
      } catch (e) {
        const details = (
          e as Error & { details?: Array<{ field?: string; message: string }> }
        ).details;
        if (Array.isArray(details) && details.length > 0) {
          setGateRefusal(details);
        } else {
          setBuildError(
            e instanceof Error ? e.message : "Failed to start the build.",
          );
        }
      } finally {
        setBuildPhase(null);
      }
    })();
  };

  // Drawer Continue (#164): resubmit the build with the resolution the drawer
  // collected (a pasted external spec) plus the same approvals runBuild
  // sends. A clean response closes the drawer and moves on to the version;
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
      goToBuild(res.tag);
    } catch (e) {
      setBuildError(
        e instanceof Error ? e.message : "Failed to start the build.",
      );
    } finally {
      setBuildPhase(null);
    }
  };

  // Version state rendered as SOFT status chips beside the title (like the
  // builds/deployments headers), so the spec page reads as part of the same
  // family instead of its own bespoke layout. Soft chips don't read as
  // buttons, so this doesn't reintroduce the #117 "looks clickable" problem.
  // No project-name subtitle: the top-bar project switcher already names the
  // project, so repeating it here is redundant.
  const publishedTag = tags.data?.latest;
  const hasDraftChanges = Boolean(tags.data?.specDirty);
  const isOffline = collab.status === "offline";

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
    <PageContent
      fullWidth
      noPadding
      sx={{ height: "100%", display: "flex", flexDirection: "column" }}
    >
      <Box
        sx={{
          height: "100%",
          minHeight: 0,
          display: "flex",
          flexDirection: "column",
        }}
      >
        {/* Header — the same height as the agent panel's, which sits beside
            it: one title bar across the top of the workspace, not two. The
            panel's header is 48px (its small controls plus the padding), so
            this one pins the same minimum and uses the same small controls. */}
        <Box
          sx={{
            px: 2,
            minHeight: 48,
            borderBottom: 1,
            borderColor: "divider",
            display: "flex",
            alignItems: "center",
            gap: 2,
            bgcolor: "background.paper",
          }}
        >
          <IconButton
            size="small"
            aria-label="Back to project overview"
            onClick={() =>
              void navigate({
                to: "/projects/$projectName",
                params: { projectName },
              })
            }
          >
            <ArrowLeft size={18} />
          </IconButton>
          <Box sx={{ flexGrow: 1, minWidth: 0 }}>
            <Stack direction="row" spacing={1.5} sx={{ alignItems: "center" }}>
              <Typography variant="h4" noWrap>
                Spec
              </Typography>
              {publishedTag && (
                <StatusChip
                  label={`${publishedTag} · published`}
                  tone="success"
                  appearance="soft"
                  dot
                />
              )}
              {hasDraftChanges && (
                <StatusChip
                  label="draft changes"
                  tone="warning"
                  appearance="soft"
                  dot
                />
              )}
              {/* `offline`, not `solo session` — the lexicon retired the latter
                  (it reads like a focus feature), and the tooltip names the
                  user's situation rather than which service is down. */}
              {isOffline && (
                <Tooltip title="Live editing is unavailable — showing the last committed version. Reconnecting…">
                  <Box sx={{ display: "inline-flex" }}>
                    <StatusChip
                      label="offline"
                      tone="neutral"
                      appearance="soft"
                    />
                  </Box>
                </Tooltip>
              )}
            </Stack>
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
          <Divider orientation="vertical" flexItem />

          {/* Phase-aware primary CTA (#159): the prominent action is always the
              next pipeline step — Generate design until a design exists, then
              Build. A dead disabled Build hid what to do next. */}
          {hasDesignFiles ? (
            <>
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
                    size="small"
                    variant="contained"
                    startIcon={<Hammer size={16} />}
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
              <Tooltip
                title={
                  agentBusy
                    ? "An agent is still working — Generate design is available once it finishes"
                    : awaitingAnswers
                      ? "The agent is waiting on your answers — finish the questions below first"
                      : hasRequirementsFiles
                        ? "Derive the component design from your requirements"
                        : "Generate requirements first"
                }
              >
                {/* span so the tooltip works while the button is disabled */}
                <span>
                  <Button
                    size="small"
                    variant="contained"
                    startIcon={<Sparkles size={16} />}
                    disabled={
                      !hasRequirementsFiles || agentBusy || awaitingAnswers
                    }
                    onClick={generateDesign}
                  >
                    Generate design
                  </Button>
                </span>
              </Tooltip>
            </>
          )}
        </Box>

        {/* The one state that needs a way out, and the only one that can be
            KNOWN rather than inferred (#562 retest). `failed` is a positive
            fact off the turn record — a turn that started and then died — so
            unlike "the workspace looks empty" it can never be true for a
            moment while something is actually working. That is why the action
            lives here and nowhere else: gated on an absence, it appeared
            during the kickoff, which is precisely when the user must not be
            invited to restart it.

            The old copy told the user to go type in the chat, which #530
            forbids — a command the UI can offer as a control is offered. */}
        {failed && (
          <Alert
            severity="error"
            sx={{ borderRadius: 0 }}
            action={
              <Button color="inherit" size="small" onClick={retryStart}>
                Retry
              </Button>
            }
          >
            <AlertTitle>The agent couldn't write your requirements</AlertTitle>
            Nothing was lost — anything already written stays browsable.
          </Alert>
        )}

        {/* Deriving a design from requirements that are still full of the
            agent's own guesses. Same component as the build refusal below, and
            deliberately NOT the same kind of thing: that one enforces and this
            one informs, which is the whole reason it carries a way past. */}
        <ProblemsDialog
          open={confirmDesign}
          title="Some decisions are still yours"
          // In the user's words, not ours: "settled", "derived" and "judgment"
          // are how we talk about the document, not how they read it. What
          // they need at this click is what the agent did (decided things,
          // marked them), what happens next (the design builds on them), and
          // what it costs to be wrong (generating again).
          intro={designWarningIntro(unsettledReasons)}
          // No per-row fix here, unlike the build refusal: every one of these is
          // settled in the same place, and `Resolve issues` already goes there.
          // A row link beside it would be a second button to the same document.
          problems={unsettledReasons.map((reason) => ({
            key: reason.key,
            label: reason.label,
          }))}
          resolve={{
            label: "Review them first",
            run: () => onRailReason("document"),
          }}
          proceed={{ label: "Generate anyway", run: runDesign }}
          onClose={() => setConfirmDesign(false)}
        />

        {/* The build gate's refusal (#372) now reads the way the rail's amber
            sections do (#575): one dialog listing what is unmet, each with the
            way to fix it. Same kind of thing, same presentation — and these
            lists run long enough (several components, each missing something)
            that a header strip pushed the workspace down to hold them.
            Build stays available; the same click re-checks. */}
        <ProblemsDialog
          open={gateRefusal !== null}
          title="Not ready to build yet"
          problems={(gateRefusal ?? []).map((d, i) => ({
            key: `${d.field ?? ""}:${i}`,
            label: d.field ? `${d.field}: ${d.message}` : d.message,
            // One handoff for the whole list rather than per row: these
            // conditions are derived from each other, so the repair is a design
            // pass over the set, not a fix per line.
            fix:
              i === 0
                ? {
                    label: "Fix via chat",
                    run: () =>
                      seedChat(
                        "/design Fix these build-gate refusals:\n" +
                          (gateRefusal ?? [])
                            .map(
                              (r) =>
                                `- ${r.field ? `${r.field}: ` : ""}${r.message}`,
                            )
                            .join("\n"),
                      ),
                  }
                : undefined,
          }))}
          onClose={() => setGateRefusal(null)}
        />

        {/* The "Cut version" ceremony (#369/#372): what the Build click does,
            before it does it. The version shown is predictive — the BACKEND
            assigns the real tag at cut time. */}
        <Dialog
          data-testid="cut-version-dialog"
          open={cutDialogOpen}
          onClose={() => setCutDialogOpen(false)}
          maxWidth="xs"
          fullWidth
        >
          <DialogTitle>Cut version {cutPreview.nextVersion}</DialogTitle>
          <DialogContent>
            <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
              Snapshots the PRD and design together as a git tag; the build runs
              against that snapshot, so you can keep editing afterwards.
            </Typography>
            <Stack spacing={0.5}>
              <Typography variant="body2">
                <b>Stories in scope:</b>{" "}
                {cutPreview.stories.length > 0
                  ? cutPreview.stories.join(", ")
                  : "—"}
              </Typography>
              <Typography variant="body2">
                <b>Milestone:</b>{" "}
                {`"${cutPreview.nextVersion}" — one per version, holding this build's tasks`}
              </Typography>
            </Stack>
          </DialogContent>
          <DialogActions>
            <Button onClick={() => setCutDialogOpen(false)}>Cancel</Button>
            <Button variant="contained" onClick={runBuild}>
              Cut {cutPreview.nextVersion} &amp; build
            </Button>
          </DialogActions>
        </Dialog>

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

        {/* Committer flush failure (D6): background or on-demand apply failed. */}
        {collab.flushError && (
          <Alert
            severity="error"
            sx={{ borderRadius: 0 }}
            onClose={() => collab.clearFlushError()}
          >
            {collab.flushError}
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
              action={
                <Button onClick={() => void spec.refetch()}>Retry</Button>
              }
            >
              Failed to load the spec
              {spec.error instanceof Error && spec.error.message
                ? `: ${spec.error.message}`
                : ""}
            </Alert>
          </Box>
        ) : roomQuestion && roomDoc ? (
          /* Collab question form (spike): a LIST of agent questions takes over
             the body — every room participant sees it and co-authors, and any
             of them submits (#430 D5). */
          <SpecQuestionForm
            doc={roomDoc}
            entry={roomQuestion}
            org={orgHandle ?? "default"}
            projectName={projectName}
          />
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
                onSelect={selectManually}
                onRegenerateDesign={generateDesign}
                regenerateDisabled={agentBusy}
                sections={railSections}
                plan={planEntries}
                onReason={onRailReason}
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
                <CellDiagramPanel
                  projectName={projectName}
                  files={files}
                  collab={collab}
                />
              ) : effectiveSelection.kind === "security" ? (
                <SecurityPanel
                  securityJson={security.securityJson}
                  live={security.live}
                  isPending={security.isPending}
                  isError={security.isError}
                />
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
                      <DesignView
                        design={structuredLive}
                        dependencyStatus={dependencyStatus}
                        dependencyUsedBy={dependencyUsedBy}
                        onResolveDependency={handleResolveDependency}
                      />
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
                        dependencyStatus={dependencyStatus}
                        dependencyUsedBy={dependencyUsedBy}
                        onResolveDependency={handleResolveDependency}
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
                        Waiting for the agent to write{" "}
                        {selectedFile.path.split("/").at(-1)}…
                      </Typography>
                    </Box>
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
                ) : fragment && collab.provider ? (
                  // Markdown gets the Tiptap editor on the file's
                  // Y.XmlFragment (#86 phase 6).
                  <SpecMdEditor
                    key={`${selectedFile.path}:md`}
                    fragment={fragment}
                    provider={collab.provider}
                    self={collab.self}
                    agentStreaming={agentBusy}
                    lenses={
                      selectedFile.path === PRD_PATH
                        ? { run: seedChat, busyReason: lensBusyReason }
                        : undefined
                    }
                    // Every markdown file, not just the PRD: a selection is
                    // something any document has, so aiming cannot be one
                    // document's privilege the way its lenses are.
                    revealUnsettled={selectedFile.path === PRD_PATH ? revealUnsettled : 0}
                    aim={{
                      path: selectedFile.path,
                      send: aimSend,
                      busyReason: anchoredTurn.ready
                        ? lensBusyReason
                        : "Still opening this project's conversation",
                    }}
                    links={{
                      path: selectedFile.path,
                      knownPaths: specPaths,
                      open: (path) => selectManually({ kind: "file", path }),
                    }}
                  />
                ) : ytext ? (
                  <CollabTextArea
                    key={`${selectedFile.path}:collab`}
                    ytext={ytext}
                    path={selectedFile.path}
                    isLocalTransaction={collab.isLocalTransaction}
                  />
                ) : (
                  /* The room is not the source for this file — it is
                     unreachable, or it genuinely does not hold the path. Git
                     is, and it is read-only there (#586). */
                  <CommittedFileView
                    key={`${selectedFile.path}:committed`}
                    path={selectedFile.path}
                    content={content.data?.content ?? null}
                    errorMessage={
                      content.isError
                        ? content.error instanceof Error &&
                          content.error.message
                          ? content.error.message
                          : "The workspace could not be reached."
                        : undefined
                    }
                    onRetry={() => void content.refetch()}
                    offline={isOffline}
                  />
                )
              ) : failed ? null /* The alert above owns this case: it names the
                   failure and carries the one Retry. A body beneath it would
                   either repeat that offer or, as it did, invite the user to
                   "select a file" in a workspace that has none. */ : nothingToShow ? (
                /* Nothing written and nothing running — a project whose
                   kickoff never landed, or one sitting between turns with no
                   document yet. Either way the workspace will not fill itself,
                   so it offers the same Retry the failure banner does: one way
                   out, one word for it. Guarded, so the panel drops it if the
                   agent turns out to be mid-exchange. */
                <EmptyState
                  title="Nothing written yet"
                  description="Your requirements and design appear here as the agent writes them."
                  action={
                    <Button variant="contained" onClick={retryStart}>
                      Retry
                    </Button>
                  }
                />
              ) : requirementsActive || (files.length === 0 && deriving) ? (
                /* An agent is writing the requirements right now — the same
                   fact the rail pulses on, so the two surfaces cannot
                   disagree. They did: this said "Agent is working on the
                   requirements document" whenever the workspace was empty,
                   including between turns when the rail correctly showed the
                   section as not started. Same centred-spinner shape the
                   architecture pane uses while a design turn runs.

                   The requirements are NAMED only when the rail attributes the
                   turn to them: a turn the rail places elsewhere (a `/design`
                   run before any kickoff landed) or cannot place still keeps
                   the empty pane honest with a plain "working", rather than
                   claiming a document it may not be writing — or, worse,
                   falling through to the Retry beneath (#629). */
                failed ? null : (
                  <Box
                    sx={{
                      height: "100%",
                      display: "flex",
                      flexDirection: "column",
                      gap: 2,
                      alignItems: "center",
                      justifyContent: "center",
                    }}
                  >
                    <CircularProgress
                      size={28}
                      aria-label={
                        requirementsActive
                          ? "Agent is writing the requirements"
                          : "Agent is working"
                      }
                    />
                    <Typography variant="body2" color="text.secondary">
                      {requirementsActive
                        ? "Agent is working on the requirements document"
                        : "Agent is working"}
                    </Typography>
                  </Box>
                )
              ) : pendingPlanPath ? (
                /* Following the write reached this document before the room
                   did (#576, ADR-0026). A write is announced when its tool
                   input resolves a path, but only SOME bodies stream into the
                   doc as they are typed — a component `design.json` arrives
                   whole, when the call executes. In that window the file is not
                   in `files` yet, so the pane fell through to "Select a file",
                   a dead end at the exact moment this feature exists to serve:
                   watching a new document land. */
                <Box
                  sx={{
                    height: "100%",
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "center",
                  }}
                >
                  <Typography variant="body2" color="text.secondary">
                    Waiting for the agent to write {fileLabel(pendingPlanPath)}…
                  </Typography>
                </Box>
              ) : (
                /* Files exist but the selection names none of them — a stale
                   manual pick whose file has since gone. The default selection
                   always lands on a real file, so this is the only way here. */
                <Typography variant="body2" color="text.secondary">
                  Select a file to view its content.
                </Typography>
              )}
            </Box>
          </Box>
        )}
      </Box>

      <BuildDependencyDrawer
        open={dependencyDrawerOpen}
        items={preflightItems}
        submitting={dependencyDrawerOpen && buildPhase === "building"}
        onClose={() => setDependencyDrawerOpen(false)}
        onContinue={(inputs) => void onContinueBuild(inputs)}
        onResolveDependency={handleResolveDrawerDependency}
      />
    </PageContent>
  );
}
