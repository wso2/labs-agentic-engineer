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

import { spawn, type ChildProcess } from "node:child_process";
import { createServer } from "node:net";
import { join } from "node:path";

export interface BootedAgent {
  /** Base address the agent answers on, e.g. `http://127.0.0.1:53412`. */
  url: string;
  /** Terminates the child. Idempotent; safe after a failed boot. */
  close: () => Promise<void>;
}

export interface BootOptions {
  /** The component's App Path — the folder holding its built entry point. */
  appDir: string;
  /**
   * The child's WHOLE environment, minus the port. Nothing is inherited: an
   * agent under evaluation must see exactly what the harness decided to give
   * it, so a variable that happens to be set on the build machine cannot
   * make a scenario pass here and fail in the cluster.
   */
  env: Record<string, string>;
  /** Relative to `appDir`; matches the component contract's Docker CMD. */
  entry?: string;
  readyTimeoutMs?: number;
  pollIntervalMs?: number;
}

const DEFAULT_ENTRY = join("dist", "main.js");
const DEFAULT_READY_TIMEOUT_MS = 60_000;
const DEFAULT_POLL_INTERVAL_MS = 250;
/** Enough of the child's output to explain a failure, not enough to flood a report. */
const OUTPUT_TAIL_LIMIT = 4_000;

interface Health {
  ok?: boolean;
  missing?: string[];
  store?: string;
}

/**
 * Boots the component under test and waits until it says it is ready.
 *
 * A deliberate, documented deviation from the spec's word "in-process": the
 * generated agent calls `listen()` at module load and owns its own process
 * lifecycle, so importing it into the harness's process would fight that
 * design — and would let a crash in the agent take the evaluation down with
 * it. A locally spawned child on an ephemeral port satisfies what the spec is
 * actually choosing — evaluate DURING the build rather than after deploy,
 * where a retry costs seconds — while keeping the agent's real HTTP contract,
 * which is the thing under test.
 *
 * The port is ephemeral rather than the contract's 9090 because a fix loop
 * boots the agent again and again: a fixed port makes the second boot fail on
 * an address still held by the first.
 *
 * Readiness is the agent's OWN `/healthz` verdict, and the wait is bounded.
 * An agent that never becomes ready fails here, quoting `missing` and
 * `store` — a scenario that failed because the agent never came up must never
 * be reported as an agent that behaved badly, and a boot that waits forever
 * turns a misconfigured agent into a stuck build.
 */
export async function bootAgent(opts: BootOptions): Promise<BootedAgent> {
  const port = await reservePort();
  const url = `http://127.0.0.1:${port}`;
  const entry = opts.entry ?? DEFAULT_ENTRY;

  const child = spawn(process.execPath, [entry], {
    cwd: opts.appDir,
    env: { ...opts.env, PORT: String(port) },
    stdio: ["ignore", "pipe", "pipe"],
  });

  let output = "";
  const collect = (chunk: Buffer): void => {
    output = (output + chunk.toString()).slice(-OUTPUT_TAIL_LIMIT);
  };
  child.stdout?.on("data", collect);
  child.stderr?.on("data", collect);

  let exit: string | undefined;
  const exited = new Promise<void>((resolve) => {
    child.on("exit", (code, signal) => {
      exit = signal !== null ? `signal ${signal}` : `exit ${String(code)}`;
      resolve();
    });
    child.on("error", (err) => {
      exit = err.message;
      resolve();
    });
  });

  const close = closer(child, exited);
  const fail = async (reason: string): Promise<never> => {
    await close();
    throw new Error(
      `agent-eval: the agent at ${opts.appDir} never became ready on ${url} — ${reason}` +
        (output === "" ? "" : `\n--- agent output ---\n${output}`),
    );
  };

  const deadline = Date.now() + (opts.readyTimeoutMs ?? DEFAULT_READY_TIMEOUT_MS);
  let lastHealth = "no answer from /healthz yet";
  for (;;) {
    // Checked before the probe as well as after it: a child that died on
    // startup will never answer, and sitting out the whole bound waiting for
    // a corpse hides the real error (a missing build, a bad entry point).
    if (exit !== undefined) {
      return await fail(`the process ended (${exit}) before it was ready`);
    }
    const health = await probe(url);
    if (health?.ok === true) return { url, close };
    if (health !== undefined) lastHealth = describe(health);
    if (Date.now() >= deadline) return await fail(lastHealth);
    await delay(opts.pollIntervalMs ?? DEFAULT_POLL_INTERVAL_MS);
  }
}

function describe(health: Health): string {
  const missing = health.missing ?? [];
  const parts = [
    missing.length > 0
      ? `/healthz reports unset environment variables: ${missing.join(", ")}`
      : "/healthz reports its environment is complete",
  ];
  if (health.store !== undefined && health.store !== "ready") {
    parts.push(`the conversation store is "${health.store}"`);
  }
  return parts.join("; ");
}

/**
 * One `close` for every exit path, including the failed ones. It resolves
 * only once the child is actually gone: returning earlier would let the next
 * scenario's boot race a process that still holds a port and a credential.
 */
function closer(child: ChildProcess, exited: Promise<void>): () => Promise<void> {
  let closing: Promise<void> | undefined;
  return () => {
    closing ??= (async () => {
      if (child.exitCode === null && child.signalCode === null) child.kill("SIGTERM");
      // An agent that ignores SIGTERM must still not outlive the run.
      const hardStop = setTimeout(() => child.kill("SIGKILL"), 2_000);
      try {
        await exited;
      } finally {
        clearTimeout(hardStop);
      }
    })();
    return closing;
  };
}

async function probe(url: string): Promise<Health | undefined> {
  try {
    const res = await fetch(new URL("/healthz", url));
    const body = (await res.json()) as Health;
    return { ...body, ok: res.status === 200 && body.ok !== false };
  } catch {
    // Not listening yet, or answering something that is not JSON. Both are
    // ordinary during startup; the bound is what decides they are fatal.
    return undefined;
  }
}

/**
 * Asks the OS for a free port and immediately gives it back. There is a
 * window between the release and the child's own `listen`, which is why the
 * boot reports what it saw rather than assuming the port is ours — but it is
 * far smaller a risk than a fixed port a previous round still holds.
 */
function reservePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const server = createServer();
    server.on("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      const port = typeof address === "object" && address ? address.port : 0;
      server.close(() => (port === 0 ? reject(new Error("no free port")) : resolve(port)));
    });
  });
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
