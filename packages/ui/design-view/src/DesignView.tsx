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

import { useMemo, useState } from "react";
import {
  Alert,
  Box,
  Button,
  Chip,
  IconButton,
  Menu,
  MenuItem,
  Typography,
} from "@wso2/oxygen-ui";
import { EllipsisVertical, Lock, TriangleAlert } from "@wso2/oxygen-ui-icons-react";
import {
  parseComponentDesign,
  type ComponentDesign,
  type Dependency,
  type DependencyCandidate,
  type DesignConfigEntry,
} from "./parse.js";

// Read-time status for ONE dependency, keyed by name in
// DesignViewProps.dependencyStatus. parse.ts deliberately never parses these
// values from raw design.json (see its file-header comment) — the server read
// model is their ONLY source. Resolution status/reason stay raw strings so an
// unrecognized server value still renders; valueState is the closed external
// readiness vocabulary shared with the configuration surfaces.
export interface DependencyStatusInfo {
  /** "resolved" | "ambiguous" | "unresolved" | "blocked". */
  status?: string | undefined;
  /** "needs-spec" | "needs-input" | "not-found" | "access-required". */
  reason?: string | undefined;
  /** Read-time development value readiness for external dependencies. */
  valueState?: "not-provisioned" | "unset" | "configured" | undefined;
}

const STATUS_COLOR: Record<string, "success" | "warning" | "error"> = {
  resolved: "success",
  ambiguous: "warning",
  unresolved: "error",
  blocked: "error",
};
const STATUS_LABEL: Record<string, string> = {
  resolved: "Resolved",
  ambiguous: "Ambiguous",
  unresolved: "Unresolved",
  blocked: "Blocked",
};
const REASON_LABEL: Record<string, string> = {
  "needs-spec": "needs a spec",
  "needs-input": "needs input",
  "not-found": "not found",
  "access-required": "access required",
};
const VALUE_STATE_COLOR: Record<
  NonNullable<DependencyStatusInfo["valueState"]>,
  "info" | "warning" | "success"
> = {
  "not-provisioned": "info",
  unset: "warning",
  configured: "success",
};
const VALUE_STATE_LABEL: Record<
  NonNullable<DependencyStatusInfo["valueState"]>,
  string
> = {
  "not-provisioned": "Platform provisioning",
  unset: "Needs values",
  configured: "Configured",
};

// Solid background per component type / dependency kind. Text color is
// computed for contrast (getContrastText), so labels stay readable in both
// themes — the same approach as the OpenAPI viewer's method badges.
const TYPE_COLOR: Record<string, string> = {
  service: "#1976d2",
  "web-application": "#7b1fa2",
  "scheduled-task": "#ed6c02",
  worker: "#2e7d32",
};
const KIND_COLOR: Record<string, string> = {
  component: "#1976d2",
  "org-service": "#7b1fa2",
  external: "#ed6c02",
  "platform-resource": "#0288d1",
};
const KIND_LABEL: Record<string, string> = {
  component: "component",
  "org-service": "org service",
  external: "external",
  "platform-resource": "platform",
};
const FALLBACK = "#616161";

function SolidBadge({ label, color }: { label: string; color: string }) {
  return (
    <Box
      component="span"
      sx={(theme) => ({
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        px: 1,
        py: 0.5,
        borderRadius: 1,
        flexShrink: 0,
        fontFamily: "monospace",
        fontSize: "0.6875rem",
        fontWeight: 700,
        letterSpacing: "0.06em",
        textTransform: "uppercase",
        bgcolor: color,
        color: theme.palette.getContrastText(color),
      })}
    >
      {label}
    </Box>
  );
}

const mono = { fontFamily: "monospace", fontSize: "0.875rem" } as const;

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <Box sx={{ display: "flex", gap: 2, alignItems: "baseline" }}>
      <Typography
        variant="body2"
        color="text.secondary"
        sx={{ minWidth: 96, flexShrink: 0 }}
      >
        {label}
      </Typography>
      <Typography component="span" sx={mono}>
        {value}
      </Typography>
    </Box>
  );
}

function SectionHeading({ children }: { children: React.ReactNode }) {
  return (
    <Typography
      variant="overline"
      color="text.secondary"
      sx={{ display: "block", mt: 3, mb: 1, fontWeight: 700, letterSpacing: "0.08em" }}
    >
      {children}
    </Typography>
  );
}

