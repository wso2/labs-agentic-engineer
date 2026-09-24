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

// The OPENCODE adapter: `RuntimePolicy` in, a running OpenCode session out.
// Which OpenCode mechanism enforces each policy clause is ADR-0015's table.
//
// Invariants that are not policy fields (conditions of running at all):
//   - `OPENCODE_DISABLE_PROJECT_CONFIG` — the checkout's own config never
//     reaches a run (the equivalent of Claude Code's `strictMcpConfig`);
//   - `OPENCODE_DISABLE_AUTOUPDATE` / `_MODELS_FETCH` / `_LSP_DOWNLOAD` — a pod
//     starts offline from the image's pre-warmed home;
//   - never set: the experimental background flag (asserted absent,
//     startup.ts) and `OPENCODE_DISABLE_CLAUDE_CODE_SKILLS` (it turns off the
//     `.claude/skills/` discovery the mirror depends on).

import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createOpencodeClient, type OpencodeClient } from "@opencode-ai/sdk/client";
import type { AccessTokenSource } from "../../lib/auth_retry.js";
import { startMcpAuthProxy } from "../../lib/mcp_auth_proxy.js";
import { scrubber } from "../../lib/progress/scrubber.js";
import { SESSION_CONTEXT_FILE } from "../../lib/run_context.js";
import { toolGlossary } from "../../lib/tool_glossary.js";
import { withTimeout } from "../../lib/with_timeout.js";
import { stagedSecretValues } from "../../lib/websearch_dlp.js";
import type { Runtime, RuntimeArtifact, RuntimePolicy, RuntimeSession } from "../port.js";
import { createOpencodeClassifier } from "./classify.js";
import { buildOpencodeConfig } from "./config.js";
import { pumpEvents } from "./event_queue.js";
import { obj, str } from "../fields.js";
import { GUARD_ENV, STARTUP_PROBE_PROMPT } from "./plugin/protocol.js";
import { freePort, startServer, type RunningServer } from "./server.js";
import { createStreamCloser, sessionStream } from "./settle.js";
import { OpencodeStartupError, startupProblems, type PermissionRuleRecord } from "./startup.js";
import { PRIMARY_AGENT, PROVIDER_ID } from "./tools.js";
import { createOpencodeAdapter } from "./translate.js";

/** The model an OpenCode run bills to when the org has not chosen: the platform's priced default. */
export const OPENCODE_DEFAULT_MODEL = "claude-sonnet-5";

/**
 * Where the image ships the guard plugin (Dockerfile, runner-opencode stage):
 * a package directory built from `plugin/` by `plugin/build.ts`.
 */
export const OPENCODE_GUARD_DIR = "/app/runtime/opencode/aep-guard";

/** How long the server gets to start, the plugin to announce itself, and the bus to connect. */
const START_TIMEOUT_MS = 60_000;
const GUARD_READY_TIMEOUT_MS = 15_000;
const CONNECT_TIMEOUT_MS = 15_000;
const PROBE_TIMEOUT_MS = 15_000;

/** The adapter's clock — see `aep.tick` in messages.ts. */
const TICK_MS = 10_000;

/** A bus connection that drops is retried this many times before the stream ends (and the run settles). */
const SSE_MAX_RETRY_ATTEMPTS = 5;

export interface OpencodeRuntimeOptions {
  /** The guard plugin's package directory; the image's by default. */
  pluginDir?: string;
  /** The `opencode` binary; resolved on the run's PATH by default. */
  command?: string;
  /** The adapter's clock interval; tests shorten it. */
  tickMs?: number;
}

/** The runtime's private files for one run — never inside the workspace. */
interface RunFiles {
  dir: string;
  instructions: string;
  secrets: string;
  ready: string;
  probe: string;
}

/**
 * The server's environment: the policy's, plus the invariant flags (header) and
 * the guard's inputs. The one place those flags are set, for every run path.
 * A leaked background flag is passed through, not removed: `startup.ts` fails
 * the run on it, where removing it would hide the leak.
 */
