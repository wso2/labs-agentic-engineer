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
 * EVERYTHING A WIRED SESSION LEAVES ON DISK, in one dot-dir.
 *
 *   <project>/.aep-playground/wire/
 *   ├── compose.yaml      generated; what `docker compose` is pointed at
 *   ├── plan.json         the plan with every secret masked — the readable record
 *   ├── key.pem, cert.pem the gateway identity (0600 on the key)
 *   ├── secrets.json     the generated database passwords (0600) — stable, see below
 *   ├── tokens.json       one mock bearer per role; not a secret
 *   ├── session.json      what a crashed session left running, so the next reaps it
 *   ├── seed.sh           the seed agent's output, replayed with no model after
 *   ├── logs/             the dev server's output (compose logs through docker)
 *   └── agents/           one transcript per agent task
 *
 * Inside `.aep-playground/`, which the engineering-agent turns never see and the
 * playground already gitignores — so a wired session leaves the project tree
 * exactly as the coding run left it.
 */

import { chmodSync, existsSync, mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { randomBytes } from "node:crypto";
import { join } from "node:path";
import { STATE_DIR } from "../../state/project.js";

/** What a session left behind, so the next start can tell a live one from a corpse. */
export interface WireSession {
  composeProject: string;
  /** The dev server's port, so a listener left behind can be found and killed. */
  webappPort?: number;
  /** ISO timestamp of the last start. */
  startedAt: string;
  /**
   * The `wire` process itself. The one honest way to tell a session that is
   * still being used from one that died hard: the compose project and the dev
   * server both outlive a SIGKILL, so their presence says nothing, and reaping
   * a colleague's running session by mistake is the worse of the two errors.
   */
  pid?: number;
}

/**
 * Whether the process recorded in this project still exists.
 *
 * Necessary but NOT sufficient, and the caller has to know why: a zombie — a
 * process that has exited and whose parent has not reaped it — still answers
 * signal 0. Seen exactly that way when a harness spawned `wire` and walked
 * away, leaving a `(node)` entry that would have refused every later session.
 * `session.ts` pairs this with "is its dev server still listening".
 */
export function sessionProcessExists(session: WireSession | null): boolean {
  if (!session?.pid) return false;
  try {
    process.kill(session.pid, 0);
    return true;
  } catch {
    return false;
  }
}

export function wireDir(projectDir: string): string {
  return join(projectDir, STATE_DIR, "wire");
}

export function wirePaths(projectDir: string) {
  const dir = wireDir(projectDir);
  return {
    dir,
    compose: join(dir, "compose.yaml"),
    plan: join(dir, "plan.json"),
    tokens: join(dir, "tokens.json"),
    secrets: join(dir, "secrets.json"),
    session: join(dir, "session.json"),
    seed: join(dir, "seed.sh"),
    initSql: join(dir, "init.sql"),
    triageLog: join(dir, "triage.log"),
    logs: join(dir, "logs"),
    agents: join(dir, "agents"),
  };
}

export function ensureWireDir(projectDir: string): string {
  const paths = wirePaths(projectDir);
  mkdirSync(paths.logs, { recursive: true });
  mkdirSync(paths.agents, { recursive: true });
  return paths.dir;
}

/** Write atomically: a half-written compose file is a confusing failure a rename cannot produce. */
export function writeFileAtomic(file: string, contents: string): void {
  const temporary = `${file}.tmp`;
  writeFileSync(temporary, contents, "utf8");
  renameSync(temporary, file);
}

/**
 * The generated passwords, kept for as long as the project is.
 *
 * Postgres writes the password into the data directory when it first
 * initializes, so a password regenerated on the next run authenticates against
 * nothing and the service exits with `password authentication failed for user`
 * — which reads like a bug in the generated app and is not one.
 */
export function databaseSecret(projectDir: string, database: string): string {
  const file = wirePaths(projectDir).secrets;
  let store: Record<string, string> = {};
  if (existsSync(file)) {
    try {
      store = JSON.parse(readFileSync(file, "utf8")) as Record<string, string>;
    } catch {
      store = {};
    }
  }
  const existing = store[database];
  if (existing) return existing;
  const minted = randomBytes(12).toString("hex");
  store[database] = minted;
  mkdirSync(wireDir(projectDir), { recursive: true });
  writeFileAtomic(file, JSON.stringify(store, null, 2));
  chmodSync(file, 0o600);
  return minted;
}

export function readWireSession(projectDir: string): WireSession | null {
  const file = wirePaths(projectDir).session;
  if (!existsSync(file)) return null;
  try {
    return JSON.parse(readFileSync(file, "utf8")) as WireSession;
  } catch {
    return null;
  }
}

export function writeWireSession(projectDir: string, session: WireSession): void {
  writeFileAtomic(wirePaths(projectDir).session, JSON.stringify(session, null, 2));
}
