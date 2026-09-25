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

// Every decision the CLI makes, in one place a test can drive without a real
// promptfoo process: grader fallback, path resolution, the child's
// environment, the stale-output guard, and the run-failure-vs-ordinary-
// report choice. `bin/agent-eval.ts` is wiring only — it supplies the real
// spawn and calls `runCli`.

import { readFileSync, writeFileSync, mkdirSync, rmSync, existsSync } from "node:fs";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { parseScenarios } from "./scenario.js";
import { buildPromptfooConfig } from "./config.js";
import { readVerdict } from "./verdict.js";
import { renderReport, renderRunFailureReport } from "./report.js";
import { readToolStubs } from "./agent-doc.js";

export interface SpawnResult {
  status: number | null;
  stderr: string;
  error?: Error | undefined;
}

export type SpawnPromptfoo = (
  args: string[],
  opts: { cwd: string; env: NodeJS.ProcessEnv },
) => SpawnResult;

export interface RunCliOptions {
  argv: string[];
  env: NodeJS.ProcessEnv;
  cwd: string;
  spawnPromptfoo: SpawnPromptfoo;
}

export interface RunCliResult {
  reportPath: string;
  markdown: string;
}

// The one mistake this CLI cannot make quietly: a key leaking into a log
// line or a report. Nothing here writes ANTHROPIC_API_KEY on purpose, but
// promptfoo's own stderr is a third party's text — scrub anything
// key-shaped before it reaches anything written to disk.
function redactKeys(text: string): string {
  return text.replace(/sk-ant-[A-Za-z0-9_-]+/g, "«redacted»");
}

function arg(argv: string[], name: string): string {
  const i = argv.indexOf(`--${name}`);
  if (i === -1 || !argv[i + 1]) throw new Error(`agent-eval: --${name} is required`);
  return argv[i + 1]!;
}

// `AGENT_EVAL_GRADER` unset falls back to the documented default; SET-BUT-
// BLANK must fall back too. `?? default` alone would hand `""` straight to
// `buildPromptfooConfig`, which rejects a blank grader on purpose — a `??`
// only catches `undefined`/`null`, not an empty string.
export function resolveGraderModel(env: NodeJS.ProcessEnv): string {
  const fromEnv = env.AGENT_EVAL_GRADER;
  if (fromEnv !== undefined && fromEnv.trim() !== "") return fromEnv;
  return "anthropic:messages:claude-sonnet-5";
}

/**
 * How long a boot of the agent under test may take.
 *
 * Two bounds, because the two situations are not the same. The FIRST boot is
 * a diagnosis: until one has succeeded, an agent that does not come up is
 * almost certainly misconfigured, and paying a long bound for that answer on
 * every scenario of every round costs a coding agent its deadline. Once one
 * boot has succeeded, a slow one is a busy machine and deserves the patience.
 *
 * `AGENT_EVAL_BOOT_TIMEOUT_MS` moves the first bound for a genuinely slow
 * component. A value that is not a positive number is IGNORED rather than
 * passed on: `Number("soon")` is NaN, which reads as an already-expired
 * deadline and would fail every boot instantly, blaming the agent for a typo
 * in an environment variable.
 */
const DEFAULT_FIRST_BOOT_TIMEOUT_MS = 20_000;
const DEFAULT_READY_TIMEOUT_MS = 60_000;

export function resolveBootTimeouts(env: NodeJS.ProcessEnv): {
  firstBootTimeoutMs: number;
  readyTimeoutMs: number;
} {
  const raw = Number(env.AGENT_EVAL_BOOT_TIMEOUT_MS);
  const first =
    env.AGENT_EVAL_BOOT_TIMEOUT_MS !== undefined && Number.isFinite(raw) && raw > 0
      ? raw
      : DEFAULT_FIRST_BOOT_TIMEOUT_MS;
  return { firstBootTimeoutMs: first, readyTimeoutMs: Math.max(first, DEFAULT_READY_TIMEOUT_MS) };
}

// Only these ever cross into the child. promptfoo does not need the rest of
// this process's environment, and the rest may hold credentials — the
// coding agent's own OAuth token among them — that have no business
// reaching a large third-party dependency tree. The model credential is not
// listed here because it is not forwarded under the name it arrived as; see
// resolveModelKey.
const ALLOWED_ENV_KEYS = ["PATH", "HOME"] as const;

