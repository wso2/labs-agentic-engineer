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
 * THE MACHINE: ports, child processes, the browser.
 *
 * Small and separate because everything here is the part of a wired session
 * that cannot be tested by reasoning about it — and because `session.ts` should
 * read as the nine steps of the flow rather than as process plumbing.
 *
 * The one rule that matters is REAPING. `npm run dev:mock` is three processes
 * deep, so the child pid is not a handle on the server: a detached spawn makes
 * the child its own process-group leader and the negative pid kills the group,
 * which is the same trick `skills/mock-verification/scripts/walk.sh` uses and
 * for the same reason — every session that skipped it leaked a dev server.
 */

import { spawn, type ChildProcess, type StdioOptions } from "node:child_process";
import { createServer, createConnection } from "node:net";
import { platform } from "node:os";

export interface RunResult {
  code: number;
  /** Combined stdout+stderr, captured only when `capture` was asked for. */
  output: string;
}

export interface RunOptions {
  cwd?: string;
  env?: NodeJS.ProcessEnv;
  /** Collect the output instead of letting it through to the terminal. */
  capture?: boolean;
  /** Called with each captured line, so a long build can still say what it is doing. */
  onLine?: (line: string) => void;
}

/** Run a command to completion. Never throws on a non-zero exit — the code IS the answer. */
export function run(command: string, args: string[], options: RunOptions = {}): Promise<RunResult> {
  return new Promise((resolve) => {
    const stdio: StdioOptions = options.capture ? ["ignore", "pipe", "pipe"] : ["ignore", "inherit", "inherit"];
    const child = spawn(command, args, {
      stdio,
      ...(options.cwd ? { cwd: options.cwd } : {}),
      ...(options.env ? { env: options.env } : {}),
    });
    let output = "";
    const absorb = (chunk: Buffer): void => {
      const text = chunk.toString();
      output += text;
      if (options.onLine) for (const line of text.split("\n")) if (line.trim()) options.onLine(line);
    };
    child.stdout?.on("data", absorb);
    child.stderr?.on("data", absorb);
    child.on("error", (error) => {
      resolve({ code: 127, output: `${output}${error.message}` });
    });
    child.on("close", (code) => {
      resolve({ code: code ?? 1, output });
    });
  });
}

/** Whether a command exists and answers. Used by preflight, so the failure names the tool. */
export async function answers(command: string, args: string[]): Promise<boolean> {
  return (await run(command, args, { capture: true })).code === 0;
}

/** Can this process bind the port right now? The only question a port assignment asks. */
export function isPortFree(port: number): Promise<boolean> {
  return new Promise((resolve) => {
    const server = createServer();
    server.once("error", () => {
      resolve(false);
    });
    server.once("listening", () => {
      server.close(() => {
        resolve(true);
      });
    });
    server.listen(port, "127.0.0.1");
  });
}

/**
 * Both loopback addresses, because "localhost" is not one address.
 *
 * Vite binds `::1`; Docker publishes on the IPv4 wildcard; a plain Node server
 * binds whichever it was told. A probe that knows only `127.0.0.1` calls a port
 * free while the other family serves it, and the two servers then answer the
 * same `localhost:<port>` URL depending on how the client resolves the name.
 */
const LOOPBACKS = ["127.0.0.1", "::1"] as const;

function connects(port: number, host: string): Promise<boolean> {
  return new Promise((resolve) => {
    const socket = createConnection({ port, host });
    const done = (busy: boolean): void => {
      socket.destroy();
      resolve(busy);
    };
    socket.once("connect", () => {
      done(true);
    });
    socket.once("error", () => {
      done(false);
    });
    socket.setTimeout(500, () => {
      done(false);
    });
  });
}

/** Is something listening there — the other half of the question, asked of a port we do not own. */
export async function isPortBusy(port: number): Promise<boolean> {
  for (const host of LOOPBACKS) {
    if (await connects(port, host)) return true;
  }
  return false;
}

/**
 * Can this session TAKE the port — the question every assignment actually
 * means, and the one neither half answers alone.
 *
 * `isPortFree` binds `127.0.0.1`, and a loopback bind SUCCEEDS while another
 * process holds the same port on the wildcard address — which is exactly how
 * Docker publishes one. So a second `wire` session read 19090 as free, compose
 * then asked for `0.0.0.0:19090`, and the daemon refused it with "port is
 * already allocated" after the image had already been built. Measured, not
 * hypothesised: two concurrent sessions on this machine, the first one's API
 * still up and answering there.
 *
 * `isPortBusy` is the half that sees it — a connect to a published container
 * port succeeds. The bind check stays too: a port nothing listens on yet can
 * still be unbindable, and a service that is up but not yet accepting would
 * pass a connect test alone.
 *
 * Both the dev-server port and every service's host port resolve through this,
 * because a predicate used by one path and not the other is the same bug with
 * a longer fuse.
 */
