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

import type { components } from "../../../generated/aep-api";

type ProjectStatus = components["schemas"]["ProjectStatus"];

// One rendered stage of the overview pipeline (#183). Views are pure
// derivations of the ProjectStatus stage aggregates — the spec stage in
// particular has no stored status: exists/version/dirty decide everything.
export type StageTone = "ghost" | "neutral" | "info" | "warning" | "success" | "error";

// StageTone → Oxygen/MUI Chip colour. Lives beside the tone union so every
// consumer maps it identically; "ghost"/"neutral" are ours, "default" is theirs.
export const CHIP_COLOR: Record<
  StageTone,
  "default" | "info" | "warning" | "success" | "error"
> = {
  ghost: "default",
  neutral: "default",
  info: "info",
  warning: "warning",
  success: "success",
  error: "error",
};

export interface StageView {
  /** Version chip text ("v1", "v1+"); "" renders an em-dash. */
  version: string;
  /** One-line state under the chip. */
  line: string;
  tone: StageTone;
  /** Spec stage only: render the Generate-spec CTA instead of a state. */
  cta?: boolean;
}

export function specStageView(status: ProjectStatus): StageView {
  const { exists, version, dirty } = status.spec;
  if (!exists) return { version: "", line: "", tone: "neutral", cta: true };
  if (!version) return { version: "", line: "draft · not published", tone: "info" };
  if (dirty) return { version: `${version}+`, line: "draft changes", tone: "warning" };
  return { version, line: "published", tone: "success" };
}

// The build stage is COUNT-FREE. A per-version task tally can only come from
// the version's milestone on GitHub, and this aggregate is polled at 5s, so the
// status read does not carry one — the Builds page renders counts from the
// issue list it already pays for. What the overview says instead is what the
// version's run is doing, which is a run row and costs nothing.
export function buildStageView(status: ProjectStatus): StageView {
  const { version, status: state } = status.build;
  switch (state) {
    case "running":
      return { version, line: "building", tone: "info" };
    case "failed":
      return { version, line: "build failed", tone: "error" };
    case "succeeded":
      return { version, line: "built", tone: "success" };
    default:
      return { version: "", line: "waiting on spec", tone: "ghost" };
  }
}

export function deployStageView(status: ProjectStatus): StageView {
  const { version, status: state, components: comps } = status.deploy;
  switch (state) {
    case "deploying":
      return {
        version,
        line: `deploying · ${comps.ready}/${comps.total} components`,
        tone: "info",
      };
    case "deployed": {
      // Validation runs after the components deploy, so its state is appended to
      // the live-in-dev line (deployed stays the deploy tone; validation is
      // informational text here — the deployments board carries the loud chip).
      const v = validationView(status.deploy.validation);
      return {
        version,
        line: v ? `live in dev · ${v.label}` : "live in dev",
        tone: "success",
      };
    }
    case "failed":
      return { version, line: "deploy failed", tone: "error" };
    default: {
      // No live deploy status — but validation only runs after the app
      // deploys, so a non-none validation state means it IS live in dev.
      // Surface it even when the binding read lags or returns nothing (a
      // transient/degraded deploy-status read must not hide validation).
      const v = validationView(status.deploy.validation);
      if (v) return { version, line: `live in dev · ${v.label}`, tone: v.tone };
      return { version: "", line: "nothing deployed", tone: "ghost" };
    }
  }
}

// The two deploy.validation values that mean the LOOP is still working, so a
// verdict beside them is the last attempt's rather than the run's answer. Neither
// is ever a stored verdict, which is why they can only come from deploy.validation.
const VALIDATION_IN_FLIGHT = new Set(["running", "awaiting-fix"]);

// The verdicts the loop repeats — delivery.ValidationVerdictFailsRun in the
// console's terms. Every other verdict is final the moment it is written, so a
// lifecycle value cannot legitimately sit over one.
const VALIDATION_REPEATED = new Set(["failed", "unreported"]);

/**
 * The validation state to RENDER, from the two facts the console holds separately:
 * `deploy.validation` (the loop's position, the only source of `running` and
 * `awaiting-fix`) and the run row's stored verdict (the last attempt's answer).
 *
 * Both are needed because neither is sufficient. `RunValidation.verdict` is a
 * COLUMN — six verdicts, no lifecycle — so a page reading it alone renders a fatal
 * verdict as terminal while the platform is repairing it and about to validate
 * again. deploy.validation carries the lifecycle but is scoped to the newest
 * validating run on the deploy version's milestone, so the run story a page keyed
 * itself on is the better source for the verdict itself.
 *
 * They are also two separate polls, so they can disagree by one interval. A
 * lifecycle value is therefore honoured only over a verdict the loop actually
 * repeats: a stale `awaiting-fix` beside a green verdict from the newer poll reads
 * as the verdict, not as a repair of something that passed.
 */
