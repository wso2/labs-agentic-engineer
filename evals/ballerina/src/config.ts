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

/**
 * EVERY knob, in one file.
 *
 * These were spread across six modules — the model in the driver, the filter
 * regex in the transcript reader, the signature-error patterns in the build
 * reader, the jar paths in preflight. Each was local to its use and none was
 * findable, which is the wrong trade for a harness whose whole job is to be
 * re-pointed: changing what counts as a wasted lookup should not require
 * knowing which file counts them.
 *
 * A value here is one you would change to tune the harness. Behaviour is not
 * here — `report.ts` still owns the accessor table that reads a metric off an
 * attempt, because a closure over a type is code — but everything that table
 * shows is named in `REPORTED_METRICS` below.
 *
 * Precedence: a CLI flag beats an env var beats the default. Env vars exist so
 * a sweep can be scripted without threading flags through a wrapper, matching
 * `evals/spec-agents`' own `EVAL_REPEATS`.
 */

import { homedir } from "node:os";
import { join } from "node:path";

const PACKAGE_ROOT = join(import.meta.dirname, "..");

export const PATHS = {
  /** The monorepo root — where `skills/` and `packages/` are found. */
  repoRoot: join(PACKAGE_ROOT, "..", ".."),
  packageRoot: PACKAGE_ROOT,
  /** Authored cases. Every immediate subdirectory is a suite. */
  casesDir: join(PACKAGE_ROOT, "cases"),
  /** Sweep artifacts, gitignored. One timestamped directory per sweep. */
  runsDir: join(PACKAGE_ROOT, ".runs"),
  /** The skill library a session's `.claude/skills/` is mirrored from. */
  skillsDir: join(PACKAGE_ROOT, "..", "..", "skills"),
  /** Where `bal` resolves an installed tool from — what a host run actually executes. */
  installedToolDir: join(homedir(), ".ballerina", "repositories", "local", "bala", "ballerinax", "tool_library"),
  /**
   * Where the tool's own build leaves its jars, compared against the installed
   * one to catch a stale sweep. A DIRECTORY, never a filename — `preflight.ts`'s
   * `workingTreeToolJars` says why.
   */
  workingTreeToolLibs: join(
    PACKAGE_ROOT,
    "..",
    "..",
    "packages",
    "bal-library-tool",
    "native",
    "build",
    "libs",
  ),
} as const;

/** The skill under test. One name, because a case is one library question. */
export const SKILL_NAME = "ballerina";

export const DEFAULTS = {
  /**
   * Attempts per case. One is enough to see a case run; three is the floor for
   * believing a delta, because a single attempt has no spread to compare against.
   */
  repeats: envInt("BAL_EVAL_REPEATS", 1),
  /** Attempts in flight at once. Attempts, not cases — repeats fill lanes too. */
  concurrency: envInt("BAL_EVAL_CONCURRENCY", 4),
  /** Per-attempt ceiling. A wedged session must not hold a lane forever. */
  timeoutMinutes: envInt("BAL_EVAL_TIMEOUT_MINUTES", 30),
} as const;

export const SESSION = {
  /**
   * Pinned rather than left to the SDK's default, which drifts across releases
   * — an unpinned playground run once resolved to claude-sonnet-4-6. A sweep
   * comparing two model versions is measuring the wrong thing.
   */
  model: envString("BAL_EVAL_MODEL", "claude-sonnet-5"),
  systemPromptPreset: "claude_code",
  /**
   * What a Ballerina case needs. No `Agent`: fan-out spreads components across
   * subagents, and a case here is one package — allowing it would scatter a
   * run's lookups across streams for no benefit.
   */
  allowedTools: ["Read", "Write", "Edit", "Bash", "Glob", "Grep"],
  /**
   * The boundary that holds under `bypassPermissions`. Session-management tools
   * are useless to a one-shot eval and a reachable-but-useless tool is somewhere
   * a run will spend a turn — which is a number in this report. WebSearch and
   * WebFetch are denied for a second reason: a case measures whether the LIBRARY
   * answered, and an agent that reads a blog post instead has evaded the
   * question rather than answered it.
   */
  disallowedTools: [
    "Agent",
    "Task",
    "WebSearch",
    "WebFetch",
    "ScheduleWakeup",
    "Monitor",
    "TaskCreate",
    "TaskUpdate",
    "TaskGet",
    "TaskList",
    "TaskStop",
    "TaskOutput",
    "Workflow",
    "Artifact",
    "AskUserQuestion",
    "EnterPlanMode",
    "ExitPlanMode",
    "SendMessage",
  ],
} as const;

