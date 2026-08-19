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
 * The entry point. Discover, refuse if the tools are stale, sweep, report.
 *
 * Exits nonzero when the sweep could not run — never merely because a case
 * scored badly. A bad score is the OUTPUT of this tool, and a harness that
 * exits 1 on it cannot be used in the loop it exists for.
 */

import { existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { discoverCases, selectCases } from "./cases.js";
import { DEFAULTS, PATHS } from "./config.js";
import { credentialNotes, preflight } from "./preflight.js";
import { pickBaseline, renderReport, summarize, type Summary } from "./report.js";
import { warmCache } from "./scratch.js";
import { runSweep } from "./sweep.js";

// Every path this CLI touches is declared in config.ts; these are the names it
// reads them under.
const { casesDir: CASES_DIR, runsDir: RUNS_DIR, skillsDir: SKILLS_DIR } = PATHS;

interface Flags {
  suite?: string;
  case?: string;
  repeats: number;
  concurrency: number;
  timeoutMs: number;
  list: boolean;
  help: boolean;
}

function parseFlags(argv: string[]): Flags {
  const flags: Flags = {
    repeats: DEFAULTS.repeats,
    concurrency: DEFAULTS.concurrency,
    timeoutMs: DEFAULTS.timeoutMinutes * 60_000,
    list: false,
    help: false,
  };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    const value = argv[i + 1];
    switch (arg) {
      case "--suite":
        if (value) flags.suite = value;
        i += 1;
        break;
      case "--case":
        if (value) flags.case = value;
        i += 1;
        break;
      case "--repeats":
        flags.repeats = Math.max(1, Number(value) || DEFAULTS.repeats);
        i += 1;
        break;
      case "--concurrency":
        flags.concurrency = Math.max(1, Number(value) || DEFAULTS.concurrency);
        i += 1;
        break;
      case "--timeout":
        flags.timeoutMs = Math.max(1, Number(value) || DEFAULTS.timeoutMinutes) * 60_000;
        i += 1;
        break;
      case "--list":
        flags.list = true;
        break;
      case "-h":
      case "--help":
        flags.help = true;
        break;
      default:
        break;
    }
  }
  return flags;
}

const USAGE = `
Ballerina coding evals — one prompt, one package, one 'bal build'.

  pnpm eval [--suite <name>] [--case <name>] [--repeats N] [--concurrency N]
            [--timeout <minutes>] [--list]

  --suite        only cases in cases/<name>/ (any folder is a suite; comma-separated)
  --case         only cases with this filename stem (comma-separated)
  --repeats      attempts per case (use 3+ before believing a delta)
  --concurrency  attempts in flight at once
  --timeout      per-attempt ceiling in minutes
  --list         print the discovered cases and exit

Defaults and every other knob live in src/config.ts. A flag beats
BAL_EVAL_REPEATS / BAL_EVAL_CONCURRENCY / BAL_EVAL_TIMEOUT_MINUTES /
BAL_EVAL_MODEL, which beat the defaults there.

Runs on HOST, on your own 'claude login' — ANTHROPIC_API_KEY and
CLAUDE_CODE_OAUTH_TOKEN are stripped from every session. Requires 'bal' and an
installed 'bal library' (packages/bal-library-tool/install-local.sh).
`.trim();