export async function isPortAvailable(port: number): Promise<boolean> {
  return (await isPortFree(port)) && !(await isPortBusy(port));
}

/** The first free port from `from`, so two wired sessions never fight over one. */
export async function findFreePort(from: number, to = from + 40): Promise<number> {
  for (let port = from; port <= to; port += 1) {
    if (await isPortAvailable(port)) return port;
  }
  throw new Error(`no free port between ${String(from)} and ${String(to)}`);
}

/** A process group: what a dev server actually is, and the only thing that reaps one. */
export interface ProcessGroup {
  child: ChildProcess;
  pid: number;
  stop: () => Promise<void>;
}

/**
 * Start a long-running child as its own process group.
 *
 * `detached` is what makes the pid a group id; killing `-pid` then reaches
 * `npm`, the shell it spawns and the `vite` underneath. Its stdin is ignored on
 * purpose: Vite reads the terminal for its own keyboard shortcuts, and under job
 * control a background process that reads the terminal is stopped with SIGTTIN.
 */
export function startGroup(command: string, args: string[], options: RunOptions = {}): ProcessGroup {
  const stdio: StdioOptions = options.capture ? ["ignore", "pipe", "pipe"] : ["ignore", "inherit", "inherit"];
  const child = spawn(command, args, {
    detached: true,
    stdio,
    ...(options.cwd ? { cwd: options.cwd } : {}),
    ...(options.env ? { env: options.env } : {}),
  });
  if (options.onLine) {
    const absorb = (chunk: Buffer): void => {
      for (const line of chunk.toString().split("\n")) if (line.trim()) options.onLine?.(line);
    };
    child.stdout?.on("data", absorb);
    child.stderr?.on("data", absorb);
  }
  const pid = child.pid ?? 0;
  return {
    child,
    pid,
    stop: async () => {
      await stopGroup(pid);
    },
  };
}

/** TERM the group, wait for it to go, then KILL what is left. */
export async function stopGroup(pid: number): Promise<void> {
  if (!pid) return;
  try {
    process.kill(-pid, "SIGTERM");
  } catch {
    return; // already gone
  }
  for (let attempt = 0; attempt < 20; attempt += 1) {
    await delay(250);
    try {
      process.kill(-pid, 0);
    } catch {
      return;
    }
  }
  try {
    process.kill(-pid, "SIGKILL");
  } catch {
    // nothing left to kill
  }
}

/** Kill whatever is listening on a port — a dev server a hard-killed session left behind. */
export async function killListener(port: number): Promise<boolean> {
  const found = await run("lsof", ["-nP", `-iTCP:${String(port)}`, "-sTCP:LISTEN", "-t"], { capture: true });
  const pids = found.output
    .split("\n")
    .map((line) => Number(line.trim()))
    .filter((pid) => Number.isInteger(pid) && pid > 0);
  for (const pid of pids) {
    try {
      process.kill(pid, "SIGTERM");
    } catch {
      // already gone
    }
  }
  return pids.length > 0;
}

export function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/**
 * Wait until a URL answers at all — a status, any status, is proof the server is up.
 *
 * Every attempt carries its own abort signal, because the deadline in the loop
 * condition cannot cancel a request already in flight: a listener that accepts
 * the connection and then never writes a response leaves `fetch` pending
 * forever, and the loop never comes back round to notice its own timeout. A
 * server that is up but slow is the case the per-attempt cap is sized for, and
 * it is clamped to what is left so the whole wait still ends when it said it
 * would.
 */
export async function waitForHttp(url: string, timeoutMs = 120_000): Promise<boolean> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const remaining = Math.max(1, deadline - Date.now());
      await fetch(url, { redirect: "manual", signal: AbortSignal.timeout(Math.min(5_000, remaining)) });
      return true;
    } catch {
      // Clamped to what is left, same as the attempt's own abort signal above —
      // otherwise this retry delay is the one thing in the loop the deadline
      // does not bound, and the wait outlives what it said it would by up to
      // 500ms on its last, failing attempt.
      const left = deadline - Date.now();
      if (left <= 0) break;
      await delay(Math.min(500, left));
    }
  }
  return false;
}

/** Open the developer's browser. Best effort: a failure here is not a failed session. */
export async function openBrowser(url: string): Promise<void> {
  const opener = platform() === "darwin" ? "open" : "xdg-open";
  await run(opener, [url], { capture: true });
}
