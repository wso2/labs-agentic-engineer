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

// The Deployments board as an ENVIRONMENT LEDGER (ADR-0027): a card per
// environment, then one ledger row per environment that has something bound.
//
// Everything here derives from reads the console already makes — the
// component/binding join (deploymentRows), the deploy aggregate on the status
// poll, and the version ledger's milestone numbers. The platform keeps no
// deployment RECORD (OpenChoreo release bindings are current state, overwritten
// on redeploy), so a row is "what this environment runs now", never a history
// entry; the design's superseded and failed past deployments have no source
// and are not invented.

import type { StatusTone } from "../../../components/StatusChip";
import type { components } from "../../../generated/aep-api";
import type { ValidationCounts } from "../../validation/lib/verdict";
import type { DeploymentBoard, DeploymentCard } from "./deploymentRows";
import { labelOf, type EnvironmentInfo } from "./environments";
import { validationView } from "./pipeline";

type DeployStage = components["schemas"]["DeployStage"];
type BuildSummary = components["schemas"]["BuildSummary"];

/**
 * An environment is a NAME the platform's pipeline gave it — not one of two
 * words the console knows. Which names exist, and in what order, is the
 * environments list's answer (`useEnvironments`), never a constant here.
 */
export type EnvironmentKey = string;

/** What to call an environment on screen: its display name, falling back to
 *  the raw name when the list does not know it (a URL naming a dead
 *  environment still has to render something). */
export function environmentLabel(
  env: EnvironmentInfo | undefined,
  name: EnvironmentKey,
): string {
  return labelOf(env, name);
}

export interface EnvironmentStatus {
  label: string;
  tone: StatusTone;
  /** A pulsing dot and a tinted row: the environment is still converging. */
  live: boolean;
}

/** A board card's state, folded onto the environment it sits in. */
function cardsStatus(cards: DeploymentCard[]): EnvironmentStatus {
  const bound = cards.filter((c) => c.deployment);
  if (bound.length === 0) {
    return { label: "Nothing deployed", tone: "neutral", live: false };
  }
  if (bound.some((c) => c.kind === "error")) {
    return { label: "Deploy failed", tone: "error", live: false };
  }
  if (bound.some((c) => c.kind === "transitional" || c.kind === "unknown")) {
    return { label: "Deploying", tone: "info", live: true };
  }
  if (bound.every((c) => c.kind === "undeployed")) {
    return { label: "Undeployed", tone: "neutral", live: false };
  }
  return { label: "Deployed", tone: "success", live: false };
}

/**
 * What an environment says about itself — the card's chip and the ledger's
 * Status cell, one vocabulary (lexicon, *Deployments*).
 *
 * The pipeline's FIRST environment answers from the deploy AGGREGATE when it
 * has one: the aggregate tracks the rollout of a completed build, which lands
 * there and nowhere else, and it is what the Builds ledger reads too, so the
 * two pages cannot disagree. The aggregate names no other environment, so
 * every later one (and the first while the status poll is still out) folds
 * its bindings instead.
 */
export function environmentStatus(
  env: EnvironmentInfo,
  cards: DeploymentCard[],
  deploy?: DeployStage | undefined,
): EnvironmentStatus {
  if (env.position === 0 && deploy) {
    switch (deploy.status) {
      case "deployed":
        return { label: "Deployed", tone: "success", live: false };
      case "deploying":
        return { label: "Deploying", tone: "info", live: true };
      case "failed":
        return { label: "Deploy failed", tone: "error", live: false };
      default:
        // `none` is the aggregate's word for "no rollout it is tracking" — but
        // a binding that is Ready is deployed whatever the aggregate tracks
        // (a version deployed before the aggregate existed, or after its run
        // settled). Live bindings under a `none` fold like a later
        // environment's do; only an empty environment reads "Nothing
        // deployed".
        return cards.some((c) => c.deployment)
          ? cardsStatus(cards)
          : { label: "Nothing deployed", tone: "neutral", live: false };
    }
  }
  return cardsStatus(cards);
}

/**
 * Chip vocabulary for one component's binding (#216): the label keeps the
 * backend's raw condition reason (the vocabulary operators see in OpenChoreo);
 * only the two join-derived states get console-authored labels.
 */
export function cardChip(card: DeploymentCard): {
  label: string;
  tone: StatusTone;
  outlined?: boolean;
} {
  switch (card.kind) {
    case "notDeployed":
      return { label: "Not deployed", tone: "neutral", outlined: true };
    case "undeployed":
      return { label: "Undeployed", tone: "neutral" };
    case "success":
      return { label: card.deployment?.status ?? "Ready", tone: "success" };
    case "error":
      return { label: card.deployment?.status ?? "Failed", tone: "error" };
    case "transitional":
      return { label: card.deployment?.status ?? "In progress", tone: "info" };
    default:
      return { label: "Pending", tone: "neutral", outlined: true };
  }
}

/** How many of the environment's components are serving. */
export function liveCount(cards: DeploymentCard[]): number {
  return cards.filter((c) => c.kind === "success").length;
}

/** The newest binding's stamp — when the environment last changed. */
export function latestDeployedAt(cards: DeploymentCard[]): string | undefined {
  return cards
    .map((c) => c.deployment?.createdAt ?? "")
    .filter(Boolean)
    .sort()
    .at(-1);
}

/**
 * "2h ago" — the card header's age. Coarse on purpose: the exact stamp is one
 * row down in the ledger, and a header that reads "2h 14m 06s ago" is a clock,
 * not a headline.
 */
