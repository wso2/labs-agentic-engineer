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

// Starting and stopping the `opencode serve` child. Speaks the SDK launcher's
// protocol (`createOpencodeServer`) without calling it: the launcher inherits
// `process.env` and no `cwd` (the run's tools need the policy's env and the
// workspace), buffers stderr without bound, and does not wait for the exit.

import { spawn, type ChildProcess } from "node:child_process";
import net from "node:net";

export interface ServerOptions {
  /** The `opencode` binary; resolved on `env.PATH` when bare. */
  command: string;
  hostname: string;
  port: number;
  config: Record<string, unknown>;
  env: Record<string, string>;
  cwd: string;
  /** `--log-level` plus `--print-logs`, so the server's log reaches `onStderr`. */
  debug: boolean;
  /** Every stderr chunk (the debug log when `debug`); the caller decides where it goes. */
  onStderr?: (chunk: string) => void;
  timeoutMs: number;
}

export interface RunningServer {
  url: string;
  /** SIGTERM, then SIGKILL if it has not exited within the grace. Resolves once it has exited. */
  close(): Promise<void>;
}

/** How much of the child's output a startup failure quotes. */
const STARTUP_OUTPUT_TAIL = 4_000;

const STOP_GRACE_MS = 5_000;

const LISTENING = /^opencode server listening.*?\bon\s+(https?:\/\/\S+)/m;

/**
 * A free loopback port, chosen here: OpenCode reads `port: 0` as "4096 if
 * free", which two runs on one host would race for. A collision in the small
 * window before the server binds fails the start loudly.
 */
export function freePort(hostname = "127.0.0.1"): Promise<number> {
  return new Promise((resolve, reject) => {
    const probe = net.createServer();
    probe.unref();
    probe.once("error", reject);
    probe.listen(0, hostname, () => {
      const address = probe.address();
      const port = typeof address === "object" && address ? address.port : 0;
      probe.close(() => (port ? resolve(port) : reject(new Error("no free port"))));
    });
  });
}

function stop(child: ChildProcess): Promise<void> {
  if (child.exitCode !== null || child.signalCode !== null) return Promise.resolve();
  return new Promise((resolve) => {
    const timer = setTimeout(() => child.kill("SIGKILL"), STOP_GRACE_MS);
    child.once("exit", () => {
      clearTimeout(timer);
      resolve();
    });
    child.kill("SIGTERM");
  });
}

/** Start the server and resolve once it says where it is listening. */
export function startServer(opts: ServerOptions): Promise<RunningServer> {
  const args = ["serve", `--hostname=${opts.hostname}`, `--port=${opts.port}`];
  if (opts.debug) args.push("--log-level=DEBUG", "--print-logs");
  const child = spawn(opts.command, args, {
    cwd: opts.cwd,
    env: { ...opts.env, OPENCODE_CONFIG_CONTENT: JSON.stringify(opts.config) },
    stdio: ["ignore", "pipe", "pipe"],
  });

  // Bounded: only the tail is kept, and only for a startup failure's message.
  let tail = "";
  const keep = (chunk: string): void => {
    tail = (tail + chunk).slice(-STARTUP_OUTPUT_TAIL);
  };
  child.stderr?.setEncoding("utf8");
  child.stderr?.on("data", (chunk: string) => {
    keep(chunk);
    opts.onStderr?.(chunk);
  });

  return new Promise((resolve, reject) => {
    let settled = false;
    let stdout = "";
    const fail = (err: Error): void => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      void stop(child);
      reject(err);
    };
    const timer = setTimeout(
      () => fail(new Error(`opencode serve did not start within ${opts.timeoutMs}ms${tail ? `: ${tail.trim()}` : ""}`)),
      opts.timeoutMs,
    );
    child.stdout?.setEncoding("utf8");
    child.stdout?.on("data", (chunk: string) => {
      if (settled) return;
      stdout = (stdout + chunk).slice(-STARTUP_OUTPUT_TAIL);
      keep(chunk);
      const m = LISTENING.exec(stdout);
      if (!m) return;
      settled = true;
      clearTimeout(timer);
      resolve({ url: m[1], close: () => stop(child) });
    });
    child.once("error", (err) => fail(err));
    child.once("exit", (code, signal) =>
      fail(new Error(`opencode serve exited (${signal ?? `code ${code}`}) before listening${tail ? `: ${tail.trim()}` : ""}`)),
    );
  });
}
