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
import { validationView } from "./pipeline";

type ProjectStatus = components["schemas"]["ProjectStatus"];

/**
 * THE TRACK: spec → build → deploy as one flow.
 *
 * This replaced three separate stage cards. Three cards with three borders read
 * as three features; they are one version moving through three gates, so they
 * are one bar whose legs are divided by a seam a chevron points across.
 *
 * The legs carried a step numeral ("01", "02", "03") until it was cut. Three
 * stages laid left to right with a chevron between each pair are already in
 * order; the numeral said the same thing a second time, in the loudest slot on
 * the row, and it read as an identifier — a version, a count — rather than as
 * a position nobody was in any doubt about.
 *
 * Two rules the card grammar could not express, both forced by states the cards
 * rendered wrong:
 *
 *   - MORE THAN ONE LEG CAN BE UNSETTLED. Amending the spec while the platform
 *     builds what you last published is a legitimate state, not a race. The old
 *     "one active card" reading had to pick, and picked wrong.
 *   - A LEG IS A LINK, NEVER A BUTTON. Every way of STARTING work lives on the
 *     page that owns it (#562 moved them there and this keeps them there), so
 *     the track navigates and summarises. It never sends.
 */

/**
 * What a leg is doing, which is not the same as what the platform is doing.
 *
 * `live` and `hold` are the distinction the design turns on: `live` is the
 * platform working (pulse, accent), `hold` is the leg waiting on the USER (no
 * pulse, amber) — a draft nobody published, a question nobody answered. A page
 * that animates while it waits for you to type is lying about who is busy.
 */
export type LegState = "waiting" | "live" | "hold" | "done" | "failed";

export interface TrackLeg {
  name: string;
  /** Version chip text; "" renders nothing — there is no em-dash placeholder. */
  version: string;
  line: string;
  state: LegState;
  /** The leg's destination. A literal so the router can typecheck it. */
  to: LegRoute;
  /**
   * A call to action, on the one state where the leg is somewhere to GO AND ACT
   * rather than somewhere to look.
   *
   * Not a button, and it must never become one: the whole leg is already the
   * link, and a button inside a link is a broken target. This is the wording
   * that tells the reader the leg they are looking at is the thing to click.
   * Absent on every other state, where the line says everything.
   */
  cta?: string;
}

/** Every leg links to the section that runs it — these three, and no others. */
type LegRoute =
  | "/projects/$projectName/spec"
  | "/projects/$projectName/builds"
  | "/projects/$projectName/deployments";

export interface TrackSummary {
  text: string;
  tone: "info" | "warning";
}

export interface TrackView {
  legs: TrackLeg[];
  /**
   * One sentence, or nothing. It earns its slot ONLY by saying something no
   * single leg can — relating two legs to each other. A summary that
   * paraphrases the leg above it is the "says the same thing twice" problem
   * this page was rebuilt to fix, so most states have no summary at all.
   */
  summary: TrackSummary | null;
}

function specLeg(status: ProjectStatus, engaged: boolean): TrackLeg {
  const { exists, version, dirty, agent } = status.spec;
  const leg = { name: "Spec", to: "/projects/$projectName/spec", version } as const;

  // A live state overrides the LINE only — the version is a separate fact, so
  // an amendment interview on v2 still reads as v2.
  if (agent === "working") {
    return {
      ...leg,
      state: "live",
      line: exists
        ? "The agent is working on your spec"
        : "The agent is writing your requirements",
    };
  }
  // No server field can produce this: `spec.agent` folds a completed turn to
  // "", and a turn that ended ON a question is exactly that. The local chat log
  // is the only thing that knows.
  if (engaged) {
    // The same words the chat panel's questions pointer uses, because they go
    // to the same place — that pointer's handler is `openSpec`, which is this
    // leg's `to`. Two surfaces naming one action, so answering is one thing the
    // reader learns once rather than two affordances they have to tell apart.
    return {
      ...leg,
      state: "hold",
      line: "The agent has questions for you",
      cta: "Answer them",
    };
  }
  // Only when nothing was written. A failed design turn over a published spec
  // is not a spec that failed to start, and the spec view banners that case.
  if (agent === "failed" && !exists) {
    return {
      ...leg,
      version: "",
      state: "failed",
      // Two sentences in a one-line slot, and the second told the reader to
      // open the very thing they were reading — the leg IS the link to the
      // spec. The recovery is a call to action like any other.
      line: "The agent couldn't start",
      cta: "Try again",
    };
  }
  if (!exists) {
    return { ...leg, version: "", state: "waiting", line: "Nothing written yet" };
  }
  if (!version) {
    return { ...leg, version: "", state: "hold", line: "Draft, not published" };
  }
  if (dirty) {
    return { ...leg, state: "hold", line: "Draft changes, not published" };
  }
  return { ...leg, state: "done", line: "Published" };
}

