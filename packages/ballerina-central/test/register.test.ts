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
 * The two registers, enforced mechanically.
 *
 * A document either IS Ballerina or it DESCRIBES a package, and without a test the
 * two drift back together — the earlier design produced a `client class Client {`
 * shell whose body was `// WARNING: 903 resource functions`, which looks like a
 * declaration, is not one, and invites an agent to transcribe from it.
 *
 * This is the mechanical form of that rule, run over every fixture and every verb,
 * so it also covers documents nobody has written yet.
 */

import assert from "node:assert/strict";
import { test } from "node:test";
import { toSyntaxString } from "../src/render/document.js";
import { operationsOf } from "../src/symbols.js";
import { renderOpsView } from "../src/views/ops.js";
import { renderOverview } from "../src/views/overview.js";
import { renderTypeView } from "../src/views/type.js";
import { indexDeclarations } from "../src/symbols.js";
import { listFixtures, loadedFixture } from "./corpus.js";

/**
 * Lines outside every fenced block.
 *
 * Fence tracking rather than a regex, because the guide is embedded verbatim and
 * carries its own fences — including tilde ones — and a line inside somebody
 * else's sample is a quotation, not this document's claim.
 */
function unfencedLines(document: string): string[] {
  const lines: string[] = [];
  let fence: string | undefined;
  for (const line of document.split("\n")) {
    const opener = /^\s*(`{3,}|~{3,})/.exec(line);
    if (fence === undefined && opener) {
      fence = opener[1];
      continue;
    }
    if (fence !== undefined && opener?.[1]?.startsWith(fence.slice(0, 3)) === true) {
      fence = undefined;
      continue;
    }
    if (fence === undefined) lines.push(line);
  }
  return lines;
}

/**
 * A report document without its embedded guide.
 *
 * The guide is the package author's verbatim Markdown, so its blank-line runs and
 * heading depth are its own business. Rules about THIS document's structure have to
 * stop where the quotation starts.
 */
function ownStructure(document: string): string {
  const guide = document.search(/^## Guide/m);
  return guide === -1 ? document : document.slice(0, guide);
}

/** Every report document a fixture can produce, keyed by what produced it. */
function reportDocuments(slug: string): [string, string][] {
  const context = loadedFixture(slug);
  const documents: [string, string][] = [["overview", renderOverview(context)]];

  for (const client of context.library.clients) {
    if (operationsOf(client).length === 0) continue;
    for (const sigs of [false, true]) {
      const view = renderOpsView(context, { sigs, client: client.name });
      assert.ok(view.ok);
      documents.push([`ops ${client.name}${sigs ? " --sigs" : ""}`, view.value]);
    }
  }

  const empty = renderOpsView(context, { sigs: false, path: "no/such/path" });
  if (empty.ok) documents.push(["ops (missing path)", empty.value]);

  return documents;
}

const fixtures = listFixtures();

// ---------------------------------------------------------------------------
// The report register
// ---------------------------------------------------------------------------

/**
 * Things that would read as a declaration. `type` is deliberately absent: it
 * appears in prose all the time ("read one with `type`") and inside `| Types |`,
 * and a keyword test on it would ban the English word.
 */
const LOOKS_LIKE_A_DECLARATION = /^\s*(client class|class|service|public annotation|enum|remote function|resource function|function)\b/;

for (const slug of fixtures) {
  test(`no report document for ${slug} carries a declaration outside a fence`, () => {
    for (const [label, document] of reportDocuments(slug)) {
      for (const line of unfencedLines(document)) {
        assert.doesNotMatch(line, LOOKS_LIKE_A_DECLARATION, `${slug} ${label}: this reads as source:\n  ${line}`);
      }
    }
  });
}

for (const slug of fixtures) {
  test(`no report document for ${slug} annotates itself with a // comment`, () => {
    for (const [label, document] of reportDocuments(slug)) {
      for (const line of unfencedLines(document)) {
        // In the code register a `//` comment annotates a real declaration, which is
        // what `// Special Agent Note:` does. Here it was the thing doing the
        // impersonating, and Markdown prose replaces it.
        assert.doesNotMatch(line, /^\/\//, `${slug} ${label}: a bare // comment outside a fence:\n  ${line}`);
      }
    }
  });
}

for (const slug of fixtures) {
  test(`every report document for ${slug} is navigable by heading`, () => {
    for (const [label, document] of reportDocuments(slug)) {
      // Structure is headings, so `grep '^## '` returns the document's sections.
      const sections = unfencedLines(document).filter((line) => /^## /.test(line));
      assert.ok(sections.length > 0, `${slug} ${label}: no sections`);
      assert.match(document, /^<!-- bal-library \w+ v1 -->\n# /, `${slug} ${label}: no marker and title`);
      assert.ok(document.endsWith("\n"), `${slug} ${label}: no trailing newline`);
      assert.doesNotMatch(
        ownStructure(document),
        /\n\n\n/,
        `${slug} ${label}: blank-line runs mean a block was emitted empty`,
      );
    }
  });
}

test("the overview's own sections are what grep returns, not the readme's", () => {
  // The guide's headings are demoted two levels for exactly this reason: without it
  // `grep '^## '` returns the package author's outline mixed with ours.
  const document = renderOverview(loadedFixture("ballerinax__postgresql"));
  const sections = unfencedLines(document).filter((line) => /^## /.test(line));
  assert.ok(sections.includes("## Guide"));
  assert.ok(sections.includes("## Next"));
  for (const section of sections) {
    assert.match(section, /^## (Next|Client|Guide|Errors|Module-level)/, `an unexpected top section: ${section}`);
  }
});

// ---------------------------------------------------------------------------
// The code register
// ---------------------------------------------------------------------------

for (const slug of fixtures) {
  test(`no code document for ${slug} carries report furniture`, () => {
    const context = loadedFixture(slug);
    const index = indexDeclarations(context.library.typeDefs);
    const first = index.names[0];
    assert.ok(first !== undefined);

    const documents = [
      ["api", toSyntaxString(context.library)],
      ["type", (() => {
        const view = renderTypeView(context, { names: [first], deps: true });
        assert.ok(view.ok);
        return view.value;
      })()],
    ] as const;

    for (const [label, document] of documents) {
      // A fence at the START of a line would be this document's own structure. One
      // inside a `#` doc comment is the package author's sample, and every fence in
      // the corpus's api snapshots is of that second kind — verified zero at line
      // start across all nine.
      assert.doesNotMatch(document, /^\s*```/m, `${slug} ${label}: a fence in the code register`);
      assert.doesNotMatch(document, /<!-- bal-library/, `${slug} ${label}: a report marker`);
      assert.doesNotMatch(document, /^\|.*\|$/m, `${slug} ${label}: a Markdown table`);
      // NOTE: `#` is not tested. A leading `# ` here is a Ballerina doc comment —
      // the language's own syntax — and banning it would ban the documentation.
    }
  });
}
