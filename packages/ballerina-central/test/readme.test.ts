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
 * `--readme` exists because the `.bala` tree's `docs/README.md` was the one
 * thing the API document could not replace, and Central ships that same file in
 * the payload we already fetch. These tests hold both halves of that claim: the
 * guide comes through untouched, and every package in the corpus has one.
 */

import assert from "node:assert/strict";
import { test } from "node:test";
import { collectReadmes, toReadmeDocument } from "../src/readme.js";
import { parseQualifiedName, parseVersion } from "../src/qualified.js";
import type { CentralDocs } from "../src/central/schema.js";
import { listFixtures, loadFixture } from "./corpus.js";

function coordinates(qualifiedName: string, version: string) {
  const qualified = parseQualifiedName(qualifiedName);
  const parsedVersion = parseVersion(version);
  assert.ok(qualified.ok && parsedVersion.ok);
  return { qualified: qualified.value, version: parsedVersion.value };
}

test("every package in the corpus publishes a guide", () => {
  for (const slug of listFixtures()) {
    const readmes = collectReadmes(loadFixture(slug));
    assert.ok(readmes.length > 0, `${slug} has no README — --readme would fail for it`);
    assert.ok(
      readmes.every((readme) => readme.markdown.length > 500),
      `${slug} has a stub README`,
    );
  }
});

test("the guide is passed through untouched, only trimmed", () => {
  const docs = loadFixture("ballerinax__kafka");
  const [readme] = collectReadmes(docs);
  assert.ok(readme !== undefined);
  assert.equal(readme.module, "kafka");
  // The bytes Central serves, which are the bytes the published .bala keeps at
  // docs/README.md — verified identical for this exact version.
  assert.equal(readme.markdown, (docs.docsData.modules[0]?.description ?? "").trim());
  assert.match(readme.markdown, /^## Overview/);
});

test("the document stamps the resolved version, so a stale file is visible", () => {
  const { qualified, version } = coordinates("ballerinax/kafka", "4.6.5");
  const document = toReadmeDocument(qualified, version, collectReadmes(loadFixture("ballerinax__kafka")));
  assert.match(document, /^<!-- Resolved: ballerinax\/kafka:4\.6\.5 -->\n/);
});

test("a module heading is emitted even when there is only one, so the format never varies", () => {
  const { qualified, version } = coordinates("ballerinax/kafka", "4.6.5");
  const document = toReadmeDocument(qualified, version, collectReadmes(loadFixture("ballerinax__kafka")));
  assert.equal(document.match(/^<!-- Module: /gm)?.length, 1);
  assert.match(document, /<!-- Module: kafka -->\n## Overview/);
});

test("a multi-module package separates its guides by module", () => {
  const { qualified, version } = coordinates("ballerinax/two", "1.0.0");
  const document = toReadmeDocument(qualified, version, [
    { module: "two", markdown: "# Root" },
    { module: "two.sub", markdown: "# Sub" },
  ]);
  assert.equal(
    document,
    "<!-- Resolved: ballerinax/two:1.0.0 -->\n<!-- Module: two -->\n# Root\n\n<!-- Module: two.sub -->\n# Sub\n",
  );
});

test("a module without a guide is dropped rather than emitted as an empty heading", () => {
  const docs = {
    docsData: { modules: [{ id: "a", description: "   " }, { id: "b", description: "# B" }] },
  } as unknown as CentralDocs;
  assert.deepEqual(collectReadmes(docs), [{ module: "b", markdown: "# B" }]);
});
