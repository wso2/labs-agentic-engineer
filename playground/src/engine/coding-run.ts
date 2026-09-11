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
 * One coding run (mirrors prod's milestone cycle, `docs/decisions/ADR-0011`;
 * playground decision: `design/decisions/ADR-0001-milestone-batch-coding-run.md`):
 * spawn the remote-worker's local entrypoint over the WHOLE project, stream
 * its NDJSON progress contract into a live timeline, and archive the run.
 *
 * There is no status write-back here. Prod's own dispatch returns before any
 * outcome is known — "a signal is a wake-up, never evidence"
 * (`services/aep-api/internal/delivery/run/doc.go`) — and the playground
 * mirrors that: this module never edits an issue file. Whether an issue is
 * done is read fresh from the project tree each run (the `aep` skill's
 * local-mode discovery step), never cached in frontmatter or flipped on an
 * exit code.
 *
 * Works on ANY directory containing `specs/` + `issues/` — no playground
 * state, no prior phases, no engineering-agent process (hard requirement 2).
 *
 * Two run modes, same `local.ts` entrypoint:
 *   docker (default) — runs inside the exact `remote-worker/Dockerfile` image
 *     production ships (Debian, pinned Go, baked Playwright/chromium, the
 *     non-root `aep` user), so a skill authored here behaves under the same
 *     toolchain a real cluster run gives it. `local.ts` is never baked into
 *     that image (`.dockerignore` — see `remote-worker/AGENTS.md`), so it is
 *     bind-mounted in at run time alongside the working-tree skill library and
 *     the project/run dirs; only the entrypoint command is overridden, the
 *     image itself is untouched.
 *   host — the prior bare `npx tsx` child process, no Docker dependency.
 *     Opt in with `--host` (faster iteration, weaker parity) when Docker
 *     isn't available or a fast loop matters more than environment fidelity.
 */

import { spawn } from "node:child_process";
import { createWriteStream, existsSync, mkdirSync, readdirSync, readFileSync, statSync } from "node:fs";
import { homedir } from "node:os";
import { basename, join } from "node:path";
import { stdout as output } from "node:process";
import {
  formatAgentStatus,
  formatEvent,
  formatOutcome,
  groupByAgent,
  mergeOutcomes,
  LEAD_AGENT_ID,
  type AgentSection,
  type RunEventView,
} from "@aep/progress-view";
import { createAgentTags, type AgentTags } from "./agent-tags.js";
import { openCrewPane } from "./crew-pane.js";
import { REPO_ROOT } from "../paths.js";

const LOCAL_ENTRY = join(REPO_ROOT, "runners", "remote-worker", "src", "local.ts");
const BUILD_RUNNER_SCRIPT = join(REPO_ROOT, "deployments", "scripts", "build-runner.sh");
// Where the image keeps the skill library. Mounting the working tree over it is
// what makes a skill edit — including the local-mode overlay — apply to the next
// run with no rebuild. It is the ONE library the run reads, and local mode mirrors
// it into the project dir's .claude/skills/, standing in for the BFF's write.
const IMAGE_LIBRARY_DIR = "/app/skills";
const RUNNER_IMAGE = process.env.AGENT_RUNNER_IMAGE || "aep-runner:dev";
// The `bal library` tool, which the `ballerina` skill drives by name. The image
// INSTALLS it (see the Dockerfile) rather than putting a command on PATH: it is a
// Ballerina CLI tool, so it lives in the `aep` user's local bala repository and
// `bal` dispatches `library` to it. What a run reads is therefore ONE jar, at a
// path the bala layout fixes.
//
// Docker mode mounts the tool's working-tree jar over that one, so the tuning
// loop is `./gradlew :native:jar` in the tool's repository and never an image
// rebuild — the same trade the skill library makes a few lines above, for the
// same reason: what is being iterated on is what the agent reads, and a rebuild
// between edit and run is what stops people iterating.
//
// Host mode gets no such overlay, because there is nothing to overlay: `bal`
// resolves the tool out of the developer's OWN home, and the loop there is the
// tool's `install-local.sh`. Rather than write into someone's `~/.ballerina`
// behind their back, host mode reports what it found (see `hostToolAdvice`).
const BAL_TOOL_REPO = join(REPO_ROOT, "packages", "bal-library-tool");

// The tool's coordinates, which its bala path is built out of. Pinned here rather
// than parsed out of the tool's `Ballerina.toml`: that file is the source of
// truth and a test holds these two to it, so a rename fails a test instead of
// silently mounting over a path no image has.
const TOOL_ORG = "ballerinax";
const TOOL_NAME = "tool_library";
/** Where the image's install puts the jar, and the working-tree jar to put there. */
export interface ToolJarOverlay {
  hostJar: string;
  imageJar: string;
}

/**
 * The tool's own build output, when its repository is checked out and built.
 *
 * Found by looking rather than by composing a version into a filename: the jar's
 * name carries the tool's version, and the mount does not care what it is —
 * what lands in the container is named by the IMAGE's side of the mount. One jar
 * or nothing, because two would mean guessing which build to send.
 */
