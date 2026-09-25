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
import {
  applyTextEdit,
  deleteDocFile,
  filesMap,
  isMarkdownPath,
  listDocPaths,
  readDocFile,
  setDocFile,
  snapshotDoc,
} from "../src/index.js";

test("setDocFile creates and diff-patches Y.Text files", () => {
  const doc = new Y.Doc();
  setDocFile(doc, "design/arch.excalidraw", '{"v":1}');
  assert.equal(readDocFile(doc, "design/arch.excalidraw"), '{"v":1}');

  setDocFile(doc, "design/arch.excalidraw", '{"v":2}');
  assert.equal(readDocFile(doc, "design/arch.excalidraw"), '{"v":2}');
});

test("applyTextEdit patches without destroying concurrent inserts elsewhere", () => {
  const doc = new Y.Doc();
  setDocFile(doc, "notes.txt", "alpha\nbeta\ngamma\n");
  const ytext = filesMap(doc).get("notes.txt")!;

  // Agent computes "beta" -> "BETA" against a snapshot; a user then edits
  // gamma before it applies. Diff-and-patch runs against apply-time text.
  ytext.insert(ytext.toString().indexOf("gamma"), "user-");
  applyTextEdit(ytext, "alpha\nBETA\nuser-gamma\n", "agent");
  assert.equal(ytext.toString(), "alpha\nBETA\nuser-gamma\n");
});

test("markdown files round-trip through fragments", () => {
  const doc = new Y.Doc();
  setDocFile(doc, "requirements/prd.md", "# Title\n\nSome body text.");
  assert.match(doc.getXmlFragment("requirements/prd.md").toString(), /Title/);
  assert.equal(
    readDocFile(doc, "requirements/prd.md")?.trim(),
    "# Title\n\nSome body text.",
  );

  setDocFile(doc, "requirements/prd.md", "# New\n\nReplaced.");
  assert.equal(readDocFile(doc, "requirements/prd.md")?.trim(), "# New\n\nReplaced.");
});

test("fragment edits replicate to synced peers in one update", () => {
  const server = new Y.Doc();
  const client = new Y.Doc();
  server.on("update", (u: Uint8Array) => Y.applyUpdate(client, u));
  setDocFile(server, "requirements/prd.md", "# Seeded");
  setDocFile(server, "requirements/prd.md", "# Agent rewrite");
  assert.match(client.getXmlFragment("requirements/prd.md").toString(), /Agent rewrite/);
});

test("transactions carry the caller's origin", () => {
  const doc = new Y.Doc();
  const origins: unknown[] = [];
  doc.on("afterTransaction", (tr: Y.Transaction) => origins.push(tr.origin));
  setDocFile(doc, "requirements/prd.md", "# X", "agent:test");
  setDocFile(doc, "data.json", "{}", "agent:test");
  assert.ok(origins.every((o) => o === "agent:test"));
});

test("listDocPaths and snapshotDoc cover both shapes", () => {
  const doc = new Y.Doc();
  setDocFile(doc, "requirements/prd.md", "# P");
  setDocFile(doc, "design/arch.excalidraw", "{}");
  assert.deepEqual(listDocPaths(doc), [
    "design/arch.excalidraw",
    "requirements/prd.md",
  ]);
  const snap = snapshotDoc(doc);
  assert.equal(snap["design/arch.excalidraw"], "{}");
  assert.match(snap["requirements/prd.md"] ?? "", /# P/);
});

test("deleteDocFile removes text entries and empties fragments", () => {
  const doc = new Y.Doc();
  setDocFile(doc, "requirements/prd.md", "# P");
  setDocFile(doc, "data.json", "{}");
  deleteDocFile(doc, "data.json");
  deleteDocFile(doc, "requirements/prd.md");
  assert.equal(readDocFile(doc, "data.json"), undefined);
  assert.equal(doc.getXmlFragment("requirements/prd.md").length, 0);
});

// --- agent.afm.md is structured, not prose -----------------------------------

test("agent.afm.md is not treated as prose markdown", () => {
  assert.equal(isMarkdownPath("specs/design/components/x/agent.afm.md"), false);
  // Prose documents are unaffected — including user-authored feature docs.
  assert.equal(isMarkdownPath("specs/requirements/prd.md"), true);
  assert.equal(isMarkdownPath("specs/requirements/features/booking.md"), true);
  assert.equal(isMarkdownPath("specs/design/design.md"), true);
  assert.equal(isMarkdownPath("specs/design/security.md"), true);
  // Non-markdown is unchanged.
  assert.equal(isMarkdownPath("specs/design/components/x/design.json"), false);
});

test("an agent.afm.md round-trips byte-exact through the doc", () => {
  // The real shape that was corrupted in production: YAML front matter whose
  // underscores, `>` and list indentation a markdown serializer would mangle
  // into `spec\_version`, `&gt;` and de-indented keys.
  const afm = [
    "---",
    'spec_version: "0.4.0"',
    "description: >",
    "  One sentence.",
    "max_iterations: 12",
    "model:",
    "  provider: \"anthropic\"",
    '  api_key: "${env:MODEL_API_KEY}"',
    "interfaces:",
    "  - type: webchat",
    "    exposure:",
    "      http:",
    '        path: "/chat"',
    "---",
    "",
    "# Role",
    "",
    "You help people.",
    "",
  ].join("\n");

  const doc = new Y.Doc();
  const path = "specs/design/components/lunch-agent/agent.afm.md";
  setDocFile(doc, path, afm);
  assert.equal(readDocFile(doc, path), afm);
});
