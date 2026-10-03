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

/** `prototype export [dir] [-o <file>] [--theme <package>]`: one self-contained HTML file, written only when the check is clean. */

import { readFileSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { checkPrototypeFiles, readPrototypeFiles } from "@wso2/prototype-kit/check";
import { parseManifestJson } from "@wso2/prototype-kit/manifest";
import { parseCommandArgs } from "../args.js";
import { HOST_SCRIPT_PATH } from "../assets.js";
import { prototypeHash } from "@wso2/prototype-kit/feedback";
import { renderHostPage } from "../host-page.js";
import { EXIT, type CliIO } from "../io.js";
import { displayPath } from "../paths.js";
import { formatHuman } from "../report.js";
import { resolveCliTheme } from "../theme.js";

export const DEFAULT_EXPORT_FILE = "prototype.html";

export async function runExport(args: readonly string[], io: CliIO): Promise<number> {
  const { values, positionals } = parseCommandArgs(args, { output: { type: "string", short: "o" }, theme: { type: "string" } }, 1);
  const dir = resolve(io.cwd, positionals[0] ?? ".");
  const theme = resolveCliTheme(values.theme, dir, io.cwd);
  const files = readPrototypeFiles(dir);
  const findings = await checkPrototypeFiles(files, { theme });
  const manifest = files.manifest !== null ? parseManifestJson(files.manifest) : null;
  if (findings.length > 0 || files.manifest === null || files.source === null || !manifest?.ok) {
    io.stderr(`prototype: export needs a clean check\n\n${formatHuman(findings)}`);
    return EXIT.failed;
  }
  const out = values.output !== undefined ? resolve(io.cwd, values.output) : join(dir, DEFAULT_EXPORT_FILE);
  const revision = { manifest: manifest.manifest, source: files.source, hash: prototypeHash(files.manifest, files.source) };
  const html = renderHostPage(
    `${manifest.manifest.name} — prototype`,
    { mode: "export", revision, frameRuntime: readFileSync(theme.frameRuntimePath, "utf8") },
    { inline: readFileSync(HOST_SCRIPT_PATH, "utf8") },
  );
  writeFileSync(out, html, "utf8");
  io.stdout(`Exported ${displayPath(io.cwd, out)}\n`);
  return EXIT.ok;
}