function buildLeg(status: ProjectStatus): TrackLeg {
  const { version, status: state } = status.build;
  const leg = { name: "Build", to: "/projects/$projectName/builds" } as const;
  switch (state) {
    case "running":
      return { ...leg, version, state: "live", line: "Building" };
    case "failed":
      return { ...leg, version, state: "failed", line: "Build failed" };
    case "succeeded":
      return { ...leg, version, state: "done", line: "Built" };
    default:
      // The user's situation, not the system's dependency: "waiting on spec"
      // named a machine's blocker, and nobody reading it was the machine.
      return { ...leg, version: "", state: "waiting", line: "Nothing built yet" };
  }
}

/**
 * Validation the platform is still doing, as opposed to a verdict it has
 * reached.
 *
 * Both of these are the platform at work and both end on their own: `running`
 * is a validation cycle in flight, and `awaiting-fix` is a coding cycle
 * repairing what validation found. Neither is waiting on the reader, which is
 * what keeps them `live` rather than `hold`.
 */
function validationInFlight(validation: string): boolean {
  return validation === "running" || validation === "awaiting-fix";
}

function deployLeg(status: ProjectStatus): TrackLeg {
  const { version, status: state, components: comps, validation } = status.deploy;
  const leg = { name: "Deploy", to: "/projects/$projectName/deployments" } as const;
  // Validation is a PHASE OF DEPLOYING, not a fourth gate: it only runs once
  // the components are up, so it rides this line rather than adding a leg that
  // would be empty in most states of the flow.
  const v = validationView(validation);
  const live = v ? `Live in dev · ${v.label.toLowerCase()}` : "Live in dev";

  // Being a phase of deploying means the leg has to STAY unsettled through it.
  // Reaching `deployed` used to fold validation down to error-or-not, so a
  // version the platform was actively validating went quiet the moment its
  // components came up — the track went dark in the middle of the one stage it
  // was reporting on, and the only page that admitted anything was happening
  // was the deployments board. Rendered here, before the switch, because the
  // binding read lags: a validation cycle can be in flight while `state` still
  // says `none`.
  if (validationInFlight(validation)) {
    return { ...leg, version, state: "live", line: live };
  }

  switch (state) {
    case "deploying":
      return {
        ...leg,
        version,
        state: "live",
        line: `Deploying · ${comps.ready}/${comps.total} components`,
      };
    case "deployed":
      // A red validation verdict does not un-deploy the version — the leg
      // fails, and the version chip stays, because that version really is
      // what is running in dev.
      return { ...leg, version, state: v?.tone === "error" ? "failed" : "done", line: live };
    case "failed":
      return { ...leg, version, state: "failed", line: "Deploy failed" };
    default:
      // Validation only runs after the app deploys, so a validation state means
      // it IS live even when the binding read lags or returns nothing.
      if (v) {
        return { ...leg, version, state: v.tone === "error" ? "failed" : "done", line: live };
      }
      return { ...leg, version: "", state: "waiting", line: "Nothing deployed yet" };
  }
}

/**
 * The summary, in the two states where one leg cannot tell the whole story.
 *
 * Both are about the relationship BETWEEN legs, which is exactly what a
 * per-leg line has no way to say. Everything else gets no summary: the legs
 * already said it, and repeating them in a sentence underneath is the
 * duplication this page was rebuilt to remove.
 */
function trackSummary(status: ProjectStatus): TrackSummary | null {
  const { spec, build, deploy } = status;
  const building = build.status === "running";

  // Amending the spec while the platform builds the last published version.
  // The danger this names is real and invisible from the legs alone: nothing
  // the user is typing is in the build they are watching.
  if (building && spec.dirty && spec.version) {
    return {
      tone: "warning",
      text: `Building ${build.version} without your draft changes. Publish the spec to include them.`,
    };
  }
  // A newer version building over an older one still serving dev.
  if (building && build.version && deploy.version && build.version !== deploy.version) {
    return {
      tone: "info",
      text: `Building ${build.version}. ${deploy.version} stays live in dev until it deploys.`,
    };
  }
  return null;
}

export function trackView(status: ProjectStatus, engaged: boolean): TrackView {
  return {
    legs: [specLeg(status, engaged), buildLeg(status), deployLeg(status)],
    summary: trackSummary(status),
  };
}
