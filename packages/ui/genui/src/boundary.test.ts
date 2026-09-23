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

import { readdirSync, readFileSync } from "node:fs";
import { join, relative, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

// The two boundaries that keep this package swappable on both sides, enforced.
// Only src/adapter/json-render/ may import the renderer library, or a library
// swap would touch the catalog too. And nothing here may import a design
// system: those live in their own packages (@aep/ui-genui-oxygen, …), so a
// host never installs one it does not use.
const SRC = fileURLToPath(new URL(".", import.meta.url));
const ADAPTER_DIR = join("adapter", "json-render") + sep;

function sourceFiles(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return sourceFiles(path);
    return /\.tsx?$/.test(entry.name) ? [path] : [];
  });
}

const DESIGN_SYSTEM_IMPORT =
  /from\s+["'](@wso2\/oxygen-ui|@mui\/|radix-ui|@radix-ui\/|tailwindcss|@aep\/ui-genui-)/;

describe("package boundaries", () => {
  it("keeps @json-render imports inside src/adapter/json-render", () => {
    const offenders = sourceFiles(SRC)
      .map((path) => relative(SRC, path))
      .filter((path) => !path.startsWith(ADAPTER_DIR) && !path.endsWith(".test.ts"))
      .filter((path) => /from\s+["']@json-render\//.test(readFileSync(join(SRC, path), "utf8")));
    expect(offenders).toEqual([]);
  });

  it("imports no design system", () => {
    const offenders = sourceFiles(SRC)
      .map((path) => relative(SRC, path))
      .filter((path) => DESIGN_SYSTEM_IMPORT.test(readFileSync(join(SRC, path), "utf8")));
    expect(offenders).toEqual([]);
  });
});