export function agoLabel(iso: string, now: number = Date.now()): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return "";
  const minutes = Math.max(0, Math.round((now - then) / 60_000));
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

export interface EnvironmentRow {
  environment: EnvironmentKey;
  label: string;
  /** The version running here; the aggregate names the first environment's
   *  only. */
  version?: string;
  cards: DeploymentCard[];
  status: EnvironmentStatus;
  live: number;
  total: number;
  deployedAt?: string;
}

/**
 * One row per environment, in the order the environments list came in — the
 * platform's promotion order, which this function never re-sorts. Every
 * environment gets a row whether or not anything is bound to it: absence is
 * information on the board, and an environment the pipeline names but nothing
 * reaches is exactly what a reader needs to see.
 */
export function environmentRows(
  board: DeploymentBoard,
  environments: EnvironmentInfo[],
  deploy?: DeployStage | undefined,
): EnvironmentRow[] {
  return environments.map((env) => {
    const cards = board.get(env.name) ?? [];
    const deployedAt = latestDeployedAt(cards);
    return {
      environment: env.name,
      label: environmentLabel(env, env.name),
      // The aggregate names one version: the one the build rolled out, in the
      // environment a build lands in.
      ...(env.position === 0 && deploy?.version ? { version: deploy.version } : {}),
      cards,
      status: environmentStatus(env, cards, deploy),
      live: liveCount(cards),
      total: cards.length,
      ...(deployedAt ? { deployedAt } : {}),
    };
  });
}

/** "Milestone #3" for the version an environment runs, when the ledger knows it. */
export function milestoneFor(
  version: string | undefined,
  builds: BuildSummary[] | undefined,
): string | undefined {
  if (!version) return undefined;
  const build = builds?.find((b) => b.tag === version);
  return build ? `Milestone #${build.milestoneNumber}` : undefined;
}

/** The ledger entry a version's tag names, when the ledger knows it — the one
 *  row that carries both the milestone number and the build's own stamps. */
export function buildFor(
  version: string | undefined,
  builds: BuildSummary[] | undefined,
): BuildSummary | undefined {
  if (!version) return undefined;
  return builds?.find((b) => b.tag === version);
}

export interface ValidationCell {
  label: string;
  tone: StatusTone;
  /** Pulses while a validation cycle is in flight. */
  live: boolean;
  /** Accessible name for a label that hedges with a mark. */
  spoken?: string;
  /** The read behind the word is still out: draw a skeleton, not a word. */
  pending?: boolean;
}

/** Whether the deployed version's verdict can be read at all (deploymentFlow
 *  `deployedValidationState`): still out, or failed. */
export type ValidationAvailability = "pending" | "failed";

/**
 * The ledger's Validation cell — counts when the criteria/report join resolved
 * them, the shared verdict vocabulary otherwise. The verdict this cell renders
 * is the deploy aggregate's, and the aggregate judges the run against the
 * deployment a build lands in — the pipeline's FIRST environment. So every
 * later environment reads "—" (there is no verdict of its own to read), and a
 * first-environment row with nothing to say yet reads "Not run".
 */
export function validationCell(
  env: EnvironmentInfo | undefined,
  validation: string | undefined,
  counts?: ValidationCounts,
  availability?: ValidationAvailability,
): ValidationCell | null {
  if (env?.position !== 0) return null;
  // An unread verdict is not "Not run" — that is a settled claim, and the
  // read that would settle it is still out, or failed (#776 review).
  if (availability === "pending") return { label: "", tone: "neutral", live: false, pending: true };
  if (availability === "failed") {
    return {
      label: "Unavailable",
      tone: "neutral",
      live: false,
      spoken: "unavailable, the run story could not be loaded",
    };
  }
  const view = validationView(validation ?? "");
  if (!view) return { label: "Not run", tone: "neutral", live: false };
  const tone: StatusTone = view.tone === "ghost" ? "neutral" : view.tone;
  if (counts && validation !== "running" && validation !== "awaiting-fix") {
    return {
      label: `${counts.passed} / ${counts.total} passed`,
      tone,
      live: false,
      ...(view.spoken ? { spoken: `${view.spoken}, ${counts.passed} of ${counts.total} passed` } : {}),
    };
  }
  return {
    label: view.label,
    tone,
    live: validation === "running",
    ...(view.spoken ? { spoken: view.spoken } : {}),
  };
}

/** The short form of a merge SHA, as GitHub prints it. */
export function shortSha(sha: string): string {
  return sha.slice(0, 7);
}

/** The commit's page on the project's repository. `repoUrl` is the platform's
 *  CLONE url, so the `.git` suffix comes off first (see BuildsPage). */
export function commitUrl(repoUrl: string | undefined, sha: string): string | undefined {
  if (!repoUrl || !sha) return undefined;
  const root = repoUrl.replace(/\/+$/, "").replace(/\.git$/, "");
  return `${root}/commit/${sha}`;
}

/** The milestone's page on the project's repository — the same `repoUrl`
 *  normalisation `commitUrl` makes, so the two links can never disagree about
 *  which repository they point at. */
export function milestoneUrl(
  repoUrl: string | undefined,
  milestoneNumber: number | undefined,
): string | undefined {
  if (!repoUrl || !milestoneNumber) return undefined;
  const root = repoUrl.replace(/\/+$/, "").replace(/\.git$/, "");
  return `${root}/milestone/${milestoneNumber}`;
}
