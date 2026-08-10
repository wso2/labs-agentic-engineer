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
 * Central's payload shape, snapshotted.
 *
 * Offline this asserts the recorded fixtures still describe what the schema
 * expects. Its real value is as the LIVE drift check: re-record the corpus
 * against Central and this diff names every field that appeared, vanished or
 * moved — including the ones the reader does not read yet, which is precisely
 * the class of change a schema that strips unknown keys cannot see.
 *
 * `AEP_BAL_UPDATE_KEYSPACE=1 pnpm test` rewrites the snapshot after a refresh.
 */

import assert from "node:assert/strict";
import { writeFileSync } from "node:fs";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { firstDifference, KEYSPACE_SNAPSHOT, listFixtures, loadRawFixture, renderKeySpace } from "./corpus.js";
import { parseCentralDocs } from "../src/central/client.js";

test("the payload key space is unchanged", () => {
  const rendered = renderKeySpace();
  if (process.env.AEP_BAL_UPDATE_KEYSPACE === "1") {
    writeFileSync(KEYSPACE_SNAPSHOT, rendered);
    return;
  }
  const difference = firstDifference(readFileSync(KEYSPACE_SNAPSHOT, "utf-8"), rendered);
  assert.equal(
    difference,
    undefined,
    `Central's payload shape changed. Review the diff, extend src/central/schema.ts if the new ` +
      `field matters, then re-record with AEP_BAL_UPDATE_KEYSPACE=1.\n${difference ?? ""}`,
  );
});

for (const slug of listFixtures()) {
  test(`${slug} still satisfies the schema`, () => {
    const parsed = parseCentralDocs(loadRawFixture(slug), slug);
    assert.equal(parsed.ok, true, parsed.ok ? "" : JSON.stringify(parsed.error, null, 2));
  });
}
