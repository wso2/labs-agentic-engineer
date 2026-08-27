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
 * Cases are DATA, and the directory tree is the only index.
 *
 * A folder under `cases/` is a suite, and there is no list of suites anywhere:
 * adding `cases/connectors/` makes `--suite connectors` work, and nothing else
 * has to be told. That is the property worth protecting — an enumerated list is
 * a second place a case can be forgotten, and the one that gets forgotten is
 * always the new one.
 *
 * The two suites that ship, `narrow/` and `full/`, are a cost distinction and
 * not a kind: narrow is one file against one package weakness, full is a whole
 * component. Nothing in this module knows those names.
 */

import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { basename, dirname, join, resolve } from "node:path";
import { parse } from "yaml";

/** One authored case, as its YAML file declares it. */
export interface EvalCase {
  /** Filename stem — the case's id within its suite. */
  name: string;
  /** The folder it was found in. */
  suite: string;
  /** Absolute path to the YAML, so a report can point at the source. */
  file: string;
  /** The task, handed to the agent verbatim as the session prompt. */
  prompt: string;
  /** Ballerina package name for the scratch project. Defaults to the case name. */
  packageName?: string;
  /**
   * Files copied into the scratch package before the session starts — an
   * openapi.yaml, a partial .bal, whatever the prompt refers to. Keyed by the
   * path to write inside the package, valued by the SOURCE file to copy, itself
   * relative to the case file's own directory and stored here resolved.
   *
   * A source path rather than inline content, because a contract is a file the
   * tools take: `bal openapi -i openapi.yaml` reads the same bytes the case
   * ships, and an editor validates it. Inlined into the YAML it is a block
   * scalar nothing can parse, re-indented by hand on every edit.
   */
  fixtures?: Record<string, string>;
  /** Deterministic post-conditions. Absent means "builds and nothing more". */
  expect?: CaseExpectations;
}

export interface CaseExpectations {
  /** `bal build` must exit 0. Defaults to true. */
  builds?: boolean;
  /** Import paths the produced code must contain. */
  imports?: string[];
  /** Import paths it must NOT contain — a wrong package chosen over the right one. */
  importsNot?: string[];
  /**
   * Regular expressions the produced `.bal` sources must match, and must not.
   *
   * The behavioural axis. `builds` and `imports` between them cannot tell a
   * service that uses a connector from one that constructs its client and never
   * calls it — measured across all 17 attempts of the 2026-08-16 sweep, where
   * every semantic defect found afterwards had passed: a fabricated per-recipient
   * delivery outcome, a Kafka producer that never publishes, a Cosmos read with
   * the wrong partition key, and `return true` where an HMAC check belongs.
   *
   * Deliberately regexes over source rather than a parsed AST. What a case wants
   * to assert is usually "this call happens with this argument", which is a line
   * of Ballerina; a matcher that needed a symbol table would be a second
   * implementation of the compiler and would still not answer it. The cost is
   * that a comment can satisfy a pattern — acceptable, because the failure being
   * defended against is a plausible fake, not a hostile one.
   */
  mustContain?: string[];
  mustNotContain?: string[];
}

/** Everything under `cases/`, in suite-then-name order. */
export function discoverCases(casesDir: string): EvalCase[] {
  const found: EvalCase[] = [];
  for (const suite of directories(casesDir)) {
    for (const file of yamlFiles(join(casesDir, suite))) {
      found.push(readCase(join(casesDir, suite, file), suite));
    }
  }
  return found.sort((a, b) => `${a.suite}/${a.name}`.localeCompare(`${b.suite}/${b.name}`));
}

/**
 * Narrow a discovered set by suite and by case name, either of which may name
 * several, comma-separated.
 *
 * Each member is exact rather than fuzzy: a typo that silently selects nothing
 * is the failure mode here, and `--case aws-submodul` matching zero cases is
 * caught by the caller's emptiness check, where a prefix match would quietly run
 * a different case. A list is not a relaxation of that — `a,nope` still selects
 * only `a`, and only a list whose every member missed is refused.
 */
export function selectCases(all: EvalCase[], suite?: string, name?: string): EvalCase[] {
  const suites = members(suite);
  const names = members(name);
  return all.filter((c) => (suites ? suites.has(c.suite) : true) && (names ? names.has(c.name) : true));
}

/** A comma-separated filter as a set, or undefined for "no filter". */
function members(value?: string): Set<string> | undefined {
  if (!value) return undefined;
  const found = value
    .split(",")
    .map((part) => part.trim())
    .filter((part) => part.length > 0);
  return found.length > 0 ? new Set(found) : undefined;
}

