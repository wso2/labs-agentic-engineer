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
 * Entry point (docs/design/playground.md §7):
 *
 *   pnpm play                                → print usage help (bare invocation)
 *   pnpm play menu                           → project picker → phase menu
 *   pnpm play <dir>                          → chat home (slash commands run phases; /menu for the dashboard)
 *   pnpm play <dir> requirements|design|chat → run one phase; exit code = result
 *   pnpm play <dir> tasks|code|check|undo    → later steps of the impl plan
 *
 * Flags: --idea "<text>", --target "<x>", --fresh, --silent, --restore, --yes,
 * -h/--help. `code` also takes --host (run the coding agent as a bare host
 * process instead of the default Docker-image run — see engine/coding-run.ts).
 */

import "./devtools-default.js"; // MUST be first: sets AGENT_DEVTOOLS before the agents config loads
import { existsSync } from "node:fs";
import { parseArgs } from "node:util";
import { stdout as output } from "node:process";
import * as clack from "@clack/prompts";
import { loadRepoSkills } from "./kit/skills.js";
import { parseStartCommand, slashSkillInstruction } from "@aep/contracts/prompts";
import { startInstruction } from "./engine/compose.js";
import { loadDotenv } from "@aep/agents/shared/env";
import {
  chatTurn,
  codeCommand,
  logCommand,
  designCommand,
  requirementsCommand,
  tasksCommand,
  undoCommand,
  type CodeOptions,
  type PhaseOptions,
  type PhaseOutcome,
} from "./commands.js";
import { checkProject } from "./engine/check.js";
import { openSession, SKILLS_DIR } from "./engine/session.js";
import { expandProjectPath, projectDirError } from "./paths.js";
import { projectSlug } from "./ports/spec-workspace.js";
import { pickProject } from "./tui/picker.js";
import { phaseMenu, type MenuAction } from "./tui/phase-menu.js";
import { chatLoop } from "./tui/chat.js";
import { tasksScreen } from "./tui/tasks.js";
import { ensureProjectDir } from "./tui/ensure-dir.js";
import { readIdea, writeDescriptor } from "./state/descriptor.js";
import { confirmCodingDir } from "./tui/consent.js";

const COMMANDS = new Set(["requirements", "design", "tasks", "code", "chat", "check", "undo", "log", "menu"]);

/** Bare `play`, `play help`, or `-h/--help` → the one-screen command reference. */
function printUsage(): void {
  output.write(
    [
      "AEP playground — spec-driven SDLC, one project at a time.",
      "",
      "Usage:",
      "  pnpm play                                 show this help",
      "  pnpm play menu                            pick a project, then the phase menu",
      "  pnpm play <dir>                           open <dir> in chat (created if missing; /menu for the dashboard)",
      "  pnpm play <dir> requirements|design       generate or refine the spec",
      "  pnpm play <dir> tasks|code|check|undo      run a later step of the impl plan",
      "  pnpm play <dir> log [--slow|--thinking]   read the last coding run in detail (developer view)",
      '  pnpm play <dir> chat "<message>"          one-shot headless chat turn',
      "",
      "Flags:",
      '  --idea "<text>"   the project idea — captured into specs/.agentic-engineer.toml',
      '  --target "<x>"    narrow the phase to one component/target',
      "  --fresh           reset the conversation before the run",
      "  --silent          suppress live turn rendering",
      "  --restore         restore the latest undo snapshot before the run",
      "  --yes             headless consent for the coding agent (bypass-permissions)",
      "  --host            (code) run the coding agent as a bare host process, not the runner image",
      "  --api-key         (code --host) authenticate with ANTHROPIC_API_KEY instead of your Claude login",
      "  --slow            (log) only the calls that took 3s or more, slowest first",
      "  --thinking        (log) the model's reasoning blocks (main agent only — subagents emit none)",
      "  --run <name>      (log) an older archived run instead of the newest",
      "  -h, --help        show this help",
      "",
      "Tracing: AI SDK DevTools is on by default — run `npx @ai-sdk/devtools` (port 4983).",
      "",
      "Example:",
      '  pnpm play playground/.projects/expense-app requirements --idea "Expense claim tracking app"',
      "",
    ].join("\n"),
  );
}

async function askIdea(): Promise<string | null> {
  const idea = await clack.text({
    message: "What are you building?",
    placeholder: "An online store for handmade ceramics",
  });
  return clack.isCancel(idea) ? null : idea;
}

