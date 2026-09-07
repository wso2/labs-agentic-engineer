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
import { validationView } from "./pipeline";

type DeployStage = components["schemas"]["DeployStage"];
type BuildSummary = components["schemas"]["BuildSummary"];

/** The two environments the platform deploys to, in promotion order. */
export const ENVIRONMENTS = ["development", "production"] as const;
export type EnvironmentKey = (typeof ENVIRONMENTS)[number];

/** The route segment as an environment, or null for anything else. */
export function parseEnvironment(segment: string): EnvironmentKey | null {
  return (ENVIRONMENTS as readonly string[]).includes(segment)
    ? (segment as EnvironmentKey)
    : null;
}

export function environmentLabel(environment: EnvironmentKey): string {
  return environment === "development" ? "Development" : "Production";
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
 * Development answers from the deploy AGGREGATE when it has one: that is the
 * platform's own word on the rollout, and it is what the Builds ledger's
 * "Deployed to development" reads too, so the two pages cannot disagree. The
 * aggregate names no other environment, so production (and development while
 * the status poll is still out) folds its bindings instead.
 */
export function environmentStatus(
  environment: EnvironmentKey,
  cards: DeploymentCard[],
  deploy?: DeployStage | undefined,
): EnvironmentStatus {
  if (environment === "development" && deploy) {
    switch (deploy.status) {
      case "deployed":
        return { label: "Deployed", tone: "success", live: false };
      case "deploying":
        return { label: "Deploying", tone: "info", live: true };
      case "failed":
        return { label: "Deploy failed", tone: "error", live: false };
      default:
        return { label: "Nothing deployed", tone: "neutral", live: false };
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
  /** The version running here; the aggregate names development's only. */
  version?: string;
  cards: DeploymentCard[];
  status: EnvironmentStatus;
  live: number;
  total: number;
  deployedAt?: string;
}

/**
 * One row per environment. Development always has a row — every component
 * gets a card there, deployed or not, because absence is information on the
 * board — while production has one only once something is bound to it.
 */
export function environmentRows(
  board: DeploymentBoard,
  deploy?: DeployStage | undefined,
): EnvironmentRow[] {
  const rowOf = (
    environment: EnvironmentKey,
    cards: DeploymentCard[],
  ): EnvironmentRow => {
    const deployedAt = latestDeployedAt(cards);
    return {
      environment,
      label: environmentLabel(environment),
      ...(environment === "development" && deploy?.version
        ? { version: deploy.version }
        : {}),
      cards,
      status: environmentStatus(environment, cards, deploy),
      live: liveCount(cards),
      total: cards.length,
      ...(deployedAt ? { deployedAt } : {}),
    };
  };
  const rows = [rowOf("development", board.development)];
  if (board.production.length > 0) {
    rows.push(rowOf("production", board.production));
  }
  return rows;
}

/** The ledger lists environments that RUN something; an empty dev board is
 *  the page's empty state, not a row reading "Nothing deployed". */
export function ledgerRows(rows: EnvironmentRow[]): EnvironmentRow[] {
  return rows.filter((r) => r.cards.some((c) => c.deployment));
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

export interface ValidationCell {
  label: string;
  tone: StatusTone;
  /** Pulses while a validation cycle is in flight. */
  live: boolean;
  /** Accessible name for a label that hedges with a mark. */
  spoken?: string;
}

/**
 * The ledger's Validation cell — counts when the criteria/report join resolved
 * them, the shared verdict vocabulary otherwise. Only development is validated
 * (the check runs against the dev deployment), so production reads "—" and
 * a dev row with nothing to say yet reads "Not run".
 */
export function validationCell(
  environment: EnvironmentKey,
  validation: string | undefined,
  counts?: ValidationCounts,
): ValidationCell | null {
  if (environment !== "development") return null;
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
