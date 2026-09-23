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

import type { StatusTone } from "../../components/StatusChip";

// The BE's canonical skill-kind vocabulary (`skills/skill_service.go`,
// `frontmatterKind`). `builtin` and `flow` are RETIRED: `repo_store.go`'s
// `legacyKindDirs` maps builtin→org and flow→platform for repos that predate
// the flat layout. `custom` is also RETIRED — it folds into `org` (the BE now
// returns `editable=true` on org skills, so the custom/org split no longer
// carries meaning). The contract types `kind` as a bare string (issue #143
// proposes an enum), so normalise anything unrecognised to `org` — the same
// default the BE applies to an unmarked SKILL.md.
export const SKILL_KINDS = ["platform", "org", "imported"] as const;

export type SkillKind = (typeof SKILL_KINDS)[number];

// Legacy "custom" folds into org (back-compat with stored skills); anything
// else unrecognised also defaults to org (the BE default for an unmarked
// SKILL.md).
export function normalizeKind(kind: string): SkillKind {
  if (kind === "custom") return "org";
  return (SKILL_KINDS as readonly string[]).includes(kind)
    ? (kind as SkillKind)
    : "org";
}

export function kindLabel(kind: SkillKind): string {
  switch (kind) {
    // "org" now covers both platform-shipped, org-visible stack skills (go,
    // react-webapp, ...) and org-authored copies (legacy "custom" folds in
    // here too) — the BE's `editable` flag, not the kind, says which is
    // which. "Org" = the organization's skills (platform-provided defaults
    // plus ones the org added) — the tooltip clarifies that scope.
    case "org":
      return "Org";
    case "platform":
      return "Platform";
    case "imported":
      return "Imported";
  }
}

// Chip tone per kind (StatusChip). Two Oxygen Chip colours are deliberately
// avoided: `warning` is the platform-update "review" status colour (SkillsSection
// STATUS_META) — a kind must not read as a status — and `secondary` resolves to
// a near-white (#e8e8e8) that is unreadable on a light surface.
export function kindChipTone(kind: SkillKind): StatusTone {
  switch (kind) {
    case "org":
      return "primary";
    case "platform":
      return "info";
    case "imported":
      return "neutral";
  }
}

// One-line explanation per kind, shown as a tooltip on the row's kind chip
// (issue #172): the flat list has no group headings to carry it, and the
// platform blurb is also where its read-only-ness is stated — the list shows
// no separate read-only chip.
export function kindBlurb(kind: SkillKind): string {
  switch (kind) {
    case "org":
      return "Your organization's skills — platform-provided defaults plus ones you've added. Editable; the platform can still offer updates.";
    case "platform":
      return "Guidance the platform's own agents follow — the generation flows (design, wireframes, prototype, tasks) and the coding run's workflow. Read-only.";
    case "imported":
      return "AgentSkills brought in from the ecosystem.";
  }
}
