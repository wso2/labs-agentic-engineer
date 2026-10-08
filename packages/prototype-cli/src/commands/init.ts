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

/** `prototype init [dir]`: scaffold a minimal valid prototype, refusing to overwrite one. */

import { copyFileSync, existsSync, mkdirSync } from "node:fs";
import { join, resolve } from "node:path";
import { MANIFEST_FILE, SOURCE_FILE } from "@wso2/prototype-kit/check";
import { parseCommandArgs } from "../args.js";
import { INIT_TEMPLATE_DIR } from "../assets.js";
import { EXIT, type CliIO } from "../io.js";
import { displayPath } from "../paths.js";

/** Template file → scaffolded file. The source template is not named `.tsx`, so the repo's tooling leaves it alone. */
const FILES: readonly [string, string][] = [
  ["prototype.json", MANIFEST_FILE],
  ["prototype.tsx.txt", SOURCE_FILE],
];

export function runInit(args: readonly string[], io: CliIO): number {
  const { positionals } = parseCommandArgs(args, {}, 1);
  const dir = resolve(io.cwd, positionals[0] ?? ".");
  const existing = FILES.map(([, name]) => join(dir, name)).filter((path) => existsSync(path));
  if (existing.length > 0) {
    io.stderr(`prototype: ${existing.map((p) => displayPath(io.cwd, p)).join(" and ")} already exist${existing.length === 1 ? "s" : ""}; init does not overwrite a prototype\n`);
    return EXIT.failed;
  }
  mkdirSync(dir, { recursive: true });
  for (const [template, name] of FILES) copyFileSync(join(INIT_TEMPLATE_DIR, template), join(dir, name));
  const shown = displayPath(io.cwd, dir);
  io.stdout(`Created ${join(shown, MANIFEST_FILE)} and ${join(shown, SOURCE_FILE)}.\nNext: prototype check ${shown} && prototype preview ${shown}\n`);
  return EXIT.ok;
}
