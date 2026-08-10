/*
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied. See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/**
 * The fixture corpus: recorded Central payloads and the Ballerina they render
 * to.
 *
 * The risk this exists to manage is invisible without it — the renderer mangles
 * some library's shape (a union, a nested record default, a resource path with
 * a quoted identifier, an annotation), the agent writes code against a wrong
 * signature, and `bal build` fails for reasons nobody traces back here.
 *
 * Fixtures are chosen to span SHAPE space rather than popularity; see
 * `README.md` for what each one pins.
 */

import { readdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { gunzipSync } from "node:zlib";
import { parseCentralDocs } from "../src/central/client.js";
import type { CentralDocs } from "../src/central/schema.js";
import { fromCentral, selectModule } from "../src/from-central.js";
import type { Library } from "../src/model.js";
import { applyPatches } from "../src/patches.js";
import { parseQualifiedName, type QualifiedName } from "../src/qualified.js";
import { toSyntaxString } from "../src/render/document.js";

const testDir = dirname(fileURLToPath(import.meta.url));
export const FIXTURES_DIR = join(testDir, "__fixtures__");
export const SNAPSHOTS_DIR = join(testDir, "__snapshots__");

/** `ballerinax/github` → `ballerinax__github`; a slash is not a filename. */
export function fixtureSlug(qualifiedName: string): string {
  return qualifiedName.replace("/", "__");
}

export function listFixtures(): string[] {
  return readdirSync(FIXTURES_DIR)
    .filter((file) => file.endsWith(".json.gz"))
    .map((file) => file.replace(/\.json\.gz$/, ""))
    .sort();
}

/** The recorded payload, still unvalidated — parsing it is what a test asserts. */
export function loadRawFixture(slug: string): unknown {
  return JSON.parse(gunzipSync(readFileSync(join(FIXTURES_DIR, `${slug}.json.gz`))).toString("utf-8")) as unknown;
}

export function loadFixture(slug: string): CentralDocs {
  const parsed = parseCentralDocs(loadRawFixture(slug), slug);
  if (!parsed.ok) throw new Error(`fixture ${slug} no longer parses: ${JSON.stringify(parsed.error)}`);
  return parsed.value;
}

/**
 * `ballerinax__github` → the coordinates a caller would have typed. The reader
 * now selects its module by requested name rather than taking `modules[0]`, so
 * the corpus has to supply the same argument the CLI would.
 */
export function qualifiedForSlug(slug: string): QualifiedName {
  const parsed = parseQualifiedName(slug.replace("__", "/"));
  if (!parsed.ok) throw new Error(`fixture slug ${slug} is not a package name`);
  return parsed.value;
}

/** The IR every view is rendered from — the pipeline up to but not including a document. */
export function libraryFor(slug: string): Library {
  const docs = loadFixture(slug);
  const module = selectModule(docs, qualifiedForSlug(slug));
  if (!module.ok) throw new Error(`fixture ${slug} has no module named after it: ${JSON.stringify(module.error)}`);
  return applyPatches(fromCentral(module.value));
}

/**
 * The whole pipeline, in process. Deliberately not a subprocess: a rendering
 * regression should surface as a diff in a test, not as an exit code from a
 * command whose output nobody kept.
 */
export function renderFixture(slug: string): string {
  return toSyntaxString(libraryFor(slug));
}

/**
 * Every distinct object shape in a payload, as sorted key lists.
 *
 * This is the drift detector. The schema makes a RENAMED or REMOVED field a
 * loud failure, because those change what gets rendered; an ADDED field is
 * harmless to a reader that ignores it, and failing a lookup over one would
 * take the capability away for a cosmetic upstream change. So additions are
 * caught here instead — as a reviewable diff in a snapshot, at no run-time
 * cost. Recorded per fixture because a shape only some packages use (a
 * listener, an inline record) is exactly where drift hides.
 */
export function keySpace(payload: unknown): string[] {
  const signatures = new Set<string>();
  const walk = (node: unknown): void => {
    if (node === null || typeof node !== "object") return;
    if (Array.isArray(node)) {
      for (const item of node) walk(item);
      return;
    }
    const keys = Object.keys(node);
    if (keys.length > 0) signatures.add([...keys].sort().join(","));
    for (const value of Object.values(node)) walk(value);
  };
  walk(payload);
  return [...signatures].sort();
}

export const KEYSPACE_SNAPSHOT = join(SNAPSHOTS_DIR, "keyspace.txt");

export function renderKeySpace(): string {
  const lines: string[] = [];
  for (const slug of listFixtures()) {
    for (const signature of keySpace(loadRawFixture(slug))) lines.push(`${slug}\t${signature}`);
  }
  return `${lines.join("\n")}\n`;
}

export function snapshotPath(slug: string): string {
  return join(SNAPSHOTS_DIR, `${slug}.bal`);
}

export function readSnapshot(slug: string): string {
  return readFileSync(snapshotPath(slug), "utf-8");
}

export function writeSnapshot(slug: string, content: string): void {
  writeFileSync(snapshotPath(slug), content);
}

/**
 * The first place two texts differ, as `line N: expected … / actual …`.
 *
 * Worth the twenty lines: the snapshots run to 20,000 lines and a bare
 * "strings are not equal" from the assertion library would print both of them.
 */
export function firstDifference(expected: string, actual: string): string | undefined {
  if (expected === actual) return undefined;
  const expectedLines = expected.split("\n");
  const actualLines = actual.split("\n");
  const limit = Math.max(expectedLines.length, actualLines.length);
  for (let i = 0; i < limit; i++) {
    if (expectedLines[i] !== actualLines[i]) {
      return [
        `line ${i + 1} of ${expectedLines.length} (actual has ${actualLines.length})`,
        `  expected: ${JSON.stringify(expectedLines[i] ?? null)}`,
        `  actual:   ${JSON.stringify(actualLines[i] ?? null)}`,
      ].join("\n");
    }
  }
  return "texts differ in length only";
}
