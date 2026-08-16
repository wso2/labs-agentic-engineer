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

// The Deployments page as ONE STORY (Deployments UX, Turn 3 / option 1c):
// Development → Validation → Production, on the same numbered rail the Builds
// page uses — because that is the order the platform moves in, and the reader's
// question ("where is my app, what is it waiting for") is a position on that
// path, not a pair of columns to diff.
//
// This module derives the three stages from reads the page already pays for:
// the deploy aggregate on the status poll, the component/binding join
// (deploymentRows), and the design's connection list (promotion). Pure
// derivations, shaped as the builds rail's SpineStage so StageRow renders them.

import {
  loudestState,
  type SpineStage,
  type StageState,
} from "../../builds/lib/stage";
import type { components } from "../../../generated/aep-api";
import type { StageTone } from "./pipeline";
import { validationView } from "./pipeline";
import type { ValidationCounts } from "../../validation/lib/verdict";
import type { DeploymentCard } from "./deploymentRows";
import { canPromote } from "./promotion";

type DeployStage = components["schemas"]["DeployStage"];

/** A board card's contribution to its stage's state. */
function cardState(card: DeploymentCard): StageState {
  switch (card.kind) {
    case "success":
      return "done";
    case "error":
      return "failed";
    case "transitional":
    case "unknown":
      return "active";
    default:
      // notDeployed / undeployed: absent, and absence here is quiet.
      return "waiting";
  }
}

/** Stage 1 — what OpenChoreo is running in dev. */
export function developmentStage(
  cards: DeploymentCard[],
  deploy: DeployStage,
): SpineStage {
  const deployed = cards.filter((c) => c.deployment);
  // Absence must not out-rank presence: an intentionally undeployed sibling
  // (or one not deployed yet) sits quietly beside a live stage rather than
  // muting it, so the fold ignores `waiting` once anything is running.
  const loud = cards.map(cardState).filter((s) => s !== "waiting");
  const state: StageState =
    deployed.length === 0 || loud.length === 0 ? "waiting" : loudestState(loud);
  // Counted from the CARDS the stage renders, never from the status
  // aggregate's components tally: the two are computed by different backend
  // paths and the aggregate has been seen to disagree with the bindings on
  // screen ("2 of 0 components ready", #401 feedback). The note must agree
  // with the rows directly under it.
  const ready = cards.filter((c) => c.kind === "success").length;
  return {
    id: "development",
    // "… environment" on both PLACE stages (#401 feedback): bare
    // "Development" read as a lifecycle phase, and naming the places is also
    // what lets "Validation" read correctly as the check between them.
    name: "Development environment",
    actor: "OpenChoreo",
    state,
    ...(deploy.version && { fact: deploy.version }),
    note:
      deployed.length === 0
        ? "Nothing running yet — agents deploy to dev when a build merges."
        : `${ready} of ${cards.length} components ready`,
  };
}

// validationView's tone, said as a rail state. `info` is the one moving case
// (validating); `awaiting-fix` is `warning`, so a repair in flight reads as a stage
// needing attention rather than as one quietly progressing.
const TONE_STATE: Partial<Record<StageTone, StageState>> = {
  success: "done",
  error: "failed",
  warning: "attention",
  info: "active",
  // `skipped` (the one neutral verdict) is SETTLED, not pending — no criteria
  // were authored, so the stage must not claim validation has yet to run
  // (#401 review).
  neutral: "done",
};

// What the stage is doing or waiting for, keyed on the VALIDATION value and not on
// the rail state it maps to: `awaiting-fix` maps to a settled state (`attention`), so
// a note derived from that claimed the system "WAS checked" mid-repair.
function validationNote(validation: string, view: ReturnType<typeof validationView>) {
  switch (validation) {
    case "running":
      return "The deployed system is being checked against the spec's validation criteria.";
    case "awaiting-fix":
      // Renders ABOVE the banner, so it names its own subject and leaves "runs again"
      // to the banner — whose sentence the Validation page's tile shares and cannot
      // drop. Deploy, not merge: validation runs against the deployed system.
      return "Waits for the implementation fix to deploy.";
    default:
      break;
  }
  if (!view) return "Runs against the dev deployment once every component is ready.";
  // The settled-but-neutral verdict (skipped): nothing WAS checked, so the note must
  // not claim it was.
  return view.tone === "neutral"
    ? "This version has no validation criteria — there was nothing to check."
    : "The deployed system was checked against the spec's validation criteria.";
}

/** Stage 2 — the validation agent's verdict on that deployment. Counts, when
 *  the criteria/report join resolved them, replace the bare label as the
 *  stage's fact — they are an upgrade, never a blocker. */
export function validationStage(
  validation: string,
  counts?: ValidationCounts,
): SpineStage {
  const view = validationView(validation);
  const state = (view && TONE_STATE[view.tone]) ?? "waiting";
  return {
    id: "validation",
    name: "Validation",
    actor: "Validation agent",
    state,
    ...(view && {
      // The label as written, marks and all — `validated*`, `validation?`. The
      // spoken form is an accessible name, never a visible substitute, and the
      // banner directly below spells the hedge out in prose anyway. Moot whenever
      // the counts resolve, which is the steady state.
      fact: counts ? `${counts.passed}/${counts.total} passed` : view.label,
    }),
    note: validationNote(validation, view),
  };
}

/** Stage 3 — production: what's live there, or the promotion waiting on you. */
export function productionStage(
  cards: DeploymentCard[],
  deploy: DeployStage,
  /** Connections needing production values; null while the read is in flight. */
  connectionCount: number | null,
): SpineStage {
  if (cards.length > 0) {
    const ready = cards.filter((c) => c.kind === "success").length;
    return {
      id: "production",
      name: "Production environment",
      actor: "OpenChoreo",
      state: loudestState(cards.map(cardState)),
      note: `${ready} of ${cards.length} components ready`,
    };
  }
  if (canPromote(deploy)) {
    return {
      id: "production",
      name: "Production environment",
      actor: "You",
      state: "attention",
      note:
        connectionCount === null
          ? "Ready to promote."
          : connectionCount === 0
            ? "Ready to promote — this design needs no live configuration."
            : `Ready to promote. You'll provide live configuration for ${connectionCount} connection${connectionCount === 1 ? "" : "s"} — dev credentials never travel.`,
    };
  }
  return {
    id: "production",
    name: "Production environment",
    actor: "You",
    state: "waiting",
    note: "Opens once the dev deployment settles and validation has its say.",
  };
}
