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

import type { ProjectBuild } from "../../builds/api/builds";
import type { ValidationOutcome } from "../../builds/model/validation";
import type { BuildOffer } from "../../builds/model/picker";
import type { DesignComment, DesignModel } from "../../design/api/designModel";
import type { DesignQueue, FeatureView } from "../../spec/model/workspace";
import type { DeployStage } from "../../deploy/api/deploy";

// Where the project is, leg by leg, worked out from what the rest of the page
// already reads: the spec workspace (the model and the live documents), the
// design review, the builds with the offer made from them, and the project's
// deploy aggregate. Pure, and read from the same state as the feature rows,
// the design card, the build picker and the Deploy Page, so the track can
// never say something they do not.

/** A leg's status lamp: finished, running now, waiting on the user, or not started. */
export type LegState = "done" | "live" | "waiting" | "notyet";

export interface TrackLeg {
  state: LegState;
  /** The short state line under the leg's title: "5 features". */
  summary: string;
}

export interface ProjectTrack {
  spec: TrackLeg;
  design: TrackLeg;
  build: TrackLeg;
  deploy: TrackLeg;
}

export interface TrackInput {
  features: Pick<FeatureView, "name" | "stage" | "blocking" | "toConfirm">[];
  /** What the next design turn takes (model/designWork.ts). */
  design: Pick<DesignQueue, "toDesign" | "outOfDate">;
  /** Each designed feature, and the spec it was designed from. */
  designedFrom: Readonly<Record<string, string>>;
  review: { running: DesignModel["running"]; comments: Pick<DesignComment, "status">[] };
  /** Oldest first. */
  builds: Pick<ProjectBuild, "version" | "status">[];
  /** The newest build's validation, once it has finished; null before then or while it loads. */
  latestOutcome: Pick<ValidationOutcome, "passed" | "total" | "failing"> | null;
  offer: Pick<BuildOffer, "rows" | "version">;
  /** The deploy aggregate: the rollout of the newest build to the pipeline's first environment. */
  deploy: Pick<DeployStage, "status" | "version">;
}

const NOT_YET: TrackLeg = { state: "notyet", summary: "Not yet" };

function plural(n: number, word: string): string {
  return `${n} ${word}${n === 1 ? "" : "s"}`;
}

/**
 * The spec: being written or an interview under way, then what waits on the
 * user in Next up's order (questions, lines to confirm, interviews), else
 * done.
 */
export function specLeg(features: TrackInput["features"]): TrackLeg {
  if (features.length === 0) return { state: "live", summary: "Writing the spec" };
  const interviewing = features.find((f) => f.stage === "Interviewing");
  if (interviewing) return { state: "live", summary: `Interviewing ${interviewing.name}` };
  const questions = features.reduce((n, f) => n + f.blocking.length, 0);
  if (questions > 0) return { state: "waiting", summary: `${plural(questions, "question")} to answer` };
  const toConfirm = features.reduce((n, f) => n + f.toConfirm, 0);
  if (toConfirm > 0) return { state: "waiting", summary: `${plural(toConfirm, "line")} to confirm` };
  const toInterview = features.filter((f) => f.stage === "Not interviewed").length;
  if (toInterview > 0) return { state: "waiting", summary: `${plural(toInterview, "feature")} to interview` };
  return { state: "done", summary: plural(features.length, "feature") };
}

/**
 * The design: a turn running, then what the design action would take (new
 * features, or designs behind their spec), then comments the user left or
 * has to check, else done once anything is designed.
 */
export function designLeg(input: Pick<TrackInput, "design" | "designedFrom" | "review">): TrackLeg {
  const { design, designedFrom, review } = input;
  const running = review.running;
  if (running?.kind === "design") return { state: "live", summary: `Designing ${plural(running.features.length, "feature")}` };
  if (running?.kind === "address") return { state: "live", summary: "Addressing comments" };
  const { toDesign, outOfDate } = design;
  if (toDesign.length > 0) {
    const allOutOfDate = toDesign.every((id) => outOfDate.includes(id));
    return {
      state: "waiting",
      summary: allOutOfDate ? `${plural(toDesign.length, "feature")} out of date` : `${plural(toDesign.length, "feature")} to design`,
    };
  }
  const open = review.comments.filter((c) => c.status === "open").length;
  if (open > 0) return { state: "waiting", summary: `${plural(open, "comment")} to address` };
  const addressed = review.comments.filter((c) => c.status === "addressed").length;
  if (addressed > 0) return { state: "waiting", summary: `${plural(addressed, "comment")} to check` };
  const designed = Object.keys(designedFrom).length;
  return designed > 0 ? { state: "done", summary: `${plural(designed, "feature")} designed` } : NOT_YET;
}

/**
 * The build: one running, then one that failed or whose validation failed (its
 * fix is the next step), then something the picker offers, then the last
 * build's outcome. Nothing built and nothing to offer is not yet.
 */
export function buildLeg(input: Pick<TrackInput, "builds" | "offer" | "latestOutcome">): TrackLeg {
  const latest = input.builds.at(-1);
  const outcome = input.latestOutcome;
  if (latest?.status === "building") return { state: "live", summary: `Building ${latest.version}` };
  if (latest?.status === "failed") return { state: "waiting", summary: `${latest.version} failed` };
  if (latest && outcome && outcome.failing.length > 0) {
    const which = outcome.failing.map((f) => f.story ?? f.name).join(", ");
    return { state: "waiting", summary: `${latest.version} built · ${which} failing` };
  }
  if (input.offer.rows.some((r) => r.state === "offered")) {
    return { state: "waiting", summary: latest ? `Ready to build ${input.offer.version}` : "Ready to build" };
  }
  if (!latest) return NOT_YET;
  return {
    state: "done",
    summary: outcome ? `${latest.version} built · ${outcome.passed}/${outcome.total} passing` : `${latest.version} built`,
  };
}

/**
 * Deploy: the rollout of the newest build to the first environment, where
 * every build lands by itself. A failed rollout waits on the user; nothing
 * deployed yet is not yet.
 */
export function deployLeg(deploy: TrackInput["deploy"]): TrackLeg {
  const version = deploy.version;
  switch (deploy.status) {
    case "deploying":
      return { state: "live", summary: version ? `Deploying ${version}` : "Deploying" };
    case "failed":
      return { state: "waiting", summary: version ? `${version} failed to deploy` : "Deploy failed" };
    case "deployed":
      return { state: "done", summary: version ? `${version} deployed` : "Deployed" };
    default:
      return NOT_YET;
  }
}

export function projectTrack(input: TrackInput): ProjectTrack {
  return {
    spec: specLeg(input.features),
    design: designLeg(input),
    build: buildLeg(input),
    deploy: deployLeg(input.deploy),
  };
}
