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
import { resetStamp } from "../../../lib/stamp";
import { externalValuesPark } from "./run";
import { orderedTasks, taskNote, taskState, type RunClaims } from "./taskRow";

// Why a build is stuck or failed, in words, and the way out: what the Build
// card says above its tasks. The platform sends codes (the run's recorded
// failure, else the reason it ended); each is put into a sentence here, once,
// copied from the old console's features/builds/lib/failure.ts. Each says what
// happened, what did not (so the reader knows nothing was coded or deployed),
// and whether trying again can help. Beside a failure, the two ways a live
// run stops moving: parked at the deploy gate for a dependency's values, or a
// task blocked on one. Both wait on the write target's Configure card.

type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type RunFailure = components["schemas"]["RunFailure"];
type TaskView = components["schemas"]["TaskView"];

/** Where the reader goes next: a project route, and an environment for its Configure card. */
export type ExplanationNext =
  | { label: string; to: "/projects/$projectName/design" | "/projects/$projectName/deploy" | "/projects/$projectName/validations" }
  | { label: string; to: "/projects/$projectName/deploy/$env/configure"; env: string };

/** The platform's own facts, for a bug report. */
export interface FailureDetails {
  code: string;
  permanent: boolean | undefined;
  attempts: string | undefined;
  detail: string | undefined;
  runId: string;
  workflowId: string | undefined;
}

export interface Explanation {
  /** `error` for a settled failure; `warning` while the platform retries or waits on a person. */
  tone: "error" | "warning";
  title: string;
  body: string;
  next?: ExplanationNext;
  /** A failure's details; none for a park or a blocked task, which are not faults. */
  details?: FailureDetails;
}

const NOTHING_HAPPENED = "Nothing was coded or deployed.";
const PROVIDER_LIMIT = "model-provider-limit";