// #252: an ambiguous dependency's `candidates` (2+ identified-but-not-pinned
// options) used to render one detailed CandidateRow per candidate (name,
// style, description, package, docs/spec links). That level of detail turned
// out to be more than the card needs — resolution always happens via the
// "Resolve via chat" affordance below, never by reading candidate metadata
// off this card — so it's collapsed to a single concise note naming up to 3
// candidates as examples. Shared by the design-view card here and mirrored
// (independently, no cross-package import) by console's
// BuildDependencyDrawer.tsx so the build-time blocker reads the same way.
const MAX_CANDIDATE_EXAMPLES = 3;

function candidateExamples(candidates: DependencyCandidate[]): string {
  const names = candidates.map((c) => c.name);
  const shown = names.slice(0, MAX_CANDIDATE_EXAMPLES).join(", ");
  return names.length > MAX_CANDIDATE_EXAMPLES ? `${shown}…` : shown;
}

function AmbiguousCandidatesNote({
  candidates,
}: {
  candidates: DependencyCandidate[];
}) {
  return (
    <Box sx={{ display: "flex", alignItems: "flex-start", gap: 0.75 }}>
      <Box
        component="span"
        sx={{ display: "inline-flex", color: "warning.main", mt: "2px", flexShrink: 0 }}
      >
        <TriangleAlert size={16} />
      </Box>
      <Typography variant="body2" color="text.secondary">
        Multiple candidates — e.g. {candidateExamples(candidates)}. Use
        &quot;Resolve in chat&quot; to pick one (or name another that fits).
      </Typography>
    </Box>
  );
}

// #252 Task 17: why a dependency's resolution chat turn is being seeded —
// "resolve" from the chat button on a non-resolved dependency (Task 9/10),
// "reconsider" from the hamburger's "Discuss in chat & modify" menu item on
// an already-resolved one. Mirrors console's own
// dependencyResolutionMessage.ts DependencyResolutionIntent (this package
// has no dependency on console's code, so it declares the same literal union
// locally rather than importing it).
export type DependencyResolutionIntent = "resolve" | "reconsider";

// #252 Task 17: the resolved-dependency affordance — a small hamburger that
// opens a one-item menu ("Discuss in chat & modify", the RECONSIDER intent).
// Never rendered alongside the non-resolved "Resolve in chat" button (see
// DependencyCard: exactly one of the two renders, based on `isResolved`).
function ReconsiderMenu({ dependencyName, onReconsider }: {
  dependencyName: string;
  onReconsider: () => void;
}) {
  const [anchorEl, setAnchorEl] = useState<HTMLElement | null>(null);
  return (
    <>
      <IconButton
        aria-label={`Actions for ${dependencyName}`}
        size="small"
        onClick={(e) => setAnchorEl(e.currentTarget)}
        sx={{ ml: "auto" }}
      >
        <EllipsisVertical size={16} />
      </IconButton>
      <Menu
        anchorEl={anchorEl}
        open={anchorEl !== null}
        onClose={() => setAnchorEl(null)}
      >
        <MenuItem
          onClick={() => {
            setAnchorEl(null);
            onReconsider();
          }}
        >
          Discuss in chat & modify
        </MenuItem>
      </Menu>
    </>
  );
}

function ConfigChip({ entry }: { entry: DesignConfigEntry }) {
  return (
    <Chip
      size="small"
      variant="outlined"
      label={entry.key}
      color={entry.secret ? "warning" : "default"}
      {...(entry.secret
        ? { icon: <Lock size={14} data-testid="secret-icon" /> }
        : {})}
    />
  );
}

