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
 * The package's own guide, which answers the question a signature cannot: how is
 * this used — auth, config, the shape of a call.
 *
 * Central serves it as `module.description`, byte-identical to the
 * `docs/README.md` a published `.bala` carries — verified against
 * `ballerinax/kafka@4.6.5`, 7,463 bytes, zero diff. Reading it here rather than
 * off disk is what makes it available BEFORE a build has resolved the package,
 * which is exactly when a connector nobody has written against is hardest to
 * guess at.
 *
 * It is the largest part of the overview for most packages — `postgresql` is 23.6
 * of 26KB, `graphql` 17.9 of 20.1 — and that is the right trade. It is the "how
 * is this used" answer, and the reason the recorded traces never found it is that
 * nothing put it in front of them.
 */

import type { CentralDocs } from "./central/schema.js";

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
 * Push every ATX heading down by `levels`, so an embedded guide sits under the
 * host document's outline instead of competing with it.
 *
 * This is what lets `grep '^## '` on an overview return the overview's own
 * sections rather than the readme's. Fenced blocks are skipped, because `#` at the
 * start of a line inside a fence is a shell comment, a Ballerina doc comment or a
 * Python comment — not a heading — and promoting one would corrupt a code sample
 * the agent is about to copy.
 *
 * Level 6 is the floor: HTML has no `h7`, and a heading pushed past it would stop
 * being a heading at all.
 */
export function demoteHeadings(markdown: string, levels: number): string {
  let inFence = false;
  return markdown
    .split("\n")
    .map((line) => {
      if (/^\s*(```|~~~)/.test(line)) {
        inFence = !inFence;
        return line;
      }
      if (inFence) return line;
      const heading = /^(#{1,6})(\s)/.exec(line);
      if (!heading) return line;
      const depth = Math.min(6, (heading[1] ?? "").length + levels);
      return `${"#".repeat(depth)}${line.slice((heading[1] ?? "").length)}`;
    })
    .join("\n");
}
