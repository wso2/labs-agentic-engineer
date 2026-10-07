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
 * One attempt: stage → code → wire → walk → stop → judge → archive. And its
 * cheap sibling, the REWALK: stage an archived attempt's generated project →
 * wire → walk → stop → judge → archive, against the CURRENT checklist — the
 * loop for tuning a checklist or the walker without paying for a coding run.
 * Both run the same phase functions below; they differ only in how the staged
 * project comes to exist.
 *
 * Failures are classified (`classify.ts`, ADR-0001): app → hard-fail scored 0;
 * environment → harness-error, never averaged in. Teardown always runs, since a
 * live `play` child holds the next attempt's ports. What the archive keeps: ADR-0003.
 */

import { cpSync, existsSync, mkdirSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { basename, join, relative, sep } from "node:path";
import { readWireSession } from "@aep/playground/src/engine/wire/state.js";
import { killListener } from "@aep/playground/src/engine/wire/runtime.js";
import { projectSlug } from "@aep/playground/src/ports/spec-workspace.js";
import { scoredItems, type EvalCase, type RunConfig } from "./case.js";
import { codingFailure, wireFailure, type Classified, type FailureCause } from "./classify.js";
import { ARCHIVE, PATHS, SAVE, TIMEOUTS } from "./config.js";
import { CredentialError, playEnv, sdkEnv } from "./credentials.js";
import { judge } from "./judge.js";
import { countEvents, lastResultCost, readRunSettled, sawRunStarted, type Tokens } from "./metrics.js";
import {
  baseUrl,
  composeDown,
  composeLogsTo,
  isStopped,
  parseFailed,
  parseReady,
  removeContainer,
  startPlay,
  waitForLine,
  type PlayProcess,
} from "./play.js";
import { scoreAttempt, type Band } from "./score.js";
import { writeProvenance } from "./provenance.js";
import { normalizeWalk, walk } from "./walker.js";

export type AttemptStatus = "scored" | "hard-fail" | "harness-error";
export type Phase = "stage" | "code" | "wire" | "walk" | "stop" | "judge" | "archive";

interface SessionCost {
  costUsd: number | null;
  tokens: Tokens;
  turns: number;
}

/** Why an attempt did not score: the phase it stopped in, whose failure that was, and what happened. */
export interface AttemptFailure {
  phase: Phase;
  cause: FailureCause;
  reason: string;
}

/** One attempt's full record — `attempt.json`, and the row every report is built from. */
export interface AttemptRecord {
  /**
   * `rewalk` records re-walk an archived attempt's code against the current
   * checklist; they never enter a sweep's statistics. Absent means `attempt`.
   */
  kind?: "attempt" | "rewalk";
  /** A rewalk's number under its attempt (`rewalk-<n>/`). */
  rewalk?: number;
  sweepId: string;
  case: string;
  config: string;
  attempt: number;
  status: AttemptStatus;
  /** 0..100 when scored, 0 on a hard fail, null on a harness error. */
  score: number | null;
  band: Band | null;
  /** A mustNot capped the band. */
  capped: boolean;
  /** A hard fail or harness error: where, whose, and why. Absent when scored. */
  failure?: AttemptFailure;
  /** A note on the record itself (an incomplete archive). */
  symptom?: string;
  failing: { id: string; weight: number; symptom: string }[];
  violated: string[];
  /** Wall clock per phase reached, milliseconds. */
  phases: Partial<Record<Phase, number>>;
  coding: { outcome: string | null; minutes: number | null; costUsd: number | null; tokens: Tokens | null };
  walk: SessionCost | null;
  judge: SessionCost | null;
  /** Where everything is, relative to the repo root. */
  archive: string;
}

/** The code failed (cause `app`) — score 0, counted. */
class HardFail extends Error {}
/** Not the code's failure (cause `environment`) — counted, excluded from the statistics. */
class HarnessError extends Error {}

/** A classified failure as the error that ends the run. */
function raise(failure: Classified): never {
  throw failure.cause === "app" ? new HardFail(failure.reason) : new HarnessError(failure.reason);
}

/** What every run needs, attempt or rewalk alike. */
export interface RunTarget {
  evalCase: EvalCase;
  config: RunConfig;
  /** `wire`'s role names for this case (`caseRoles`). */
  roles: string[];
  token: string;
  /** `deployments/.env`, parsed — only an opencode connection's key is read from it. */
  dotenv: Record<string, string | undefined>;
  keep: boolean;
  signal: AbortSignal;
  say: (line: string) => void;
}

export interface AttemptContext extends RunTarget {
  sweepId: string;
  attempt: number;
}

export interface RewalkContext extends RunTarget {
  /** The archived attempt directory (`.runs/<sweep>/<case>/<config>/attempt-<n>`), absolute. */
  attemptDir: string;
  /** The parent attempt's record — its sweep, case, config and number. */
  parent: AttemptRecord;
}

/** One run in flight: its record, its directories, and the children it must take down. */
interface Run {
  label: string;
  record: AttemptRecord;
  archive: string;
  stage: string;
  live: { code?: PlayProcess; wire?: PlayProcess };
  codingRunDir: string | null;
  codeKilled: boolean;
  /** A rewalk's project is its parent's; only an attempt archives one. */
  archiveProject: boolean;
  target: RunTarget;
  /** The phase in progress, or the last one entered — where a failure is recorded. */
  phase: Phase;
}

export function attemptDirs(sweepId: string, caseName: string, configId: string, attempt: number): { archive: string; stage: string } {
  return {
    archive: join(PATHS.runsDir, sweepId, caseName, configId, `attempt-${String(attempt)}`),
    // Short and flat: its basename becomes the compose project `aep-wire-<slug>`.
    stage: join(PATHS.stageRoot, sweepId, `${caseName}-${configId}-${String(attempt)}`),
  };
}

/** The next free `rewalk-<n>` under an archived attempt, and where to stage it. */
export function rewalkDirs(attemptDir: string, parent: AttemptRecord): { archive: string; stage: string; n: number } {
  const taken = existsSync(attemptDir)
    ? readdirSync(attemptDir)
        .map((name) => /^rewalk-(\d+)$/.exec(name)?.[1])
        .filter((n): n is string => n !== undefined)
        .map(Number)
    : [];
  const n = Math.max(0, ...taken) + 1;
  return {
    n,
    archive: join(attemptDir, `rewalk-${String(n)}`),
    stage: join(PATHS.stageRoot, "rewalk", `${parent.case}-${parent.config}-${String(parent.attempt)}r${String(n)}`),
  };
}

function newRecord(fields: Pick<AttemptRecord, "sweepId" | "case" | "config" | "attempt" | "archive"> & Partial<AttemptRecord>): AttemptRecord {
  return {
    kind: "attempt",
    status: "harness-error",
    score: null,
    band: null,
    capped: false,
    failing: [],
    violated: [],
    phases: {},
    coding: { outcome: null, minutes: null, costUsd: null, tokens: null },
    walk: null,
    judge: null,
    ...fields,
  };
}

export async function runAttempt(ctx: AttemptContext): Promise<AttemptRecord> {
  const { archive, stage } = attemptDirs(ctx.sweepId, ctx.evalCase.name, ctx.config.id, ctx.attempt);
  const run: Run = {
    label: `${ctx.evalCase.name} × ${ctx.config.id} #${String(ctx.attempt)}`,
    record: newRecord({
      sweepId: ctx.sweepId,
      case: ctx.evalCase.name,
      config: ctx.config.id,
      attempt: ctx.attempt,
      archive: relative(PATHS.repoRoot, archive),
    }),
    archive,
    stage,
    live: {},
    codingRunDir: null,
    codeKilled: false,
    archiveProject: true,
    target: ctx,
    phase: "stage",
  };
  return execute(run, async (env) => {
    await timed(run, "stage", async () => {
      rmSync(stage, { recursive: true, force: true });
      mkdirSync(stage, { recursive: true });
      for (const name of SAVE.caseDirs) cpSync(join(ctx.evalCase.dir, name), join(stage, name), { recursive: true });
    });
    interrupted(run);
    await codePhase(run, env);
  });
}

/**
 * Re-walk an archived attempt's code. The staged project is the archive's
 * `project/` minus the wire state — `wire` regenerates its plan, keys and
 * tokens, and a stale `session.json` would name a pid that may since belong to
 * another process. Dependencies were never archived, so `wire` reinstalls the
 * app's on this host, exactly as it does after a docker coding run.
 */
export async function runRewalk(ctx: RewalkContext): Promise<AttemptRecord> {
  const { archive, stage, n } = rewalkDirs(ctx.attemptDir, ctx.parent);
  const run: Run = {
    label: `${ctx.parent.case} × ${ctx.parent.config} #${String(ctx.parent.attempt)} rewalk-${String(n)}`,
    record: newRecord({
      kind: "rewalk",
      rewalk: n,
      sweepId: ctx.parent.sweepId,
      case: ctx.parent.case,
      config: ctx.parent.config,
      attempt: ctx.parent.attempt,
      // The code is the parent's, so its coding facts are too.
      coding: ctx.parent.coding,
      archive: relative(PATHS.repoRoot, archive),
    }),
    archive,
    stage,
    live: {},
    codingRunDir: null,
    codeKilled: false,
    archiveProject: false,
    target: ctx,
    phase: "stage",
  };
  return execute(run, async () => {
    await timed(run, "stage", async () => {
      const project = join(ctx.attemptDir, "project");
      if (!existsSync(project)) throw new HarnessError(`${project} does not exist — nothing to rewalk`);
      rmSync(stage, { recursive: true, force: true });
      mkdirSync(stage, { recursive: true });
      cpSync(project, stage, { recursive: true, verbatimSymlinks: true });
      rmSync(join(stage, ".aep-playground", "wire"), { recursive: true, force: true });
    });
    interrupted(run);
  });
}

/**
 * The shared body: `prepare` brings the staged project into existence, then
 * wire → walk → stop → judge, every failure classified, teardown and archive
 * always.
 */
async function execute(run: Run, prepare: (env: NodeJS.ProcessEnv) => Promise<void>): Promise<AttemptRecord> {
  const { target, record } = run;
  for (const dir of ["coding", "wire", join("walk", "shots"), "judge"]) {
    if (dir === "coding" && !run.archiveProject) continue;
    mkdirSync(join(run.archive, dir), { recursive: true });
  }
  // The sweep's SIGINT reaches a phase that is waiting on a child through
  // the child: stopping it resolves whatever is awaiting its exit.
  const onAbort = (): void => {
    void run.live.code?.stop(TIMEOUTS.killGraceSeconds * 1000);
    void run.live.wire?.stop(TIMEOUTS.wireStopMinutes * 60_000);
  };
  target.signal.addEventListener("abort", onAbort, { once: true });

  try {
    // First, before anything can change underneath it: what this run ran against.
    writeProvenance(run.archive, target.config);
    // The checklist this attempt is scored against, as it stood. The case's own
    // file is hand-edited and replanned freely, so after a change the archive is
    // the only place that still says what an old score meant.
    cpSync(join(target.evalCase.dir, "checklist.yaml"), join(run.archive, "checklist.yaml"));
    let env: NodeJS.ProcessEnv;
    try {
      env = playEnv(process.env, target.token, target.config, target.dotenv);
    } catch (e) {
      throw new HarnessError(e instanceof Error ? e.message : String(e));
    }
    await prepare(env);
    await servePhases(run, env);
  } catch (e) {
    const message = e instanceof Error ? e.message : String(e);
    if (e instanceof HardFail) {
      Object.assign(record, { status: "hard-fail" as const, score: 0, band: "fail" as const });
      record.failure = { phase: run.phase, cause: "app", reason: message };
    } else {
      // CredentialError, HarnessError, and anything unexpected: none of them is the code's.
      record.status = "harness-error";
      const reason = e instanceof CredentialError || e instanceof HarnessError ? message : `harness crash: ${message}`;
      record.failure = { phase: run.phase, cause: "environment", reason };
    }
  } finally {
    target.signal.removeEventListener("abort", onAbort);
    await finalize(run);
  }
  return record;
}

async function timed<T>(run: Run, phase: Phase, fn: () => Promise<T>): Promise<T> {
  const started = Date.now();
  run.phase = phase;
  // Labelled: at --concurrency > 1 these lines interleave across attempts.
  run.target.say(`    · ${run.label} · ${phase}`);
  try {
    return await fn();
  } finally {
    run.record.phases[phase] = Date.now() - started;
  }
}

function interrupted(run: Run): void {
  if (run.target.signal.aborted) throw new HarnessError("interrupted");
}

async function codePhase(run: Run, env: NodeJS.ProcessEnv): Promise<void> {
  const { record, stage, target } = run;
  await timed(run, "code", async () => {
    const play = startPlay([stage, "code", "--yes"], { env, logFile: join(run.archive, "coding", "play.log") });
    run.live.code = play;
    const limitMs = (target.evalCase.meta.timeoutMinutes ?? TIMEOUTS.codingMinutes) * 60_000;
    let stopping: Promise<"exited" | "killed"> | undefined;
    const timer = setTimeout(() => {
      stopping = play.stop(TIMEOUTS.killGraceSeconds * 1000);
    }, limitMs);
    const exit = await play.exited;
    clearTimeout(timer);
    const timedOut = stopping !== undefined;
    if (stopping) run.codeKilled = (await stopping) === "killed";
    delete run.live.code;
    run.codingRunDir = newestCodingRun(stage);
    interrupted(run);

    const progress = run.codingRunDir ? readText(join(run.codingRunDir, "progress.ndjson")) : "";
    const settled = readRunSettled(progress);
    record.coding.outcome = settled?.outcome ?? null;
    record.coding.tokens = settled?.tokens ?? null;
    record.coding.costUsd = run.codingRunDir ? lastResultCost(readText(join(run.codingRunDir, ".logs", "runtime.log"))) : null;

    const events = countEvents(progress);
    const failure = codingFailure({
      timedOut,
      limitMinutes: limitMs / 60_000,
      exitCode: exit.code,
      events,
      agentStarted: sawRunStarted(progress),
      settled,
      builtAnything: componentAppPaths(stage).length > 0,
    });
    // Nothing reached the feed: `play` itself says why in its last lines (docker, the image, the credential).
    if (failure && events === 0) raise({ ...failure, reason: `${failure.reason} — ${lastLines(play)}` });
    if (failure) raise(failure);
  });
  interrupted(run);
}

/** wire → walk → stop → judge → score: everything after the project exists. */
async function servePhases(run: Run, env: NodeJS.ProcessEnv): Promise<void> {
  const { record, stage, target } = run;
  const url = await timed(run, "wire", async () => {
    const firstRole = target.roles[0] ?? "";
    const play = startPlay([stage, "wire", "--yes", "--fresh", "--no-open", "--no-triage", "--role", firstRole], {
      env,
      logFile: join(run.archive, "wire", "wire.log"),
    });
    run.live.wire = play;
    // `wire` says whose failure a bring-up was, in one line, before it exits.
    let failed: Classified | null = null;
    play.onLine((line) => {
      failed = parseFailed(line) ?? failed;
    });
    const ready = await waitForLine(play, parseReady, TIMEOUTS.wireReadyMinutes * 60_000);
    interrupted(run);
    if (ready.kind === "line") return baseUrl(ready.value);
    const failure = wireFailure({
      ended: ready.kind,
      exitCode: ready.kind === "exited" ? ready.code : null,
      failed,
      limitMinutes: TIMEOUTS.wireReadyMinutes,
      tail: lastLines(play),
    });
    raise(failure.cause === "app" ? { ...failure, reason: `unwireable: ${failure.reason}` } : failure);
  });

  const items = scoredItems(target.evalCase.checklist);
  const walkDir = join(run.archive, "walk");
  const walked = await timed(run, "walk", () =>
    walk({
      baseUrl: url,
      roles: target.roles,
      items,
      mustNot: target.evalCase.checklist.extras.mustNot,
      walkDir,
      sessionName: `eval-${projectSlug(stage)}`,
      env: sdkEnv(process.env, target.token),
      signal: target.signal,
    }),
  );
  record.walk = { costUsd: walked.costUsd, tokens: walked.tokens, turns: walked.numTurns };
  if (walked.credentialRefused) throw new HarnessError(walked.error ?? "walker credential refused");
  // Whatever the walker answered after its browser stopped answering is not evidence about the app.
  if (walked.halted) throw new HarnessError(walked.halted);
  interrupted(run);
  if (!walked.output) throw new HarnessError(`the walk produced no result: ${walked.error ?? "unknown"}`);
  const walkResult = normalizeWalk(items, walked.output);
  writeFileSync(join(walkDir, "result.json"), JSON.stringify(walkResult, null, 2));

  await timed(run, "stop", () => stopWire(run));

  const verdict = await timed(run, "judge", () =>
    judge({
      items,
      mustNot: target.evalCase.checklist.extras.mustNot,
      walk: walkResult,
      cwd: join(run.archive, "judge"),
      env: sdkEnv(process.env, target.token),
      transcriptFile: join(run.archive, "judge", "transcript.jsonl"),
      signal: target.signal,
    }),
  );
  record.judge = { costUsd: verdict.costUsd, tokens: verdict.tokens, turns: verdict.numTurns };
  interrupted(run);
  if (!verdict.output) throw new HarnessError(`the judge produced no verdict: ${verdict.error ?? "unknown"}`);
  writeFileSync(join(run.archive, "judge", "verdict.json"), JSON.stringify(verdict.output, null, 2));

  const score = scoreAttempt(items, verdict.output, new Map(walkResult.items.map((entry) => [entry.id, entry.verdict])));
  Object.assign(record, {
    status: "scored" as const,
    score: score.score,
    band: score.band,
    capped: score.capped,
    failing: score.failing,
    violated: score.violated,
  });
}

/** Teardown, archive, records, and the staged directory — whichever way the run ended. */
async function finalize(run: Run): Promise<void> {
  const { record, stage } = run;
  if (record.phases.code !== undefined) record.coding.minutes = round(record.phases.code / 60_000);
  if (run.live.code) {
    run.codeKilled = (await run.live.code.stop(TIMEOUTS.killGraceSeconds * 1000)) === "killed" || run.codeKilled;
    delete run.live.code;
    run.codingRunDir ??= newestCodingRun(stage);
  }
  // A SIGKILLed `play code` could not remove its own container.
  if (run.codeKilled && run.codingRunDir) await removeContainer(`aep-play-${basename(run.codingRunDir).replace(/-code$/, "")}`);
  await stopWire(run);
  const started = Date.now();
  try {
    archiveRun(run);
  } catch (e) {
    record.symptom = `${record.symptom ? `${record.symptom}; ` : ""}archive incomplete: ${e instanceof Error ? e.message : String(e)}`;
  }
  record.phases.archive = Date.now() - started;
  writeFileSync(join(run.archive, "metrics.json"), JSON.stringify({ coding: record.coding, walk: record.walk, judge: record.judge }, null, 2));
  writeFileSync(join(run.archive, "attempt.json"), JSON.stringify(record, null, 2));
  if (!run.target.keep) {
    try {
      rmSync(stage, { recursive: true, force: true });
    } catch (e) {
      // A file the container wrote with other ownership; the record already stands.
      run.target.say(`    ! could not remove ${stage}: ${e instanceof Error ? e.message : String(e)}`);
    }
  }
}

/**
 * Capture logs, SIGTERM until STOPPED, SIGKILL past the deadline, then always
 * `compose down -v` with built images: `wire` keeps its volume for a person, an
 * attempt must not.
 */
async function stopWire(run: Run): Promise<void> {
  const play = run.live.wire;
  if (!play) return;
  delete run.live.wire;
  const project = `aep-wire-${projectSlug(run.stage)}`;
  // Before the SIGTERM: `wire`'s teardown removes the containers and their logs.
  const logs = join(run.archive, "wire", "logs");
  mkdirSync(logs, { recursive: true });
  await composeLogsTo(project, join(logs, "services.log"));
  let sawStopped = false;
  play.onLine((line) => {
    if (isStopped(line)) sawStopped = true;
  });
  const how = await play.stop(TIMEOUTS.wireStopMinutes * 60_000);
  const file = join(run.stage, ".aep-playground", "wire", "compose.yaml");
  await composeDown(project, existsSync(file) ? file : undefined);
  if (how === "killed" || !sawStopped) {
    const port = readWireSession(run.stage)?.webappPort;
    if (port) await killListener(port);
  }
}

/** The run dir the coding run just wrote: the newest `.aep-playground/runs/*-code`. */
function newestCodingRun(stage: string): string | null {
  const runs = join(stage, ".aep-playground", "runs");
  if (!existsSync(runs)) return null;
  const newest = readdirSync(runs)
    .filter((name) => name.endsWith("-code"))
    .sort()
    .pop();
  return newest ? join(runs, newest) : null;
}

/** Every component App Path the run wrote, relative to the project. None means it built nothing at all. */
function componentAppPaths(stage: string): string[] {
  const components = join(stage, "specs", "design", "components");
  if (!existsSync(components)) return [];
  return readdirSync(components).flatMap((name) => {
    try {
      const design = JSON.parse(readFileSync(join(components, name, "design.json"), "utf8")) as { appPath?: unknown };
      return typeof design.appPath === "string" && existsSync(join(stage, design.appPath)) ? [design.appPath] : [];
    } catch {
      return [];
    }
  });
}

/**
 * Paths (relative to the stage) kept out of `project/`: reinstallable,
 * regenerable, secret, or archived elsewhere. Pure.
 */
export function excludedFromProject(rel: string, appPaths: readonly string[] = []): boolean {
  if (rel.split(sep).includes("node_modules")) return true;
  const output = appPaths.flatMap((appPath) => ARCHIVE.buildOutputDirs.map((dir) => join(appPath, dir)));
  if (output.some((path) => rel === path || rel.startsWith(path + sep))) return true;
  const wire = join(".aep-playground", "wire");
  return [
    join(wire, "secrets.json"),
    join(wire, "tokens.json"),
    join(wire, "key.pem"),
    join(".aep-playground", "undo"),
    join(".aep-playground", "runs"),
  ].some((path) => rel === path || rel.startsWith(path + sep));
}

function archiveRun(run: Run): void {
  const { stage, archive } = run;
  if (!existsSync(stage)) return;
  if (run.archiveProject) {
    const appPaths = componentAppPaths(stage);
    cpSync(stage, join(archive, "project"), {
      recursive: true,
      verbatimSymlinks: true,
      filter: (src) => !excludedFromProject(relative(stage, src), appPaths),
    });
  }
  if (run.codingRunDir && existsSync(run.codingRunDir)) {
    // `bearer` is the run's MCP credential file — empty locally, and never worth keeping.
    cpSync(run.codingRunDir, join(archive, "coding", basename(run.codingRunDir)), {
      recursive: true,
      verbatimSymlinks: true,
      filter: (src) => basename(src) !== "bearer",
    });
  }
  const wire = join(stage, ".aep-playground", "wire");
  for (const name of ["plan.json", "compose.yaml", "logs"]) {
    if (existsSync(join(wire, name))) cpSync(join(wire, name), join(archive, "wire", name), { recursive: true });
  }
}

function lastLines(play: PlayProcess): string {
  return play
    .tail(8)
    .map((line) => line.trim())
    .filter(Boolean)
    .join(" ⏎ ");
}

function readText(file: string): string {
  try {
    return readFileSync(file, "utf8");
  } catch {
    return "";
  }
}

function round(n: number): number {
  return Math.round(n * 10) / 10;
}
