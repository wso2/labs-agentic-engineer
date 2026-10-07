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
 * The entry point: `save`, `run`, `rewalk`, `replan`, `log`, `report`, `list`.
 *
 * Exits nonzero when something could not be DONE — a refused credential, a
 * case that will not load, docker down, a planner that never produced a valid
 * list — and never because an app scored badly (ADR-0005).
 */

import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, readdirSync, statSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { parseArgs, parseEnv } from "node:util";
import { runRewalk, type AttemptRecord } from "./attempt.js";
import { listCases, loadCase, loadConfigs, unknownRoles, type EvalCase } from "./case.js";
import { DEFAULTS, MODELS, PATHS } from "./config.js";
import { CredentialError, readOAuthToken } from "./credentials.js";
import { renderLogView, type LogView } from "@aep/playground/src/engine/log-read.js";
import { resolveCodingRun, usageByAgent } from "./log.js";
import { dockerAnswers } from "./play.js";
import { failureText, pickBaseline, renderReport, summarize, type Summary } from "./report.js";
import { caseRoles, replanCase, saveCase } from "./save.js";
import { runSweep } from "./sweep.js";
import { walkerProblem } from "./walker.js";

const USAGE = `
Codegen evals — a saved case → play code → play wire → a walker clicks the app
through a frozen checklist → a judge scores what a user would see.

  pnpm eval save --from <abs project dir> --name <name> [--force]
  pnpm eval run [--case <name>]… [--config <id>]… [--repeats N] [--concurrency N] [--keep] [--list]
  pnpm eval rewalk (--attempt <archived attempt dir>… | --from-sweep <id> [--case <name>]…) [--keep]
  pnpm eval replan --case <name>
  pnpm eval log --attempt <archived attempt dir> [--slow|--thinking|--usage]
  pnpm eval report [--sweep <id>]
  pnpm eval list

  save          derive a case from a playground project (pre-code snapshot) + plan its checklist
  run           sweep cases × configs × repeats; default: every case, the FIRST config in configs.yaml
    --case        a case under cases/ (repeatable; default all)
    --config      an id from configs.yaml (repeatable; default the first)
    --repeats     attempts per case × config (3+ before believing a delta)
    --concurrency attempts in flight (each is a container, a compose project and a browser)
    --keep        keep each attempt's staged project under ~/.aep-evals/codegen/
    --list        print the matrix and exit
  rewalk        wire → walk → judge an archived attempt's code again, against the CURRENT
                checklist, into <attempt>/rewalk-<n>/ — no coding run. Listed in the sweep's
                report, excluded from its statistics
  replan        re-derive a saved case's checklist.yaml from its committed specs (extras kept)
  log           an archived attempt's coding run through the playground's developer view
                (same views as \`play log\`: per step; --slow; --thinking; plus --usage: tokens per agent)
  report        re-render a sweep's report.md (default: the newest sweep)
  list          the cases and the configs

Credential: ONLY the Claude OAuth token in deployments/.env AEP_CODING_ANTHROPIC_KEY
(sk-ant-oat…). Refused otherwise; an API key is never used. Every knob is in
src/config.ts; a flag beats CODEGEN_EVAL_* env vars, which beat the defaults.
`.trim();

async function main(): Promise<number> {
  const argv = process.argv.slice(2);
  // `pnpm --filter … eval -- <args>` (the Makefile's shape, and the playground's
  // `eval-save`) forwards the `--` itself; it separates nothing here.
  if (argv[0] === "--") argv.shift();
  const [command, ...rest] = argv;
  switch (command) {
    case "save":
      return save(rest);
    case "run":
      return run(rest);
    case "rewalk":
      return rewalk(rest);
    case "replan":
      return replan(rest);
    case "log":
      return log(rest);
    case "report":
      return report(rest);
    case "list":
      return list();
    case undefined:
    case "help":
    case "-h":
    case "--help":
      console.log(USAGE);
      return 0;
    default:
      console.error(`unknown command "${command}"\n\n${USAGE}`);
      return 2;
  }
}

