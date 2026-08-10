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
 * The report documents, snapshotted, plus the composition rules that decide their
 * shape.
 *
 * `views-agree.test.ts` proves a view never invents a signature; this proves the
 * documents themselves do not change silently. The two together are why a
 * rendering change has to be a reviewable diff rather than something an agent
 * discovers at run time.
 *
 * Snapshots are rendered under a FIXED provenance and a fixed version, because the
 * real header is run-order-dependent — the same command prints `central` then
 * `cache` — and a snapshot that encoded the live one would fail on its second run
 * instead of on a regression.
 */

import assert from "node:assert/strict";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { operationsOf } from "../src/symbols.js";
import { MAX_INLINE_OPERATIONS, MAX_INLINE_SIGNATURE_BYTES, renderOverview } from "../src/views/overview.js";
import { renderOpsView } from "../src/views/ops.js";
import { firstDifference, libraryFor, listFixtures, loadedFixture, SNAPSHOTS_DIR } from "./corpus.js";

function viewSnapshotPath(slug: string, view: string): string {
  return join(SNAPSHOTS_DIR, `${slug}.${view}.md`);
}

/**
 * Compare against the committed document, or write it when `UPDATE_SNAPSHOTS=1`.
 *
 * The escape hatch is deliberate and narrow: these documents are 8 to 45KB and
 * hand-editing them after an intentional change is not review, it is transcription.
 */
function matchesSnapshot(slug: string, view: string, rendered: string): void {
  const path = viewSnapshotPath(slug, view);
  if (process.env["UPDATE_SNAPSHOTS"] === "1") {
    writeFileSync(path, rendered);
    return;
  }
  const expected = readFileSync(path, "utf-8");
  const difference = firstDifference(expected, rendered);
  assert.equal(difference, undefined, `${slug} ${view} changed\n${difference ?? ""}`);
}

const fixtures = listFixtures();

for (const slug of fixtures) {
  test(`the overview of ${slug} is unchanged`, () => {
    matchesSnapshot(slug, "overview", renderOverview(loadedFixture(slug)));
  });
}

for (const slug of fixtures) {
  test(`the ops tree of ${slug} is unchanged`, () => {
    const client = firstOperationalClient(slug);
    const view = renderOpsView(loadedFixture(slug), { sigs: false, ...(client === undefined ? {} : { client }) });
    assert.ok(view.ok, `ops failed for ${slug}`);
    matchesSnapshot(slug, "ops", view.value);
  });
}

/**
 * The client `ops` would resolve on its own, named explicitly so the snapshot does
 * not move when a package gains a second one.
 */
function firstOperationalClient(slug: string): string | undefined {
  const client = libraryFor(slug).clients.find((candidate) => operationsOf(candidate).length > 0);
  return client?.name;
}

// ---------------------------------------------------------------------------
// The composition rules
// ---------------------------------------------------------------------------

/**
 * Which fixtures take the path tree instead of an inline listing, and why.
 *
 * Both halves of the rule are asserted separately, because on this corpus they
 * agree and a test that only checked the outcome would not notice one of them
 * being dropped.
 */
test("the listing rule fires exactly where the measurements say it should", () => {
  const expected = new Map([
    ["ballerinax__github", true],
    ["ballerinax__slack", true],
    ["ballerinax__googleapis.gmail", false],
    ["ballerinax__sap", false],
    ["ballerina__http", false],
  ]);

  for (const [slug, tree] of expected) {
    const document = renderOverview(loadedFixture(slug));
    assert.equal(
      /\*\*Not listed here\*\*/.test(document),
      tree,
      `${slug} should ${tree ? "" : "not "}fall back to the tree`,
    );
  }
});

test("each half of the listing rule is load-bearing on its own", () => {
  for (const slug of fixtures) {
    for (const client of libraryFor(slug).clients) {
      const operations = operationsOf(client);
      if (operations.length === 0) continue;
      const bytes = operations.reduce(
        (sum, operation) => sum + Buffer.byteLength(operation.fn.description, "utf-8") + 200,
        0,
      );
      // The count is the readable limit; the byte guard is what stops a verbose
      // 80-operation connector from producing a 40KB document that passes the count.
      // At gmail's ~480 bytes per operation, 100 operations is about 48KB.
      if (operations.length > MAX_INLINE_OPERATIONS) {
        assert.ok(bytes > MAX_INLINE_SIGNATURE_BYTES, `${slug} ${client.name}: over the count is also over the bytes`);
      }
    }
  }
});

