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
  Check,
  ChevronDown,
  ChevronRight,
  FileText,
  RefreshCw,
  Network,
  LayoutDashboard,
  TriangleAlert,
} from "@wso2/oxygen-ui-icons-react";
import { WorkingPulse } from "../../agent-chat/components/WorkingIndicator";
import { PRD_PATH, type SpecFileEntry } from "../api/mapping";
import {
  mostSignificant,
  reasonCount,
  type RailSection,
  type SectionReason,
} from "../lib/railSections";
import { ProblemsDialog } from "./ProblemsDialog";
import {
  buildDesignSection,
  selectionKey,
  type SpecSelection,
} from "../api/designTree";

function basename(path: string): string {
  return path.split("/").at(-1) ?? path;
}

const OPENAPI_RE = /\/openapi\.ya?ml$/;
const COMPONENT_DESIGN_RE = /^specs\/design\/components\/[^/]+\/design\.json$/;
const VALIDATION_CRITERIA_RE = /^specs\/validation\/validation-criteria\.json$/;
const DESIGN_ROOT = "specs/design/design.md";
const SECURITY = "specs/design/security.md";

/**
 * A document's NAME, never its filename (#575).
 *
 * The user is reading a document tree, not a repository — `prd.md` and
 * `security.md` are storage details that leaked into the one surface they read
 * throughout the journey. The repo paths deliberately do not change; this is
 * the mapping, and the lexicon holds the same table in words.
 *
 * A file with no entry here falls back to its filename, which keeps an
 * agent-invented document readable rather than blank. Feature files land there
 * on purpose: their filename IS the feature's name, so the fallback is already
 * the right answer.
 */
const TITLES: Record<string, string> = {
  [PRD_PATH]: "Product requirements",
  [DESIGN_ROOT]: "Design overview",
  [SECURITY]: "Security",
};

function fileLabel(path: string): string {
  if (Object.hasOwn(TITLES, path)) return TITLES[path] as string;
  if (OPENAPI_RE.test(path)) return "API";
  if (COMPONENT_DESIGN_RE.test(path)) return "Design overview";
  if (VALIDATION_CRITERIA_RE.test(path)) return "Acceptance criteria";
  // A document nothing above names — a feature file most of the time, where
  // the filename IS the feature's name once the extension is off it. Keeping
  // `.md` would leave the one surface the user reads throughout still showing
  // them a file.
  return basename(path).replace(/\.md$/, "");
}

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

  // The PRD leads, whatever it sorts as. Everything else under Requirements
  // elaborates it — a feature file is depth on a story the PRD defines — and on
  // path alone `features/…` sorts ABOVE `prd.md`, burying the document the
  // whole flow is written against beneath its own footnotes. `files` arrives
  // path-sorted and sort is stable, so the rest keeps that order.
  const requirements = files
    .filter((f) => f.group === "requirements")
    .sort((a, b) => Number(b.path === PRD_PATH) - Number(a.path === PRD_PATH));
  const validation = files.filter((f) => f.group === "validation");
  const design = buildDesignSection(files);

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
              sx={{ height: 20, cursor: "pointer" }}
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
  const row = (
    sel: SpecSelection,
    label: string,
    icon: React.ReactNode,
    indent?: boolean,
  ) => (
    <ListItemButton
      key={selectionKey(sel)}
      selected={isSel(sel)}
      onClick={() => onSelect(sel)}
      sx={{ pl: indent ? 4 : 2, pr: 2 }}
    >
      <ListItemIcon sx={{ minWidth: 32 }}>{icon}</ListItemIcon>
      <ListItemText primary={label} slotProps={{ primary: { noWrap: true } }} />
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

      {/* Design — grouped by component, with synthetic diagram entries. */}
      <Box sx={{ mb: 1 }}>
        {sectionHeader(
          sectionOf("design"),
          (design.hasComponents || design.overview.length > 0) && (
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
        {design.hasComponents || design.hasCellDsl || design.overview.length > 0 ? (
          <List dense disablePadding>
            {design.hasCellDsl &&
              row({ kind: "cell-diagram" }, "Architecture", <Network size={16} />)}
            {design.overview.map((f) =>
              row(fileSel(f.path), fileLabel(f.path), <LayoutDashboard size={16} />),
            )}
            {design.components.map((c) => {
              const collapsed = collapsedComponents.has(c.name);
              return (
                <Box key={c.name} sx={{ mt: 0.5 }}>
                  <ListItemButton
                    onClick={() => toggleComponent(c.name)}
                    sx={{ px: 2, py: 0.25, minHeight: 0 }}
                    aria-expanded={!collapsed}
                    aria-label={`${collapsed ? "Expand" : "Collapse"} ${c.name}`}
                  >
                    <ListItemIcon sx={{ minWidth: 20 }}>
                      {collapsed ? (
                        <ChevronRight size={14} />
                      ) : (
                        <ChevronDown size={14} />
                      )}
                    </ListItemIcon>
                    <ListItemText
                      primary={c.name}
                      slotProps={{
                        primary: {
                          variant: "body2",
                          fontWeight: 600,
                          color: "text.secondary",
                        },
                      }}
                    />
                  </ListItemButton>
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