async function save(args: string[]): Promise<number> {
  const { values } = parseArgs({
    args,
    options: { from: { type: "string" }, name: { type: "string" }, force: { type: "boolean" } },
  });
  if (!values.from || !values.name) {
    console.error("save needs --from <abs project dir> and --name <name>");
    return 2;
  }
  const from = fromInvocation(values.from);
  console.log(`saving ${values.name} from ${from}`);
  const dir = await saveCase({ from, name: values.name, force: values.force === true, say: (line) => console.log(line) });
  console.log(`✓ saved ${dir}\n  review checklist.yaml before the first sweep — it is the rubric every attempt is scored by`);
  return 0;
}

async function run(args: string[]): Promise<number> {
  const { values } = parseArgs({
    args,
    options: {
      case: { type: "string", multiple: true },
      config: { type: "string", multiple: true },
      repeats: { type: "string" },
      concurrency: { type: "string" },
      keep: { type: "boolean" },
      list: { type: "boolean" },
    },
  });
  const repeats = positiveInt(values.repeats, DEFAULTS.repeats);
  const concurrency = positiveInt(values.concurrency, DEFAULTS.concurrency);

  const allCases = listCases(PATHS.casesDir);
  const caseNames = values.case?.length ? values.case : allCases;
  const missing = caseNames.filter((name) => !allCases.includes(name));
  if (missing.length) {
    console.error(`no such case: ${missing.join(", ")} (have: ${allCases.join(", ") || "none"})`);
    return 2;
  }
  if (caseNames.length === 0) {
    console.error("no cases — save one first: pnpm play <dir> eval-save <name>");
    return 2;
  }
  const allConfigs = loadConfigs(PATHS.configsFile);
  const configs = values.config?.length
    ? values.config.map((id) => allConfigs.find((c) => c.id === id))
    : allConfigs.slice(0, 1);
  if (configs.some((c) => !c)) {
    console.error(`unknown config in ${values.config?.join(", ") ?? ""} (have: ${allConfigs.map((c) => c.id).join(", ")})`);
    return 2;
  }
  const selected = configs.filter((c) => c !== undefined);

  const cases = caseNames.map(loadWalkableCase);

  if (values.list) {
    for (const { evalCase } of cases) {
      for (const config of selected) {
        console.log(`${evalCase.name} × ${config.id} (${config.runtime}, ${config.model}) × ${String(repeats)}`);
      }
    }
    return 0;
  }

  const walkBlocked = walkerProblem();
  if (walkBlocked) {
    console.error(`refusing to run: ${walkBlocked} — every attempt would code for an hour and then fail its walk`);
    return 2;
  }
  const token = readOAuthToken(PATHS.envFile);
  const dotenv = parseEnv(readFileSync(PATHS.envFile, "utf8"));
  if (!(await dockerAnswers())) {
    console.error("refusing to run: docker is not answering (is Colima up?) — every attempt would be a harness error");
    return 2;
  }

  const sweepId = new Date().toISOString().replace(/[:.]/g, "-");
  const sweepDir = join(PATHS.runsDir, sweepId);
  mkdirSync(sweepDir, { recursive: true });
  const facts: Record<string, string> = {
    configs: selected.map((c) => `${c.id} (${c.runtime}, ${c.model})`).join(", "),
    repeats: String(repeats),
    concurrency: String(concurrency),
    "walker / judge": `${MODELS.walker} / ${MODELS.judge}`,
    ...provenance(),
  };
  writeFileSync(join(sweepDir, "facts.json"), JSON.stringify(facts, null, 2));

  const controller = new AbortController();
  const interrupt = (): void => {
    if (controller.signal.aborted) return;
    console.log("\n  interrupted — stopping every live attempt (this takes a minute: containers come down cleanly)");
    controller.abort();
  };
  process.on("SIGINT", interrupt);
  process.on("SIGTERM", interrupt);

  const total = cases.length * selected.length * repeats;
  console.log(`sweep ${sweepId}: ${String(total)} attempt(s), ${String(concurrency)} at a time\n`);
  const records = await runSweep({
    sweepId,
    cases,
    configs: selected,
    repeats,
    concurrency,
    token,
    dotenv,
    keep: values.keep === true,
    signal: controller.signal,
    say: (line) => console.log(line),
  });
  process.off("SIGINT", interrupt);
  process.off("SIGTERM", interrupt);

  const notes = controller.signal.aborted ? ["The sweep was INTERRUPTED; attempts not started are absent."] : [];
  const text = writeReport(sweepId, records, facts, notes);
  console.log(`\n${text}\nartifacts: ${sweepDir}`);
  return 0;
}

