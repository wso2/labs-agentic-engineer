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

import type { MouseEvent } from "react";
import { Chip, Tooltip } from "@wso2/oxygen-ui";
import { GitHub, GitPullRequest } from "@wso2/oxygen-ui-icons-react";

// The affordance for something that lives on the host: a glyph paired with the
// number (`⌾ #12`, `⑂ #41`) so the link states WHAT it opens instead of showing a
// bare, ambiguous icon. Two of these often sit side by side — an issue and the pull
// request answering it — which is why the kinds carry different glyphs rather than
// the same octocat twice.
//
// The component owns the form (chip, glyph, number, new tab, the icon-margin fix);
// the caller owns the words, because the same chip is "the GitHub issue" on a task
// row, "the validation issue" in a page header, and "cycle 2's pull request" in a
// log — and a page that shows the same pull request twice needs the two to be
// distinguishable by name.

const KIND = {
  issue: { icon: GitHub, tooltip: "Open the GitHub issue", name: "GitHub issue" },
  pull: {
    icon: GitPullRequest,
    tooltip: "Open the pull request",
    name: "Pull request",
  },
} as const;

// Hoisted rather than written inline: an sx literal is a NEW object every render,
// which emotion has to re-serialize each time. One module-level object means one
// serialization for every chip on the page, for the life of the bundle.
const CHIP_SX = {
  fontVariantNumeric: "tabular-nums",
  // The lucide-style icon doesn't inherit the Chip's icon margins, so it sits
  // flush against the pill's left edge — space it explicitly.
  "& .MuiChip-icon": { ml: 0.75, mr: -0.25 },
} as const;

export function GitHubRefChip({
  kind,
  number,
  url,
  name,
  tooltip,
  onClick,
}: {
  kind: keyof typeof KIND;
  number: number;
  url: string;
  /** What the thing IS, for the accessible name; `#{number}` is appended. */
  name?: string;
  tooltip?: string;
  /** Inside a clickable row or accordion summary, stop the click there. */
  onClick?: (e: MouseEvent<HTMLElement>) => void;
}) {
  const spec = KIND[kind];
  const Icon = spec.icon;
  return (
    <Tooltip title={tooltip ?? spec.tooltip}>
      <Chip
        component="a"
        href={url}
        target="_blank"
        rel="noreferrer"
        clickable
        size="small"
        variant="outlined"
        icon={<Icon size={14} />}
        label={`#${number}`}
        aria-label={`${name ?? spec.name} #${number}`}
        {...(onClick ? { onClick } : {})}
        sx={CHIP_SX}
      />
    </Tooltip>
  );
}
