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

import type { StatusTone } from "../../../components/StatusChip";
import type { components } from "../../../generated/aep-api";

type ProjectDeployment = components["schemas"]["ProjectDeployment"];
type DeploymentComponent = components["schemas"]["DeploymentComponent"];
type ValidationSummary = components["schemas"]["ValidationSummary"];

/**
 * Pure derivations for the Deployments board and the deployment detail page
 * (ADR-0020 §5, §6).
 */

export interface DeploymentChip {
  label: string;
  tone: StatusTone;
  live: boolean;
}

/** A deployment's own state, as the table's Status cell says it. */
export function deploymentChip(d: ProjectDeployment): DeploymentChip {
  switch (d.status) {
    case "live":
      return { label: "Live", tone: "success", live: false };
    case "deploying":
      return { label: "Deploying", tone: "info", live: true };
    case "validating":
      return { label: "Validating", tone: "info", live: true };
    case "failed":
      return { label: "Failed", tone: "error", live: false };
    case "superseded":
      // Join-derived, not a state the platform writes: this deployment
      // succeeded and something later replaced it in the same environment.
      return { label: "Superseded", tone: "neutral", live: false };
    default:
      return { label: "Unknown", tone: "neutral", live: false };
  }
}

/** Is this deployment still moving? Drives the board's poll. */
export function isDeploymentLive(d: ProjectDeployment): boolean {
  return d.status === "deploying" || d.status === "validating";
}

export interface ValidationCell {
  label: string;
  tone: StatusTone;
  /** A pill when there is a verdict; plain text while it is still a count. */
  chip: boolean;
}

/**
 * The Validation cell.
 *
 * A verdict earns a pill; a run in flight is a sentence, because "18 of 24
 * checked" is progress and dressing it as a pill would read as a result. A
 * deployment nobody validated says so rather than showing 0 / 0, which looks
 * like a run that found nothing.
 */
export function validationCell(v: ValidationSummary | undefined): ValidationCell {
  if (!v || v.state === "not_run") {
    return { label: "Not run", tone: "neutral", chip: false };
  }
  const passed = v.passed ?? 0;
  const total = v.total ?? 0;
  switch (v.state) {
    case "passed":
      return { label: `${passed} / ${total} passed`, tone: "success", chip: true };
    case "failed":
      return { label: `${passed} / ${total} passed`, tone: "error", chip: true };
    case "running":
      return {
        label: total > 0 ? `${passed} of ${total} checked` : "Validating…",
        tone: "info",
        chip: false,
      };
    default:
      return { label: "Not run", tone: "neutral", chip: false };
  }
}

/**
 * Can this version be promoted out of its environment?
 *
 * The gate the Production card explains: only a version whose validation has
 * PASSED is promotable. A running validation is not a pass, and neither is a
 * deployment that never ran one.
 */
export function isPromotable(d: ProjectDeployment | undefined): boolean {
  return d?.status === "live" && d.validation?.state === "passed";
}

export interface ComponentAction {
  /** The button's words, or null when this component affords nothing. */
  label: string | null;
  /** Which icon the caller should mount — kept as a name so this stays pure. */
  icon: "flask" | "external" | "chat" | null;
}

/**
 * What a component row offers.
 *
 * Driven by `kind` rather than the endpoint's shape, because a web app and a
 * service both have URLs and they are not the same invitation. A component with
 * no endpoint affords nothing however it is kinded — a dead button is worse
 * than none.
 */
export function componentAction(c: DeploymentComponent): ComponentAction {
  if (!c.endpointUrl) return { label: null, icon: null };
  switch (c.kind) {
    case "service":
      return { label: "Try API", icon: "flask" };
    case "webapp":
      return { label: "Visit", icon: "external" };
    case "agent":
      return { label: "Try agent", icon: "chat" };
    default:
      return { label: null, icon: null };
  }
}

/** A component's readiness, in OpenChoreo's own words where it has them. */
export function componentChip(c: DeploymentComponent): DeploymentChip {
  const status = c.status || "Pending";
  const normalized = status.toLowerCase();
  if (normalized === "ready" || normalized === "succeeded" || normalized === "active") {
    return { label: status, tone: "success", live: false };
  }
  if (normalized === "failed" || normalized === "error") {
    return { label: status, tone: "error", live: false };
  }
  if (normalized === "progressing" || normalized === "pending") {
    return { label: status, tone: "info", live: true };
  }
  // An unrecognised reason is still shown — the operator's vocabulary is the
  // point — just without a tone that would assert something about it.
  return { label: status, tone: "neutral", live: false };
}

/**
 * "4 of 4 components live" — the environment card's one-line summary.
 * Returns null when there is nothing deployed to describe.
 */
export function componentTally(
  components: DeploymentComponent[] | undefined,
): { ready: number; total: number } | null {
  if (!components || components.length === 0) return null;
  const ready = components.filter((c) => componentChip(c).tone === "success").length;
  return { ready, total: components.length };
}

/**
 * The newest deployment in an environment — what the environment card is about.
 *
 * `items` arrives newest-first, but this does not lean on that: an environment
 * card showing the wrong deployment because the server reordered would be a
 * silent, plausible-looking lie.
 */
export function currentDeployment(
  items: ProjectDeployment[] | undefined,
  environment: string,
): ProjectDeployment | undefined {
  const inEnv = (items ?? []).filter((d) => d.environment === environment);
  if (inEnv.length === 0) return undefined;
  return inEnv.reduce((newest, d) =>
    new Date(d.deployedAt).getTime() > new Date(newest.deployedAt).getTime() ? d : newest,
  );
}