/**
 * The org's Anthropic key, under either of the two names it can arrive as.
 *
 * In a build pod the platform mounts it as `AEP_EVAL_ANTHROPIC_API_KEY`. It
 * cannot use `ANTHROPIC_API_KEY` there: that name already belongs to Claude
 * Code, which ranks it above `CLAUDE_CODE_OAUTH_TOKEN`, so an org that bills
 * its coding agent to an OAuth token would have its whole coding session
 * silently moved onto this key instead (ADR-0016). Outside a pod —
 * a developer running the harness in the monorepo — `ANTHROPIC_API_KEY` is
 * the only key there is, so it is the fallback.
 *
 * `AEP_EVAL_KEY_MANAGED` is what tells the two apart, and it is why the
 * fallback is not unconditional. On a pod, `ANTHROPIC_API_KEY` is the
 * organisation's CODING credential — possibly an override it chose to bill
 * coding and nothing else — so falling back to it would grade agents on a
 * budget the org ring-fenced. The platform sets the declaration on every
 * dispatch, so its presence means: if no evaluation key came with it, this run
 * has none, and saying so is the documented behaviour.
 *
 * `CLAUDE_CODE_OAUTH_TOKEN` is NEVER a fallback. It is the platform's own
 * coding budget, and it authenticates none of the API calls the judge makes.
 *
 * `||`, not `??`: ESO can materialise an EMPTY secret, and an empty key is no
 * key rather than a key that fails to authenticate. The difference decides
 * whether the report says the agent never became ready or the judge is pointed
 * at an endpoint with a blank credential.
 */
function resolveModelKey(env: NodeJS.ProcessEnv): string | undefined {
  const evaluationKey = env.AEP_EVAL_ANTHROPIC_API_KEY || undefined;
  if (evaluationKey !== undefined) return evaluationKey;
  if (env.AEP_EVAL_KEY_MANAGED) return undefined;
  return env.ANTHROPIC_API_KEY || undefined;
}

export function buildChildEnv(env: NodeJS.ProcessEnv): NodeJS.ProcessEnv {
  const out: NodeJS.ProcessEnv = {
    PROMPTFOO_DISABLE_TELEMETRY: "1",
    PROMPTFOO_DISABLE_UPDATE: "1",
    PROMPTFOO_DISABLE_SHARING: "1",
  };
  for (const key of ALLOWED_ENV_KEYS) {
    const value = env[key];
    if (value !== undefined) out[key] = value;
  }
  // The agent under test and the judge share ONE credential — the org's
  // Anthropic key — under the two names each expects. It travels as an
  // environment variable and never through the emitted config file, which
  // is written into the build's output directory.
  const modelKey = resolveModelKey(env);
  if (modelKey !== undefined) {
    out.ANTHROPIC_API_KEY = modelKey;
    out.MODEL_API_KEY = modelKey;
  }
  return out;
}

// Sibling to this module, not a `../src` hop from bin/ — that hop breaks
// once this file is compiled to dist/src/cli.js, since dist never carries
// the raw .ts sources bin/ used to reach across to. Matching THIS module's
// own extension (".ts" under tsx, ".js" once built) means the reference is
// correct wherever it runs, without needing to know which one that is.
function resolveProviderPath(): string {
  const ext = import.meta.url.endsWith(".ts") ? "ts" : "js";
  return fileURLToPath(new URL(`./provider.${ext}`, import.meta.url));
}

// Tries each candidate directory in order and writes to the first one that
// accepts it. Exists for the case a `--out` itself turns out to be
// unwritable: falling back to the same bad path would just fail silently a
// second time, and a failure that never lands on disk is indistinguishable
// from a silent clean pass to whoever reads the build afterward.
function writeReportBestEffort(candidates: string[], markdown: string): string {
  let lastCandidate = ".";
  for (const dest of candidates) {
    lastCandidate = dest;
    try {
      mkdirSync(dest, { recursive: true });
      const reportPath = join(dest, "report.md");
      writeFileSync(reportPath, markdown);
      return reportPath;
    } catch {
      continue;
    }
  }
  return join(lastCandidate, "report.md");
}