export function workingTreeToolJar(): string | undefined {
  const libs = join(BAL_TOOL_REPO, "native", "build", "libs");
  if (!existsSync(libs)) return undefined;
  const [jar, ...extra] = readdirSync(libs).filter((entry) => entry.endsWith(".jar"));
  return jar !== undefined && extra.length === 0 ? join(libs, jar) : undefined;
}

/**
 * The jar mount for a docker run, or undefined to leave the image's own install
 * alone — which is the honest outcome when the tool's repository is not checked
 * out beside this one, since the image's copy is then the only one there is.
 *
 * The container-side path is composed from the tool's declared version, because
 * that is what the image's install used. Bumping that version without rebuilding
 * the image therefore aims this mount at a path that image does not have;
 * `make build-runner FORCE=1` is what keeps the two together, and the line this
 * prints on every run names the version it is mounting as.
 */
export function toolJarOverlay(): ToolJarOverlay | undefined {
  const hostJar = workingTreeToolJar();
  const version = installedToolVersion();
  if (!hostJar || !version) return undefined;
  const libs = `/home/aep/.ballerina/repositories/local/bala/${TOOL_ORG}/${TOOL_NAME}/${version}/any/tool/libs`;
  return { hostJar, imageJar: `${libs}/native-${version}.jar` };
}

/**
 * The version the image installed.
 *
 * Read from the tool's `gradle.properties`, which is where `make-dist.sh`
 * derives it — and `make-dist.sh` is what the image's first stage runs, so this
 * is the same value by construction rather than by agreement. It used to be read
 * from a checked-in `vendor/.../VERSION`; ADR-0008 deleted that file, and a
 * missing file here returns undefined, which silently disables the overlay.
 */
function installedToolVersion(): string | undefined {
  try {
    const properties = readFileSync(join(BAL_TOOL_REPO, "gradle.properties"), "utf8");
    return /^version=(.+)$/m.exec(properties)?.[1]?.trim() || undefined;
  } catch {
    return undefined;
  }
}

/** Every tool jar installed in this developer's own bala repository. */
function installedToolJars(): string[] {
  const root = join(homedir(), ".ballerina", "repositories", "local", "bala", TOOL_ORG, TOOL_NAME);
  if (!existsSync(root)) return [];
  return readdirSync(root).flatMap((version) => {
    const libs = join(root, version, "any", "tool", "libs");
    if (!existsSync(libs)) return [];
    return readdirSync(libs)
      .filter((entry) => entry.endsWith(".jar"))
      .map((entry) => join(libs, entry));
  });
}

/**
 * What host mode will actually resolve `bal library` to, when that is worth
 * saying — nothing when the installed tool already IS the working-tree build.
 *
 * Reported rather than repaired. Installing is a write into the developer's own
 * `~/.ballerina`, and a dev harness silently replacing a tool they may have
 * installed from a release is not a trade worth making for two saved seconds.
 *
 * "Built after it was installed", by mtime, is the question — NOT "are the bytes
 * the same". A gradle jar is not byte-reproducible (the zip carries entry
 * timestamps), so comparing content calls an unchanged rebuild stale and the
 * advice becomes noise nobody reads. `install-local.sh` copies without `-p`, so
 * the installed copy is stamped when it landed, which is exactly the ordering
 * this needs.
 *
 * A missing tool is advice, never a precondition — the `ballerina` skill treats a
 * failed lookup the way it treats any other: write from `code-rules.md` and let
 * `bal build` name what is wrong. Blocking the run would cost more than the
 * accuracy it buys.
 */