export function childEnvironment(
  policy: Pick<RuntimePolicy, "workspace" | "env" | "logDir">,
  files: Pick<RunFiles, "secrets" | "ready" | "instructions" | "probe">,
): Record<string, string> {
  return {
    ...policy.env,
    OPENCODE_DISABLE_PROJECT_CONFIG: "1",
    OPENCODE_DISABLE_AUTOUPDATE: "1",
    OPENCODE_DISABLE_MODELS_FETCH: "1",
    OPENCODE_DISABLE_LSP_DOWNLOAD: "1",
    [GUARD_ENV.workspace]: policy.workspace,
    [GUARD_ENV.secretsFile]: files.secrets,
    [GUARD_ENV.readyFile]: files.ready,
    [GUARD_ENV.appendixFile]: files.instructions,
    [GUARD_ENV.probeFile]: files.probe,
    [GUARD_ENV.sessionLog]: sessionContextPath(policy),
  };
}

function sessionContextPath(policy: Pick<RuntimePolicy, "logDir">): string {
  return path.join(policy.logDir, SESSION_CONTEXT_FILE);
}

/**
 * The run's private files, in a fresh 0700 temp directory removed at `close()`.
 * Not `policy.logDir`: that is inside the clone, and the secrets file holds
 * staged secret VALUES.
 */
function writeRunFiles(policy: RuntimePolicy): RunFiles {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "aep-opencode-"));
  const files: RunFiles = {
    dir,
    instructions: path.join(dir, "instructions.md"),
    secrets: path.join(dir, "guard-secrets.json"),
    ready: path.join(dir, "aep-guard.ready"),
    probe: path.join(dir, "aep-guard.probe"),
  };
  // The whole appendix, glossary last; the plugin reads it too, to keep it to the lead.
  fs.writeFileSync(files.instructions, policy.skills.preloadBodies, { mode: 0o600 });
  // The values the runner's egress predicates were built from: a closure cannot
  // cross into the plugin's process, and the config is an env var any child reads.
  fs.writeFileSync(files.secrets, JSON.stringify(stagedSecretValues(policy.env)), { mode: 0o600 });
  return files;
}

async function waitForFile(file: string, timeoutMs: number): Promise<boolean> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (fs.existsSync(file)) return true;
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  return fs.existsSync(file);
}

/** The token source the loopback proxy drives, built from the port's two members. */
function tokenSource(mcp: NonNullable<RuntimePolicy["mcp"]>): AccessTokenSource {
  return { getToken: () => mcp.token(), invalidate: () => mcp.invalidate?.() };
}

/**
 * Whether the plugin's system transform fires: one prompt on a throwaway
 * session, which the plugin marks at the transform and refuses before the model
 * call (plugin/startup_probe.ts). The synchronous prompt returns once that
 * refusal has ended the turn; the session is then deleted, so none of it can
 * reach the run's event stream.
 */
async function probeSystemTransform(client: OpencodeClient, policy: RuntimePolicy, files: RunFiles): Promise<boolean> {
  const created = await client.session.create({ body: { title: "aep startup probe" } });
  const id = created.data?.id ?? "";
  try {
    await withTimeout(
      client.session
        .prompt({
          path: { id },
          body: {
            agent: PRIMARY_AGENT,
            model: { providerID: PROVIDER_ID, modelID: policy.model },
            parts: [{ type: "text", text: STARTUP_PROBE_PROMPT }],
          },
        })
        .catch(() => undefined),
      PROBE_TIMEOUT_MS,
      "the startup probe",
    ).catch(() => undefined);
    return fs.existsSync(files.probe);
  } finally {
    await client.session.delete({ path: { id } }).catch(() => undefined);
  }
}

/**
 * The four start-time assertions, asked of the running server. The prompt has
 * not been sent, so a failure here costs no model call.
 */
async function assertStartup(client: OpencodeClient, policy: RuntimePolicy, files: RunFiles): Promise<void> {
  // A project request boots the instance, which is what loads plugins.
  await client.config.get();
  const guardReady = await waitForFile(files.ready, GUARD_READY_TIMEOUT_MS);
  const tools = await client.tool.list({ query: { provider: PROVIDER_ID, model: policy.model } });
  const agents = await client.app.agents();
  const aep = (agents.data ?? []).find((a) => a.name === PRIMARY_AGENT) as { permission?: unknown } | undefined;
  const rules = Array.isArray(aep?.permission) ? (aep.permission as PermissionRuleRecord[]) : undefined;
  const task = (tools.data ?? []).find((t) => t.id === "task");
  const systemTransformLive = guardReady ? await probeSystemTransform(client, policy, files) : false;
  const problems = startupProblems({
    guardReady,
    systemTransformLive,
    toolIds: (tools.data ?? []).map((t) => t.id),
    agentRules: aep ? (rules ?? []) : undefined,
    taskParameters: Object.keys(obj(obj(task?.parameters).properties)),
  });
  if (problems.length > 0) throw new OpencodeStartupError(problems);
}