// A dependency reads as: what kind it is (the badge), its name, an optional
// read-time status chip (#252 Task 9 — from DesignViewProps.dependencyStatus,
// NEVER computed here), and a one-line description, followed by its intent
// (candidates/config) and a state-based affordance (#252 Task 17):
// a non-resolved dependency gets the reason plus a "Resolve in chat" button;
// a resolved one gets a hamburger → "Discuss in chat & modify" instead. Never
// both for the same dependency — `isResolved` gates which one renders.
function DependencyCard({
  dep,
  status,
  usedBy,
  onResolve,
  onReconsider,
}: {
  dep: Dependency;
  status?: DependencyStatusInfo | undefined;
  /**
   * #252 Task 15: every OTHER component (besides the one this card's design
   * belongs to) that declares an equivalent dependency (same kind+identity —
   * see DesignViewProps.dependencyUsedBy). Rendered as a "Used by" line only
   * when 2+ entries are present; a component-local dependency (the common
   * case) gets nothing extra.
   */
  usedBy?: string[] | undefined;
  /** Non-resolved only (#252 Task 9/10) — the "Resolve in chat" button. */
  onResolve?: (() => void) | undefined;
  /**
   * Resolved only (#252 Task 17) — the hamburger's "Discuss in chat &
   * modify" menu item. Uniform across every resolved kind (component /
   * org-service / external / platform-resource) — never gated by `dep.kind`.
   */
  onReconsider?: (() => void) | undefined;
}) {
  const color = KIND_COLOR[dep.kind] ?? FALLBACK;
  const kindLabel = KIND_LABEL[dep.kind] ?? dep.kind;
  const resolutionStatus = status?.status;
  const valueState = dep.kind === "external" ? status?.valueState : undefined;
  const isResolved = resolutionStatus === "resolved";
  const showResolution = Boolean(resolutionStatus) && !isResolved;
  const showUsedBy = (usedBy?.length ?? 0) > 1;

  return (
    <Box
      sx={{
        border: 1,
        borderColor: "divider",
        borderRadius: 1,
        p: 1.5,
        mb: 1,
      }}
    >
      <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
        <SolidBadge label={kindLabel} color={color} />
        <Typography component="span" sx={{ ...mono, fontWeight: 600 }}>
          {dep.name}
        </Typography>
        {resolutionStatus && (
          <Chip
            size="small"
            color={STATUS_COLOR[resolutionStatus] ?? "default"}
            label={STATUS_LABEL[resolutionStatus] ?? resolutionStatus}
          />
        )}
        {valueState && (
          <Chip
            size="small"
            color={VALUE_STATE_COLOR[valueState]}
            label={VALUE_STATE_LABEL[valueState]}
          />
        )}
        {isResolved && onReconsider && (
          <ReconsiderMenu dependencyName={dep.name} onReconsider={onReconsider} />
        )}
      </Box>
      {dep.description && (
        <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
          {dep.description}
        </Typography>
      )}

      {showUsedBy && usedBy && (
        <Box sx={{ mt: 1, display: "flex", alignItems: "center", gap: 0.5, flexWrap: "wrap" }}>
          <Typography variant="caption" color="text.secondary">
            Used by:
          </Typography>
          {usedBy.map((name) => (
            <Chip key={name} size="small" variant="outlined" label={name} />
          ))}
        </Box>
      )}

      {dep.candidates && dep.candidates.length > 0 && (
        <Box sx={{ mt: 1 }}>
          <AmbiguousCandidatesNote candidates={dep.candidates} />
        </Box>
      )}

      {dep.config && dep.config.length > 0 && (
        <Box sx={{ mt: 1 }}>
          <Typography variant="caption" color="text.secondary" sx={{ display: "block" }}>
            Config
          </Typography>
          <Box sx={{ display: "flex", flexWrap: "wrap", gap: 0.5, mt: 0.5 }}>
            {dep.config.map((entry) => (
              <ConfigChip key={entry.key} entry={entry} />
            ))}
          </Box>
        </Box>
      )}

      {showResolution && (
        <Box sx={{ mt: 1, display: "flex", alignItems: "center", gap: 1.5, flexWrap: "wrap" }}>
          {status?.reason && (
            <Typography variant="caption" color="error">
              {REASON_LABEL[status.reason] ?? status.reason}
            </Typography>
          )}
          {onResolve && (
            <Button size="small" variant="outlined" onClick={onResolve}>
              Resolve in chat
            </Button>
          )}
        </Box>
      )}
    </Box>
  );
}

function DesignBody({
  design,
  dependencyStatus,
  dependencyUsedBy,
  onResolveDependency,
}: {
  design: ComponentDesign;
  dependencyStatus?: Record<string, DependencyStatusInfo> | undefined;
  dependencyUsedBy?: Record<string, string[]> | undefined;
  onResolveDependency?:
    | ((dependencyName: string, intent: DependencyResolutionIntent) => void)
    | undefined;
}) {
  const typeColor = TYPE_COLOR[design.type] ?? FALLBACK;
  return (
    <Box sx={{ height: "100%", overflow: "auto", p: 3 }}>
      <Box sx={{ maxWidth: 960, mx: "auto" }}>
        {/* Header bar — type + version sit above the name as an eyebrow row */}
        {(design.type || design.version) && (
          <Box sx={{ display: "flex", alignItems: "center", gap: 1, mb: 1 }}>
            {design.type && <SolidBadge label={design.type} color={typeColor} />}
            {design.version && (
              <Chip label={`v${design.version}`} size="small" variant="outlined" />
            )}
          </Box>
        )}
        <Typography variant="h4" sx={{ fontWeight: 700, lineHeight: 1.2 }}>
          {design.name || "component"}
        </Typography>

        {/* Facts — each labeled so a bare value like "docker" isn't ambiguous */}
        <Box sx={{ mt: 2, display: "flex", flexDirection: "column", gap: 0.5 }}>
          {design.language && <Fact label="Language" value={design.language} />}
          {design.buildpack && <Fact label="Buildpack" value={design.buildpack} />}
          {design.exposure && <Fact label="Exposure" value={design.exposure} />}
          {design.appPath && <Fact label="App path" value={design.appPath} />}
          {design.entrypoint && <Fact label="Entrypoint" value={design.entrypoint} />}
          {design.endpoint && <Fact label="Endpoint" value={design.endpoint.name} />}
        </Box>

        {/* Description */}
        {design.description && (
          <>
            <SectionHeading>Description</SectionHeading>
            <Typography variant="body1" color="text.secondary">
              {design.description}
            </Typography>
          </>
        )}

        {/* Dependencies */}
        <SectionHeading>Dependencies</SectionHeading>
        {design.dependencies.length === 0 ? (
          <Typography variant="body2" color="text.secondary">
            No dependencies.
          </Typography>
        ) : (
          design.dependencies.map((dep, i) => (
            <DependencyCard
              key={`${dep.kind}:${dep.name}:${i}`}
              dep={dep}
              status={dependencyStatus?.[dep.name]}
              usedBy={dependencyUsedBy?.[dep.name]}
              onResolve={
                onResolveDependency
                  ? () => onResolveDependency(dep.name, "resolve")
                  : undefined
              }
              onReconsider={
                onResolveDependency
                  ? () => onResolveDependency(dep.name, "reconsider")
                  : undefined
              }
            />
          ))
        )}
      </Box>
    </Box>
  );
}

