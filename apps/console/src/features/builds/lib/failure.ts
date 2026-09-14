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

type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type RunFailure = components["schemas"]["RunFailure"];

/**
 * The one place a run's failure is put into words.
 *
 * The platform sends CODES — `RunFailure.code` when it recorded a fault, the
 * run's `terminalReason` when it only named the phase — and this module owns
 * the sentence for each, the way `@aep/progress-view` owns a notice code's. The
 * ledger chip, the build page's card and the overview's track all read from
 * here, so one outcome cannot be said three ways (lexicon, *Builds › A failed
 * run explains itself*).
 *
 * Every sentence names the situation, not the state machine (naming rule 6),
 * and says three things in order: what happened, what it did NOT do (so the
 * reader knows nothing was coded or deployed), and whether trying again can
 * help. The next step is a link when the platform knows where the fix lives.
 */

/** Where the reader goes next. `to` is a TanStack route; `search` its params. */
export interface FailureNext {
  label: string;
  to: "/projects/$projectName/spec" | "/projects/$projectName/deployments" | "/projects/$projectName/validation";
  search?: { file: string };
}

export interface FailureCopy {
  /** `error` for a settled failure, `warning` while the platform is still retrying. */
  tone: "error" | "warning";
  title: string;
  body: string;
  next?: FailureNext;
  /** The card's disclosure: the platform's own facts, for a bug report. */
  details: FailureDetails;
}

export interface FailureDetails {
  code: string;
  permanent: boolean | undefined;
  attempts: string | undefined;
  window: string | undefined;
  detail: string | undefined;
  runId: string;
  workflowId: string | undefined;
}

/** The short qualifier after the middot: `Failed · <this>`. */
export function failureLabel(code: string | undefined): string | undefined {
  if (!code) return undefined;
  return SHORT_LABELS[code] ?? code;
}

const SHORT_LABELS: Record<string, string> = {
  // RunFailure codes.
  "dependency-unprovisionable": "Dependency could not be provisioned",
  "dependency-provision-failed": "Dependency provisioning failed",
  "plan-turn-failed": "Planning failed",
  "repository-unavailable": "Repository unavailable",
  // Terminal reasons, for a run with no record.
  "plan-failed": "Planning failed",
  "redispatch-budget": "Coding agent stopped",
  "build-retrigger-budget": "Component build failed",
  "deploy-budget": "Deployment did not become ready",
  "version-incomplete": "Version incomplete",
  "fix-chain-budget": "Repair budget spent",
  "conflict-budget": "Merge conflicts unresolved",
  "no-progress": "No progress",
  "cycle-ceiling": "Cycle ceiling reached",
  "validation-failed": "Validation failed",
  "validation-unreported": "Validation not reported",
  "agent-quota-blocked": "Agent quota reached",
  "publisher-credentials-missing": "Publisher credentials missing",
};

/**
 * The card's copy for a run, or `undefined` when there is nothing to explain:
 * a run that met no fault and did not fail, or a cancelled run — a person
 * stopping an increment is not a fault with a cause to report.
 */
export function failureCopy(run: MilestoneRunView): FailureCopy | undefined {
  const failed = run.state === "failed";
  const f = run.failure;
  if (run.state === "cancelled") return undefined;
  if (!failed && !f) return undefined;
  // A run still moving with a record is a fault being RETRIED; a blocked run's
  // card is the existing message's job. Only failed and retrying render here.
  if (!failed && run.state !== "planning" && run.state !== "running") return undefined;

  const details = detailsOf(run);
  if (f) {
    const retrying = !failed;
    const copy = codeCopy(f, retrying);
    return { ...copy, tone: retrying ? "warning" : "error", details };
  }
  return { ...reasonCopy(run), tone: "error", details };
}

function subject(f: RunFailure): string {
  return f.dependency ? `\`${f.dependency}\`` : "a dependency";
}

function owner(f: RunFailure): string {
  return f.component ? `${f.component} depends on it. ` : "";
}

function attemptsPhrase(f: RunFailure): string {
  if (f.maxAttempts > 0) return `attempt ${f.attempts} of ${f.maxAttempts}`;
  return `attempt ${f.attempts}`;
}

const NOTHING_HAPPENED = "Nothing was coded or deployed.";

