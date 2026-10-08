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

/** `prototype preview [dir] [--port <n>] [--persist] [--theme <package>] [--open]`: serve until interrupted. */

import { spawn } from "node:child_process";
import { resolve } from "node:path";
import { parseCommandArgs } from "../args.js";
import { EXIT, type CliIO } from "../io.js";
import { startPreviewServer, type RunningPreview } from "../preview/server.js";
import { resolveCliTheme } from "../theme.js";
import { UsageError } from "../usage.js";

/** The port tried when `--port` is not given; a busy one falls back to any free port. */
export const DEFAULT_PORT = 4321;

function parsePort(text: string): number {
  const port = Number(text);
  if (!/^\d+$/.test(text) || port > 65535) throw new UsageError(`--port ${JSON.stringify(text)} is not a port number (0–65535)`);
  return port;
}

function openBrowser(url: string, io: CliIO): void {
  const [command, args] = process.platform === "darwin" ? ["open", [url]] : process.platform === "win32" ? ["cmd", ["/c", "start", "", url]] : ["xdg-open", [url]];
  const child = spawn(command, args, { stdio: "ignore", detached: true });
  child.on("error", () => io.stderr(`prototype: could not open a browser; open ${url}\n`));
  child.unref();
}

function untilInterrupted(): Promise<void> {
  return new Promise((done) => {
    process.once("SIGINT", () => done());
    process.once("SIGTERM", () => done());
  });
}

export async function runPreview(args: readonly string[], io: CliIO): Promise<number> {
  const { values, positionals } = parseCommandArgs(
    args,
    { port: { type: "string" }, persist: { type: "boolean", default: false }, theme: { type: "string" }, open: { type: "boolean", default: false } },
    1,
  );
  const dir = resolve(io.cwd, positionals[0] ?? ".");
  const theme = resolveCliTheme(values.theme, dir, io.cwd);
  const explicit = values.port !== undefined;
  const port = values.port !== undefined ? parsePort(values.port) : DEFAULT_PORT;
  let preview: RunningPreview;
  try {
    preview = await startPreviewServer({ dir, port, persist: values.persist, theme });
  } catch (e) {
    if ((e as NodeJS.ErrnoException).code !== "EADDRINUSE") throw e;
    if (explicit) throw new UsageError(`port ${port} is in use; pick another with --port, or leave --port out to use any free port`);
    preview = await startPreviewServer({ dir, port: 0, persist: values.persist, theme });
  }
  io.stdout(`Preview ready at ${preview.url}\n`);
  if (values.open) openBrowser(preview.url, io);
  await untilInterrupted();
  await preview.close();
  return EXIT.ok;
}
