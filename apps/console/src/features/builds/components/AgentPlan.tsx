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

import { Box, Typography } from "@wso2/oxygen-ui";
import { planTone, type CrewPlanItem } from "@aep/progress-view";
import { toneColor } from "../../../components/logTone";

// THE AGENT'S OWN PLAN — the task list it keeps for itself, as rows.
//
// The `aep` skill tells a run "the platform shows that list to the person
// watching, so it is the one place your plan has to be true". This is the half
// of that sentence the console owes: the run states its intent, and a reader can
// see it beside the work rather than having to infer it from tool calls.
//
// It is deliberately QUIET. A plan is context for the agent row it sits under,
// not the headline — the headline is whether anything is stuck. So the title
// stays in the body colour every other sub-row uses, and only the glyph carries
// the entry's weight, which keeps a lead's fifteen-entry list from becoming the
// loudest thing on the page.
//
// Nothing here folds anything: `CrewMember.plan` arrives already folded and
// already placed on its owner by `buildCrew`, which is the same model the
// playground's crew block draws from. Two folds of one event is exactly how the
// two surfaces came to describe one list two ways.

/**
 * Where an entry stands, as a glyph.
 *
 * Never the only signal — the status word sits at the end of the row and the
 * glyph's SHAPE differs per status, so the row reads the same to somebody who
 * cannot tell green from grey. An unknown status still gets a box: a plan entry
 * a newer runtime described in words this build does not know is still an entry.
 */
const PLAN_GLYPHS: Record<string, string> = {
  pending: "☐",
  in_progress: "▸",
  completed: "☑",
};

/** `in_progress` → `in progress`. The wire's vocabulary is not a reader's. */
function planStatusLabel(status: string): string {
  return status.replace(/_/g, " ");
}

/**
 * One plan entry, as a row under the agent whose plan it is.
 *
 * `depth` indents it under its owner in the tree exactly as a backgrounded
 * command is indented; the inspector passes none, because there is only one
 * agent in that panel and nothing to be nested under.
 */
export function PlanRow({ item, depth = 0 }: { item: CrewPlanItem; depth?: number }) {
  // The title is the entry's subject, but only the creating event is guaranteed
  // to carry one — an id is a poor name and a true one, and better than a row
  // that renders as an empty line.
  const title = item.title || item.id;
  return (
    <Box
      sx={{
        display: "flex",
        alignItems: "baseline",
        gap: 1,
        pl: 2 + depth * 2,
        py: 0.25,
        color: "grey.500",
      }}
    >
      <Typography
        component="span"
        aria-hidden
        sx={{ font: "inherit", flexShrink: 0, color: toneColor(planTone(item.status)) }}
      >
        {PLAN_GLYPHS[item.status] ?? "☐"}
      </Typography>
      <Typography
        component="span"
        title={title}
        sx={{ font: "inherit", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}
      >
        {title}
      </Typography>
      <Typography component="span" sx={{ font: "inherit", ml: "auto", flexShrink: 0 }}>
        {planStatusLabel(item.status)}
      </Typography>
    </Box>
  );
}

/**
 * One agent's whole plan, for the inspector.
 *
 * Labelled, unlike the tree's rows: there the owning row directly above says
 * whose list it is, and here the panel is already about one agent but the rows
 * would otherwise sit unexplained between its report and its steps.
 *
 * Renders nothing at all when the agent kept no list. Most agents do not — an
 * empty "Plan" heading would report an absence as a section.
 */
export function AgentPlan({ plan }: { plan: readonly CrewPlanItem[] }) {
  if (plan.length === 0) return null;
  return (
    <Box sx={{ mb: 1 }}>
      <Typography variant="caption" color="text.secondary" component="div">
        Plan
      </Typography>
      {plan.map((item) => (
        <PlanRow key={item.id} item={item} />
      ))}
    </Box>
  );
}