async function rewalk(args: string[]): Promise<number> {
  const { values } = parseArgs({
    args,
    options: {
      attempt: { type: "string", multiple: true },
      "from-sweep": { type: "string" },
      case: { type: "string", multiple: true },
      keep: { type: "boolean" },
    },
  });
  let dirs: string[];
  if (values.attempt?.length) {
    dirs = values.attempt.map(fromInvocation);
  } else if (values["from-sweep"]) {
    dirs = archivedAttempts(values["from-sweep"]).filter((dir) => !values.case?.length || values.case.includes(dir.split("/").at(-3) ?? ""));
  } else {
    console.error("rewalk needs --attempt <dir> or --from-sweep <id>");
    return 2;
  }
  if (dirs.length === 0) {
    console.error("no archived attempts with a project/ matched");
    return 2;
  }
  const parents = dirs.map((dir) => {
    const file = join(dir, "attempt.json");
    if (!existsSync(file) || !existsSync(join(dir, "project"))) throw new Error(`${dir} is not an archived attempt with a project/`);
    const parent = JSON.parse(readFileSync(file, "utf8")) as AttemptRecord;
    if (parent.kind === "rewalk") throw new Error(`${dir} is itself a rewalk — point at its attempt`);
    return { dir, parent };
  });
  const walkBlocked = walkerProblem();
  if (walkBlocked) {
    console.error(`refusing to run: ${walkBlocked}`);
    return 2;
  }
  const configs = loadConfigs(PATHS.configsFile);
  const token = readOAuthToken(PATHS.envFile);
  const dotenv = parseEnv(readFileSync(PATHS.envFile, "utf8"));
  if (!(await dockerAnswers())) {
    console.error("refusing to run: docker is not answering (is Colima up?)");
    return 2;
  }
  const controller = new AbortController();
  const interrupt = (): void => {
    if (controller.signal.aborted) return;
    console.log("\n  interrupted — stopping the live rewalk");
    controller.abort();
  };
  process.on("SIGINT", interrupt);
  process.on("SIGTERM", interrupt);
  const sweeps = new Set<string>();
  for (const { dir, parent } of parents) {
    if (controller.signal.aborted) break;
    const config = configs.find((c) => c.id === parent.config);
    if (!config) throw new Error(`config ${parent.config} is no longer in configs.yaml`);
    const { evalCase, roles } = loadWalkableCase(parent.case);
    console.log(`  ▶ rewalk ${parent.case} × ${parent.config} #${String(parent.attempt)} (checklist: ${String(evalCase.checklist.items.length)} items)`);
    const record = await runRewalk({
      attemptDir: dir,
      parent,
      evalCase,
      config,
      roles,
      token,
      dotenv,
      keep: values.keep === true,
      signal: controller.signal,
      say: (line) => console.log(line),
    });
    const score = record.score === null ? "" : ` ${String(record.score)} ${record.band ?? ""}`;
    console.log(`  ${record.status === "scored" ? "✓" : "✗"} rewalk-${String(record.rewalk)} — ${record.status}${score}${failureText(record) ? ` · ${failureText(record)}` : ""}`);
    for (const failing of record.failing) console.log(`      ✗ ${failing.id}: ${failing.symptom}`);
    console.log(`    ${record.archive}`);
    sweeps.add(parent.sweepId);
  }
  process.off("SIGINT", interrupt);
  process.off("SIGTERM", interrupt);
  for (const sweepId of sweeps) rerenderReport(sweepId);
  return 0;
}

async function replan(args: string[]): Promise<number> {
  const { values } = parseArgs({ args, options: { case: { type: "string" } } });
  if (!values.case) {
    console.error("replan needs --case <name>");
    return 2;
  }
  console.log(`re-planning ${values.case}`);
  const file = await replanCase(values.case, (line) => console.log(line));
  console.log(`✓ wrote ${file} — review it; every attempt from now on is scored by it`);
  return 0;
}

