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

/**
 * Write-gate behavior for `specs/design/roles.json`. These assert the zod source
 * of truth directly; the Go save-gate (internal/platform/rolesspec) validates
 * the SAME published JSON Schema plus the same referential rules, and has its
 * own parity tests — a document that passes one gate MUST pass the other.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { checkRolesDesign } from "../src/roles-design-schema.ts";
import { FileBundle } from "../src/bundle.ts";

const PATH = "specs/design/roles.json";

/** A minimal valid roles document. */
function roles(overrides: Record<string, unknown> = {}): string {
  return JSON.stringify({
    version: 1,
    coldStartRole: "Viewer",
    publicComponents: [],
    roles: [
      {
        name: "Viewer",
        description: "Reads submitted claims.",
        stories: [1],
        grantedBy: "first sign-in",
        permissions: [{ component: "expense-api", actions: ["read own claims"] }],
      },
    ],
    testUsers: [{ username: "test-viewer", role: "Viewer" }],
    ...overrides,
  });
}

test("a well-formed roles document passes", () => {
  assert.equal(checkRolesDesign(PATH, roles()), null);
});

test("the gate claims only specs/design/roles.json", () => {
  assert.equal(checkRolesDesign("specs/design/components/api/roles.json", "not json"), null);
  assert.equal(checkRolesDesign("roles.json", "not json"), null);
});

test("unparseable JSON is INVALID_JSON", () => {
  const problem = checkRolesDesign(PATH, "{");
  assert.equal(problem?.code, "INVALID_JSON");
});

test("an unknown property is rejected — no secret can be smuggled in", () => {
  const problem = checkRolesDesign(
    PATH,
    roles({
      testUsers: [{ username: "test-viewer", role: "Viewer", password: "hunter2" }],
    }),
  );
  assert.equal(problem?.code, "SCHEMA_VIOLATION");
  assert.match(problem!.message, /password/);
});

test("a version other than 1 is rejected", () => {
  assert.equal(checkRolesDesign(PATH, roles({ version: 2 }))?.code, "SCHEMA_VIOLATION");
});

test("at least one role is required", () => {
  assert.equal(checkRolesDesign(PATH, roles({ roles: [], testUsers: [], coldStartRole: null }))?.code, "SCHEMA_VIOLATION");
});

test("a test user naming an undeclared role is rejected", () => {
  const problem = checkRolesDesign(PATH, roles({ testUsers: [{ username: "test-admin", role: "Admin" }] }));
  assert.equal(problem?.code, "SCHEMA_VIOLATION");
  assert.match(problem!.message, /no roles\[\] entry declares/);
});

test("a coldStartRole naming an undeclared role is rejected", () => {
  const problem = checkRolesDesign(PATH, roles({ coldStartRole: "Nobody" }));
  assert.match(problem!.message, /not a declared role/);
});

test("coldStartRole may be null", () => {
  assert.equal(checkRolesDesign(PATH, roles({ coldStartRole: null })), null);
});

test("a duplicate role name is rejected, case-insensitively", () => {
  const problem = checkRolesDesign(
    PATH,
    roles({
      roles: [
        {
          name: "Viewer",
          description: "a",
          stories: [1],
          grantedBy: "first sign-in",
          permissions: [{ component: "api", actions: ["read"] }],
        },
        {
          name: "viewer",
          description: "b",
          stories: [2],
          grantedBy: "Viewer",
          permissions: [{ component: "api", actions: ["read"] }],
        },
      ],
    }),
  );
  assert.match(problem!.message, /declared twice/);
});

test("a permission granting neither actions nor screens is rejected", () => {
  const problem = checkRolesDesign(
    PATH,
    roles({
      roles: [
        {
          name: "Viewer",
          description: "a",
          stories: [1],
          grantedBy: "first sign-in",
          permissions: [{ component: "api" }],
        },
      ],
    }),
  );
  assert.match(problem!.message, /grants nothing/);
});

test("a username the directory cannot hold is rejected", () => {
  const problem = checkRolesDesign(PATH, roles({ testUsers: [{ username: "Test Viewer", role: "Viewer" }] }));
  assert.match(problem!.message, /usable directory username/);
});

test("a duplicate username is rejected", () => {
  const problem = checkRolesDesign(
    PATH,
    roles({
      testUsers: [
        { username: "test-viewer", role: "Viewer" },
        { username: "test-viewer", role: "Viewer" },
      ],
    }),
  );
  assert.match(problem!.message, /listed twice/);
});

test("an empty testUsers list passes the gate — the build supplies the missing users", () => {
  assert.equal(checkRolesDesign(PATH, roles({ testUsers: [] })), null);
});

test("the FileBundle refuses a bad roles.json and stays byte-for-byte unchanged", () => {
  const bundle = new FileBundle({ [PATH]: roles() });
  const before = bundle.snapshot()[PATH];
  const res = bundle.addFile(PATH, roles({ version: 9 }));
  assert.equal(res.ok, false);
  assert.equal(bundle.snapshot()[PATH], before);
});
