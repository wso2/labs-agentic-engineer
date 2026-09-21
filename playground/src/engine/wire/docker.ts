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
 * COMPOSE, as the four calls a wired session makes.
 *
 * Every one of them names the project explicitly (`-p`) rather than letting
 * compose derive it from a directory name. That name — `aep-wire-<slug>` — is
 * the handle a session that died hard leaves behind for the next start to reap.
 */

import { run, type RunResult } from "./runtime.js";

export interface ComposeTarget {
  /** The generated compose file under the project's state dir. */
  file: string;
  /** `aep-wire-<slug>`. */
  project: string;
}

function compose(target: ComposeTarget, args: string[]): string[] {
  return ["compose", "-f", target.file, "-p", target.project, ...args];
}

/** Build every image and start everything, returning only once each is healthy. */
export function composeUp(target: ComposeTarget, onLine?: (line: string) => void): Promise<RunResult> {
  return run("docker", compose(target, ["up", "--build", "--wait", "--remove-orphans", "-d"]), {
    capture: true,
    ...(onLine ? { onLine } : {}),
  });
}

/** Rebuild and restart ONE service — the loop for a developer editing service code. */
export function composeUpOne(
  target: ComposeTarget,
  service: string,
  onLine?: (line: string) => void,
): Promise<RunResult> {
  return run("docker", compose(target, ["up", "--build", "--wait", "-d", service]), {
    capture: true,
    ...(onLine ? { onLine } : {}),
  });
}

/**
 * Stop everything.
 *
 * Without `-v` unless asked: the database volume is a session's work, and a
 * developer who set up data by hand should find it again tomorrow. `--fresh`
 * is the way to say otherwise, and it says it once, at the start.
 */
export function composeDown(target: ComposeTarget, volumes = false): Promise<RunResult> {
  return run("docker", compose(target, volumes ? ["down", "-v"] : ["down"]), { capture: true });
}

/** The tail of one service's log — the first thing to read when a bring-up fails. */
export async function composeLogs(target: ComposeTarget, service: string, lines = 200): Promise<string> {
  const result = await run("docker", compose(target, ["logs", "--no-color", "--tail", String(lines), service]), {
    capture: true,
  });
  return result.output;
}

/** Which services exist and how they stand, as `up --wait` left them. */
export async function composePs(target: ComposeTarget): Promise<{ name: string; state: string; health: string }[]> {
  const result = await run("docker", compose(target, ["ps", "--format", "json"]), { capture: true });
  const rows: { name: string; state: string; health: string }[] = [];
  // One JSON object per line (compose v2), not a JSON array.
  for (const line of result.output.split("\n")) {
    const text = line.trim();
    if (!text.startsWith("{")) continue;
    try {
      const parsed = JSON.parse(text) as { Service?: string; Name?: string; State?: string; Health?: string };
      rows.push({
        name: parsed.Service ?? parsed.Name ?? "?",
        state: parsed.State ?? "?",
        health: parsed.Health ?? "",
      });
    } catch {
      // A line that is not a row tells us nothing; the caller has the exit code.
    }
  }
  return rows;
}
