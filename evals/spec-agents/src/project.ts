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

/** Throwaway eval project directories under the sanctioned gitignored home. */

import { cpSync, existsSync, mkdirSync, readdirSync, readFileSync, rmSync } from "node:fs";
import { join, relative } from "node:path";
import { FIXTURES_DIR, PROJECTS_HOME } from "./config.js";

/**
 * Fresh project dir for one run; `fixture` (a dir name under
 * scenarios/fixtures/) seeds its starting `specs/` state — the section-alone
 * input decided in #356 (captured-then-curated).
 */
export function prepareProject(name: string, fixture?: string): string {
  const dir = join(PROJECTS_HOME, name);
  rmSync(dir, { recursive: true, force: true });
  mkdirSync(dir, { recursive: true });
  if (fixture) {
    const src = join(FIXTURES_DIR, fixture);
    if (!existsSync(src)) throw new Error(`fixture not found: ${src}`);
    cpSync(src, dir, { recursive: true });
  }
  return dir;
}

const REQUIREMENTS_DIR = "specs/requirements";

/**
 * Every requirements file (skills/prd-contract), repo-relative, in reading
 * order: the product page, then the feature files, then product-wide.md and
 * its topic files. Reference documents are the user's inputs, not
 * requirements, so `references/` is left out.
 */
export function listRequirementFiles(projectDir: string): string[] {
  const root = join(projectDir, REQUIREMENTS_DIR);
  if (!existsSync(root)) return [];
  const rank = (rel: string): number =>
    rel === "prd.md" ? 0 : rel.startsWith("features/") ? 1 : rel.startsWith("product-wide") ? 2 : 3;
  return readdirSync(root, { recursive: true, withFileTypes: true })
    .filter((e) => e.isFile() && e.name.endsWith(".md"))
    .map((e) => relative(root, join(e.parentPath, e.name)))
    .filter((rel) => !rel.startsWith("references/"))
    .sort((a, b) => rank(a) - rank(b) || a.localeCompare(b, "en", { numeric: true }))
    .map((rel) => `${REQUIREMENTS_DIR}/${rel}`);
}

/** Read a project file, "" when absent (structural checks handle emptiness). */
export function readProjectFile(projectDir: string, rel: string): string {
  const abs = join(projectDir, rel);
  return existsSync(abs) ? readFileSync(abs, "utf8") : "";
}
