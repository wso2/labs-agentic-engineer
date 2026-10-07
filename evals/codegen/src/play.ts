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
 * `play` as a direct child (`node --import tsx`, not `pnpm play`): wrappers do
 * not reliably forward SIGTERM to the process that owns the teardown.
 */

import { spawn, execFile, type ChildProcess } from "node:child_process";
import { createWriteStream } from "node:fs";
import type { WireFailure } from "@aep/playground/src/engine/wire/failure.js";
import { PATHS } from "./config.js";

export interface PlayProcess {
  child: ChildProcess;
  /** Resolves when the process has exited, with its code or the signal that ended it. */
  exited: Promise<{ code: number | null; signal: NodeJS.Signals | null }>;
  /** The last lines it printed (stdout and stderr interleaved) — what a symptom quotes. */
  tail(n?: number): string[];
  /** Calls `fn` with every line as it arrives. */
  onLine(fn: (line: string) => void): void;
  /** SIGTERM, wait up to `graceMs`, then SIGKILL. Resolves once it has exited either way. */
  stop(graceMs: number): Promise<"exited" | "killed">;
}

const TAIL_LINES = 200;

export function startPlay(args: string[], opts: { env: NodeJS.ProcessEnv; logFile: string }): PlayProcess {
  const log = createWriteStream(opts.logFile, { flags: "a" });
  const child = spawn(process.execPath, ["--import", "tsx", "src/cli.ts", ...args], {
    cwd: PATHS.playgroundDir,
    env: opts.env,
    stdio: ["ignore", "pipe", "pipe"],
  });
  const lines: string[] = [];
  const listeners: ((line: string) => void)[] = [];
  const feed = (): ((chunk: Buffer) => void) => {
    let buffer = "";
    return (chunk) => {
      log.write(chunk);
      buffer += chunk.toString("utf8");
      for (;;) {
        const nl = buffer.indexOf("\n");
        if (nl < 0) break;
        const line = buffer.slice(0, nl);
        buffer = buffer.slice(nl + 1);
        lines.push(line);
        if (lines.length > TAIL_LINES) lines.shift();
        for (const fn of listeners) fn(line);
      }
    };
  };
  child.stdout?.on("data", feed());
  child.stderr?.on("data", feed());

  const exited = new Promise<{ code: number | null; signal: NodeJS.Signals | null }>((resolve) => {
    child.on("error", (err) => {
      log.write(`\n[harness] spawn failed: ${err.message}\n`);
      resolve({ code: 2, signal: null });
    });
    child.on("close", (code, signal) => {
      log.end();
      resolve({ code, signal });
    });
  });

  let done = false;
  void exited.then(() => {
    done = true;
  });

  return {
    child,
    exited,
    tail: (n = 30) => lines.slice(-n),
    onLine: (fn) => {
      listeners.push(fn);
    },
    stop: async (graceMs) => {
      if (done) return "exited";
      child.kill("SIGTERM");
      const graceful = await Promise.race([
        exited.then(() => true),
        new Promise<boolean>((resolve) => setTimeout(() => resolve(false), graceMs).unref()),
      ]);
      if (graceful) return "exited";
      child.kill("SIGKILL");
      await exited;
      return "killed";
    },
  };
}

/** `READY <url>` — `wire`'s one line for a driver (`readyLine` in `playground/src/engine/wire/panel.ts`). */
export function parseReady(line: string): string | null {
  const match = /^\s*READY (https?:\/\/\S+)\s*$/.exec(line);
  return match?.[1] ?? null;
}

/**
 * `FAILED <cause> <reason>` — `wire`'s one line for a failed bring-up, saying
 * whose failure it was (`failedLine` in `playground/src/engine/wire/failure.ts`).
 */
export function parseFailed(line: string): WireFailure | null {
  const match = /^\s*FAILED (app|environment) (.+)$/.exec(line);
  if (!match?.[1] || !match[2]) return null;
  return { cause: match[1] === "app" ? "app" : "environment", reason: match[2].trim() };
}

