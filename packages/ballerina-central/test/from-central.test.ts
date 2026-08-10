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
 * Which module of a payload gets rendered.
 *
 * Every fixture in the corpus is single-module, so this is the one behaviour the
 * corpus cannot test by construction — and the one the cache's coordinate check
 * depends on, since verifying one module and rendering another verifies nothing.
 * The payloads here are therefore assembled rather than recorded.
 */

import assert from "node:assert/strict";
import { test } from "node:test";
import { parseCentralDocs } from "../src/central/client.js";
import type { CentralDocs } from "../src/central/schema.js";
import { fromCentral, selectModule } from "../src/from-central.js";
import { parseQualifiedName, type QualifiedName } from "../src/qualified.js";
import { loadRawFixture } from "./corpus.js";

function qualified(name: string): QualifiedName {
  const parsed = parseQualifiedName(name);
  assert.ok(parsed.ok);
  return parsed.value;
}

/**
 * A recorded payload rearranged into several modules, so "the requested one" and
 * "the first one" are different answers. `kafka` is the donor because it is small
 * and its module carries every array the schema requires.
 */
function multiModule(ids: readonly string[], org = "ballerinax"): CentralDocs {
  const raw = structuredClone(loadRawFixture("ballerinax__kafka")) as {
    docsData: { modules: { id: string; orgName: string; summary?: string }[] };
  };
  const template = raw.docsData.modules[0];
  assert.ok(template);
  raw.docsData.modules = ids.map((id) => ({ ...structuredClone(template), id, orgName: org, summary: `I am ${id}` }));
  const parsed = parseCentralDocs(raw, "assembled");
  assert.ok(parsed.ok, JSON.stringify(parsed.ok ? "" : parsed.error));
  return parsed.value;
}

test("the requested module is rendered, not whichever one Central listed first", () => {
  const docs = multiModule(["other", "kafka", "another"]);
  const selected = selectModule(docs, qualified("ballerinax/kafka"));
  assert.ok(selected.ok);
  assert.equal(fromCentral(selected.value).name, "ballerinax/kafka");
  assert.equal(fromCentral(selected.value).description, "I am kafka");
});

test("a submodule is reached through its dotted id, the way Central names it", () => {
  const docs = multiModule(["googleapis", "googleapis.gmail"]);
  const selected = selectModule(docs, qualified("ballerinax/googleapis.gmail"));
  assert.ok(selected.ok);
  assert.equal(fromCentral(selected.value).name, "ballerinax/googleapis.gmail");
});

test("the org has to match too, so a same-named module from another org is not substituted", () => {
  const docs = multiModule(["kafka"], "someoneelse");
  const selected = selectModule(docs, qualified("ballerinax/kafka"));
  assert.equal(selected.ok, false);
});

test("no matching module fails loudly and names what Central actually returned", () => {
  const docs = multiModule(["notkafka"]);
  const selected = selectModule(docs, qualified("ballerinax/kafka"));
  assert.equal(selected.ok, false);
  if (selected.ok) return;
  assert.equal(selected.error.kind, "schema-drift");
  assert.match(JSON.stringify(selected.error), /ballerinax\/notkafka/);
  assert.match(JSON.stringify(selected.error), /suggestion/);
});