export function hostToolAdvice(): string | undefined {
  const installed = installedToolJars();
  if (installed.length === 0) {
    return (
      "`bal library` is not installed, so the ballerina skill's lookups will fail and it will " +
      "write from code-rules.md instead — install it with packages/bal-library-tool/install-local.sh"
    );
  }
  const hostJar = workingTreeToolJar();
  if (!hostJar) return undefined;
  const built = statSync(hostJar).mtimeMs;
  if (installed.some((jar) => statSync(jar).mtimeMs >= built)) return undefined;
  return (
    "`bal library` resolves to an installed jar older than your working-tree build — " +
    "re-run packages/bal-library-tool/install-local.sh to put this run on your changes"
  );
}
// The Agent SDK's project state inside the image: the lead's own transcript, a
// fanned-out subagent's (`<slug>/<session>/subagents/agent-<taskId>.jsonl`), and
// the output files its backgrounded tasks wrote. Those transcripts are the ONLY
// record of what an agent was doing beyond the one-line summary on the feed.
// They used to die with the container, exactly when it mattered: two consecutive
// todo-api99 runs each lost a subagent to `Agent stalled: no progress for 600s
// (stream watchdog did not recover)`, and the evidence that would distinguish a
// dropped stream from a model that genuinely emitted nothing was already gone.
//
// NOT `/tmp/claude-<uid>`, which is where the failure event points: the files it
// keeps under `tasks/` are symlinks into this directory, so copying that one out
// of a container yields nothing but broken links. The `taskId` in the failure
// event is the `agent-<taskId>.jsonl` here — that is the whole mapping.
//
// The lead's transcript is duplicated here (we already stream it to `.logs/`), a
// few hundred KB per run and worth not having to special-case a session id.
//
// Copied at BOTH ends of a subagent's life, and the asymmetry is the SDK's: it
// deletes a subagent's transcript the moment that subagent completes — probing a
// live container found `subagents/agent-<id>.meta.json` mid-run and an empty
// directory minutes later, which is also why the symlinks under
// `/tmp/claude-<uid>/tasks/` are broken by the time a run exits. So a FAILED
// subagent is snapshotted the instant its failure reaches the feed, which is the
// last moment its file exists, while everything that survives to the end (the
// lead, the task outputs) is taken once when the run settles.
//
// Neither mounting nor redirecting this path works, so don't retry them: the CLI
// refuses a temp dir whose owner is not its own uid, and on macOS every path
// inside a bind mount reports the HOST uid no matter who created it — so any
// mounted location fails the check at startup and takes the whole run with it.
// `docker cp` touches nothing the run can see, which keeps the environment
// identical to production's.
//
// Playground-only. Production's oneshot pod keeps this on its own ephemeral
// filesystem, and giving it a durable home there is a decision for the pod's
// log/artifact story, not something to smuggle in through a dev harness.
const IMAGE_AGENT_SESSION_DIR = "/home/aep/.claude/projects";

/** One NDJSON event off the runner's v2 feed, as this harness reads it. */
export type ProgressEvent = RunEventView;

// Attribution is a fixed-width tag so the glyphs stay in one column and a
// fanned-out run still scans as a single timeline. The tag is a NUMBER, not the
// agent's label: labels run to a full sentence ("Implement todo-api Ballerina
// service (issue #3)") and would push every line off the right edge. The label
// is announced once, by the agent's own `agent_started` row.
const TAG_WIDTH = "[#1] ".length;

export type TimelineRenderer = (e: ProgressEvent) => string[];

// The one place the local harness legitimately says something the console does
// not. A local run has no remote and no GitHub, so a push or a `gh` call is a
// no-op the agent may not realise it made — worth flagging where it is true,
// and meaningless in a cluster run where both exist.
function annotateForLocalMode(e: ProgressEvent, text: string): string {
  if (!text) return text;
  if (e.kind === "git_push") return `${text} — no remote in local mode`;
  if (e.kind === "gh_action") return `${text} — no GitHub in local mode`;
  return text;
}

/**
 * Build the renderer for ONE run: it numbers agents as they appear, so
 * concurrent fan-outs stay tellable apart across lines, and indents them by the
 * depth the feed declares. Returns zero lines for a silent event, one for a
 * normal row, and more when a settling agent brings its closing report with it.
 *
 * The WORDING of every line comes from @aep/progress-view, the same module the
 * console renders through — so what you iterate on here is what a cluster run
 * shows, and a wording defect cannot hide in one surface. Only the terminal
 * presentation (the tags, the column, the indent) is this harness's own.
 *
 * A terminal cannot go back and rewrite a row it printed, so this pass is
 * honest about that: a section's header is its `agent_started` row where it
 * appeared, and its report arrives later with its `agent_settled`. The merged
 * pass below is the console-shaped rendering of the same events.
 */
export function createTimelineRenderer(tags: AgentTags = createAgentTags()): TimelineRenderer {
  // An agent's label and depth are declared once, by its `agent_started`. A
  // stream rendered event by event has to remember them: without this a settling
  // agent reports under its opaque runtime id, which is not a name a reader can
  // match to the section it just watched. The console's grouping holds the same
  // memory in its section; here it is these two maps.
  const labels = new Map<string, string>();
  const depths = new Map<string, number>();

  return function render(e: ProgressEvent): string[] {
    if (e.kind === "agent_started") {
      if (e.depth) depths.set(e.agentId, e.depth);
      if (e.label) labels.set(e.agentId, e.label);
    }
    const named: ProgressEvent = e.label ? e : { ...e, label: labels.get(e.agentId) };

    const { text, report } = formatEvent(named);
    const body = annotateForLocalMode(named, text);
    if (!body) return [];

    // The lead is untagged: it is the overwhelming majority of a run's rows, and
    // an unstamped row reading as "the lead" is what keeps the feed quiet. The
    // registry is SHARED with the crew block, so `[#2]` on a step line and `#2`
    // on a crew row are the same agent — see `agent-tags.ts`.
    const short = tags(e.agentId);
    const tag = short ? `[${short}]` : "";
    // Depth 1 sits in the tag column; deeper agents step right, so a grandchild
    // reads as nested rather than as another sibling of the lead.
    const depth = e.agentId === LEAD_AGENT_ID ? 0 : (depths.get(e.agentId) ?? 1);
    const nest = "  ".repeat(Math.max(0, depth - 1));
    const gutter = tag ? `${tag} `.padEnd(TAG_WIDTH) : " ".repeat(TAG_WIDTH);

    const out = [`  ${gutter}${nest}${body}`];
    // The agent's own account of what it did. It is the ONLY copy — a spawned
    // agent's transcript never reaches this feed — so it is printed in full
    // rather than truncated onto the row above.
    if (report) {
      for (const line of report.split("\n")) out.push(`  ${" ".repeat(TAG_WIDTH)}${nest}  ${line}`);
    }
    return out;
  };
}