/**
 * Capture the project's idea AT CREATION — the same moment the console's create
 * form captures it — and write `specs/.agentic-engineer.toml`. `--idea` supplies
 * it non-interactively; otherwise a TTY is asked once, here, so `/start` never
 * has to.
 *
 * Only on creation: opening an existing project never prompts. A project that
 * ends up with no descriptor (headless creation, or a cancelled prompt) is fine
 * — the start skill opens by asking for the idea instead.
 */
async function captureIdeaOnCreate(projectDir: string, idea: string | undefined): Promise<void> {
  if (readIdea(projectDir)) return;
  let text = idea?.trim() ?? "";
  if (!text && process.stdin.isTTY) text = (await askIdea())?.trim() ?? "";
  if (!text) return;
  writeDescriptor(projectDir, projectSlug(projectDir), text);
}

function printCheckFindings(projectDir: string): boolean {
  let allOk = true;
  for (const f of checkProject(projectDir)) {
    output.write(f.ok ? `  ✓ ${f.path} — ${f.message}\n` : `  ✗ ${f.path} — ${f.message}\n`);
    if (!f.ok) allOk = false;
  }
  return allOk;
}

async function runHeadless(
  command: string,
  projectDir: string,
  opts: CodeOptions,
  commandArg?: string,
): Promise<number> {
  let outcome: PhaseOutcome;
  switch (command) {
    case "requirements":
      outcome = await requirementsCommand(projectDir, opts, process.stdin.isTTY ? askIdea : undefined);
      break;
    case "design":
      outcome = await designCommand(projectDir, opts);
      break;
    case "tasks":
      outcome = await tasksCommand(projectDir, opts);
      break;
    case "code":
      // One session works the whole project — the `aep` skill decides
      // discovery, ordering and fan-out (see its SKILL.md).
      outcome = await codeCommand(projectDir, opts, confirmCodingDir(projectDir));
      break;
    case "undo":
      outcome = undoCommand(projectDir, opts);
      break;
    case "log":
      // Developer detail, read from the archived run rather than folded into the
      // progress feed — the feed answers a different question (see log-read.ts).
      outcome = logCommand(projectDir, opts);
      break;
    case "chat": {
      // Headless one-shot chat turn: `play <dir> chat "message"` — same
      // general conversation as the TUI chat screen (scriptable follow-ups).
      if (!commandArg) {
        output.write('usage: play <dir> chat "<message>"\n');
        return 1;
      }
      const session = await openSession(projectDir, opts);
      try {
        // Same `/<skill>` shortcut as the interactive chat loop (e.g.
        // `play <dir> chat "/spec an expense app"`); a plain message rides
        // through verbatim. No reserved control words in the one-shot verb.
        //
        // `/start` is expanded HERE rather than sent verbatim: production
        // relies on aep-api to expand it, but the playground talks to the
        // agents service directly, so it does the server's job itself.
        const start = parseStartCommand(commandArg);
        const instruction = start
          ? startInstruction(start.inlineIdea || readIdea(projectDir))
          : (slashSkillInstruction(commandArg) ?? commandArg);
        outcome = await chatTurn(session, instruction, opts);
      } finally {
        await session.close();
      }
      break;
    }
    case "check":
      return printCheckFindings(projectDir) ? 0 : 1;
    default:
      output.write(`"${command}" is not wired yet (see docs/design/playground.md §13)\n`);
      return 2;
  }
  if (!outcome.ok) {
    output.write(`✗ ${command}: ${outcome.detail ?? "failed"}\n`);
    return 1;
  }
  output.write(`✓ ${command} done\n`);
  return 0;
}

/** Chat home: open the session, run the chat loop, hand off to the menu on `/menu`. */
async function runChat(projectDir: string, opts: PhaseOptions): Promise<number> {
  const session = await openSession(projectDir, opts);
  let next: "menu" | "quit";
  try {
    next = await chatLoop(session, opts);
  } finally {
    await session.close();
  }
  return next === "quit" ? 0 : runMenu(projectDir, opts);
}

async function runMenu(projectDir: string, opts: PhaseOptions): Promise<number> {
  clack.intro("AEP playground");
  for (;;) {
    const skillCount = loadRepoSkills(SKILLS_DIR).length;
    const action: MenuAction = await phaseMenu(projectDir, projectSlug(projectDir), skillCount);
    if (action === "quit") break;
    if (action === "code") {
      // One session works the whole project (VS Code is the file browser —
      // no per-issue picking, no review detour).
      await runHeadless("code", projectDir, opts);
      continue;
    }
    if (action === "tasks") {
      const tasksAction = await tasksScreen(projectDir);
      if (tasksAction.kind === "plan") await runHeadless("tasks", projectDir, opts);
      if (tasksAction.kind === "code") await runHeadless("code", projectDir, opts);
      continue;
    }
    if (action === "chat") {
      const session = await openSession(projectDir, opts);
      try {
        const next = await chatLoop(session, opts);
        if (next === "quit") break;
      } finally {
        await session.close();
      }
      continue;
    }
    const code = await runHeadless(action, projectDir, opts);
    if (code === 2) continue; // unwired action — back to the menu
  }
  clack.outro("bye");
  return 0;
}

