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
 * The whole capability, in two functions: resolve which version to read, then
 * read it.
 *
 * They are separate because the resolution rule is the part with a cost. An
 * explicit version is free; a `Dependencies.toml` is a file read; asking
 * Central is a round trip. Callers that already know the version should never
 * pay for the ones that follow.
 */

import { fetchDocs, lockedVersion, resolveLatestVersion, type HttpOptions } from "./central/client.js";
import { fromCentral } from "./from-central.js";
import { applyPatches } from "./patches.js";
import { collectReadmes, toReadmeDocument, type ModuleReadme } from "./readme.js";
import { toSyntaxString } from "./render.js";
import { err, ok, type Result } from "./result.js";
import { parseVersion, type QualifiedName, type Version } from "./qualified.js";
import type { Library } from "./model.js";

export interface ResolveOptions extends HttpOptions {
  /** An explicit version wins over everything else. */
  readonly version?: string;
  /** A component directory whose `Dependencies.toml` a build may have written. */
  readonly projectDir?: string;
}

/**
 * Which version of the package to read.
 *
 * `Dependencies.toml` outranks Central's latest deliberately: once a build has
 * resolved the package, the locked version is the one the component will
 * actually compile against, and reading a newer one produces signatures that do
 * not exist for this caller.
 */
export async function resolveVersion(
  qualified: QualifiedName,
  options: ResolveOptions = {},
): Promise<Result<Version>> {
  if (options.version !== undefined) return parseVersion(options.version);
  if (options.projectDir !== undefined) {
    const locked = lockedVersion(options.projectDir, qualified);
    if (locked !== undefined) return parseVersion(locked);
  }
  return resolveLatestVersion(qualified, options);
}

/** The guides Central holds for one published version. */
export async function loadReadmes(
  qualified: QualifiedName,
  version: Version,
  options: HttpOptions = {},
): Promise<Result<readonly ModuleReadme[]>> {
  const docs = await fetchDocs(qualified, version, options);
  if (!docs.ok) return docs;
  const readmes = collectReadmes(docs.value);
  if (readmes.length === 0) {
    return err({
      kind: "no-readme",
      qualified: `${qualified.org}/${qualified.name}:${version}`,
      suggestion: "The package publishes no guide. Read its API instead: drop --readme.",
    });
  }
  return ok(readmes);
}

/** Central's docs for one published version, as the IR. */
export async function loadLibrary(
  qualified: QualifiedName,
  version: Version,
  options: HttpOptions = {},
): Promise<Result<Library>> {
  const docs = await fetchDocs(qualified, version, options);
  if (!docs.ok) return docs;
  return ok(applyPatches(fromCentral(docs.value)));
}

/**
 * The rendered document, with the resolved coordinates on the first line.
 *
 * That header is the reason this wrapper exists: without a version stamped into
 * the output, a file left over from a previous lookup is indistinguishable from
 * a fresh one, and an agent grepping it has no way to tell.
 */
export async function renderLibrary(
  qualified: QualifiedName,
  options: ResolveOptions = {},
): Promise<Result<string>> {
  const version = await resolveVersion(qualified, options);
  if (!version.ok) return version;
  const library = await loadLibrary(qualified, version.value, options);
  if (!library.ok) return library;
  return ok(`// Resolved: ${qualified.org}/${qualified.name}:${version.value}\n${toSyntaxString(library.value)}`);
}

/**
 * The same package, read as its guide rather than its API.
 *
 * Version resolution is shared with `renderLibrary` on purpose: a guide that
 * describes a different version than the one the component compiles against is
 * worse than no guide, and `Dependencies.toml` outranking Central's latest is
 * exactly what prevents that.
 */
export async function renderReadme(
  qualified: QualifiedName,
  options: ResolveOptions = {},
): Promise<Result<string>> {
  const version = await resolveVersion(qualified, options);
  if (!version.ok) return version;
  const readmes = await loadReadmes(qualified, version.value, options);
  if (!readmes.ok) return readmes;
  return ok(toReadmeDocument(qualified, version.value, readmes.value));
}