test("the largest overview in the corpus is ballerina/http, and --client narrows it", () => {
  // Worth pinning because it is counter-intuitive: the listing rule is PER CLIENT and
  // http has eight, none of which individually exceeds either limit.
  const sizes = fixtures
    .map((slug) => ({ slug, bytes: Buffer.byteLength(renderOverview(loadedFixture(slug)), "utf-8") }))
    .sort((a, b) => b.bytes - a.bytes);
  assert.equal(sizes[0]?.slug, "ballerina__http");

  const narrowed = Buffer.byteLength(renderOverview(loadedFixture("ballerina__http"), { client: "Client" }), "utf-8");
  assert.ok(narrowed < (sizes[0]?.bytes ?? 0) / 1.5, "--client should roughly halve it");
});

test("a package with no clients is a normal case, not an error", () => {
  // An `io`-shaped package is all module-level functions. Nothing in the corpus has
  // zero clients, so this is asserted against a library with them removed.
  const context = loadedFixture("ballerina__http");
  const document = renderOverview({ ...context, library: { ...context.library, clients: [] } });
  assert.match(document, /\| Clients \| none — the callable surface is module-level functions \|/);
  assert.match(document, /^## Module-level functions — 7, call with `\.`$/m);
});

test("--client naming a client that does not exist says so instead of printing an empty document", () => {
  const document = renderOverview(loadedFixture("ballerina__http"), { client: "Nope" });
  assert.match(document, /\*\*No client named `Nope`\.\*\*/);
  assert.doesNotMatch(document, /^## Client /m);
});

test("errors are listed, and only for the packages that declare any", () => {
  // The one type family that stays: `ballerinax/github` declares zero errors and all
  // 903 of its operations return the language-level `error`, so nothing in its API
  // document names `http:ClientRequestError` — the lookup eight of nine recorded runs
  // came for.
  const withErrors = new Map([
    ["ballerina__http", 56],
    ["ballerina__graphql", 9],
    ["ballerinax__kafka", 3],
    ["ballerinax__googleapis.gmail", 3],
    ["ballerinax__googleapis.sheets", 3],
    ["ballerinax__sap", 1],
  ]);

  for (const slug of fixtures) {
    const document = renderOverview(loadedFixture(slug));
    const expected = withErrors.get(slug);
    if (expected === undefined) {
      assert.doesNotMatch(document, /^## Errors/m, `${slug} declares none, so the section is omitted`);
      assert.match(document, /\| Errors \| none declared; operations return the language-level `error` \|/);
      continue;
    }
    assert.match(document, new RegExp(`^## Errors — ${expected}$`, "m"), slug);
  }
});

test("the subtype chain is what the Errors section is for", () => {
  const document = renderOverview(loadedFixture("ballerina__http"));
  // Unlearnable before A1: all 56 rendered as `type X error;`.
  assert.match(document, /^type ClientRequestError distinct \(ApplicationResponseError&error<Detail>\);$/m);
  assert.match(document, /^type SslError distinct ClientError;$/m);
  assert.match(document, /^type Error distinct error;$/m);
});

test("the guide goes last, demoted, so the overview's own outline survives", () => {
  const document = renderOverview(loadedFixture("ballerinax__postgresql"));
  const guide = document.indexOf("\n## Guide\n");
  assert.ok(guide > 0);
  assert.ok(document.indexOf("\n## Client ") < guide, "the API half comes first");
  // postgresql is 23.6 of 26KB guide, which is the right trade: it is the "how is
  // this used" answer and the reason the recorded traces never found it.
  assert.ok(document.length - guide > (document.length * 2) / 3);
  assert.doesNotMatch(document.slice(guide + 10), /^## /m, "a readme heading must not become a top-level section");
});
