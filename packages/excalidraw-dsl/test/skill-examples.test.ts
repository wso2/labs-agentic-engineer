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
 * The `wireframes` skill teaches the DSL by example, and an agent copies those
 * examples verbatim. An example this compiler rejects therefore becomes an
 * INVALID_DSL the agent cannot diagnose — the skill told it to write that.
 * So every complete DSL document in the skill is held to the same bar as a
 * document an agent writes: it parses, it lays out, and it compiles.
 *
 * Caught in practice: a `flow` block whose member line carried a trailing
 * `// comment`, which the flow parser matches whole and rejects.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import {
  dslToExcalidraw,
  validateWireframeLayout,
  validateWireframeSyntax,
} from "../src/index.js";

const here = dirname(fileURLToPath(import.meta.url));
const skillDir = join(here, "..", "..", "..", "skills", "wireframes");

/**
 * Every skill file that may carry DSL, with how many complete documents it is
 * expected to hold. The count is asserted, so a fence renamed or an example
 * deleted fails here instead of silently reducing this suite to a no-op.
 *
 * `implementing.md` expects ZERO on purpose: it explains how a DSL becomes
 * routes and never quotes one. It stays in the list so that adding a DSL
 * example to it later is caught and checked.
 */
const SKILL_FILES: ReadonlyArray<{ file: string; documents: number }> = [
  { file: "SKILL.md", documents: 1 },
  { file: "references/authoring.md", documents: 1 },
  { file: "references/implementing.md", documents: 0 },
];

/**
 * A fenced block turned into something the compiler can accept, or "" for a
 * block that is not DSL at all. The grammar template is excluded on purpose:
 * it is pseudo-syntax (`screen <Name>`, `<kind> "<label>"`) illustrating the
 * shape of a line, not a document.
 *
 * A snippet showing only `flow` blocks still has to parse, but the compiler
 * needs the screens those flows reference — so they are synthesized. A member
 * line the flow parser would reject (a trailing comment, say) does not match
 * as a name, gets no stub, and surfaces as the parse error it really is.
 */
function asDocument(block: string): string {
  if (block.includes("<Name>") || block.includes("<kind>")) return "";
  if (/^screen\s+\w/m.test(block)) return block;
  if (!/^flow\s/m.test(block)) return "";

  const referenced = new Set<string>();
  for (const line of block.split("\n")) {
    const member = /^\s{2,}([A-Za-z_]\w*)\s*$/.exec(line);
    if (member) referenced.add(member[1]);
  }
  if (referenced.size === 0) return "";

  const stubs = [...referenced]
    .map((name) => `screen ${name} "stub for ${name}"\n  heading "${name}"\n`)
    .join("\n");
  return `${stubs}\n${block}`;
}

function dslExamples(markdown: string): string[] {
  return [...markdown.matchAll(/```text\n([\s\S]*?)```/g)]
    .map((m) => asDocument(m[1]))
    .filter((doc) => doc !== "");
}

for (const { file, documents } of SKILL_FILES) {
  const markdown = readFileSync(join(skillDir, file), "utf8");
  const examples = dslExamples(markdown);

  test(`${file}: every DSL example is valid`, () => {
    assert.equal(
      examples.length,
      documents,
      `${file} should yield ${documents} DSL document(s) — update SKILL_FILES when that changes on purpose`,
    );
    for (const [i, dsl] of examples.entries()) {
      const where = `${file} example #${i + 1}`;
      assert.deepEqual(validateWireframeSyntax(dsl), [], `${where}: syntax`);
      assert.deepEqual(validateWireframeLayout(dsl), [], `${where}: layout`);
      assert.doesNotThrow(() => dslToExcalidraw("wireframes", dsl), `${where}: compile`);
    }
  });
}

test("the worked example is actually being checked", () => {
  // A guard on the extractor: a truncated worked example would still satisfy
  // the count above, so assert it is the substantial multi-screen document.
  const authoring = readFileSync(join(skillDir, "references", "authoring.md"), "utf8");
  const examples = dslExamples(authoring);
  assert.ok(
    examples.some((e) => e.split("\n").length > 50),
    "the worked example should be a substantial multi-screen document",
  );
});
