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
 * The walker: an agent that uses the running app through a real browser and
 * records what it saw; it neither scores nor fixes (ADR-0004). Its cwd and tools
 * are confined to `walk/` by a PreToolUse hook, hence the held-open prompt
 * stream (`session.ts`).
 */

import { execFile, spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { isAbsolute, relative, resolve } from "node:path";
import type { HookCallbackMatcher, SDKMessage } from "@anthropic-ai/claude-agent-sdk";
import { agentBrowserProblem, withAgentBrowserFirst } from "@aep/playground/src/engine/agent-browser.js";
import { z } from "zod";
import type { Item, MustNot } from "./case.js";
import { MODELS, TIMEOUTS, WALKER } from "./config.js";
import { runSession, type SessionResult } from "./session.js";
import { WIRED_AUTH_SEMANTICS } from "./wired-auth.js";

export const WalkItemSchema = z
  .object({
    id: z.string(),
    verdict: z.enum(["pass", "fail", "blocked"]),
    observed: z.string(),
    steps_taken: z.string(),
    screenshots: z.array(z.string()),
    console_errors: z.array(z.string()),
    failed_requests: z.array(z.string()),
  })
  .strict();
export type WalkItem = z.infer<typeof WalkItemSchema>;

export const WalkResultSchema = z
  .object({
    items: z.array(WalkItemSchema),
    notes: z.string(),
  })
  .strict();
export type WalkResult = z.infer<typeof WalkResultSchema>;

export type GuardDecision = { allow: true } | { allow: false; reason: string };

/**
 * Split a command line into words, honouring single and double quotes — enough
 * to find the verb and the flags of one `agent-browser` call. Shell
 * metacharacters never reach here: `guardTool` refuses them first.
 */
export function shellWords(command: string): string[] {
  const words: string[] = [];
  let current = "";
  let quote: '"' | "'" | null = null;
  let inWord = false;
  for (const ch of command) {
    if (quote) {
      if (ch === quote) quote = null;
      else current += ch;
      continue;
    }
    if (ch === '"' || ch === "'") {
      quote = ch;
      inWord = true;
      continue;
    }
    if (/\s/.test(ch)) {
      if (inWord) words.push(current);
      current = "";
      inWord = false;
      continue;
    }
    current += ch;
    inWord = true;
  }
  if (inWord) words.push(current);
  return words;
}

/**
 * The walker's whole permission model, as a pure function of one tool call.
 *
 *   Bash   — exactly one `agent-browser` invocation: no shell metacharacters,
 *            no forbidden verb, subcommand or flag (`WALKER` in config.ts).
 *            A `batch` runs each of its arguments as a command of its own, so
 *            each one is held to the same rules.
 *   Read / Write — a path inside `walkDir`, and nowhere else.
 *   anything else — not this guard's to decide; the session's `tools` list
 *            already makes no other built-in exist.
 */
export function guardTool(tool: string, input: unknown, walkDir: string): GuardDecision {
  const args = (input ?? {}) as Record<string, unknown>;
  if (tool === "Bash") {
    const command = typeof args.command === "string" ? args.command.trim() : "";
    if (!command.startsWith("agent-browser ")) return deny("only `agent-browser …` commands may run");
    const meta = WALKER.forbiddenShell.find((token) => command.includes(token));
    if (meta) return deny(`shell metacharacter ${JSON.stringify(meta)} — run one agent-browser command per call`);
    return guardBrowserArgs(shellWords(command).slice(1));
  }
  if (tool === "Read" || tool === "Write") {
    const path = typeof args.file_path === "string" ? args.file_path : "";
    if (!path) return deny("a file_path is required");
    const rel = relative(walkDir, resolve(walkDir, path));
    if (rel === "" || rel.startsWith("..") || isAbsolute(rel)) return deny(`only files under ${walkDir} may be read or written`);
    return { allow: true };
  }
  return { allow: true };
}

/**
 * The arguments of one `agent-browser` command. A `batch` runs every positional
 * argument as a command line of its own, so each is split and held to these
 * same rules — a nested `batch` included.
 */
function guardBrowserArgs(words: string[]): GuardDecision {
  const flag = words.find((word) => WALKER.forbiddenFlags.some((f) => word === f || word.startsWith(`${f}=`)));
  if (flag) return deny(`${flag} is not available in this walk`);
  const positional = words.filter((word) => !word.startsWith("-"));
  const verb = positional[0] ?? "";
  if ((WALKER.forbiddenVerbs as readonly string[]).includes(verb)) return deny(`agent-browser ${verb} is not available in this walk`);
  const sub = positional[1] ?? "";
  if (WALKER.forbiddenSubcommands[verb]?.includes(sub)) return deny(`agent-browser ${verb} ${sub} is not available in this walk`);
  if (verb === "batch") {
    for (const inner of positional.slice(1)) {
      const decision = guardBrowserArgs(shellWords(inner));
      if (!decision.allow) return decision;
    }
    // Without --bail a batch continues after a failed command, and later keys land wherever focus is.
    if (!words.includes("--bail")) return deny("use `agent-browser batch --bail …`: a command after a failed one acts on the wrong element");
  }
  return { allow: true };
}

function deny(reason: string): GuardDecision {
  return { allow: false, reason };
}

/**
 * One browser command at a time: overlapping commands race on the one page, and
 * the session runs parallel tool calls at once.
 */
export class BrowserLane {
  private running: string | undefined;

  /** A `Bash` call that the guard allowed is about to run. */
  enter(toolUseId: string): GuardDecision {
    if (this.running !== undefined && this.running !== toolUseId) {
      return deny(
        "another agent-browser command is still running. Send one command, read its result, then send the next; " +
          "for a fixed sequence, send one `agent-browser batch`",
      );
    }
    this.running = toolUseId;
    return { allow: true };
  }

  /** That call finished, or failed. */
  leave(toolUseId: string): void {
    if (this.running === toolUseId) this.running = undefined;
  }
}

/** The guard and the browser lane as the SDK hooks that enforce them. */
export function guardHooks(walkDir: string): Partial<Record<"PreToolUse" | "PostToolUse" | "PostToolUseFailure", HookCallbackMatcher[]>> {
  const lane = new BrowserLane();
  const release: HookCallbackMatcher = {
    matcher: "Bash",
    hooks: [
      async (hookInput) => {
        if (hookInput.hook_event_name === "PostToolUse" || hookInput.hook_event_name === "PostToolUseFailure") {
          lane.leave(hookInput.tool_use_id);
        }
        return {};
      },
    ],
  };
  return {
    PreToolUse: [
      {
        matcher: "Bash|Read|Write",
        hooks: [
          async (hookInput) => {
            if (hookInput.hook_event_name !== "PreToolUse") return {};
            let decision = guardTool(hookInput.tool_name, hookInput.tool_input, walkDir);
            if (decision.allow && hookInput.tool_name === "Bash") decision = lane.enter(hookInput.tool_use_id);
            if (decision.allow) return {};
            return {
              hookSpecificOutput: {
                hookEventName: "PreToolUse" as const,
                permissionDecision: "deny" as const,
                permissionDecisionReason: decision.reason,
              },
            };
          },
        ],
      },
    ],
    PostToolUse: [release],
    PostToolUseFailure: [release],
  };
}

/**
 * Stops the walk when `limit` consecutive Bash results carry `timedOutAfterMs`
 * with none completing between. Pure.
 */
export class BrowserWatchdog {
  private readonly bashCalls = new Set<string>();
  private streak = 0;

  constructor(private readonly limit: number) {}

  /** Feed every message of the walk; returns why it must stop, once. */
  observe(message: SDKMessage): string | undefined {
    if (message.type === "assistant") {
      for (const block of message.message.content) {
        if (block.type === "tool_use" && block.name === "Bash") this.bashCalls.add(block.id);
      }
      return undefined;
    }
    if (message.type !== "user" || typeof message.message.content === "string") return undefined;
    const bash = message.message.content.find((block) => block.type === "tool_result" && this.bashCalls.has(block.tool_use_id));
    if (!bash || bash.type !== "tool_result") return undefined;
    const timedOutAfterMs = (message.tool_use_result as { timedOutAfterMs?: unknown } | undefined)?.timedOutAfterMs;
    if (typeof timedOutAfterMs !== "number") {
      // Only a command that actually ran and finished proves the browser answered.
      if (bash.is_error !== true) this.streak = 0;
      return undefined;
    }
    this.streak += 1;
    if (this.streak !== this.limit) return undefined;
    return (
      `browser unresponsive: ${String(this.limit)} agent-browser commands in a row ran to their timeout ` +
      `(the last after ${String(Math.round(timedOutAfterMs / 1000))}s) — the walk was stopped rather than scored`
    );
  }
}

const SYSTEM_PROMPT = `You test web applications the way a careful user would, in a real browser, and report exactly what you observed.
You never fix, work around or excuse what you find. You answer with one JSON object in the requested schema.`;

/**
 * The wall-clock time as the browser reads it: `2026-10-04 20:21 (Asia/Colombo)`.
 * Chrome runs on this machine, so its time zone is the browser's.
 */
export function browserClock(now: Date, timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone): string {
  const parts = Object.fromEntries(
    new Intl.DateTimeFormat("en-GB", { timeZone, year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hourCycle: "h23" })
      .formatToParts(now)
      .map((part) => [part.type, part.value]),
  ) as Record<string, string>;
  return `${parts.year ?? ""}-${parts.month ?? ""}-${parts.day ?? ""} ${parts.hour ?? ""}:${parts.minute ?? ""} (${timeZone})`;
}

export function walkerPrompt(opts: { baseUrl: string; roles: string[]; items: Item[]; mustNot: MustNot[]; now?: Date }): string {
  const url = opts.baseUrl.replace(/\/$/, "");
  const checklist = opts.items
    .map(
      (item, index) =>
        `${String(index + 1)}. id: ${item.id}\n   role: ${item.role}\n   screen: ${item.screen}\n   steps: ${item.steps}\n   expect: ${item.expect}`,
    )
    .join("\n");
  const mustNot = opts.mustNot.length
    ? `\nALSO WATCH, THROUGHOUT — things that must never happen. If you see one, describe it in \`notes\` with its id:\n${opts.mustNot.map((m) => `- ${m.id}: ${m.description}`).join("\n")}\n`
    : "";
  return `You are testing a running web application against a checklist. You have not seen its source and you will not: the browser is the only way you learn anything.

THE BROWSER is the \`agent-browser\` CLI, through Bash, ONE command per call — pipes, \`;\`, \`&&\`, redirects and substitutions are refused. Send one call at a time and read its result before the next: a command sent while another runs is refused. Send a fixed sequence (click a field, then press its keys) as one \`agent-browser batch --bail "click @e4" "press 2" …\`, with refs from a snapshot taken after the page last changed. Your session is already isolated; never pass --session. \`agent-browser skills get core\` prints the full reference. Two readings of a page, and they are not interchangeable:
  agent-browser snapshot                   what the page SHOWS (text, rows, badges) — judge from this
  agent-browser snapshot -i                the controls only, with @refs to act on — it hides text and rows
\`snapshot -c\` drops plain text, such as a hint under a form field: never judge from it.
Screenshots go to shots/<id>.png. You may Read and Write files in your working directory (screenshots included) and nowhere else.

${confirmEachAction()}

THE APP is at ${url}/
- Enter as a role by loading ${url}/?role=<Role>. "no role" is ${url}/?role= (signed in, holding no role); "signed out" is ${url}/?auth=out.
- Roles: ${opts.roles.join(", ")}.
- The backend is real and its database started EMPTY. What you create persists, across role switches too.
- When the walk started, the browser's clock read ${browserClock(opts.now ?? new Date())}. Use it for any date or time that an item asks for relative to now.

${WIRED_AUTH_SEMANTICS}

THE METHOD, for each item in order:
1. Reach — enter as the item's role (unless you already are), then get to the item's screen the way a user would: the app's own navigation, from where the role lands. Load a URL directly only to switch role or when the item's steps say to. Note the address of every screen you reach (\`agent-browser get url\`): a later item may ask you to open it under another role.
2. Act — do the steps, with the values given.
3. Request — a change counts only when a request leaves the page. After a create, edit, delete, approve or similar, check \`agent-browser network requests\` for it and its status. A row that changes on screen with no request behind it, or a request that failed, is a FAIL.
4. Judge the \`expect\` against what you SEE — a full \`snapshot\` (or the screenshot), never \`snapshot -i\`, which omits everything that is not a control. Before you fail an item for text that is not there, look at the screenshot too. pass: it holds. fail: it does not — say what you saw instead. blocked: an earlier failure made this item impossible to attempt — name that item.
5. Screenshot at the moment you judge: \`agent-browser screenshot shots/<id>.png\`.
6. On a failure, read \`agent-browser console\`, \`agent-browser errors\` and the failed entries of \`agent-browser network requests\`; record what they show in console_errors and failed_requests (short, verbatim where possible).
At most three tries on one item, then record it and move on. Walk every item; do not stop early.

NEVER fix or work around the app: do not hunt for a URL to reach a screen its navigation does not take you to, do not retry until something flaky passes once, do not invent data the UI cannot create. You report what a user would experience.
${mustNot}
CHECKLIST
${checklist}

ANSWER: \`items\` — one entry per checklist id, in checklist order: verdict, observed (what the page did, concretely), steps_taken (what you actually did), screenshots (paths you saved), console_errors, failed_requests. \`notes\` — anything a developer should know that the items do not carry: crashes, a pattern across items, must-not sightings.`;
}

/**
 * Fill in what the walker did not report: an item it never reached is
 * `blocked` / "not reached", so the judge sees every item and the score cannot
 * be raised by the walker running out of time. Entries for ids not on the
 * checklist are dropped. Pure.
 */
export function normalizeWalk(items: Item[], walk: WalkResult): WalkResult {
  const byId = new Map(walk.items.map((entry) => [entry.id, entry]));
  return {
    items: items.map(
      (item) =>
        byId.get(item.id) ?? {
          id: item.id,
          verdict: "blocked" as const,
          observed: "not reached",
          steps_taken: "",
          screenshots: [],
          console_errors: [],
          failed_requests: [],
        },
    ),
    notes: walk.notes,
  };
}

export interface WalkRequest {
  baseUrl: string;
  roles: string[];
  items: Item[];
  mustNot: MustNot[];
  /** `<attempt>/walk/` — the session's cwd and the only place it may touch. */
  walkDir: string;
  /** Unique per attempt: the agent-browser session name. */
  sessionName: string;
  env: NodeJS.ProcessEnv;
  signal?: AbortSignal;
}

/**
 * The `## Confirm each action` section of the platform's agent-browser skill,
 * heading included, up to the next `## ` heading. Throws when the heading is
 * gone: a walker prompt that silently lost it would walk with no read-back rule.
 */
export function confirmEachAction(): string {
  const { file, heading } = WALKER.confirmSection;
  const lines = readFileSync(file, "utf8").split("\n");
  const start = lines.indexOf(heading);
  if (start < 0) throw new Error(`${file} has no "${heading}" section — the walker prompt embeds it`);
  const end = lines.findIndex((line, index) => index > start && line.startsWith("## "));
  return lines
    .slice(start, end < 0 ? undefined : end)
    .join("\n")
    .trim();
}

/**
 * Why a walk cannot start, or undefined — checked before a sweep or a rewalk
 * spends anything: the pinned CLI must be installed, and the prompt's skill
 * section must exist.
 */
export function walkerProblem(): string | undefined {
  const missing = agentBrowserProblem(WALKER.binDir);
  if (missing) return missing;
  try {
    confirmEachAction();
    return undefined;
  } catch (err) {
    return err instanceof Error ? err.message : String(err);
  }
}

/** `agent-browser --version` as a walk resolves it, for `provenance.json`; null when it does not answer. */
export function walkerAgentBrowserVersion(): string | null {
  const result = spawnSync("agent-browser", ["--version"], {
    encoding: "utf8",
    timeout: 30_000,
    env: withAgentBrowserFirst(process.env, WALKER.binDir),
  });
  if (result.error || result.status !== 0) return null;
  return result.stdout.trim() || null;
}

export async function walk(req: WalkRequest): Promise<SessionResult<WalkResult>> {
  const env: NodeJS.ProcessEnv = {
    ...withAgentBrowserFirst(req.env, WALKER.binDir),
    AGENT_BROWSER_SESSION: req.sessionName,
    AGENT_BROWSER_ALLOWED_DOMAINS: WALKER.allowedDomains,
  };
  const watchdog = new BrowserWatchdog(WALKER.unresponsiveAfter);
  try {
    return await runSession({
      prompt: walkerPrompt(req),
      systemPrompt: SYSTEM_PROMPT,
      cwd: req.walkDir,
      model: MODELS.walker,
      tools: WALKER.tools,
      hooks: guardHooks(req.walkDir),
      schema: WalkResultSchema,
      maxTurns: WALKER.maxTurns,
      timeoutMs: TIMEOUTS.walkMinutes * 60_000,
      env,
      watch: (message) => watchdog.observe(message),
      transcriptFile: resolve(req.walkDir, "transcript.jsonl"),
      debugFile: resolve(req.walkDir, "claude-debug.log"),
      ...(req.signal ? { signal: req.signal } : {}),
    });
  } finally {
    await closeBrowser(req.sessionName);
  }
}

/** Close THIS attempt's browser session — never `--all`, which would close everyone's. */
export function closeBrowser(sessionName: string): Promise<void> {
  return new Promise((done) => {
    execFile(
      "agent-browser",
      ["close"],
      { timeout: 30_000, env: { ...withAgentBrowserFirst(process.env, WALKER.binDir), AGENT_BROWSER_SESSION: sessionName } },
      () => done(),
    );
  });
}