export const PATH_METRICS = {
  /**
   * What counts as narrowing a document with a FILTER rather than with a flag.
   * `--help` calls this out and the skill repeats it; measured at 19/19 both
   * before and after, so this counts the habit rather than polices it.
   */
  filters: /\|\s*(head|grep|sed|tail|awk|cut|wc)\b/,
  /**
   * The tool's verbs. An unknown token is reported under its own name, not as
   * "?", so a verb missing here still shows in the census — but its TARGET is
   * dropped, which silently removes the call from detour attribution. That is
   * why the list has to track the CLI: `guide` (ADR-0017) was invisible to
   * `worstDetour` until it was added, and the 2026-08-17 kind split replaced
   * `search`/`ops` with `find`/`client`/`class`/`funcs`.
   */
  verbs: ["find", "overview", "client", "class", "funcs", "type", "guide", "api"],
  /**
   * A result at or under this many characters carried a suggestion or nothing.
   * `api <pkg> | grep X` returning "(Bash completed with no output)" is 31
   * characters and worth exactly as little as a failure.
   */
  noBodyChars: 64,
  /**
   * Characters per token, for reporting lookup cost. Approximate on purpose —
   * compared against itself across sweeps, never billed.
   */
  charsPerToken: 4,
} as const;

export const BUILD_METRICS = {
  /**
   * Error messages that make a claim about ANOTHER package's API — the class
   * `bal library` exists to prevent, and the only class that is evidence about
   * the tool. Everything else is the agent's own arithmetic.
   */
  signaturePatterns: [
    "undefined (?:field|method|function|parameter|module|symbol)",
    "missing non-defaultable required record field",
    "incompatible types",
    "too many arguments",
    "undefined defaultable parameter",
  ],
  /**
   * A message naming a foreign coordinate is a signature error whatever its
   * shape: `incompatible types: expected 'ballerinax/aws.s3:4.0.0:…'` is about
   * an API, while the same message between two local records is not.
   */
  foreignCoordinate: /\b(?:ballerina|ballerinax)\/[a-z0-9_.]+:\d/i,
} as const;

/**
 * The columns a report prints, in order. `report.ts` holds the accessor for
 * each — a closure over an attempt is code, not configuration — but nothing is
 * reported that is not named here.
 */
export const REPORTED_METRICS = [
  // The PRIMARY axis leads. The 2026-08-17 redesign deliberately buys bytes with
  // calls — `googleapis.sheets` goes from one call to two while `ballerina/crypto`
  // goes from 64,310 bytes to about 1,000 — so a report that led with call count
  // would score the change as a regression on the axis it was not optimising.
  // Calls are a diagnostic and sit below the outcomes they explain.
  "lookupTokens",
  "sigErrors",
  "buildCycles",
  "otherErrors",
  "turns",
  "invocations",
  // FIRST, above the outcomes, because a nonzero value means none of them are
  // evidence: the tool was not there to answer. It is the only metric here that
  // invalidates a row rather than scoring it.
  "toolMissing",
  "piped",
  // Below `piped` and not instead of it: the habit and the damage are different
  // findings, and only this one is evidence about the tool. A sweep can read 6/6
  // piped and 0 truncated, which is a style note, or 6/6 and 6, which is a defect.
  "truncated",
  "failed",
  "sawNext",
  "detourCalls",
  "costUsd",
  // Seconds, not the milliseconds the transcript carries. A reported metric's
  // key is what `summary.json` stores it under, so a key naming one unit while
  // the value is another misleads exactly the reader who cannot see the label.
  "durationSeconds",
] as const;

export type ReportedMetric = (typeof REPORTED_METRICS)[number];

function envInt(name: string, fallback: number): number {
  const raw = process.env[name]?.trim();
  if (!raw) return fallback;
  const parsed = Number(raw);
  // A malformed value falls back rather than becoming NaN or 0 — a sweep that
  // silently ran once because `BAL_EVAL_REPEATS=three` is worse than one that
  // ignored it.
  return Number.isFinite(parsed) && parsed > 0 ? Math.floor(parsed) : fallback;
}

function envString(name: string, fallback: string): string {
  return process.env[name]?.trim() || fallback;
}
