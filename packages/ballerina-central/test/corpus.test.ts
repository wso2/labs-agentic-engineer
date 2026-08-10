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
 * Coverage across Ballerina libraries: every fixture renders to exactly the
 * bytes in its snapshot.
 *
 * Offline and deterministic, so this runs on every PR in seconds. What it
 * cannot catch — Central changing under us — is `keyspace.test.ts`'s job.
 */

import assert from "node:assert/strict";
import { test } from "node:test";
import { firstDifference, listFixtures, readSnapshot, renderFixture } from "./corpus.js";

const fixtures = listFixtures();

test("the corpus is not empty", () => {
  // A misconfigured glob would otherwise make this whole file pass vacuously.
  assert.ok(fixtures.length >= 8, `expected the recorded corpus, found ${fixtures.length} fixtures`);
});

for (const slug of fixtures) {
  test(`renders ${slug} exactly as snapshotted`, () => {
    const difference = firstDifference(readSnapshot(slug), renderFixture(slug));
    assert.equal(difference, undefined, `rendered output changed for ${slug}\n${difference ?? ""}`);
  });
}