/** The app's base URL — the READY line carries the `?role=` it was entered as. */
export function baseUrl(url: string): string {
  const parsed = new URL(url);
  parsed.search = "";
  parsed.hash = "";
  return parsed.toString();
}

/** `wire`'s line once its teardown is complete. */
export function isStopped(line: string): boolean {
  return line.trim() === "STOPPED";
}

/**
 * Wait for a line matching `pick`, the process to exit, or the timeout —
 * whichever comes first. Lines already printed are not replayed, so call this
 * right after `startPlay`.
 */
export function waitForLine<T>(
  play: PlayProcess,
  pick: (line: string) => T | null,
  timeoutMs: number,
): Promise<{ kind: "line"; value: T } | { kind: "exited"; code: number | null } | { kind: "timeout" }> {
  return new Promise((resolve) => {
    let settled = false;
    const finish = (result: { kind: "line"; value: T } | { kind: "exited"; code: number | null } | { kind: "timeout" }): void => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      resolve(result);
    };
    const timer = setTimeout(() => finish({ kind: "timeout" }), timeoutMs);
    play.onLine((line) => {
      const value = pick(line);
      if (value !== null) finish({ kind: "line", value });
    });
    void play.exited.then(({ code }) => finish({ kind: "exited", code }));
  });
}

/** Is the docker daemon answering? Checked once before a sweep spends anything. */
export function dockerAnswers(): Promise<boolean> {
  return new Promise((resolve) => {
    execFile("docker", ["info"], { timeout: 30_000 }, (err) => resolve(!err));
  });
}

/**
 * An attempt's compose project, gone: containers, volumes — and the images it
 * BUILT. `--rmi local` removes only the images compose named itself (a service
 * with no `image:` key): the project's own Dockerfile builds.
 *
 * `file` is `wire`'s compose file: compose resolves which images it built from
 * the file's services, and by project name alone, once `wire`'s teardown has
 * removed the containers, it finds nothing to remove. Without it, containers
 * and volumes go by name.
 */
export async function composeDown(project: string, file?: string): Promise<void> {
  const down = (args: string[]): Promise<boolean> =>
    new Promise((resolve) => {
      execFile("docker", ["compose", ...args], { timeout: 180_000 }, (err) => resolve(!err));
    });
  const byName = ["-p", project, "down", "-v", "--remove-orphans"];
  // A file compose can no longer read still leaves the project to take down by name.
  if (file && (await down(["-f", file, ...byName, "--rmi", "local"]))) return;
  await down(byName);
}

/**
 * A compose project's service logs — every container's own stdout, timestamped
 * — streamed into `file`. Taken before the project comes down: `wire`'s own
 * teardown removes the containers, and with them the one record of what the
 * generated API printed while the walk drove it.
 */
export function composeLogsTo(project: string, file: string): Promise<void> {
  return new Promise((resolve) => {
    const out = createWriteStream(file);
    const child = spawn("docker", ["compose", "-p", project, "logs", "--no-color", "--timestamps"], {
      stdio: ["ignore", "pipe", "pipe"],
    });
    child.stdout.pipe(out, { end: false });
    child.stderr.pipe(out, { end: false });
    const timer = setTimeout(() => child.kill("SIGKILL"), 120_000);
    const done = (): void => {
      clearTimeout(timer);
      out.end(() => resolve());
    };
    child.on("error", (err) => {
      out.write(`[harness] docker compose logs failed: ${err.message}\n`);
      done();
    });
    child.on("close", done);
  });
}

/**
 * The backstop for a coding run that had to be SIGKILLed: its container, by
 * the name `coding-run.ts` gives it (`aep-play-<run stamp>`). A killed `play`
 * cannot remove it, and `docker run` without `--rm` leaves it behind running.
 */
export function removeContainer(name: string): Promise<void> {
  return new Promise((resolve) => {
    execFile("docker", ["rm", "-f", name], { timeout: 60_000 }, () => resolve());
  });
}