export function validationState(deployValidation: string, verdict: string): string {
  if (
    VALIDATION_IN_FLIGHT.has(deployValidation) &&
    (verdict === "" || VALIDATION_REPEATED.has(verdict))
  ) {
    return deployValidation;
  }
  return verdict;
}

// validationView maps deploy.validation to a label + tone, shared by the overview
// deploy line (suffix) and the deployments board chip. null = nothing to show yet.
//
// deploy.validation MIRRORS the run's verdict rather than folding it, so this is a
// one-to-one rename into human words with nothing discarded on the way. That is
// what lets every label name the outcome instead of naming an artifact the reader
// would have to open to find the outcome out — the failing of the old vocabulary,
// where "completed" covered a green run and a red one alike.
//
// `spoken` is the accessible name for the two labels that carry their meaning in
// PUNCTUATION — "validated*" and "validation?". Screen readers do not announce
// either mark, so both would collapse onto a neighbouring state's name ("Validated",
// "Validation") and the distinction the vocabulary exists for would be sighted-only.
// It is absent everywhere else, so the default stays "the name is the visible label"
// and a caller that ignores the field is still correct for seven of the nine states.
export function validationView(
  validation: string,
): { label: string; tone: StageTone; spoken?: string } | null {
  switch (validation) {
    // Two lifecycle values with something to say, both derived from the run's latest
    // CYCLE rather than from "the run is live and has no verdict" — that older rule
    // made the chip claim to be validating through every coding cycle of every run.
    // A validation cycle is in flight:
    case "running":
      return { label: "validating", tone: "info" };

    // A live run REPAIRING a failed validation. Deliberately terse and deliberately
    // silent about WHAT is being fixed: naming it costs a pill twice the width of its
    // neighbours, and the two surfaces a reader lands on next — the verdict tile and
    // the validation cycle's feed — both say it in full. What the label must not do is
    // imply validation is what's being repaired; the cycle in flight is ordinary coding
    // work on the defect validation found. Warning rather than error because the
    // verdict is real but not final: `error` belongs to the settled "validation failed"
    // below, and using it here would read as terminal while the platform is actively
    // resolving it.
    case "awaiting-fix":
      return { label: "awaiting fix", tone: "warning" };

    // The rest are the run's verdict verbatim, which is why each label can name
    // the outcome instead of naming an artifact to go and open.
    case "passed":
      return { label: "validated", tone: "success" };
    case "partial":
      // Something passed, nothing failed, and some criteria were never covered.
      // Green like `passed` (#401 review): nothing about this run failed, and the
      // deployments surface prints the actual counts ("3/6 passed") beside the
      // chip, so the numbers carry the hedge the old asterisk did. The spoken
      // form keeps the distinction screen readers can't get from a shared label.
      return { label: "validated", tone: "success", spoken: "validated, partially" };
    case "failed":
      return { label: "validation failed", tone: "error" };
    case "inconclusive":
      // Nothing was automated, so nothing here is confirmed — and the question mark
      // is exactly that: the state has no answer to report, so the label poses the
      // question instead of dressing "we don't know" up as an outcome. "validation"
      // prefixes it because the chip is read on surfaces (the overview line, the
      // board) where the subject is otherwise the deployment.
      return {
        label: "validation?",
        tone: "warning",
        spoken: "validation inconclusive",
      };
    case "unreported":
      // No usable report at the validation cycle's merge commit, so the run learned
      // nothing. The label points at validation ITSELF having broken rather than at
      // anything the criteria concluded — no criterion produced a result here — and
      // leaves the tile's sentence to say what broke.
      return { label: "validation error", tone: "error" };
    case "skipped":
      // No criteria authored, so the run reached validation and passed over it.
      // Distinct from `none`, which is the run not having got here yet: this one is
      // settled, and its emptiness is a choice the project can revisit.
      return { label: "validation skipped", tone: "neutral" };

    default: // "none" | "" | unknown
      return null;
  }
}
