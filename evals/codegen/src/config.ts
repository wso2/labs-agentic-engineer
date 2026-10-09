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
 * Every tunable of the harness. Precedence: CLI flag > env var > default.
 */

import { homedir } from "node:os";
import { join } from "node:path";
import { agentBrowserBinDir } from "@aep/playground/src/engine/agent-browser.js";

const PACKAGE_ROOT = join(import.meta.dirname, "..");
const REPO_ROOT = join(PACKAGE_ROOT, "..", "..");

export const PATHS = {
  repoRoot: REPO_ROOT,
  packageRoot: PACKAGE_ROOT,
  /** Committed cases: one directory per case, `case.yaml` + `checklist.yaml` + `specs/` + `issues/`. */
  casesDir: join(PACKAGE_ROOT, "cases"),
  /** The run matrix — `{id, runtime, model}` entries; the first is the default. */
  configsFile: join(PACKAGE_ROOT, "configs.yaml"),
  /** Sweep archives, gitignored. One directory per sweep, named by its id. */
  runsDir: join(PACKAGE_ROOT, ".runs"),
  /** The playground package — `play` is run out of it, see `play.ts`. */
  playgroundDir: join(REPO_ROOT, "playground"),
  /** The one credential file (`credentials.ts`). */
  envFile: join(REPO_ROOT, "deployments", ".env"),
  /**
   * Where an attempt's project is staged: under $HOME (Colima shares only $HOME
   * with its VM) and outside the repo (the coding agent reads its whole project).
   */
  stageRoot: envString("CODEGEN_EVAL_STAGE_ROOT", join(homedir(), ".aep-evals", "codegen")),
} as const;