/**
 * The skills the server discovered, for the loop's preload check. `GET /skill`
 * is not in the v1 client, so it is asked directly; a failure is an empty list,
 * which the check reports as every requested skill missing — loud, not silent.
 */
async function discoveredSkills(baseUrl: string, workspace: string): Promise<string[]> {
  try {
    const res = await fetch(`${baseUrl}/skill?directory=${encodeURIComponent(workspace)}`);
    if (!res.ok) return [];
    const body: unknown = await res.json();
    return Array.isArray(body) ? body.map((s) => str(obj(s).name)).filter(Boolean) : [];
  } catch {
    return [];
  }
}

export function createOpencodeRuntime(opts: OpencodeRuntimeOptions = {}): Runtime {
  return {
    name: "opencode",
    defaultModel: OPENCODE_DEFAULT_MODEL,
    toolGlossary: () => toolGlossary("opencode"),
    start: (prompt, policy) => startOpencodeSession(prompt, policy, opts),
  };
}

/**
 * A server that is up, configured and PROVEN — the start-time assertions have
 * passed — and has not been prompted. Everything a session needs from the
 * server's boot, and the teardown that undoes it.
 */
export interface BootedServer {
  client: OpencodeClient;
  url: string;
  /** The skills the server discovered, for the loop's preload check. */
  skills: string[];
  /** The config it was started with (no credential in it). */
  config: Record<string, unknown>;
  artifacts: RuntimeArtifact[];
  /** Undo everything, in reverse: server, proxy, sinks, the run's private files. */
  close(): Promise<void>;
}

/**
 * Start the server for a policy and prove it is fit to run, WITHOUT sending a
 * prompt — so a failure costs no model call, and so the whole boot can be
 * checked model-free (the image's verification drives exactly this).
 */
export async function bootOpencode(policy: RuntimePolicy, opts: OpencodeRuntimeOptions = {}): Promise<BootedServer> {
  // An API key is the only credential OpenCode runs on; refused before a spawn.
  if (!policy.env.ANTHROPIC_API_KEY) {
    throw new OpencodeStartupError([
      "OpenCode authenticates with ANTHROPIC_API_KEY and this run has none — an OAuth coding token cannot run OpenCode",
    ]);
  }

  const cleanups: (() => Promise<void> | void)[] = [];
  const close = async (): Promise<void> => {
    for (const fn of cleanups.reverse()) {
      try {
        await fn();
      } catch {
        // best effort: a failed cleanup must not mask the run's own outcome
      }
    }
    cleanups.length = 0;
  };

  try {
    const files = writeRunFiles(policy);
    cleanups.push(() => fs.rmSync(files.dir, { recursive: true, force: true }));

    let mcpUrl: string | undefined;
    if (policy.mcp) {
      const proxy = await startMcpAuthProxy({
        upstreamUrl: policy.mcp.url,
        source: tokenSource(policy.mcp),
        canRefresh: policy.mcp.invalidate !== undefined,
        onToken: policy.mcp.onToken,
        onFatal: (err) => policy.mcp?.onFatal?.(err),
      });
      cleanups.push(() => proxy.close());
      mcpUrl = proxy.url;
    }

    const config = buildOpencodeConfig({
      model: policy.model,
      instructionsPath: files.instructions,
      pluginDir: opts.pluginDir ?? OPENCODE_GUARD_DIR,
      skillAllow: policy.skills.allow,
      deniedCapabilities: policy.deniedCapabilities,
      ...(mcpUrl ? { mcpUrl } : {}),
      debug: policy.debug,
    });

    // Beside `runtime.log`: the session record on every run; the server's log
    // and its config (no credential in it) only under debug.
    const artifacts: RuntimeArtifact[] = [{ path: sessionContextPath(policy), kind: "log" }];
    let stderrSink: fs.WriteStream | undefined;
    if (policy.debug) {
      fs.mkdirSync(policy.logDir, { recursive: true });
      const configPath = path.join(policy.logDir, "opencode.config.json");
      fs.writeFileSync(configPath, JSON.stringify(config, null, 2) + "\n");
      const stderrPath = path.join(policy.logDir, "opencode.stderr");
      stderrSink = fs.createWriteStream(stderrPath, { flags: "w" });
      const sink = stderrSink;
      cleanups.push(() => new Promise<void>((resolve) => sink.end(resolve)));
      artifacts.push({ path: stderrPath, kind: "log" }, { path: configPath, kind: "log" });
    }

    const server: RunningServer = await startServer({
      command: opts.command ?? "opencode",
      hostname: "127.0.0.1",
      port: await freePort(),
      config,
      env: childEnvironment(policy, files),
      cwd: policy.workspace,
      debug: policy.debug,
      ...(stderrSink ? { onStderr: (chunk: string) => stderrSink?.write(scrubber.scrub(chunk)) } : {}),
      timeoutMs: START_TIMEOUT_MS,
    });
    cleanups.push(() => server.close());

    const client = createOpencodeClient({ baseUrl: server.url, throwOnError: true, directory: policy.workspace });
    await assertStartup(client, policy, files);
    const skills = await discoveredSkills(server.url, policy.workspace);
    return { client, url: server.url, skills, config, artifacts, close };
  } catch (err) {
    await close();
    throw err;
  }
}

