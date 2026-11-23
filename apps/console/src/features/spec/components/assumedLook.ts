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

import { soft } from "./Tag";

// How a line tagged `*assumed*` looks and settles, wherever one is drawn: the
// spec editor's decorations (SpecEditor.tsx) and the Design page's technical
// decisions (DecisionsView.tsx) read the same look from here.

/** How long a line the user just settled shows "by you". */
export const BY_YOU_MS = 4000;

/** An assumed line: the warning's wash. */
export const assumedLineSx = { bgcolor: soft("warning", 0.14) } as const;

/** The quiet "assumed" tag itself. */
export const assumedTagSx = { fontSize: "0.75rem", color: "warning.main" } as const;

/** The row of buttons after an assumed line's words. */
export const assumedActionsSx = { display: "inline-flex", flexWrap: "wrap", gap: 0.5, ml: 0.75, verticalAlign: "baseline" } as const;

/** A line the user just settled. */
export const byYouLineSx = { textDecoration: "underline dotted var(--oxygen-palette-success-main)", textUnderlineOffset: "4px" } as const;

/** The "by you" beside it. */
export const byYouSx = { fontSize: "0.6875rem", color: "success.main", whiteSpace: "nowrap", ml: 1, textDecoration: "none", display: "inline-block" } as const;

/** A pill button inside a document's text (an assumed line's Keep), in one palette colour. */
export function pillSx(tone: "warning") {
  return {
    fontFamily: "inherit",
    fontStyle: "normal",
    fontWeight: 600,
    fontSize: "0.6875rem",
    lineHeight: 1.6,
    px: 1,
    borderRadius: 999,
    border: 1,
    borderColor: `${tone}.main`,
    color: `${tone}.main`,
    bgcolor: "background.paper",
    cursor: "pointer",
    "&:hover": { bgcolor: soft(tone) },
    "&:focus-visible": { outline: 2, outlineColor: `${tone}.main`, outlineOffset: 1 },
  } as const;
}