function codeCopy(f: RunFailure, retrying: boolean): Omit<FailureCopy, "tone" | "details"> {
  switch (f.code) {
    case "dependency-unprovisionable":
      return {
        title: `The platform could not provision ${subject(f)}`,
        body:
          `${owner(f)}The design declares it in a shape the platform cannot create a resource from — ` +
          `an org resource with no configuration keys, or a resource type the cluster does not have. ` +
          `${NOTHING_HAPPENED} Retrying cannot fix this; the design needs a change, then a new build.`,
        ...(f.dependency ? { next: openInDesign(f.dependency) } : {}),
      };
    case "dependency-provision-failed":
      return retrying
        ? {
            title: `Provisioning ${subject(f)} failed — retrying (${attemptsPhrase(f)})`,
            body: `${owner(f)}The platform met an error it does not consider final and is trying again. No action is needed yet.`,
          }
        : {
            title: `The platform could not provision ${subject(f)}`,
            body:
              `${owner(f)}It tried ${f.attempts} time${f.attempts === 1 ? "" : "s"} and met the same error each time. ` +
              `${NOTHING_HAPPENED} This may be transient — build again; if it repeats, the details below name the error.`,
          };
    case "plan-turn-failed":
      return retrying
        ? {
            title: `Planning the version's tasks failed — retrying (${attemptsPhrase(f)})`,
            body: "The planning turn met an error the platform is retrying. No action is needed yet; cancel the build if it does not recover.",
          }
        : {
            title: "The platform could not plan the version's tasks",
            body: `The planning turn failed. ${NOTHING_HAPPENED} Build again; if it repeats, the details below name the error.`,
          };
    case "repository-unavailable":
      return {
        title: "The project's repository could not be reached",
        body: `The repository, an issue, or the credential the platform uses for it is gone. ${NOTHING_HAPPENED} Retrying cannot fix this — check the repository connection in Settings.`,
      };
    default:
      return {
        title: `The build failed — ${failureLabel(f.code) ?? f.code}`,
        body: `${NOTHING_HAPPENED} The details below carry what the platform recorded.`,
      };
  }
}

/** A run that failed before the record existed, or in a phase no producer records yet. */
function reasonCopy(run: MilestoneRunView): Omit<FailureCopy, "tone" | "details"> {
  const reason = run.terminalReason;
  const newest = run.cycles.at(-1);
  const agentReason = newest?.agentReason ? ` The runner reported: ${newest.agentReason}.` : "";
  switch (reason) {
    case "plan-failed":
      return {
        title: "The build failed while preparing the version",
        body: `The platform could not provision the version's connections or plan its tasks. ${NOTHING_HAPPENED} It recorded no further details for this run.`,
      };
    case "redispatch-budget":
      return {
        title: "The coding agent stopped without opening a pull request",
        body: `The platform dispatched it twice and it stopped both times.${agentReason} Open the coding agent log for what it did before it stopped.`,
      };
    case "fix-chain-budget":
    case "cycle-ceiling":
    case "no-progress":
      return {
        title: "The run used up its repair attempts",
        body: "The coding agent kept working the version's tasks without landing them. Open the coding agent log for the last cycle; a spec change and a new build is the way forward.",
      };
    case "conflict-budget":
      return {
        title: "The run could not resolve its merge conflicts",
        body: "Two conflict cycles did not produce a mergeable pull request. Open the coding agent log for the last cycle.",
      };
    case "build-retrigger-budget":
      return {
        title: "A component's build failed twice",
        body: "The code merged but did not build. The Build logs section below carries the failing step.",
      };
    case "deploy-budget":
    case "version-incomplete":
      return {
        title: "The version built but did not come up",
        body: "Its components were built, and a deployment never reached Ready. The Deployments board names the component.",
        next: { label: "Go to Deployments", to: "/projects/$projectName/deployments" },
      };
    case "validation-failed":
    case "validation-unreported":
      return {
        title: reason === "validation-failed" ? "Validation failed" : "Validation reported nothing",
        body: "The version deployed and its validation criteria were not met. The Validation page carries the report.",
        next: { label: "View validations", to: "/projects/$projectName/validation" },
      };
    default:
      return {
        title: reason ? `The build failed — ${failureLabel(reason)}` : "The build failed",
        body: "The platform recorded no further details for this run.",
      };
  }
}

function openInDesign(dependency: string): FailureNext {
  return {
    label: `Open ${dependency} in the design`,
    to: "/projects/$projectName/spec",
    search: { file: `specs/design/dependencies/${dependency}/dependency.json` },
  };
}

function detailsOf(run: MilestoneRunView): FailureDetails {
  const f = run.failure;
  return {
    code: f?.code ?? run.terminalReason ?? "",
    permanent: f?.permanent,
    attempts: f ? (f.maxAttempts > 0 ? `${f.attempts} of ${f.maxAttempts}` : `${f.attempts}`) : undefined,
    window: f ? `${formatStamp(f.firstAt)} → ${formatStamp(f.lastAt)}` : undefined,
    detail: f?.detail || undefined,
    runId: run.id,
    workflowId: f?.workflowId,
  };
}

function formatStamp(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

/** The details as one block of text — what a reader pastes into a bug report. */
export function detailsText(d: FailureDetails): string {
  const lines = [
    `code: ${d.code}${d.permanent === undefined ? "" : d.permanent ? " · permanent" : " · retryable"}`,
    d.attempts ? `attempts: ${d.attempts}` : undefined,
    d.window ? `window: ${d.window}` : undefined,
    d.detail ? `recorded: ${d.detail}` : undefined,
    `run: ${d.runId}`,
    d.workflowId ? `workflow: ${d.workflowId}` : undefined,
  ];
  return lines.filter((l): l is string => Boolean(l)).join("\n");
}
