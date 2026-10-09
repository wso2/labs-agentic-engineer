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

import { test } from "node:test";
import assert from "node:assert/strict";
import * as Y from "yjs";
import { seedDocument, filesMap } from "./seed.js";
import { fragmentToMarkdown } from "@aep/collab-doc";

const bundle = [
  { path: "requirements/prd.md", content: "# PRD\n", sha: "t1" },
  { path: "design/arch.excalidraw", content: '{"type":"excalidraw-dsl"}', sha: "t2" },
];

test("seeds md files as Y.XmlFragments, others as Y.Text entries", () => {
  const doc = new Y.Doc();
  seedDocument(doc, bundle);
  const fragment = doc.getXmlFragment("requirements/prd.md");
  assert.ok(fragment.length > 0);
  assert.match(fragment.toString(), /<heading level="1">PRD<\/heading>/);
  const map = filesMap(doc);
  assert.equal(map.size, 1);
  assert.equal(
    map.get("design/arch.excalidraw")?.toString(),
    '{"type":"excalidraw-dsl"}',
  );
});

test("md fragments round-trip back to markdown (committer seam)", () => {
  const doc = new Y.Doc();
  seedDocument(doc, bundle);
  assert.equal(
    fragmentToMarkdown(doc.getXmlFragment("requirements/prd.md")).trim(),
    "# PRD",
  );
});

test("never overwrites existing entries (live content wins over reseed)", () => {
  const doc = new Y.Doc();
  seedDocument(doc, bundle);
  const fragment = doc.getXmlFragment("requirements/prd.md");
  const sizeAfterFirstSeed = fragment.length;
  const drawing = filesMap(doc).get("design/arch.excalidraw");
  assert.ok(drawing);
  drawing.insert(drawing.length, " user edit");

  seedDocument(doc, bundle);
  assert.equal(fragment.length, sizeAfterFirstSeed);
  assert.equal(
    filesMap(doc).get("design/arch.excalidraw")?.toString(),
    '{"type":"excalidraw-dsl"} user edit',
  );
});

test("seed changes replicate to a synced peer doc", () => {
  const server = new Y.Doc();
  const client = new Y.Doc();
  server.on("update", (u: Uint8Array) => Y.applyUpdate(client, u));
  seedDocument(server, bundle);
  assert.match(
    client.getXmlFragment("requirements/prd.md").toString(),
    /PRD/,
  );
});
