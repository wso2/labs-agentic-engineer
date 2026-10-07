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
 * PORT LEASES: a machine-wide registry of which wired session owns which host
 * port, so concurrent sessions are never handed one port before either binds
 * it (ADR-0004).
 */

import { randomBytes } from "node:crypto";
import { linkSync, mkdirSync, readFileSync, renameSync, statSync, unlinkSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

/** One registry per machine user: every playground checkout and every project share it. */
export function defaultLeaseDir(): string {
  return join(homedir(), ".aep-playground", "ports");
}

/** Who holds a port: the process, and which session in it (a TUI can run more than one). */
interface Lease {
  pid: number;
  session: string;
}

type Registry = Record<string, Lease>;

export interface PortLeaseOptions {
  /** The machine-wide directory holding the registry and its lock. */
  dir?: string;
  /** Whether a port is free of everything that is NOT a playground session. */
  isAvailable: (port: number) => Promise<boolean>;
  /** This process. A seam so a test can stand in for another session. */
  pid?: number;
  /** Whether a lease holder still runs. A seam for the stale-holder tests. */
  isAlive?: (pid: number) => boolean;
  /** How long to wait for the lock before giving up with an error that names it. */
  lockTimeoutMs?: number;
}

export interface PortLeases {
  /** Lease the port if no live session holds it and the probe finds it free. */
  take: (port: number) => Promise<boolean>;
  /** Give back every port this session leased. Idempotent, never throws. */
  release: () => Promise<void>;
}

/** A critical section is a few file operations; a lock this old was abandoned mid-way. */
const STALE_LOCK_MS = 30_000;

export function processAlive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    // EPERM: it exists, it is just not ours to signal.
    return (error as NodeJS.ErrnoException).code === "EPERM";
  }
}

export function portLeases(options: PortLeaseOptions): PortLeases {
  const dir = options.dir ?? defaultLeaseDir();
  const pid = options.pid ?? process.pid;
  const isAlive = options.isAlive ?? processAlive;
  const lockTimeoutMs = options.lockTimeoutMs ?? 10_000;
  const session = randomBytes(8).toString("hex");
  const registryFile = join(dir, "leases.json");
  const lockFile = join(dir, "leases.lock");

  const read = (): Registry => {
    try {
      return JSON.parse(readFileSync(registryFile, "utf8")) as Registry;
    } catch {
      return {};
    }
  };

  const live = (registry: Registry): Registry =>
    Object.fromEntries(Object.entries(registry).filter(([, lease]) => lease.pid === pid || isAlive(lease.pid)));

  const write = (registry: Registry): void => {
    const temporary = `${registryFile}.${String(pid)}.${session}.tmp`;
    writeFileSync(temporary, JSON.stringify(registry, null, 2), "utf8");
    // rename(2) replaces atomically, so a reader never sees half a registry.
    renameSync(temporary, registryFile);
  };

  /** `link(2)` fails atomically when the lock exists and publishes the holder's pid. */
  const tryLock = (): boolean => {
    const temporary = `${lockFile}.${String(pid)}.${session}`;
    writeFileSync(temporary, String(pid), "utf8");
    try {
      linkSync(temporary, lockFile);
      return true;
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "EEXIST") throw error;
      if (lockAbandoned()) removeQuietly(lockFile);
      return false;
    } finally {
      removeQuietly(temporary);
    }
  };

  const lockAbandoned = (): boolean => {
    try {
      const holder = Number(readFileSync(lockFile, "utf8"));
      const age = Date.now() - statSync(lockFile).mtimeMs;
      return !Number.isInteger(holder) || holder <= 0 || !isAlive(holder) || age > STALE_LOCK_MS;
    } catch {
      return false; // gone already: the next attempt simply takes it
    }
  };

  const withLock = async <T>(body: () => T): Promise<T> => {
    mkdirSync(dir, { recursive: true });
    const deadline = Date.now() + lockTimeoutMs;
    while (!tryLock()) {
      if (Date.now() > deadline) {
        throw new Error(`could not lock the port registry ${lockFile} — remove it if no \`play wire\` is starting`);
      }
      await new Promise((resolve) => setTimeout(resolve, 10 + Math.random() * 20));
    }
    try {
      return body();
    } finally {
      removeQuietly(lockFile);
    }
  };

  const reserve = (port: number): Promise<boolean> =>
    withLock(() => {
      const registry = live(read());
      if (registry[String(port)]) return false;
      registry[String(port)] = { pid, session };
      write(registry);
      return true;
    });

  const giveBack = (port: number): Promise<void> =>
    withLock(() => {
      const registry = read();
      if (registry[String(port)]?.session !== session) return;
      delete registry[String(port)];
      write(registry);
    });

  return {
    take: async (port) => {
      // Reserve FIRST, then probe: a probe outside the reservation is the race
      // this module exists to close, and a probe inside the lock would hold
      // every other session's assignment up behind a socket timeout.
      if (!(await reserve(port))) return false;
      if (await options.isAvailable(port)) return true;
      await giveBack(port);
      return false;
    },
    release: async () => {
      // Never throws, so every exit path can call it. A lease it fails to give
      // back is dropped anyway once this process is gone.
      try {
        await withLock(() => {
          const registry = read();
          const kept = Object.fromEntries(Object.entries(registry).filter(([, lease]) => lease.session !== session));
          if (Object.keys(kept).length !== Object.keys(registry).length) write(kept);
        });
      } catch {
        // see above
      }
    },
  };
}

function removeQuietly(file: string): void {
  try {
    unlinkSync(file);
  } catch {
    // already gone
  }
}