async function main(): Promise<number> {
  // Load deployments/.env up front: the coding path spawns the runner with
  // the CLI's env (no openSession), so ANTHROPIC_API_KEY must be resolved here.
  loadDotenv();
  const { values, positionals } = parseArgs({
    args: process.argv.slice(2),
    options: {
      idea: { type: "string" },
      target: { type: "string" },
      fresh: { type: "boolean" },
      silent: { type: "boolean" },
      restore: { type: "boolean" },
      yes: { type: "boolean" },
      host: { type: "boolean" },
      "api-key": { type: "boolean" },
      slow: { type: "boolean" },
      thinking: { type: "boolean" },
      run: { type: "string" },
      help: { type: "boolean", short: "h" },
    },
    allowPositionals: true,
  });

  // Bare `play` (no positionals), `play help`, or `-h/--help` → usage help.
  // The interactive project picker now lives behind `play menu`.
  if (values.help || positionals.length === 0 || positionals[0] === "help") {
    printUsage();
    return 0;
  }

  const opts: CodeOptions = {
    ...(values.idea ? { idea: values.idea } : {}),
    ...(values.target ? { target: values.target } : {}),
    ...(values.fresh ? { fresh: true } : {}),
    ...(values.silent ? { silent: true } : {}),
    ...(values.restore ? { restore: true } : {}),
    ...(values.yes ? { yes: true } : {}),
    ...(values.host ? { host: true } : {}),
    ...(values["api-key"] ? { apiKey: true } : {}),
    // `log` defaults to the per-step view; --slow and --thinking narrow it.
    ...(values.slow ? { view: "slow" as const } : values.thinking ? { view: "thinking" as const } : {}),
    ...(values.run ? { run: values.run } : {}),
  };

  let [dirArg, command, commandArg] = positionals as [string | undefined, string | undefined, string | undefined];
  // `pnpm play requirements` inside a project dir: first positional is a command.
  if (dirArg && COMMANDS.has(dirArg) && !existsSync(expandProjectPath(dirArg))) {
    commandArg = command;
    command = dirArg;
    dirArg = undefined;
  }

  // paths.ts is the single fence: relative paths resolve against the user's
  // invocation dir (INIT_CWD — pnpm rewrites the process cwd to the package),
  // and inside the repo only the gitignored playground/.projects/ subtree is
  // a legal project home.
  let projectDir = dirArg ? expandProjectPath(dirArg) : null;
  let createdProject = false;
  if (projectDir) {
    // A directly-supplied dir is fenced and created here: TTY prompts before
    // creating a missing dir; headless refuses (it can't ask). Non-directories
    // and illegal in-repo paths are rejected with the helper's message.
    const ensured = await ensureProjectDir(projectDir, { interactive: process.stdin.isTTY });
    if (!ensured.ok) {
      output.write(`${ensured.message}\n`);
      return 1;
    }
    projectDir = ensured.path;
    createdProject = ensured.created;
  } else {
    if (command && !process.stdin.isTTY) {
      output.write("a project directory is required in headless mode\n");
      return 1;
    }
    const picked = await pickProject();
    if (!picked) return 0;
    projectDir = picked.path;
    createdProject = picked.created;
  }
  // Re-fence the resolved dir: ensureProjectDir already fenced a supplied path,
  // but a picker recent can carry a stale pre-fence path.
  const fenceError = projectDirError(projectDir);
  if (fenceError) {
    output.write(`${fenceError}\n`);
    return 1;
  }

  // A brand-new project captures its idea now, before any surface opens, so
  // both the chat `/start` and the headless `requirements` verb find it.
  if (createdProject) await captureIdeaOnCreate(projectDir, values.idea);

  // Chat is the home surface: `play <dir>` drops straight in; `play menu` opens
  // the dashboard; any other verb runs headless.
  if (command === "menu") return runMenu(projectDir, opts);
  if (command) return runHeadless(command, projectDir, opts, commandArg);
  return runChat(projectDir, opts);
}

main().then(
  (code) => {
    process.exitCode = code;
  },
  (err: unknown) => {
    output.write(`playground error: ${err instanceof Error ? (err.stack ?? err.message) : String(err)}\n`);
    process.exitCode = 1;
  },
);