/** Load a case and refuse it when its checklist names a role `wire` would not accept. */
function loadWalkableCase(name: string): { evalCase: EvalCase; roles: string[] } {
  const evalCase = loadCase(PATHS.casesDir, name);
  const roles = caseRoles(evalCase.dir);
  const bad = unknownRoles(evalCase.checklist, roles);
  if (bad.length) throw new Error(`${name}/checklist.yaml names roles wire does not know: ${bad.join("; ")} (roles: ${roles.join(", ")})`);
  return { evalCase, roles };
}

/** Every `attempt-<n>` directory of a sweep that archived a project. */
function archivedAttempts(sweepId: string): string[] {
  const root = join(PATHS.runsDir, sweepId);
  if (!existsSync(root)) throw new Error(`no sweep ${sweepId} under ${PATHS.runsDir}`);
  const out: string[] = [];
  for (const caseName of readdirSync(root)) {
    const caseDir = join(root, caseName);
    if (!statSync(caseDir).isDirectory()) continue;
    for (const config of readdirSync(caseDir)) {
      for (const attempt of readdirSync(join(caseDir, config))) {
        const dir = join(caseDir, config, attempt);
        if (/^attempt-\d+$/.test(attempt) && existsSync(join(dir, "project"))) out.push(dir);
      }
    }
  }
  return out.sort();
}

/**
 * A path as the person typed it: relative to where they invoked `pnpm` or
 * `make` (pnpm's `INIT_CWD`), not to this package, which is pnpm's cwd for us.
 */
function fromInvocation(path: string): string {
  return resolve(process.env.INIT_CWD ?? process.cwd(), path);
}

function log(args: string[]): number {
  const { values } = parseArgs({
    args,
    options: { attempt: { type: "string" }, slow: { type: "boolean" }, thinking: { type: "boolean" }, usage: { type: "boolean" } },
  });
  if (!values.attempt) {
    console.error("log needs --attempt <archived attempt dir>");
    return 2;
  }
  const found = resolveCodingRun(fromInvocation(values.attempt));
  if (!found.ok) {
    console.error(found.reason);
    return 2;
  }
  if (values.usage) {
    console.log(`  usage per agent — ${found.runDir}`);
    const rows = usageByAgent(readFileSync(join(found.runDir, ".logs", "runtime.log"), "utf8"));
    for (const r of rows) {
      const minutes = r.durationMs !== undefined ? `${(r.durationMs / 60_000).toFixed(1)} min` : "";
      console.log(
        `  ${r.agent.padEnd(44)} calls ${String(r.toolCalls).padStart(4)}  ${minutes.padStart(9)}  cache-read ${String(r.cacheRead).padStart(10)}  cache-write ${String(r.cacheCreation).padStart(8)}`,
      );
    }
    console.log("  (input/output tokens per agent are not in the transcript; the run total is run_settled in progress.ndjson)");
    return 0;
  }
  // Same precedence as `play log`.
  const view: LogView = values.slow ? "slow" : values.thinking ? "thinking" : "steps";
  console.log(`  ${view} — ${found.runDir}`);
  for (const line of renderLogView(found.runDir, view)) console.log(line);
  return 0;
}

function report(args: string[]): number {
  const { values } = parseArgs({ args, options: { sweep: { type: "string" } } });
  const sweepId = values.sweep ?? sweepIds().pop();
  if (!sweepId) {
    console.error("no sweeps yet");
    return 2;
  }
  console.log(rerenderReport(sweepId));
  return 0;
}

/** Re-render a sweep's report from its own `attempts.json` and `facts.json`. */
function rerenderReport(sweepId: string): string {
  const dir = join(PATHS.runsDir, sweepId);
  const records = JSON.parse(readFileSync(join(dir, "attempts.json"), "utf8")) as AttemptRecord[];
  const facts = existsSync(join(dir, "facts.json"))
    ? (JSON.parse(readFileSync(join(dir, "facts.json"), "utf8")) as Record<string, string>)
    : {};
  return writeReport(sweepId, records, facts, []);
}