// Where an outcome's column starts in the merged pass. Wide enough for the
// commands a real run issues; anything longer pushes its outcome right rather
// than being cut, because a truncated command is worse than a ragged column.
const OUTCOME_COLUMN = 62;

/**
 * The whole run again, once every event is in hand: one row per step with its
 * outcome attached, and each agent's work gathered under its own report.
 *
 * This exists because a terminal cannot go back and rewrite a line it printed.
 * The live stream above is honest about that — an outcome follows as a
 * continuation row — but it means the fast local loop is NOT shaped like the
 * console, which is the surface being iterated on. Printing a merged pass at the
 * end gives both: live while it runs, console-shaped afterwards.
 */
export function renderMergedTimeline(events: readonly ProgressEvent[]): string[] {
  const out: string[] = [];
  const row = (indent: string, text: string, outcome: string): void => {
    if (!text) return;
    out.push(outcome ? `${(indent + text).padEnd(OUTCOME_COLUMN)} ${outcome}` : `${indent}${text}`);
  };
  const reportBlock = (indent: string, report: string | undefined): void => {
    if (!report) return;
    for (const line of report.split("\n")) out.push(`${indent}${line}`);
  };

  const walk = (section: AgentSection<ProgressEvent>, indent: string): void => {
    // An agent's own events are merged as ONE stream: an action and its outcome
    // are routinely separated by a nested section that spoke in between, so
    // pairing has to survive the gap. Looked up per event afterwards, which
    // keeps each section printed where its agent first spoke.
    const merged = new Map(
      mergeOutcomes(section.rows.flatMap((r) => (r.kind === "event" ? [r.event] : []))).map((m) => [
        m.line,
        m,
      ]),
    );
    for (const r of section.rows) {
      if (r.kind === "section") {
        out.push(`${indent}⑂ ${r.section.agent.label} — ${formatAgentStatus(r.section.agent)}`);
        reportBlock(`${indent}  `, r.section.agent.report);
        walk(r.section, `${indent}│ `);
        continue;
      }
      const m = merged.get(r.event);
      if (!m) continue; // folded into an earlier action's row
      const { text } = formatEvent(r.event);
      const { detail, duration } = formatOutcome(m.outcome);
      row(indent, annotateForLocalMode(r.event, text), [detail, duration].filter(Boolean).join(" · "));
    }
  };

  const lead = groupByAgent(events);
  walk(lead, "  ");
  // The lead's own closing report last, where the run ends — it is about the
  // whole run rather than any one step of it.
  reportBlock("  ", lead.agent.report);
  return out;
}

export interface CodingRunOptions {
  projectDir: string;
  /** The authored skill library (repo-root `skills/`); the run reads nothing else. */
  skillsDir: string;
  silent?: boolean;
  /** "docker" (default): same image prod runs. "host": bare `npx tsx`, no Docker. */
  mode?: "docker" | "host";
  /**
   * `--api-key`: host mode authenticates with `ANTHROPIC_API_KEY` instead of the
   * developer's own Claude credentials. Ignored by docker mode, which always
   * needs the key. See `hostInvocation`.
   */
  useApiKey?: boolean;
}

export interface CodingRunResult {
  /** 0 = the session did what it could; 1 = the agent gave up; 2 = setup/crash. */
  exitCode: number;
  runDir: string;
}

interface Invocation {
  command: string;
  args: string[];
  env: NodeJS.ProcessEnv;
}

/**
 * Host mode runs on the developer's own machine, so it uses the developer's own
 * Claude credentials: the key is WITHHELD and the Agent SDK falls back to
 * whatever `claude login` left in the OS credential store.
 *
 * Deliberate, not an omission. The playground CLI resolves `ANTHROPIC_API_KEY`
 * out of `deployments/.env` for the ENGINEERING agent, which is an AI SDK model
 * call and has no other way to authenticate. The coding agent is not — it is a
 * Claude Code session, which already knows how to authenticate itself — so
 * inheriting that key would silently bill a local skill-tuning loop to the
 * platform's key instead of the developer's own subscription, and would put a
 * shared credential in the environment of a bypassPermissions process running
 * against the developer's filesystem.
 *
 * Docker mode keeps the key (`dockerInvocation`): a container reaches no
 * keychain, so there is nothing to fall back to.
 *
 * `--api-key` opts back into key auth, and it is a FLAG rather than the
 * repo's usual "an exported env var beats `.env`" rule because that rule cannot
 * be evaluated here: `@aep/agents` calls `loadDotenv()` at module scope, so
 * `deployments/.env` is already merged into `process.env` before this package's
 * own entrypoint runs, and a shell-exported key is by then indistinguishable
 * from a file-supplied one. An explicit flag says what an unreadable heuristic
 * only implied.
 *
 * `AEP_CODING_ANTHROPIC_KEY` (see `codingCredential`) changes WHICH credential
 * `--api-key` opts into — the coding-agent one rather than the platform key —
 * but it does not opt in on its own. Setting a variable is not the same act as
 * asking this run to authenticate with it, and host mode's default has to stay
 * "the developer's own login" for the reason above: a bypassPermissions process
 * on a developer's filesystem should not silently acquire a shared credential
 * because a file elsewhere happened to define one.
 *
 *   host                 → nothing; `claude login` answers
 *   host --api-key       → AEP_CODING_ANTHROPIC_KEY, else ANTHROPIC_API_KEY
 *   docker               → AEP_CODING_ANTHROPIC_KEY, else ANTHROPIC_API_KEY
 *
 * See ADR-0016 for the platform half.
 */