const SHORT_LABELS: Record<string, string> = {
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

function subject(f: RunFailure): string {
  return f.dependency ? `\`${f.dependency}\`` : "a dependency";
}

function owner(f: RunFailure): string {
  return f.component ? `${f.component} depends on it. ` : "";
}

function attemptsPhrase(f: RunFailure): string {
  return f.maxAttempts > 0 ? `attempt ${f.attempts} of ${f.maxAttempts}` : `attempt ${f.attempts}`;
}

type Copy = Omit<Explanation, "tone" | "details">;

function noWriteTargetCopy(phase?: string): Copy {
  const happened =
    phase === "coding"
      ? "Nothing was coded, built or deployed"
      : phase === "deploying"
        ? "The code merged and built; nothing was deployed"
        : "Nothing was deployed";
  return {
    title: "The project has no environment to deploy into",
    body:
      "Its deployment pipeline is missing, empty or circular, so the platform cannot tell which environment the project writes into. " +
      `${happened}, and no fix task was filed: code cannot repair a pipeline. ` +
      "Retrying cannot fix this. Fix the project's deployment pipeline, then build again.",
  };
}

function codeCopy(f: RunFailure, retrying: boolean): Copy {
  switch (f.code) {
    case "dependency-unprovisionable":
      return {
        title: `The platform could not provision ${subject(f)}`,
        body:
          `${owner(f)}The design declares it in a shape the platform cannot create a resource from: ` +
          "an org resource with no configuration keys, or a resource type the cluster does not have. " +
          `${NOTHING_HAPPENED} Retrying cannot fix this; the design needs a change, then a new build.`,
        next: { label: "Open the design", to: "/projects/$projectName/design" },
      };
    case "dependency-provision-failed":
      return retrying
        ? {
            title: `Provisioning ${subject(f)} failed, retrying (${attemptsPhrase(f)})`,
            body: `${owner(f)}The platform met an error it does not consider final and is trying again. Nothing is needed from you yet.`,
          }
        : {
            title: `The platform could not provision ${subject(f)}`,
            body:
              `${owner(f)}It tried ${f.attempts} time${f.attempts === 1 ? "" : "s"} and met the same error each time. ` +
              `${NOTHING_HAPPENED} This may be transient: retry; if it repeats, the details name the error.`,
          };
    case "plan-turn-failed":
      return retrying
        ? {
            title: `Planning the version's tasks failed, retrying (${attemptsPhrase(f)})`,
            body: "The planning turn met an error the platform is retrying. Nothing is needed from you yet; cancel the run if it does not recover.",
          }
        : {
            title: "The platform could not plan the version's tasks",
            body: `The planning turn failed. ${NOTHING_HAPPENED} Retry; if it repeats, the details name the error.`,
          };
    case "no-write-target":
      return noWriteTargetCopy(f.phase);
    case "repository-unavailable":
      return {
        title: "The project's repository could not be reached",
        body: `The repository, an issue, or the credential the platform uses for it is gone. ${NOTHING_HAPPENED} Retrying cannot fix this: check the GitHub connection in Settings.`,
      };
    default:
      return {
        title: `The build failed: ${f.code}`,
        body: `${NOTHING_HAPPENED} The details carry what the platform recorded.`,
      };
  }
}

function providerLimitCopy(f: RunFailure | undefined, now: Date): Copy {
  const host = f?.host;
  const resume = f?.resetAt
    ? `Retry after it resets (${resetStamp(f.resetAt, now)}).`
    : "Retry once it resets; the provider did not say when.";
  return {
    title: host ? `${host}'s usage limit was reached` : "The model provider's usage limit was reached",
    body: `The coding agent stopped when ${host ?? "its model provider"} refused further requests, and nothing from that session was merged. ${resume}`,
  };
}

function dispatchesPhrase(attempts: number | undefined): string {
  if (attempts === undefined || attempts <= 1) return "The platform started it once and it stopped without opening one.";
  if (attempts === 2) return "The platform started it twice and it stopped both times.";
  return `The platform started it ${attempts} times and it stopped every time.`;
}

function reasonCopy(run: MilestoneRunView): Copy {
  const reason = run.terminalReason;
  const newest = run.cycles.at(-1);
  const agentReason = newest?.agentReason ? ` The runner reported: ${newest.agentReason}.` : "";
  switch (reason) {
    case "plan-failed":
      return {
        title: "The build failed while preparing the version",
        body: `The platform could not provision the version's connections or plan its tasks. ${NOTHING_HAPPENED}`,
      };
    case "redispatch-budget":
      return {
        title: "The coding agent stopped without opening a pull request",
        body: `${dispatchesPhrase(newest?.attempts)}${agentReason} The coding agent's log shows what it did before it stopped.`,
      };
    case "fix-chain-budget":
    case "cycle-ceiling":
    case "no-progress":
      return {
        title: "The run used up its repair attempts",
        body: "The coding agent kept working the version's tasks without landing them. Its log has the last session; a spec change and a new build is the way forward.",
      };
    case "conflict-budget":
      return {
        title: "The run could not resolve its merge conflicts",
        body: "Two conflict sessions did not produce a mergeable pull request. The coding agent's log has the last one.",
      };
    case "build-retrigger-budget":
      return {
        title: "A component's build failed twice",
        body: "The code merged but did not build. The build logs carry the failing step.",
      };
    case "deploy-budget":
    case "version-incomplete":
      return {
        title: "The version built but did not come up",
        body: "Its components were built, and a deployment never became ready. The Deploy page names the component.",
        next: { label: "Go to Deploy", to: "/projects/$projectName/deploy" },
      };
    case "no-write-target":
      return noWriteTargetCopy();
    case "validation-failed":
    case "validation-unreported":
      return {
        title: reason === "validation-failed" ? "Validation failed" : "Validation reported nothing",
        body: "The version deployed and its acceptance scenarios were not met. Its validation carries the report.",
        next: { label: "Go to Validation", to: "/projects/$projectName/validations" },
      };
    default:
      return {
        title: reason ? `The build failed: ${SHORT_LABELS[reason] ?? reason}` : "The build failed",
        body: "The platform recorded no further details for this run.",
      };
  }
}

function detailsOf(run: MilestoneRunView): FailureDetails {
  const f = run.failure;
  return {
    code: f?.code ?? run.terminalReason ?? "",
    permanent: f?.permanent,
    attempts: f ? (f.maxAttempts > 0 ? `${f.attempts} of ${f.maxAttempts}` : `${f.attempts}`) : undefined,
    detail: f?.detail || undefined,
    runId: run.id,
    workflowId: f?.workflowId,
  };
}

/**
 * Why the run failed, or what it is retrying; undefined when there is nothing
 * to explain: a run that met no fault, or one a person cancelled.
 */
export function failureExplanation(run: MilestoneRunView, now: Date = new Date()): Explanation | undefined {
  const failed = run.state === "failed";
  const f = run.failure;
  if (run.state === "cancelled") return undefined;
  // Amber: not the platform's fault, and waiting fixes it.
  if (f?.code === PROVIDER_LIMIT || run.terminalReason === PROVIDER_LIMIT) {
    return { ...providerLimitCopy(f, now), tone: "warning", details: detailsOf(run) };
  }
  if (!failed && !f) return undefined;
  // A run still moving with a recorded fault is retrying it.
  if (!failed && run.state !== "planning" && run.state !== "running") return undefined;
  if (f) return { ...codeCopy(f, !failed), tone: failed ? "error" : "warning", details: detailsOf(run) };
  return { ...reasonCopy(run), tone: "error", details: detailsOf(run) };
}

function configure(environment: { name: string; label: string }): ExplanationNext {
  return { label: `Configure ${environment.label}`, to: "/projects/$projectName/deploy/$env/configure", env: environment.name };
}

/**
 * What the Build card says above its tasks: the run's failure first, then a
 * park at the deploy gate, then the first blocked task; null when the build
 * is moving or finished cleanly. A park and a blocked task wait on a value
 * only a person can give, in the write target (`environment`), so their way
 * out is its Configure card; null `environment` while the pipeline loads.
 */
export function buildExplanation(input: {
  run: MilestoneRunView | undefined;
  tasks: readonly TaskView[];
  claims: RunClaims;
  environment: { name: string; label: string } | null;
  now?: Date;
}): Explanation | null {
  const { run, tasks, claims, environment } = input;
  const failure = run ? failureExplanation(run, input.now) : undefined;
  if (failure) return failure;
  const park = externalValuesPark(run);
  if (park) {
    const where = environment?.label ?? "the first environment";
    return {
      tone: "warning",
      title: park.length ? `Waiting for configuration: ${park.join(", ")}` : "Waiting for configuration",
      body: `Everything built. It deploys once every dependency has its values in ${where}; the run then resumes on its own, with nothing to restart.`,
      ...(environment ? { next: configure(environment) } : {}),
    };
  }
  const blocked = orderedTasks(tasks).find((t) => taskState(t, claims) === "blocked");
  if (blocked) {
    const note = taskNote(blocked);
    return {
      tone: "warning",
      title: `#${blocked.issueNumber} ${blocked.title} is blocked`,
      body: note ? (/[.!?]$/.test(note) ? note : `${note}.`) : "It waits on something only a person can give.",
      ...(environment ? { next: configure(environment) } : {}),
    };
  }
  return null;
}

/** The details as one block of text: what a reader pastes into a bug report. */
export function detailsText(d: FailureDetails): string {
  return [
    `code: ${d.code}${d.permanent === undefined ? "" : d.permanent ? " · permanent" : " · retryable"}`,
    d.attempts && `attempts: ${d.attempts}`,
    d.detail && `recorded: ${d.detail}`,
    `run: ${d.runId}`,
    d.workflowId && `workflow: ${d.workflowId}`,
  ]
    .filter(Boolean)
    .join("\n");
}
