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

import type React from "react";
import { useState } from "react";
import {
  Box,
  Chip,
  Collapse,
  IconButton,
  List,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  Stack,
  Tooltip,
  Typography,
} from "@wso2/oxygen-ui";
import {
  Boxes,
  Check,
  ChevronDown,
  ChevronRight,
  Database,
  FileText,
  RefreshCw,
  Network,
  LayoutDashboard,
  ShieldCheck,
  TriangleAlert,
  Workflow,
} from "@wso2/oxygen-ui-icons-react";
import { WorkingPulse } from "../../agent-chat/components/WorkingIndicator";
import { PRD_PATH, type SpecFileEntry } from "../api/mapping";
import { fileLabel } from "../api/labels";
import {
  mostSignificant,
  reasonCount,
  type RailPlanEntry,
  type RailSection,
  type SectionReason,
} from "../lib/railSections";
import { ProblemsDialog } from "./ProblemsDialog";
import {
  buildDesignSection,
  selectionKey,
  DESIGN_CELL_PATH,
  DOMAIN_MODEL_PATH,
  SECURITY_JSON_PATH,
  type SpecSelection,
} from "../api/designTree";

function fileSel(path: string): SpecSelection {
  return { kind: "file", path };
}

export function SpecFileList({
  files,
  selection,
  onSelect,
  onRegenerateDesign,
  regenerateDisabled,
  sections,
  plan,
  onReason,
}: {
  files: SpecFileEntry[];
  selection: SpecSelection | null;
  onSelect: (sel: SpecSelection) => void;
  /** Re-generate the design (#159) — shown in the Designs header once a design
   *  exists; fires the same design-generation room turn as the header CTA. */
  onRegenerateDesign: () => void;
  /** Disabled while an agent turn runs — a re-generate would be dropped mid-turn. */
  regenerateDisabled?: boolean;
  /** The rail's own state per section (#575) — what is ready, being worked on,
   *  wanting attention, or not begun, plus why. */
  sections: RailSection[];
  /** The declared plan's entries (#576): ghosts for what is coming, a pulse on
   *  what is being written, an error mark on what died. Empty when no plan is
   *  live and no wreckage stands. */
  plan?: RailPlanEntry[];
  /** A reason row was clicked: open the requirements document, or re-derive. */
  onReason: (action: SectionReason["action"]) => void;
}) {
  const sectionOf = (id: RailSection["id"]) =>
    sections.find((sec) => sec.id === id) ?? {
      id,
      title: id,
      state: "not-started" as const,
      reasons: [],
    };
  const selKey = selection ? selectionKey(selection) : null;
  const isSel = (sel: SpecSelection) => selKey === selectionKey(sel);

  // The declared plan (#576). A planned path with no committed file yet is a
  // GHOST: it takes a row where the file will live — same grouping rules, so
  // the list never re-arranges when the write lands — but is disabled, because
  // a control that selects nothing is worse than prose. The moment the write
  // starts, the path exists in the live doc, the row becomes real, and the
  // status pulses on it.
  const planByPath = new Map((plan ?? []).map((e) => [e.path, e.status]));
  const committed = new Set(files.map((f) => f.path));
  const ghosts: SpecFileEntry[] = (plan ?? [])
    .filter((e) => e.section !== null && !committed.has(e.path))
    .map((e) => ({
      path: e.path,
      sha: "",
      group: e.section === "design" ? ("designs" as const) : (e.section as "requirements" | "validation"),
    }));
  // Merged in PATH order, not appended: a ghost has to sit where its file will
  // sit, or the row hops up the list the moment the write lands — the visible
  // re-arrangement holding a place was supposed to prevent. `files` arrives
  // path-sorted and the group sorts below are stable, so one sort here is
  // enough for every group.
  const allFiles = [...files, ...ghosts].sort((a, b) => a.path.localeCompare(b.path));


  // The PRD leads, whatever it sorts as. Everything else under Requirements
  // elaborates it — a feature file is depth on a story the PRD defines — and on
  // path alone `features/…` sorts ABOVE `prd.md`, burying the document the
  // whole flow is written against beneath its own footnotes. `files` arrives
  // path-sorted and sort is stable, so the rest keeps that order.
  const requirements = allFiles
    .filter((f) => f.group === "requirements")
    .sort((a, b) => Number(b.path === PRD_PATH) - Number(a.path === PRD_PATH));
  const validation = allFiles.filter((f) => f.group === "validation");
  const design = buildDesignSection(allFiles);

  // Per-component expand/collapse — default expanded, remembered by name so
  // toggling one component survives unrelated re-derivations of the list.
  const [collapsedComponents, setCollapsedComponents] = useState<Set<string>>(
    () => new Set(),
  );
  const toggleComponent = (name: string) => {
    setCollapsedComponents((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  };
  // The Flows group collapses like a component group; both start open.
  const [flowsCollapsed, setFlowsCollapsed] = useState(false);

  // The design section has content to show (and a design to re-generate)
  // once any of its documents, flows or components exist.
  const hasDesign =
    design.hasCellDsl ||
    design.overview.length > 0 ||
    design.flows.length > 0 ||
    design.hasComponents;

  // "Not created yet" — flat, and true. The old note claimed agents were
  // "being derived…" over sections nobody had asked for yet, which stated
  // something untrue about what the platform was doing. Active wording now
  // lives in the section's own state, where it is earned.
  const emptyNote = "Not created yet";

  // The section header's ornament. Work in progress is the app's existing
  // pulse — the same 8px dot the chat uses — so "working" looks identical
  // everywhere rather than growing a second animation per surface.
  // Colour comes from the THEME, through `currentColor` on a wrapper — not from
  // MUI's palette CSS variables, which this theme does not define at all
  // (`--mui-palette-success-main` resolves to nothing here). Passing them as an
  // icon `color` silently produced an unstyled mark, so every state read the
  // same shade and the rail's states were not distinguishable.
  const ornament = (state: RailSection["state"]) => {
    if (state === "active") return <WorkingPulse />;
    // Nothing for `attention`: that state always carries a chip, and the chip
    // already leads with a warning triangle. Both together put two identical
    // marks a few pixels apart, which reads as two problems rather than one.
    if (state !== "ready") return null;
    return (
      <Box sx={{ display: "flex", flexShrink: 0, color: "success.main" }}>
        <Check size={14} />
      </Box>
    );
  };

  const sectionHeader = (section: RailSection, action?: React.ReactNode) => (
    <Box
      sx={{
        px: 2,
        py: 0.5,
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        gap: 1,
      }}
    >
      <Stack direction="row" spacing={0.75} sx={{ alignItems: "center", minWidth: 0 }}>
        <Typography
          variant="overline"
          color={
            section.state === "attention"
              ? "warning.main"
              : section.state === "active"
                ? "primary.main"
                : section.state === "not-started"
                  ? "text.disabled"
                  : "text.secondary"
          }
          noWrap
        >
          {section.title}
        </Typography>
        {ornament(section.state)}
        {/* The count, not just the mark: three assumptions and one would
            otherwise look identical, and "how much" is the thing a glance is
            for. The hover carries the most significant one so a peek costs no
            click; the click carries all of them. */}
        {/* Gated on the STATE, not on `reasons.length`. The model keeps a
            section's reasons while an agent works on it — SpecView reads them
            for the design warning — but the rail must not show them: an agent
            re-deriving a stale design is already resolving it, and warning
            about the thing being fixed while it is being fixed reads as a
            fault. `attention` is the only state this chip belongs to; the other
            three carry no reasons or, when active, deliberately do not show
            them. */}
        {/* The denominator (#576): how far the declared plan has come. Answers
            "how long do I wait" — honest BECAUSE it grows in waves; it counts
            only what the turn actually declared. */}
        {section.progress && (
          <Typography
            variant="caption"
            color="text.secondary"
            sx={{ flexShrink: 0, fontVariantNumeric: "tabular-nums" }}
          >
            {section.progress.done} of {section.progress.total}
          </Typography>
        )}
        {section.state === "attention" && section.reasons.length > 0 && (
          <Tooltip title={mostSignificant(section.reasons)?.label ?? ""}>
            <Chip
              size="small"
              color="warning"
              variant="outlined"
              icon={<TriangleAlert size={12} />}
              label={reasonCount(section.reasons)}
              onClick={() => setProblemsFor(section)}
              aria-label={`${section.title}: ${reasonCount(section.reasons)} to resolve`}
              sx={{
                height: 20,
                cursor: "pointer",
                // MUI's small chip gives its icon a 4px leading margin and its
                // label 8px of trailing padding, so the pill was lopsided: the
                // triangle sat almost against the left border while the count
                // had twice the room on the right. Even it up.
                "& .MuiChip-icon": { ml: 0.75, mr: -0.25 },
              }}
            />
          </Tooltip>
        )}
      </Stack>
      {action}
    </Box>
  );

  // Which section's problems are open, if any. Local: the dialog is a detail of
  // reading the rail, and lifting it would make every consumer of this list
  // carry state it has no other use for.
  const [problemsFor, setProblemsFor] = useState<RailSection | null>(null);

  // `indent` bumps a row one level deeper than the top-level tree (matching
  // the old console's depth-based pl: files inside an expanded component sit
  // right of both the top-level entries and the component's own header row).
  // `statusPath` is for the synthetic rows (Architecture, Security, Wireframe)
  // whose selection is not a file path; a plain file row derives it itself.
  const row = (
    sel: SpecSelection,
    label: string,
    icon: React.ReactNode,
    indent?: boolean,
    statusPath?: string,
  ) => {
    const path = statusPath ?? (sel.kind === "file" ? sel.path : undefined);
    const status = path !== undefined ? planByPath.get(path) : undefined;
    // A row with no document behind it selects nothing, so it is disabled
    // whatever the plan says about it — an entry the turn DIED on has no file
    // any more than one it never reached. `writing` is the exception: its
    // document is arriving, the editor is already following it, and the pane
    // says so.
    const ghost =
      path !== undefined &&
      !committed.has(path) &&
      (status === "planned" || status === "error");
    return (
      <ListItemButton
        key={selectionKey(sel)}
        selected={isSel(sel)}
        onClick={() => onSelect(sel)}
        disabled={ghost}
        sx={{ pl: indent ? 4 : 2, pr: 2, ...(ghost ? { opacity: 0.55 } : {}) }}
      >
        <ListItemIcon sx={{ minWidth: 32 }}>{icon}</ListItemIcon>
        <ListItemText
          primary={label}
          slotProps={{
            primary: {
              noWrap: true,
              ...(status === "writing"
                ? { color: "primary" }
                : status === "error"
                  ? { color: "error" }
                  : {}),
            },
          }}
        />
        {status === "writing" && <WorkingPulse />}
        {status === "error" && (
          <Box sx={{ display: "flex", flexShrink: 0, color: "error.main" }}>
            <TriangleAlert size={14} />
          </Box>
        )}
      </ListItemButton>
    );
  };

  // A collapsible group's header — Flows and every component share it, so
  // the two kinds of group read the same: chevron, glyph, name. The glyph is
  // what tells a flows group from a component group at a glance (#686).
  const groupHeader = (
    label: string,
    icon: React.ReactNode,
    collapsed: boolean,
    onToggle: () => void,
  ) => (
    <ListItemButton
      onClick={onToggle}
      sx={{ px: 2, py: 0.25, minHeight: 0 }}
      aria-expanded={!collapsed}
      aria-label={`${collapsed ? "Expand" : "Collapse"} ${label}`}
    >
      <ListItemIcon sx={{ minWidth: 20 }}>
        {collapsed ? <ChevronRight size={14} /> : <ChevronDown size={14} />}
      </ListItemIcon>
      <ListItemIcon sx={{ minWidth: 24, color: "text.secondary" }}>{icon}</ListItemIcon>
      <ListItemText
        primary={label}
        slotProps={{
          primary: {
            variant: "body2",
            fontWeight: 600,
            color: "text.secondary",
          },
        }}
      />
    </ListItemButton>
  );

  const flatGroup = (section: RailSection, groupFiles: SpecFileEntry[]) => (
    <Box sx={{ mb: 1 }}>
      {sectionHeader(section)}
      {groupFiles.length > 0 ? (
        <List dense disablePadding>
          {groupFiles.map((f) =>
            row(fileSel(f.path), fileLabel(f.path), <FileText size={16} />),
          )}
        </List>
      ) : (
        <Typography
          variant="body2"
          color="text.secondary"
          sx={{ px: 2, py: 0.5, fontStyle: "italic" }}
        >
          {emptyNote}
        </Typography>
      )}
    </Box>
  );

  return (
    <Box component="nav" aria-label="Spec files" sx={{ py: 1 }}>
      {flatGroup(sectionOf("requirements"), requirements)}

      {/* Design — the documents as rows (Architecture, Domain model, Security),
          then the groups: Flows first, then one per component. */}
      <Box sx={{ mb: 1 }}>
        {sectionHeader(
          sectionOf("design"),
          hasDesign && (
            <Tooltip
              title={
                regenerateDisabled
                  ? "An agent is still working — re-generate is available once it finishes"
                  : "Re-generate design from the current requirements"
              }
            >
              {/* span so the tooltip works while the button is disabled */}
              <span>
                <IconButton
                  size="small"
                  aria-label="Re-generate design"
                  onClick={onRegenerateDesign}
                  disabled={regenerateDisabled ?? false}
                >
                  <RefreshCw size={16} />
                </IconButton>
              </span>
            </Tooltip>
          ),
        )}
        {hasDesign ? (
          <List dense disablePadding>
            {design.hasCellDsl &&
              row(
                { kind: "cell-diagram" },
                "Architecture",
                <Network size={16} />,
                false,
                DESIGN_CELL_PATH,
              )}
            {design.overview.map((f) =>
              row(
                fileSel(f.path),
                fileLabel(f.path),
                f.path === DOMAIN_MODEL_PATH ? <Database size={16} /> : <FileText size={16} />,
              ),
            )}
            {/* ONE rail entry for security.json — present file shows the row;
                missing file hides it. */}
            {design.hasSecurity &&
              row(
                { kind: "security" },
                "Security",
                <ShieldCheck size={16} />,
                false,
                SECURITY_JSON_PATH,
              )}
            {/* The key flows — one group, one row per flow, shown only once a
                flow exists or a turn has planned one (a planned flow rides in
                as a ghost like any other declared path). */}
            {design.flows.length > 0 && (
              <Box sx={{ mt: 0.5 }}>
                {groupHeader("Flows", <Workflow size={14} />, flowsCollapsed, () =>
                  setFlowsCollapsed((v) => !v),
                )}
                <Collapse in={!flowsCollapsed} unmountOnExit>
                  {design.flows.map((f) =>
                    row(fileSel(f.path), fileLabel(f.path), <FileText size={16} />, true),
                  )}
                </Collapse>
              </Box>
            )}
            {design.components.map((c) => {
              const collapsed = collapsedComponents.has(c.name);
              return (
                <Box key={c.name} sx={{ mt: 0.5 }}>
                  {groupHeader(c.name, <Boxes size={14} />, collapsed, () =>
                    toggleComponent(c.name),
                  )}
                  <Collapse in={!collapsed} unmountOnExit>
                    {c.files.map((f) =>
                      row(fileSel(f.path), fileLabel(f.path), <FileText size={16} />, true),
                    )}
                    {c.wireframeDslPath &&
                      row(
                        {
                          kind: "wireframe",
                          component: c.name,
                          dslPath: c.wireframeDslPath,
                        },
                        "Wireframe",
                        <LayoutDashboard size={16} />,
                        true,
                        c.wireframeDslPath,
                      )}
                  </Collapse>
                </Box>
              );
            })}
          </List>
        ) : (
          <Typography
            variant="body2"
            color="text.secondary"
            sx={{ px: 2, py: 0.5, fontStyle: "italic" }}
          >
            {emptyNote}
          </Typography>
        )}
      </Box>

      {flatGroup(sectionOf("validation"), validation)}

      <ProblemsDialog
        open={problemsFor !== null}
        title={problemsFor ? `${problemsFor.title} — to resolve` : ""}
        problems={(problemsFor?.reasons ?? []).map((reason) => ({
          key: reason.key,
          label: reason.label,
          fix: {
            label: reason.action === "update-design" ? "Update the design" : "Open the document",
            run: () => onReason(reason.action),
          },
        }))}
        onClose={() => setProblemsFor(null)}
      />
    </Box>
  );
}
