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

// The cases for ../app/mock/authz/contract.ts. NOT named *.test.mjs on purpose:
// importing a .ts module needs `--experimental-strip-types` on the Node this
// repo pins, and the repo-wide runner (`node --test $(find skills -name
// '*.test.mjs')`) passes no flags. ./mock-contract.test.mjs is the wrapper that
// runs this file with the flag; run it directly with
//
//   node --experimental-strip-types --test mock-contract.cases.mjs
//
// when you want the per-case output.
//
// What is being pinned: the mock gateway refuses exactly what the DEPLOYED
// gateway refuses. Every rule below is the one aep-api applies when it renders
// the real RestApi (internal/spec/openapi_operations.go), so a contract that
// reads one way in mock mode cannot read another way in a cell.

import { test } from "node:test";
import assert from "node:assert/strict";
import { projectOperations } from "../app/mock/authz/contract.ts";

/** A minimal document that satisfies every structural rule. */
function doc(paths, overrides = {}) {
  return {
    openapi: "3.0.3",
    components: { securitySchemes: { oauth2: { type: "oauth2", flows: {} } } },
    security: [{ oauth2: [] }],
    paths,
    ...overrides,
  };
}

const find = (ops, method, path) => ops.find((o) => o.method === method && o.path === path);

test("an operation's declared handle is the one the gateway wants", () => {
  const ops = projectOperations(
    doc({ "/claims": { get: { security: [{ oauth2: ["claims:read"] }] } } }),
    "t.yaml",
  );
  assert.equal(find(ops, "GET", "/claims").scope, "claims:read");
  assert.equal(find(ops, "GET", "/claims").isPublic, false);
});

test("`security: []` is public — the handler reads no identity", () => {
  const ops = projectOperations(doc({ "/health": { get: { security: [] } } }), "t.yaml");
  assert.equal(find(ops, "GET", "/health").isPublic, true);
  assert.equal(find(ops, "GET", "/health").scope, null);
});

// The two signed-in spellings have to agree: one is the document default
// inherited, the other is that default written out on the operation.
test("no security block, and an empty oauth2 list, both mean signed-in", () => {
  const ops = projectOperations(
    doc({ "/me": { get: {} }, "/also-me": { get: { security: [{ oauth2: [] }] } } }),
    "t.yaml",
  );
  for (const path of ["/me", "/also-me"]) {
    assert.equal(find(ops, "GET", path).scope, null, path);
    assert.equal(find(ops, "GET", path).isPublic, false, path);
  }
});

// The document default is what makes a FORGOTTEN security block fail closed.
// Without it, "absent" would read as public and the mock would serve what the
// cell refuses.
test("a document with no signed-in default is refused", () => {
  assert.throws(
    () => projectOperations(doc({ "/me": { get: {} } }, { security: undefined }), "t.yaml"),
    /declares no `security: \[\{oauth2: \[\]\}\]` default/,
  );
  assert.throws(
    () => projectOperations(doc({ "/me": { get: {} } }, { security: [{ oauth2: ["x:y"] }] }), "t.yaml"),
    /declares no `security: \[\{oauth2: \[\]\}\]` default/,
  );
});

test("a contract with no oauth2 scheme is not a gateway-fronted API", () => {
  const legacy = {
    components: { securitySchemes: { bearerAuth: { type: "http" } } },
    paths: { "/x": { get: {} } },
  };
  assert.equal(projectOperations(legacy, "t.yaml"), null);
});

test("two requirements, two schemes in one, or two scopes are all refused", () => {
  const cases = [
    [{ oauth2: ["a:b"] }, { oauth2: ["c:d"] }],
    [{ oauth2: ["a:b"], apiKey: [] }],
    [{ oauth2: ["a:b", "c:d"] }],
  ];
  for (const security of cases) {
    assert.throws(
      () => projectOperations(doc({ "/x": { get: { security } } }), "t.yaml"),
      /more than one requirement|names 2 schemes|names 2 scopes/,
      JSON.stringify(security),
    );
  }
});

// An OIDC scope rides EVERY access token, so requiring one admits every
// signed-in account while looking guarded.
test("an OIDC scope as an operation's handle is refused", () => {
  assert.throws(
    () => projectOperations(doc({ "/x": { get: { security: [{ oauth2: ["openid"] }] } } }), "t.yaml"),
    /rides every token/,
  );
});

test("a path template matches one segment, and only that path", () => {
  const ops = projectOperations(
    doc({ "/claims/{claimId}/approve": { post: { security: [{ oauth2: ["claims:approve"] }] } } }),
    "t.yaml",
  );
  const re = new RegExp(find(ops, "POST", "/claims/{claimId}/approve").pattern);
  assert.ok(re.test("/claims/abc/approve"));
  assert.ok(!re.test("/claims/abc/def/approve"), "a parameter must not span a /");
  assert.ok(!re.test("/claims/abc/approve/extra"));
  assert.ok(!re.test("/claims//approve"), "an empty segment is not a parameter");
});

// Scanned in order, first match wins — so a literal sibling has to sort ahead
// of the template that would otherwise swallow it.
test("a literal path sorts before the template that would swallow it", () => {
  const ops = projectOperations(
    doc({
      "/todos/{id}": { get: { security: [{ oauth2: ["todos:read"] }] } },
      "/todos/archived": { get: { security: [{ oauth2: ["todos:read-all"] }] } },
    }),
    "t.yaml",
  );
  const first = ops.find((o) => new RegExp(o.pattern).test("/todos/archived"));
  assert.equal(first.path, "/todos/archived");
});

test("HEAD and TRACE are skipped, not refused", () => {
  const ops = projectOperations(
    doc({ "/x": { get: {}, head: {}, trace: {} } }),
    "t.yaml",
  );
  assert.deepEqual(ops.map((o) => o.method), ["GET"]);
});

// A key under `paths` that is not an operation — `parameters`, `summary`, an
// x- extension — must not become a row.
test("non-operation keys under a path item are ignored", () => {
  const ops = projectOperations(
    doc({ "/x": { get: {}, parameters: [], summary: "things" } }),
    "t.yaml",
  );
  assert.deepEqual(ops.map((o) => o.method), ["GET"]);
});

test("every message names the file it came from", () => {
  assert.throws(
    () => projectOperations(doc({ "/x": { get: { security: "nope" } } }), "contracts/todo.yaml"),
    /contracts\/todo\.yaml: GET \/x/,
  );
});