export function hostInvocation(opts: CodingRunOptions, runDir: string): Invocation {
  // Host mode has no image, so every tool a skill names comes off the developer's
  // own machine — which is exactly what --host already means for `bal`, `go` and
  // `playwright-cli`, and now for `bal library` too: it is a `bal` tool, resolved
  // out of `~/.ballerina`, so there is no PATH entry to point anywhere. What that
  // resolves to is reported by `hostToolAdvice`, not patched here.
  const env: NodeJS.ProcessEnv = {
    ...process.env,
    AEP_LOCAL_PROJECT_DIR: opts.projectDir,
    AEP_LOCAL_RUN_DIR: runDir,
    AEP_LOCAL_SKILLS_DIR: opts.skillsDir,
  };
  const coding = opts.useApiKey ? codingCredential() : undefined;
  if (coding) {
    applyCodingCredential(env, coding);
    return { command: "npx", args: ["tsx", LOCAL_ENTRY], env };
  }
  // No coding credential in play. A token reaching this process came from
  // `deployments/.env` (the platform's file) and never from `claude login`,
  // which stores credentials in the OS keychain rather than the environment —
  // so it is withheld unconditionally, including under `--api-key`, which opts
  // into the API KEY specifically.
  delete env.CLAUDE_CODE_OAUTH_TOKEN;
  if (!opts.useApiKey) delete env.ANTHROPIC_API_KEY;
  return { command: "npx", args: ["tsx", LOCAL_ENTRY], env };
}

/** A resolved coding credential: the value plus how it must be presented. */
export interface CodingCredential {
  value: string;
  /** The env var Claude Code must read it from. */
  envVar: "ANTHROPIC_API_KEY" | "CLAUDE_CODE_OAUTH_TOKEN";
}

/**
 * The coding agent's own credential for a local run: the playground's half of
 * the platform's per-org coding-agent key, so a developer can point local
 * coding runs at the same separate credential their organization bills them to
 * (ADR-0016).
 *
 * Accepts EITHER a Console API key or a `claude setup-token` OAuth token, and
 * reports which env var it has to arrive as. The prefix is the discriminator:
 * the two are minted by different systems and cannot collide.
 *
 * `AEP_CODING_ANTHROPIC_KEY` is the ONLY source, deliberately — a bare
 * `CLAUDE_CODE_OAUTH_TOKEN` is NOT adopted even though Claude Code would read
 * one. `@aep/agents` calls `loadDotenv()` at module scope, so anything in
 * `deployments/.env` is already in `process.env` before this package runs, and
 * that file is the PLATFORM's generated env rather than a developer's personal
 * one. Honouring a token found there would let a shared file silently redirect
 * who gets billed — the same unreadable heuristic `--api-key` exists to avoid.
 *
 * Returns undefined when unset or blank — blank must NOT count as "set", or a
 * stray `export AEP_CODING_ANTHROPIC_KEY=` would authenticate the run with an
 * empty string instead of falling back.
 */
export function codingCredential(): CodingCredential | undefined {
  const value = process.env.AEP_CODING_ANTHROPIC_KEY?.trim();
  if (!value) return undefined;
  return {
    value,
    envVar: value.startsWith("sk-ant-oat") ? "CLAUDE_CODE_OAUTH_TOKEN" : "ANTHROPIC_API_KEY",
  };
}

/**
 * Put the credential into `env` under its own name, and REMOVE the other one.
 *
 * The removal is the whole point. Claude Code ranks `ANTHROPIC_API_KEY` above
 * `CLAUDE_CODE_OAUTH_TOKEN`, and `deployments/.env` hands almost every
 * developer an `ANTHROPIC_API_KEY` — so an OAuth token merely ADDED to the
 * environment would be silently ignored and the default key billed, which is
 * exactly the outcome setting a coding credential is meant to prevent.
 */
export function applyCodingCredential(env: NodeJS.ProcessEnv, cred: CodingCredential): void {
  delete env.ANTHROPIC_API_KEY;
  delete env.CLAUDE_CODE_OAUTH_TOKEN;
  env[cred.envVar] = cred.value;
}