async function main(): Promise<number> {
  const flags = parseFlags(process.argv.slice(2));
  if (flags.help) {
    console.log(USAGE);
    return 0;
  }

  const all = discoverCases(CASES_DIR);
  const selected = selectCases(all, flags.suite, flags.case);

  if (flags.list) {
    for (const c of all) console.log(`${c.suite}/${c.name}`);
    return 0;
  }
  if (selected.length === 0) {
    console.error(`no cases matched (suite=${flags.suite ?? "*"} case=${flags.case ?? "*"})`);
    console.error(`available: ${all.map((c) => `${c.suite}/${c.name}`).join(", ") || "none"}`);
    return 2;
  }

  // Before anything is spent. A sweep against a stale tool produces numbers,
  // not an error, and that is the expensive failure this refuses.
  const checks = preflight();
  if (checks.blockers.length > 0) {
    console.error("refusing to run:\n");
    for (const blocker of checks.blockers) console.error(`  - ${blocker}`);
    return 2;
  }
  const notes = credentialNotes(process.env);

  const stamp = new Date().toISOString().replace(/[:.]/g, "-");
  const runsRoot = join(RUNS_DIR, stamp);
  mkdirSync(runsRoot, { recursive: true });

  const attempts = selected.length * flags.repeats;
  console.log(
    `${selected.length} case(s) × ${flags.repeats} = ${attempts} attempt(s), ${flags.concurrency} at a time\n`,
  );
  for (const note of notes) console.log(`  note: ${note}`);

  warmCache(warmablePackages(selected.flatMap((c) => c.expect?.imports ?? [])));

  const results = await runSweep({
    cases: selected,
    repeats: flags.repeats,
    concurrency: flags.concurrency,
    timeoutMs: flags.timeoutMs,
    runsRoot,
    skillsDir: SKILLS_DIR,
    onEvent: (event) => {
      if (event.kind === "start") console.log(`  ▶ ${event.case} #${event.attempt}`);
      if (event.kind === "done") {
        const mark = event.green ? "✓" : "✗";
        console.log(
          `  ${mark} ${event.case} #${event.attempt} — ${event.lookups} lookups, ${Math.round(event.ms / 1000)}s`,
        );
      }
      if (event.kind === "failed") console.log(`  ! ${event.case} #${event.attempt} — ${event.reason}`);
      // Loud, and it names the attempt it was noticed at: every attempt BEFORE this
      // one may have run without the tool, and their numbers are not evidence.
      if (event.kind === "repaired") console.log(`  ⚠ ${event.case} #${event.attempt} — ${event.what}`);
    },
  });

  const summaries = summarize(results);
  const baseline = previousSummaries(
    runsRoot,
    summaries.map((s) => s.key),
  );
  const report = renderReport({
    summaries,
    facts: checks.facts,
    notes,
    concurrency: flags.concurrency,
    ...(baseline ? { baseline } : {}),
  });

  writeFileSync(join(runsRoot, "attempts.json"), JSON.stringify(results, null, 2));
  writeFileSync(join(runsRoot, "summary.json"), JSON.stringify(summaries, null, 2));
  writeFileSync(join(runsRoot, "report.md"), `${report}\n`);
  console.log(`\n${report}\n`);
  console.log(`artifacts: ${runsRoot}`);
  return 0;
}

/**
 * The baseline summary, read off disk. Which one to pick is `pickBaseline`'s —
 * that choice is part of the comparison and lives beside `compare` in
 * `report.ts`, not in the entry point, which cannot be imported without running
 * a sweep.
 */
function previousSummaries(currentRunDir: string, currentKeys: string[]): Summary[] | undefined {
  if (!existsSync(RUNS_DIR)) return undefined;
  const current = currentRunDir.split("/").pop();
  const candidates = readdirSync(RUNS_DIR).filter((d) => d !== current);
  return pickBaseline(candidates, currentKeys, (dir) => {
    try {
      return JSON.parse(readFileSync(join(RUNS_DIR, dir, "summary.json"), "utf8")) as Summary[];
    } catch {
      return undefined;
    }
  });
}

/**
 * The packages a sweep is about to need, taken from what the cases require.
 *
 * `expect.imports` is the only declaration of a coordinate a case involves, and
 * it is the right one to warm: a package the produced code must import is a
 * package the agent will look up. A case that pins nothing warms nothing, which
 * costs its first attempt one cold fetch and no correctness.
 */
function warmablePackages(imports: string[]): string[] {
  return [...new Set(imports.filter((i) => /^[a-z0-9_.]+\/[a-z0-9_.]+$/.test(i)))];
}

process.exitCode = await main();