async function startOpencodeSession(
  prompt: string,
  policy: RuntimePolicy,
  opts: OpencodeRuntimeOptions,
): Promise<RuntimeSession> {
  const booted = await bootOpencode(policy, opts);
  const { client, skills } = booted;
  const extra: (() => void)[] = [];
  const teardown = async (): Promise<void> => {
    for (const fn of extra) fn();
    await booted.close();
  };

  try {
    // Subscribe before the session exists: its `session.created` must be on this stream.
    const abort = new AbortController();
    extra.push(() => abort.abort());
    const subscription = await client.event.subscribe({ signal: abort.signal, sseMaxRetryAttempts: SSE_MAX_RETRY_ATTEMPTS });
    const queue = pumpEvents(subscription.stream, { tickMs: opts.tickMs ?? TICK_MS });
    extra.push(() => queue.stop());
    await withTimeout(queue.connected, CONNECT_TIMEOUT_MS, "the event stream connecting");

    const created = await client.session.create({ body: { title: `AEP ${policy.taskKind} run` } });
    const rootId = created.data?.id ?? "";

    const adapter = createOpencodeAdapter({
      model: policy.model,
      taskKind: policy.taskKind,
      ...(policy.write.onDenied ? { onWorkspaceDenied: policy.write.onDenied } : {}),
    });
    const closer = createStreamCloser();

    await client.session.promptAsync({
      path: { id: rootId },
      body: {
        agent: PRIMARY_AGENT,
        model: { providerID: PROVIDER_ID, modelID: policy.model },
        parts: [{ type: "text", text: prompt }],
      },
    });

    // Every permission ask is rejected: an unanswered ask in server mode is a
    // hang. The notice is the classifier's (stall_signal).
    const reject = (props: Record<string, unknown>): void => {
      const id = str(props.id);
      const sessionID = str(props.sessionID);
      if (!id || !sessionID) return;
      client
        .postSessionIdPermissionsPermissionId({ path: { id: sessionID, permissionID: id }, body: { response: "reject" } })
        .catch((err: unknown) => {
          console.warn(`[opencode] could not reject permission ${id}: ${err instanceof Error ? err.message : String(err)}`);
        });
    };

    return {
      stream: {
        messages: sessionStream(queue.messages, closer, { skills, onPermissionAsked: reject }),
        // A child session id, from the classifier's task bookkeeping.
        stopTask: async (taskId) => {
          adapter.markStopped(taskId);
          await client.session.abort({ path: { id: taskId } });
        },
        // No held-open input: the prompt went out with `promptAsync`, and the
        // stream's end is decided by the close rule (settle.ts), not by input.
      },
      translate: adapter.translate,
      classify: createOpencodeClassifier(),
      usage: adapter.usage,
      artifacts: async () => [...booted.artifacts],
      close: async () => {
        // Anything still running is stopped before the server goes, so no
        // tool outlives the run that started it.
        const busy = new Set([...closer.busySessions(), rootId].filter(Boolean));
        for (const id of busy) adapter.markStopped(id);
        await Promise.all([...busy].map((id) => client.session.abort({ path: { id } }).catch(() => undefined)));
        await teardown();
      },
    };
  } catch (err) {
    await teardown();
    throw err;
  }
}