// Mounts local.ts + the library/project/run dirs over the unmodified production
// image and overrides only the command (the image's CMD runs oneshot.ts) — the
// image itself never gains playground-only bytes. The library is mounted over the
// path the image already bakes it at, so the runner's own default resolves to the
// working tree and there is ONE library in play rather than two.
export function dockerInvocation(opts: CodingRunOptions, runDir: string, containerName: string): Invocation {
  // Forward exactly ONE credential variable — the one this run will actually
  // authenticate with. Passing both names would hand the container a developer's
  // subscription token even on runs that bill the API key (which outranks it),
  // putting a secret somewhere it is never read.
  const coding = codingCredential();
  const credentialVar = coding?.envVar ?? "ANTHROPIC_API_KEY";
  const toolJar = toolJarOverlay();
  const args = [
    "run",
    // NOT `--rm`. The run's transcripts and its backgrounded tasks' output files
    // live inside the container, and the end-of-run copy has to happen after the
    // process that wrote them has exited — `--rm` deletes them at exactly that
    // moment, which is the race this drops the flag to avoid (`docker cp` reads
    // a stopped container happily; it cannot read a removed one). The harness
    // removes the container itself once the copy is done, and reaps any left
    // behind by a run that was killed outright (see `reapExitedRuns`).
    "--name",
    // Named so the container can be copied out of and then removed — and so a
    // FAILED subagent's transcript can be rescued mid-run, while it is still on
    // disk (see IMAGE_AGENT_SESSION_DIR).
    containerName,
    "--entrypoint",
    // The image's OWN entrypoint wrapper, not `npx` directly. The wrapper sets
    // the rlimits every process a run spawns inherits (`ulimit -c 0`, so a
    // crashing JVM or chromium cannot drop a core into the project working
    // tree) and then execs its argv — which is what the three tokens after the
    // image name below become, exactly as the image's CMD does in a pod.
    // Overriding the entrypoint with `npx` skipped that, leaving the playground
    // as the one run path without the limits.
    "/usr/local/bin/aep-runner-entrypoint",
    "--shm-size=1g",
    "-v",
    `${LOCAL_ENTRY}:/app/src/local.ts:ro`,
    "-v",
    `${opts.skillsDir}:${IMAGE_LIBRARY_DIR}:ro`,
    ...(toolJar ? ["-v", `${toolJar.hostJar}:${toolJar.imageJar}:ro`] : []),
    "-v",
    `${opts.projectDir}:/workspace/project`,
    "-v",
    `${runDir}:/workspace/run`,
    "-e",
    credentialVar,
    "-e",
    "AEP_LOCAL_PROJECT_DIR=/workspace/project",
    "-e",
    "AEP_LOCAL_RUN_DIR=/workspace/run",
    "-e",
    `AEP_LOCAL_SKILLS_DIR=${IMAGE_LIBRARY_DIR}`,
    RUNNER_IMAGE,
    "npx",
    "tsx",
    "src/local.ts",
  ];
  // The credential is forwarded BY NAME above and substituted through docker's
  // OWN environment, so the secret never lands in an argv that any user on the
  // machine can read out of `ps`.
  let env = process.env;
  if (coding) {
    env = { ...process.env };
    applyCodingCredential(env, coding);
  }
  return { command: "docker", args, env };
}

/**
 * A spawned agent that ended in failure — a stall, a crash, a killed task.
 *
 * v2 says so outright: `agent_settled` carries the runtime's own verdict, so
 * this no longer has to infer a whole agent's fate from the outcome of the tool
 * call that spawned it. `stopped` is deliberately NOT a failure — the work was
 * taken away rather than going wrong, and snapshotting a cancelled agent's
 * transcript as a failure would file a diagnostic for a defect that never was.
 */
export function isFailedAgent(e: ProgressEvent): boolean {
  return e.kind === "agent_settled" && e.status === "failed";
}

/**
 * Copy the runtime's own scratch — session transcripts and the output files its
 * backgrounded tasks wrote — out of the container into the run dir.
 *
 * ONE mechanism, called at two moments, because the evidence has two different
 * last-possible-instants:
 *
 *   `<agentId>` — when a SPAWNED agent fails, while the container is still
 *     running. The SDK deletes a subagent's transcript the moment that subagent
 *     completes, so a failure line on the feed is the last time the file exists
 *     at all (see IMAGE_AGENT_SESSION_DIR).
 *   `final` — when the run ends. The lead's transcript and every backgrounded
 *     task's `output_file` live under the same tree and survive to the end; this
 *     is what puts them beside the run's `progress.ndjson` and `.logs/`.
 *
 * LOCAL PLANE ONLY, per the storage decision: nothing here is uploaded and the
 * console never shows it. A pod keeps this on its own ephemeral filesystem, and
 * giving it a durable home there is a decision for the pod's artifact story
 * rather than something to smuggle in through a dev harness.
 *
 * Docker mode only — a host run's transcripts are already in the developer's own
 * `~/.claude`, which is theirs and not this harness's to copy around.
 *
 * Best-effort and silent: a run is not worth failing over diagnostics, and the
 * copy legitimately finds nothing when a failure lands before the SDK has
 * written anything.
 */
