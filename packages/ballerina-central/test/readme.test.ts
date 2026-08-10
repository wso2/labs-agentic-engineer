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
 * The package's own guide: that it is there, that it is passed through untouched,
 * and that embedding it does not corrupt the code samples inside it.
 *
 * The last one is the risk worth a test. The guide is the section an agent copies
 * a working call out of, and a heading transform that reached inside a fenced
 * block would rewrite `# comment` lines in Ballerina, shell and Python samples.
 */

import assert from "node:assert/strict";
import { test } from "node:test";
import { collectReadmes, demoteHeadings } from "../src/readme.js";
import type { CentralDocs } from "../src/central/schema.js";
import { listFixtures, loadFixture } from "./corpus.js";

/** One module's worth of payload, with only the fields `collectReadmes` reads. */
function docsWith(modules: readonly { id: string; description?: string }[]): CentralDocs {
  return { docsData: { modules } } as unknown as CentralDocs;
}

test("every package in the corpus publishes a guide", () => {
  // The overview leans on this: the guide is most packages' largest section and
  // the answer to "how is this used". A fixture without one would make the
  // overview tests pass for the wrong reason.
  for (const slug of listFixtures()) {
    const readmes = collectReadmes(loadFixture(slug));
    assert.ok(readmes.length >= 1, `${slug} publishes no guide`);
    assert.ok((readmes[0]?.markdown.length ?? 0) > 500, `${slug}'s guide is suspiciously short`);
  }
});

test("the guide is passed through untouched, only trimmed", () => {
  const docs = docsWith([{ id: "kafka", description: "\n\n## Overview\n\nbody\n\n" }]);
  assert.deepEqual(collectReadmes(docs), [{ module: "kafka", markdown: "## Overview\n\nbody" }]);
});

test("a module without a guide is dropped rather than carried as an empty section", () => {
  const docs = docsWith([{ id: "a", description: "   " }, { id: "b" }, { id: "c", description: "real" }]);
  assert.deepEqual(
    collectReadmes(docs).map((readme) => readme.module),
    ["c"],
  );
});

test("headings are demoted so the host document keeps one outline", () => {
  assert.equal(demoteHeadings("# Top\n## Second\ntext", 2), "### Top\n#### Second\ntext");
});

test("a heading cannot be demoted past level 6, because that stops being a heading", () => {
  assert.equal(demoteHeadings("##### Five\n###### Six", 2), "###### Five\n###### Six");
});

test("a # inside a fenced block is left alone, because it is a comment in someone's sample", () => {
  const guide = ["## Setup", "", "```ballerina", "# The star count.", "int stars = 0;", "```", "", "## Next"].join("\n");
  const demoted = demoteHeadings(guide, 2);
  assert.match(demoted, /^#### Setup$/m);
  assert.match(demoted, /^#### Next$/m);
  assert.match(demoted, /^# The star count\.$/m, "a Ballerina doc comment must survive verbatim");
});

test("a tilde fence counts as a fence too", () => {
  assert.equal(demoteHeadings("~~~\n# not a heading\n~~~", 2), "~~~\n# not a heading\n~~~");
});

test("a line that only looks like a heading is left alone", () => {
  // No space after the hashes, so it is not an ATX heading.
  assert.equal(demoteHeadings("#hashtag\n####### seven", 2), "#hashtag\n####### seven");
});