function finishWithFailure(candidates: string[], component: string, error: string): RunCliResult {
  const markdown = renderRunFailureReport({ component, error: redactKeys(error) });
  const reportPath = writeReportBestEffort(candidates, markdown);
  return { reportPath, markdown };
}

/**
 * NEVER throws and NEVER encodes a "should this fail the build" decision —
 * evaluation reports, it does not gate. Every failure path below ends in a
 * written report, including ones this function cannot recover from cleanly
 * (a missing `--out` has nowhere sensible to write TO, so it falls back to
 * `cwd`, but it still writes something rather than raising).
 */
export function runCli(opts: RunCliOptions): RunCliResult {
  let outDir: string | undefined;
  let component = "unknown component";
  try {
    // `out` is resolved before `app` on purpose: it is where a failure
    // report belongs, so establishing it early means a missing `--app`
    // still has a real destination to report into, not just `cwd`.
    const scenariosPath = resolve(opts.cwd, arg(opts.argv, "scenarios"));
    outDir = resolve(opts.cwd, arg(opts.argv, "out"));
    mkdirSync(outDir, { recursive: true });
    const appDir = resolve(opts.cwd, arg(opts.argv, "app"));
    // Required, not optional. The agent document is what says which provider
    // contracts must be stubbed; without it the agent boots with its tool
    // addresses unset, and every failure that follows reads as bad behaviour
    // rather than as a harness that never wired the tools.
    const afmPath = resolve(opts.cwd, arg(opts.argv, "afm"));
    const toolStubs = readToolStubs(afmPath);

    const file = parseScenarios(JSON.parse(readFileSync(scenariosPath, "utf8")));
    component = file.component;

    const configPath = join(outDir, "promptfooconfig.json");
    const outJsonPath = join(outDir, "out.json");
    const reportPath = join(outDir, "report.md");

    writeFileSync(
      configPath,
      JSON.stringify(
        buildPromptfooConfig(file, {
          providerPath: resolveProviderPath(),
          graderModel: resolveGraderModel(opts.env),
          // The stubs are STARTED by the provider, inside the promptfoo
          // child, not here: `spawnPromptfoo` blocks this process's event
          // loop for the whole run, so a server listening here would never
          // answer a request. The CLI decides WHAT to stub; the provider
          // serves it.
          providerConfig: { appDir, toolStubs, ...resolveBootTimeouts(opts.env) },
        }),
        null,
        2,
      ),
    );

    // A previous round's output must never be mistaken for this round's:
    // promptfoo does not truncate `-o` up front, so a crash before it
    // writes would otherwise leave a stale file behind for the read below
    // to pick up as if it were fresh — a silent clean pass wearing the
    // previous round's score.
    rmSync(outJsonPath, { force: true });

    const run = opts.spawnPromptfoo(["eval", "-c", configPath, "-o", outJsonPath, "--no-cache"], {
      cwd: appDir,
      env: buildChildEnv(opts.env),
    });

    // promptfoo's OWN exit code means "did every assertion pass", not "did
    // the run succeed" — it is non-zero for an ordinary low-scoring or
    // errored SCENARIO too, and that is report content the Verdict already
    // renders, not a run failure. The signal for "the run itself failed" is
    // whether it left a readable result behind at all.
    let outJson: unknown;
    try {
      if (run.error !== undefined) throw run.error;
      if (!existsSync(outJsonPath)) {
        throw new Error(
          `promptfoo produced no output (exit ${String(run.status)})\n${run.stderr}`,
        );
      }
      outJson = JSON.parse(readFileSync(outJsonPath, "utf8"));
    } catch (e) {
      const detail = e instanceof Error ? e.message : String(e);
      return finishWithFailure([outDir], component, detail);
    }

    const verdict = readVerdict(outJson, file);
    const markdown = renderReport(verdict, {
      component,
      promptChanged: opts.env.AGENT_EVAL_PROMPT_CHANGED === "1",
    });
    writeFileSync(reportPath, markdown);
    return { reportPath, markdown };
  } catch (e) {
    const detail = e instanceof Error ? (e.stack ?? e.message) : String(e);
    const candidates = outDir !== undefined ? [outDir, opts.cwd] : [opts.cwd];
    return finishWithFailure(candidates, component, detail);
  }
}