async function snapshotAgentSessions(containerName: string, runDir: string, label: string): Promise<void> {
  const dest = join(runDir, "agent-sessions", label);
  try {
    mkdirSync(dest, { recursive: true });
    await runProcess("docker", ["cp", `${containerName}:${IMAGE_AGENT_SESSION_DIR}/.`, dest], "ignore");
  } catch {
    // nothing to rescue, or docker refused — the run's own outcome is unaffected
  }
}

/** Drop this run's container, once everything worth keeping is out of it. */
async function removeContainer(containerName: string): Promise<void> {
  try {
    await runProcess("docker", ["rm", "-f", containerName], "ignore");
  } catch {
    // already gone, or docker is not answering — neither changes the run's result
  }
}

/**
 * Remove containers a previous run left behind.
 *
 * Dropping `--rm` moves cleanup into this process, and a process can be killed
 * outright (`SIGKILL`, a closed terminal) between the container exiting and the
 * removal. Filtered to EXITED ones so a playground run in another terminal is
 * never touched: a running `aep-play-*` container is somebody's live run.
 */
async function reapExitedRuns(): Promise<void> {
  try {
    await runProcess(
      "bash",
      ["-c", 'ids=$(docker ps -aq --filter "name=^aep-play-" --filter status=exited); [ -n "$ids" ] && docker rm $ids || true'],
      "ignore",
    );
  } catch {
    // best effort — a stale container costs disk, not correctness
  }
}

function runProcess(
  command: string,
  args: string[],
  stdio: "inherit" | "ignore",
  env?: NodeJS.ProcessEnv,
): Promise<void> {
  return new Promise((resolvePromise, reject) => {
    const child = spawn(command, args, { cwd: REPO_ROOT, stdio, ...(env ? { env } : {}) });
    child.on("error", reject);
    child.on("close", (code) => (code === 0 ? resolvePromise() : reject(new Error(`${command} exited ${code}`))));
  });
}

async function ensureRunnerImage(silent?: boolean): Promise<void> {
  const stdio = silent ? "ignore" : "inherit";
  try {
    await runProcess("docker", ["info"], "ignore");
  } catch {
    throw new Error("docker daemon not reachable — start it (e.g. `colima start`), or pass --host to skip Docker");
  }
  // Idempotent: skips the (multi-minute, first-time) build when the tag
  // already exists. SKIP_IMPORT=1 — this is a plain local docker run, not a
  // k3d node; without it the script also tries (and, absent/stale k3d, fails
  // noisily at) a k3d image import that a playground run has no use for.
  await runProcess("bash", [BUILD_RUNNER_SCRIPT], stdio, { ...process.env, SKIP_IMPORT: "1" }).catch((err) => {
    throw new Error(`runner image build failed: ${err instanceof Error ? err.message : String(err)}`);
  });
}

/**
 * Spawn one coding run over the WHOLE project; resolves with the exit code +
 * archived run dir. Success (`exitCode === 0`) means the session completed,
 * NOT that every issue got resolved — leaving some open is normal (mirrors
 * prod: "a later cycle picks it up"). Which issues actually landed is never
 * read back here; it is whatever the project tree looks like afterward.
 */
