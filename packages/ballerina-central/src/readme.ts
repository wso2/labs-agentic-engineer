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
 * The other document a package can answer with: its own guide.
 *
 * `render.ts` turns the payload into Ballerina; this turns the same payload
 * into the Markdown the package's authors wrote. They are separate because they
 * answer different questions — a signature is looked up by name, a guide is
 * read from the top — and because the guide is prose we pass through untouched.
 *
 * Central serves it as `module.description`, byte-identical to the `docs/README.md`
 * a published `.bala` carries. Reading it here rather than off disk is what makes
 * it available before a build has resolved the package at all.
 */

import type { CentralDocs } from "./central/schema.js";
import type { QualifiedName, Version } from "./qualified.js";

/** One module's guide. A package publishes one document per module. */
export interface ModuleReadme {
  /** The module id — `kafka`, or `googleapis.gmail`. */
  readonly module: string;
  readonly markdown: string;
}

/**
 * Every module that wrote a guide, in Central's order.
 *
 * Modules without one are dropped rather than emitted empty: a heading with
 * nothing under it reads as a truncated download.
 */
export function collectReadmes(docs: CentralDocs): readonly ModuleReadme[] {
  return docs.docsData.modules
    .map((module) => ({ module: module.id, markdown: (module.description ?? "").trim() }))
    .filter((readme) => readme.markdown !== "");
}

/**
 * The document written to stdout.
 *
 * Both stamps are HTML comments: they survive `grep`, and every Markdown reader
 * — including the one an agent pipes this into — renders them as nothing. The
 * version stamp exists for the reason `renderLibrary`'s does, that a file left
 * over from an earlier lookup is otherwise indistinguishable from a fresh one.
 * The module stamp is emitted even for the single-module packages that are the
 * common case, so the format never depends on the package.
 */
export function toReadmeDocument(
  qualified: QualifiedName,
  version: Version,
  readmes: readonly ModuleReadme[],
): string {
  const header = `<!-- Resolved: ${qualified.org}/${qualified.name}:${version} -->\n`;
  return header + readmes.map((readme) => `<!-- Module: ${readme.module} -->\n${readme.markdown}\n`).join("\n");
}
