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
 * The theme under the kit's CLI seam: the built `prototype` bin as a child
 * process, run with `--theme @wso2/prototype-theme-oxygen` (or the default
 * theme, to compare), over the CLI's own fixture prototypes.
 */

import { spawn, type ChildProcess } from "node:child_process";
import { cpSync, mkdtempSync, readdirSync } from "node:fs";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

export const THEME = "@wso2/prototype-theme-oxygen";

/** The theme package's root: the CLI resolves `--theme` from its cwd, and this package has itself installed. */
const PACKAGE_ROOT = fileURLToPath(new URL("..", import.meta.url));
const CLI_ROOT = dirname(createRequire(import.meta.url).resolve("@wso2/prototype-cli/package.json"));
const BIN = join(CLI_ROOT, "dist", "bin.js");
const FIXTURES = join(CLI_ROOT, "test", "fixtures");

/** PATH and HOME are what node needs, and no more. */
const CHILD_ENV = { PATH: process.env["PATH"] ?? "", HOME: process.env["HOME"] ?? "" };

export function fixtures(kind: "valid" | "invalid"): string[] {
  return readdirSync(join(FIXTURES, kind)).sort();
}

export function fixturePath(name: string): string {
  return join(FIXTURES, name);
}

export interface Finding {
  code: string;
  file: string;
  location: string;
  message: string;
}

/** `prototype check --json`, with this theme or (without `theme`) the CLI's default. */
export function check(dir: string, theme?: string): Promise<{ status: number | null; ok: boolean; findings: Finding[] }> {
  const args = [BIN, "check", "--json", dir, ...(theme === undefined ? [] : ["--theme", theme])];
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, args, { cwd: PACKAGE_ROOT, env: CHILD_ENV, stdio: ["ignore", "pipe", "pipe"] });
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", (chunk: Buffer) => (stdout += chunk.toString("utf8")));
    child.stderr.on("data", (chunk: Buffer) => (stderr += chunk.toString("utf8")));
    child.once("error", reject);
    child.once("close", (status) => {
      try {
        resolve({ status, ...(JSON.parse(stdout) as { ok: boolean; findings: Finding[] }) });
      } catch {
        reject(new Error(`check printed no report (exit ${String(status)}):\n${stdout}${stderr}`));
      }
    });
  });
}

/** A running `prototype preview --theme`, on a writable copy of a fixture, on a free port. */
export interface PreviewProcess {
  url: string;
  child: ChildProcess;
  stop(): Promise<void>;
}

export function startPreview(fixture: string): Promise<PreviewProcess> {
  const dir = mkdtempSync(join(tmpdir(), "prototype-theme-oxygen-"));
  cpSync(fixturePath(fixture), dir, { recursive: true });
  const child = spawn(process.execPath, [BIN, "preview", dir, "--port", "0", "--theme", THEME], { cwd: PACKAGE_ROOT, env: CHILD_ENV, stdio: ["ignore", "pipe", "pipe"] });
  const stop = () =>
    new Promise<void>((resolve) => {
      if (child.exitCode !== null) return resolve();
      child.once("exit", () => resolve());
      child.kill("SIGTERM");
    });
  return new Promise((resolve, reject) => {
    let output = "";
    const timer = setTimeout(() => reject(new Error(`preview did not start:\n${output}`)), 30_000);
    const onData = (chunk: Buffer) => {
      output += chunk.toString("utf8");
      const ready = /Preview ready at (\S+)/.exec(output);
      if (ready) {
        clearTimeout(timer);
        resolve({ url: ready[1]!, child, stop });
      }
    };
    child.stdout.on("data", onData);
    child.stderr.on("data", onData);
    child.once("exit", (code) => {
      clearTimeout(timer);
      reject(new Error(`preview exited with ${String(code)}:\n${output}`));
    });
  });
}