function list(): number {
  console.log("cases:");
  for (const name of listCases(PATHS.casesDir)) {
    const evalCase = loadCase(PATHS.casesDir, name);
    console.log(`  ${name} — ${String(evalCase.checklist.items.length)} items · from ${evalCase.meta.source.snapshot} · ${evalCase.meta.description}`);
  }
  console.log("configs (first is the default):");
  for (const config of loadConfigs(PATHS.configsFile)) console.log(`  ${config.id} — ${config.runtime}, ${config.model}`);
  return 0;
}

function writeReport(sweepId: string, records: AttemptRecord[], facts: Record<string, string>, notes: string[]): string {
  const dir = join(PATHS.runsDir, sweepId);
  const summaries = summarize(records);
  const others = sweepIds().filter((id) => id !== sweepId);
  const baselines = new Map<string, { sweep: string; summary: Summary }>();
  for (const summary of summaries) {
    const base = pickBaseline(summary.key, sweepId, others, readSummaries);
    if (base) baselines.set(summary.key, base);
  }
  const text = renderReport({ sweepId, facts, notes, summaries, records, rewalks: rewalksOf(sweepId), baselines });
  writeFileSync(join(dir, "attempts.json"), JSON.stringify(records, null, 2));
  writeFileSync(join(dir, "summary.json"), JSON.stringify(summaries, null, 2));
  writeFileSync(join(dir, "report.md"), `${text}\n`);
  return text;
}

/** The rewalk records filed under a sweep's attempts (`attempt-<n>/rewalk-<k>/attempt.json`). */
function rewalksOf(sweepId: string): AttemptRecord[] {
  const root = join(PATHS.runsDir, sweepId);
  if (!existsSync(root)) return [];
  const records: AttemptRecord[] = [];
  for (const attemptDir of archivedAttempts(sweepId)) {
    for (const name of readdirSync(attemptDir).filter((n) => /^rewalk-\d+$/.test(n)).sort((a, b) => Number(a.slice(7)) - Number(b.slice(7)))) {
      try {
        records.push(JSON.parse(readFileSync(join(attemptDir, name, "attempt.json"), "utf8")) as AttemptRecord);
      } catch {
        // A rewalk interrupted before it wrote its record has nothing to list.
      }
    }
  }
  return records;
}

/** Sweep ids, oldest first. The `planner/` transcripts directory is not a sweep. */
function sweepIds(): string[] {
  if (!existsSync(PATHS.runsDir)) return [];
  return readdirSync(PATHS.runsDir, { withFileTypes: true })
    .filter((entry) => entry.isDirectory() && /^\d{4}-\d{2}-\d{2}T/.test(entry.name))
    .map((entry) => entry.name)
    .sort();
}

function readSummaries(sweepId: string): Summary[] | undefined {
  try {
    return JSON.parse(readFileSync(join(PATHS.runsDir, sweepId, "summary.json"), "utf8")) as Summary[];
  } catch {
    // An unreadable older summary costs the comparison, never the run.
    return undefined;
  }
}

/**
 * What the sweep ran against, so a report can be placed later: the commit, and
 * whether the skill library had uncommitted edits — the coding run reads
 * `skills/` from the working tree, so a dirty tree is a different skill set
 * than the commit names.
 */
function provenance(): Record<string, string> {
  const git = (args: string[]): string => {
    try {
      return execFileSync("git", args, { cwd: PATHS.repoRoot, encoding: "utf8" }).trim();
    } catch {
      return "";
    }
  };
  const head = git(["rev-parse", "--short", "HEAD"]);
  const dirtySkills = git(["status", "--porcelain", "--", "skills"]).split("\n").filter(Boolean).length;
  return {
    commit: head ? `${head} (${git(["rev-parse", "--abbrev-ref", "HEAD"])})` : "unknown",
    skills: dirtySkills ? `${String(dirtySkills)} uncommitted change(s) under skills/` : "clean",
  };
}

function positiveInt(raw: string | undefined, fallback: number): number {
  const n = Number(raw);
  return raw !== undefined && Number.isFinite(n) && n > 0 ? Math.floor(n) : fallback;
}

main().then(
  (code) => {
    process.exitCode = code;
  },
  (err: unknown) => {
    // A refused credential names the rule, not a stack.
    console.error(err instanceof CredentialError ? `refusing to run: ${err.message}` : err instanceof Error ? err.message : String(err));
    process.exitCode = 2;
  },
);