export interface DesignViewProps {
  /** Raw component design.json text. */
  design: string;
  /**
   * OPTIONAL read-time resolution status per dependency name, from #252
   * Task 2's `GET /projects/{p}/design/dependencies` endpoint supplies
   * `status`/`reason`; the independent per-component dependency-status
   * endpoint supplies external `valueState`. parse.ts
   * deliberately does not parse these fields from the raw design.json (see
   * its file-header comment): they are computed server-side on every read and
   * never authored/persisted, so recomputing them here would drift from that
   * single read-time authority.
   * Optional and keyed defensively (a missing entry just renders without a
   * status chip) so existing callers that don't fetch this endpoint are
   * unaffected.
   */
  dependencyStatus?: Record<string, DependencyStatusInfo> | undefined;
  /**
   * OPTIONAL cross-component "Used by" (#252 Task 15), keyed by THIS design's
   * own dependency `name`: every other component that also declares an
   * equivalent dependency (same kind + identity — see
   * BuildDependencyDrawer.tsx's `groupPreflightItems`, the same dedupe rule
   * applied to the build-time drawer). This package has no notion of "other
   * components" of its own — the caller (console's SpecView) computes this
   * across every component's dependencies via `computeDependencyUsedBy` and
   * passes only the slice relevant to the design being rendered. Optional and
   * keyed defensively, like `dependencyStatus` above: a missing/absent entry
   * (the common case — a dependency only this component declares) just
   * renders without a "Used by" line.
   */
  dependencyUsedBy?: Record<string, string[]> | undefined;
  /**
   * Called with a dependency's `name` and the reason its chat turn is being
   * seeded (#252 Task 17) when the user clicks either affordance:
   *  - "resolve" — the "Resolve in chat" button on a non-resolved card (only
   *    rendered when `dependencyStatus` marks that dependency non-resolved).
   *  - "reconsider" — the resolved card's hamburger → "Discuss in chat &
   *    modify" menu item (only rendered when `dependencyStatus` marks that
   *    dependency resolved).
   * Exactly one of the two affordances renders per card, so this is never
   * called with both intents for the same dependency in the same render.
   * This package has no chat/collab knowledge of its own — the caller
   * (console's SpecView) looks up that dependency's full endpoint entry and
   * seeds the existing conversation via #252 Task 5's
   * `useResolveDependencyViaChat`. Optional, like `dependencyStatus` above.
   */
  onResolveDependency?:
    | ((dependencyName: string, intent: DependencyResolutionIntent) => void)
    | undefined;
}

export function DesignView({
  design,
  dependencyStatus,
  dependencyUsedBy,
  onResolveDependency,
}: DesignViewProps) {
  const parsed = useMemo(() => parseComponentDesign(design), [design]);
  if ("kind" in parsed) {
    return (
      <Box sx={{ p: 3 }}>
        <Alert severity="error">
          Couldn't parse this component's design.json: {parsed.message}
        </Alert>
      </Box>
    );
  }
  return (
    <DesignBody
      design={parsed}
      dependencyStatus={dependencyStatus}
      dependencyUsedBy={dependencyUsedBy}
      onResolveDependency={onResolveDependency}
    />
  );
}