/** What `save` reads off a playground project. */
export const SAVE = {
  /** A case is these two directories and nothing else (ADR-0001). */
  caseDirs: ["specs", "issues"],
  /** Platform-compiled renderings, which code generation never reads. */
  renderedSuffixes: [".excalidraw", ".gen.json"],
  /**
   * Headings only an agent writes into an issue file. One of them in any issue
   * means the tree is post-run, whatever its top level looks like — `play undo`
   * restores the directories, and a hand-copied project can carry them too.
   */
  agentWrittenSections: [/^## Progress\s*$/m, /^## Mock verification\s*$/m],
  /**
   * A case name becomes part of a staged directory name, which becomes the
   * compose project `aep-wire-<slug>` and the container names under it. Short
   * and kebab keeps all of those legal.
   */
  namePattern: /^[a-z0-9][a-z0-9-]{1,30}$/,
} as const;

/** What an attempt's archive keeps of the generated project (`excludedFromProject` in attempt.ts). */
export const ARCHIVE = {
  /** Regenerable build output (`bal build` target/, `vite build` dist/), dropped only at an App Path's root. */
  buildOutputDirs: ["target", "dist"],
} as const;

export const DEFAULTS = {
  /** Attempts per case × config. */
  repeats: envInt("CODEGEN_EVAL_REPEATS", 1),
  /** Attempts in flight. Each attempt is a coding container, a compose project and a browser. */
  concurrency: envInt("CODEGEN_EVAL_CONCURRENCY", 1),
} as const;

/** Per-phase ceilings. Every one ends in the same teardown, never an orphan. */
export const TIMEOUTS = {
  /** `play code`; a case's `timeoutMinutes` overrides it. */
  codingMinutes: envInt("CODEGEN_EVAL_CODING_TIMEOUT_MINUTES", 90),
  /** SIGTERM → this → SIGKILL, for every `play` child. The coding run copies transcripts out on SIGTERM. */
  killGraceSeconds: 60,
  /** `play wire` until `READY <url>`. Compose builds every image cold, so minutes, not seconds. */
  wireReadyMinutes: envInt("CODEGEN_EVAL_WIRE_TIMEOUT_MINUTES", 20),
  /** SIGTERM to `STOPPED`/exit before the harness takes the compose project down itself. */
  wireStopMinutes: 2,
  walkMinutes: envInt("CODEGEN_EVAL_WALK_TIMEOUT_MINUTES", 30),
  /** The planner and the judge: one structured answer each. */
  plannerMinutes: 15,
  judgeMinutes: 10,
} as const;

/**
 * The harness's own agents' models (the model under test is in configs.yaml),
 * pinned so they cannot drift between sweeps (ADR-0003).
 */
export const MODELS = {
  planner: envString("CODEGEN_EVAL_PLANNER_MODEL", "claude-sonnet-5-5"),
  walker: envString("CODEGEN_EVAL_WALKER_MODEL", "claude-sonnet-5-5"),
  judge: envString("CODEGEN_EVAL_JUDGE_MODEL", "claude-sonnet-5-5"),
} as const;

export const PLANNER = {
  tools: ["Read", "Glob", "Grep"],
  maxTurns: 60,
  /** Structured output that fails the schema is retried once, then refused. */
  attempts: 2,
} as const;

export const WALKER = {
  /** Bash is `agent-browser` only and Read/Write stay in `walk/` — `walker.ts`'s guard enforces both. */
  tools: ["Bash", "Read", "Write"],
  /** This package's `agent-browser`, held equal to the runner image's by a test, first on the walk's PATH. */
  binDir: agentBrowserBinDir(PACKAGE_ROOT),
  /** The skill section the walker prompt embeds, so both walks confirm an action by the same text. */
  confirmSection: { file: join(REPO_ROOT, "skills", "agent-browser", "SKILL.md"), heading: "## Confirm each action" },
  maxTurns: envInt("CODEGEN_EVAL_WALK_MAX_TURNS", 400),
  /** Consecutive agent-browser timeouts before the browser is declared unresponsive (`BrowserWatchdog`). */
  unresponsiveAfter: 3,
  /**
   * Shell metacharacters a walker command may not contain. With these gone a
   * command is one `agent-browser` invocation and nothing else: no chaining,
   * no substitution, no redirect to a file the guard never saw.
   */
  forbiddenShell: [";", "&", "|", "$(", "`", ">", "<", "\n"],
  /** Verbs refused as the first argument: they act behind the page's UI or outside the isolated session (ADR-0003). */
  forbiddenVerbs: ["chat", "eval", "webmcp", "connect", "auth", "install", "upgrade", "dashboard", "stream", "inspect"],
  /**
   * `network route`/`unroute` would let the walker answer the app's own
   * requests — the one workaround that makes every item pass.
   */
  forbiddenSubcommands: { network: ["route", "unroute"] } as Record<string, string[]>,
  /**
   * Flags refused anywhere on the line. `--all` is `close --all`, which closes
   * EVERY session on the machine, someone else's included; the session and
   * profile flags would leave the attempt's own isolated session.
   */
  forbiddenFlags: [
    "--all",
    "--session",
    "--session-name",
    "--profile",
    "--state",
    "--cdp",
    "--auto-connect",
    "--provider",
    "--headed",
    "--allow-file-access",
    "--allowed-domains",
  ],
  /** Navigation is held to the app on this machine (`--allowed-domains`, via its env var). */
  allowedDomains: "localhost,127.0.0.1",
} as const;

export const JUDGE = {
  tools: [] as string[],
  attempts: 2,
} as const;

/** What every attempt records about the tree it ran against (`provenance.ts`). */
export const PROVENANCE = {
  /** Uncommitted paths under these roots change what an attempt measured. */
  dirtyRoots: ["skills/", "runners/remote-worker/", "playground/", "evals/codegen/"],
  /** `skills.diff` covers this root — the part a skill edit loop changes between sweeps. */
  diffRoot: "skills/",
} as const;

/** Verdict bands, on a 0..100 score — the spec-agents evals' (`evals/spec-agents/src/scoring/bands.ts`). */
export const BANDS = {
  pass: 75,
  review: 50,
} as const;

/**
 * The `AEP_MODEL_*` names a coding run reads as a model connection — the
 * playground's `CODING_CONNECTION_ENV` (`playground/src/kit/model-connection.ts`)
 * plus `AEP_MODEL_CONTEXT_WINDOW`. Copied, not imported: that module loads
 * `@aep/ae-design-agent`, whose module scope merges `deployments/.env` into
 * `process.env`, and this process must never hold that file's API key.
 */
export const CONNECTION_ENV = [
  "AEP_MODEL_FORMAT",
  "AEP_MODEL_BASE_URL",
  "AEP_MODEL_AUTH_SCHEME",
  "AEP_MODEL_API_KEY",
  "AEP_MODEL_WEB_SEARCH",
  "AEP_MODEL_CONTEXT_WINDOW",
] as const;

function envInt(name: string, fallback: number): number {
  const raw = process.env[name]?.trim();
  if (!raw) return fallback;
  const parsed = Number(raw);
  return Number.isFinite(parsed) && parsed > 0 ? Math.floor(parsed) : fallback;
}

function envString(name: string, fallback: string): string {
  return process.env[name]?.trim() || fallback;
}