function readCase(file: string, suite: string): EvalCase {
  const raw: unknown = parse(readFileSync(file, "utf8"));
  if (!raw || typeof raw !== "object") {
    throw new Error(`${file}: expected a YAML mapping`);
  }
  const doc = raw as Record<string, unknown>;
  const name = basename(file).replace(/\.ya?ml$/, "");
  const prompt = typeof doc.prompt === "string" ? doc.prompt.trim() : "";
  // A case with no prompt would run an empty session and report a clean zero on
  // every metric, which reads as a pass. Refuse it at load.
  if (!prompt) throw new Error(`${file}: 'prompt' is required and must be a non-empty string`);
  return {
    name,
    suite,
    file,
    prompt,
    ...(typeof doc.packageName === "string" ? { packageName: doc.packageName } : {}),
    ...(isRecord(doc.fixtures) ? { fixtures: resolveFixtures(doc.fixtures, file) } : {}),
    ...(isRecord(doc.expect) ? { expect: readExpectations(doc.expect, file) } : {}),
  };
}

/**
 * Resolve each fixture's source against the case file, and refuse a missing one
 * HERE rather than at plant time.
 *
 * A case whose `openapi.yaml` never arrived does not fail — it runs, and the
 * agent writes the service from the prompt alone. That is a different task
 * scoring under the same name, and nothing downstream can see it happened. The
 * empty-prompt refusal above exists for the same reason.
 */
function resolveFixtures(doc: Record<string, unknown>, file: string): Record<string, string> {
  const resolved: Record<string, string> = {};
  for (const [destination, source] of Object.entries(stringMap(doc))) {
    const from = resolve(dirname(file), source);
    if (!existsSync(from)) throw new Error(`${file}: fixture '${destination}' has no source at ${source}`);
    resolved[destination] = from;
  }
  return resolved;
}

function readExpectations(doc: Record<string, unknown>, file: string): CaseExpectations {
  const known = new Set(["builds", "imports", "importsNot", "mustContain", "mustNotContain"]);
  // An unknown key is refused rather than ignored, for the reason the missing-fixture
  // refusal above exists: a case whose `mustContian:` typo is silently dropped asserts
  // nothing and reports a pass, and nothing downstream can see it happened.
  for (const key of Object.keys(doc)) {
    if (!known.has(key)) {
      throw new Error(`${file}: unknown expectation '${key}', expected one of ${[...known].join(", ")}`);
    }
  }
  return {
    ...(typeof doc.builds === "boolean" ? { builds: doc.builds } : {}),
    ...(Array.isArray(doc.imports) ? { imports: stringList(doc.imports) } : {}),
    ...(Array.isArray(doc.importsNot) ? { importsNot: stringList(doc.importsNot) } : {}),
    ...(Array.isArray(doc.mustContain) ? { mustContain: patterns(doc.mustContain, file, "mustContain") } : {}),
    ...(Array.isArray(doc.mustNotContain)
      ? { mustNotContain: patterns(doc.mustNotContain, file, "mustNotContain") }
      : {}),
  };
}

/**
 * Validate each pattern by COMPILING it here, at load, rather than at match time.
 *
 * A malformed regex discovered mid-sweep would surface as a harness error on one
 * attempt an hour in; discovered at load it fails the run before any session
 * starts, which is the only point at which it costs nothing.
 */
function patterns(value: unknown[], file: string, key: string): string[] {
  const found = stringList(value);
  for (const pattern of found) {
    try {
      new RegExp(pattern);
    } catch (e) {
      throw new Error(`${file}: ${key} pattern ${JSON.stringify(pattern)} is not a valid regex: ${String(e)}`);
    }
  }
  return found;
}

function directories(dir: string): string[] {
  return entries(dir).filter((e) => statSync(join(dir, e)).isDirectory());
}

function yamlFiles(dir: string): string[] {
  return entries(dir).filter((e) => /\.ya?ml$/.test(e));
}

/** Sorted, and without the dot-entries a checkout accumulates. */
function entries(dir: string): string[] {
  return readdirSync(dir)
    .filter((e) => !e.startsWith("."))
    .sort();
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === "object" && !Array.isArray(value);
}

function stringList(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((v): v is string => typeof v === "string") : [];
}

function stringMap(value: Record<string, unknown>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(value)) {
    if (typeof v === "string") out[k] = v;
  }
  return out;
}