export async function runCodingAgent(opts: CodingRunOptions): Promise<CodingRunResult> {
  const stamp = new Date().toISOString().replace(/[:.]/g, "-");
  const runDir = join(opts.projectDir, ".aep-playground", "runs", `${stamp}-code`);
  mkdirSync(runDir, { recursive: true });
  const progressLog = createWriteStream(join(runDir, "progress.ndjson"), { flags: "w" });

  const mode = opts.mode ?? "docker";

  if (mode === "docker") {
    // A container reaches no credential store, so the key is the only auth it
    // can have. Checked here rather than inside the runner: this is knowable
    // before a multi-minute image build, and the runner itself is now
    // mode-agnostic about the key (see `local.ts`).
    // Either credential satisfies it: a coding-specific one is what the
    // container would be given, so demanding ANTHROPIC_API_KEY as well would
    // reject a correctly-configured run.
    if (!codingCredential() && !process.env.ANTHROPIC_API_KEY) {
      progressLog.end();
      if (!opts.silent) {
        output.write(
          "  ✗ No Anthropic credential is set — docker mode needs one (export ANTHROPIC_API_KEY, or add it to deployments/.env).\n" +
            "    Set AEP_CODING_ANTHROPIC_KEY instead to bill coding runs separately — an API key,\n" +
            "    or a `claude setup-token` token to bill your Claude subscription.\n" +
            "    `--host` instead runs on your own Claude credentials (`claude login`).\n",
        );
      }
      return { exitCode: 2, runDir };
    }
    try {
      await ensureRunnerImage(opts.silent);
    } catch (err) {
      progressLog.end();
      if (!opts.silent) output.write(`  ✗ ${err instanceof Error ? err.message : String(err)}\n`);
      return { exitCode: 2, runDir };
    }
    await reapExitedRuns();
  }

  // Which `bal library` this run reads, when that is not simply "the one the
  // image installed". It is the ballerina skill's source for every signature, and
  // a run reading a stale jar looks exactly like a run reading a fresh one — so
  // the one case worth a line is the one where they differ.
  if (!opts.silent) {
    const overlay = mode === "docker" ? toolJarOverlay() : undefined;
    const note = overlay
      ? `bal library: mounting ${basename(overlay.hostJar)} over the image's install`
      : mode === "host"
        ? hostToolAdvice()
        : undefined;
    if (note) output.write(`  ℹ ${note}\n`);
  }

  // Names this run's container so its scratch can be copied out after it exits.
  // The run dir's timestamp is already unique per run; `docker` accepts it as-is.
  const containerName = mode === "docker" ? `aep-play-${stamp}` : "";
  const { command, args, env } =
    mode === "docker" ? dockerInvocation(opts, runDir, containerName) : hostInvocation(opts, runDir);

  return new Promise((resolvePromise) => {
    const child = spawn(command, args, { cwd: REPO_ROOT, env, stdio: ["ignore", "pipe", "pipe"] });

    // ONE registry of agent tags for the whole run, so the streamed lines and the
    // crew block name the same agent the same way.
    const tags = createAgentTags();
    // One renderer per run — it numbers this run's agents (see above).
    const render = createTimelineRenderer(tags);
    // The crew, pinned under the stream. On anything that is not a terminal this
    // is a plain printer and the output is byte-identical to what it always was
    // — the playground's transcripts are piped and archived.
    const pane = openCrewPane({ out: output, isTTY: !opts.silent && output.isTTY === true, tag: tags });
    // Kept so the run can be re-rendered console-shaped once it ends. Bounded by
    // the run itself, same as the progress.ndjson beside it.
    const events: ProgressEvent[] = [];
    let buffer = "";
    child.stdout.on("data", (chunk: Buffer) => {
      buffer += chunk.toString("utf8");
      for (;;) {
        const nl = buffer.indexOf("\n");
        if (nl < 0) break;
        const line = buffer.slice(0, nl);
        buffer = buffer.slice(nl + 1);
        if (line.trim() === "") continue;
        progressLog.write(line + "\n");
        let event: ProgressEvent;
        try {
          event = JSON.parse(line) as ProgressEvent;
        } catch {
          // non-NDJSON runner logging — pass through
          if (!opts.silent) pane.line(`  ${line}`);
          continue;
        }
        events.push(event);
        // Fire-and-forget: the copy races the runtime's own cleanup of that
        // agent's files, so it starts now rather than after this batch of
        // events is rendered.
        if (containerName && isFailedAgent(event)) {
          void snapshotAgentSessions(containerName, runDir, event.agentId || `seq-${String(events.length)}`);
        }
        if (opts.silent) continue;
        for (const rendered of render(event)) pane.line(rendered);
        // Every event, every time: the throttle lives in the pane, which is the
        // only thing that knows when it last drew.
        pane.update(events);
      }
    });
    // Line-buffered, so the child's diagnostics go through the pane like every
    // other line rather than landing in the middle of the pinned block.
    let errBuffer = "";
    child.stderr.on("data", (chunk: Buffer) => {
      if (opts.silent) return;
      errBuffer += chunk.toString("utf8");
      for (;;) {
        const nl = errBuffer.indexOf("\n");
        if (nl < 0) break;
        pane.line(errBuffer.slice(0, nl));
        errBuffer = errBuffer.slice(nl + 1);
      }
    });

    // `error` and `close` can both fire for one child. Settling twice would run
    // the end-of-run copy and the container removal a second time.
    let settled = false;
    const settle = async (exitCode: number): Promise<void> => {
      if (settled) return;
      settled = true;
      progressLog.end();
      // A partial stderr line the child never terminated is still evidence.
      if (!opts.silent && errBuffer) pane.line(errBuffer);
      errBuffer = "";
      pane.close();
      // The container has exited but still exists, which is the whole reason
      // `--rm` is not passed: this is the only moment the lead's transcript and
      // the backgrounded tasks' output files can be taken out of it.
      if (containerName) {
        await snapshotAgentSessions(containerName, runDir, "final");
        await removeContainer(containerName);
      }
      if (!opts.silent && events.length > 0) {
        output.write("\n  ── the run, merged ──\n");
        for (const line of renderMergedTimeline(events)) output.write(line + "\n");
      }
      resolvePromise({ exitCode, runDir });
    };
    child.on("error", (err) => {
      if (!opts.silent) pane.line(`  ✗ spawn failed: ${err.message}`);
      void settle(2);
    });
    child.on("close", (code, signal) => {
      void settle(signal ? 130 : (code ?? 2));
    });

    // Ctrl-C: kill the child.
    const onInt = (): void => {
      child.kill("SIGTERM");
    };
    process.once("SIGINT", onInt);
    child.on("close", () => process.removeListener("SIGINT", onInt));
  });
}
