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
 * Write-gate behavior for `specs/design/security.json` v2. These assert the zod
 * source of truth directly; the Go save-gate (internal/platform/securityspec)
 * validates the SAME published JSON Schema plus the same referential rules,
 * and has its own parity tests — a document that passes one gate MUST pass the
 * other.
 *
 * The three documents under `fixtures/security/` are the design's own worked
 * examples (Expense Tracker, Clinic Appointments, Vendor Portal), transcribed
 * verbatim where the design writes a whole document. They are the shared
 * fixtures for every later phase, so a change that stops one parsing is a
 * change to the design, not to a test.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

import { checkSecurityDesign } from "../src/security-design-schema.ts";
import { catalogHandles, roleGrants } from "../src/security-design-catalog.ts";
import { FileBundle } from "../src/bundle.ts";

const PATH = "specs/design/security.json";

const FIXTURES = ["expense-tracker", "clinic", "vendor"] as const;

function fixture(name: (typeof FIXTURES)[number]): string {
  return readFileSync(new URL(`./fixtures/security/${name}.json`, import.meta.url), "utf8");
}

/** The Expense Tracker fixture as a mutable object, for the negative cases. */
function doc(overrides: Record<string, unknown> = {}): string {
  return JSON.stringify({ ...JSON.parse(fixture("expense-tracker")), ...overrides });
}

// --- the design's own documents ---------------------------------------------

for (const name of FIXTURES) {
  test(`the design's ${name} document passes the gate`, () => {
    assert.equal(checkSecurityDesign(PATH, fixture(name)), null);
  });
}

test("the catalog helpers fold a real document", () => {
  const parsed = JSON.parse(fixture("expense-tracker"));
  const handles = catalogHandles(parsed);
  assert.ok(handles.has("claims:read-all"));
  assert.ok(handles.has("reports:export"));
  assert.equal(handles.size, 7);
  assert.deepEqual(roleGrants(parsed).get("Employee"), new Set(["claims:read", "claims:submit"]));
});

// --- v1 is refused with one sentence, not a Zod dump ------------------------

/** The v1 document this repo shipped before the catalog existed. */
const V1 = JSON.stringify({
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
  thunder: { name: "expense-app", type: "browser" },
});

test("a v1 document is refused by one message naming the fields v2 removed", () => {
  const problem = checkSecurityDesign(PATH, V1);
  assert.equal(problem?.code, "SCHEMA_VIOLATION");
  const message = problem!.message;
  assert.match(message, /v1 is not accepted/);
  for (const removed of [
    "coldStartRole",
    "publicComponents",
    "thunder",
    "roles\\[\\].grantedBy",
    "roles\\[\\].permissions",
    "testUsers\\[\\].role",
  ]) {
    assert.match(message, new RegExp(removed), `expected the message to name ${removed}`);
  }
  // One sentence about the migration, NOT a per-issue Zod dump.
  assert.doesNotMatch(message, /violates the SecurityDesign schema/);
});

test("a half-migrated document still carrying a removed field is refused as v1", () => {
  const problem = checkSecurityDesign(PATH, doc({ thunder: { name: "expense-app", type: "browser" } }));
  assert.match(problem!.message, /v1 is not accepted: remove thunder/);
});

test("a version other than 2 is refused", () => {
  assert.equal(checkSecurityDesign(PATH, doc({ version: 3 }))?.code, "SCHEMA_VIOLATION");
});

// --- shape ------------------------------------------------------------------

test("the gate claims only specs/design/security.json", () => {
  assert.equal(checkSecurityDesign("specs/design/roles.json", "not json"), null);
  assert.equal(checkSecurityDesign("roles.json", "not json"), null);
  assert.equal(checkSecurityDesign("specs/design/components/api/roles.json", "not json"), null);
});

test("unparseable JSON is INVALID_JSON", () => {
  assert.equal(checkSecurityDesign(PATH, "{")?.code, "INVALID_JSON");
});

test("an unknown top-level field is rejected", () => {
  const problem = checkSecurityDesign(PATH, doc({ owner: "platform-team" }));
  assert.equal(problem?.code, "SCHEMA_VIOLATION");
  assert.match(problem!.message, /owner/);
});

test("an unknown nested field is rejected — no secret can be smuggled in", () => {
  const problem = checkSecurityDesign(
    PATH,
    doc({ testUsers: [{ username: "test-employee", roles: ["Employee"], password: "hunter2" }] }),
  );
  assert.equal(problem?.code, "SCHEMA_VIOLATION");
  assert.match(problem!.message, /password/);
});

test("an action without ownership is rejected", () => {
  const problem = checkSecurityDesign(
    PATH,
    doc({
      permissions: [
        { resource: "claims", component: "expense-api", actions: [{ handle: "read" }] },
      ],
    }),
  );
  assert.equal(problem?.code, "SCHEMA_VIOLATION");
  assert.match(problem!.message, /ownership/);
});

test("an ownership other than own or any is rejected", () => {
  const problem = checkSecurityDesign(
    PATH,
    doc({
      permissions: [
        {
          resource: "claims",
          component: "expense-api",
          actions: [{ handle: "read", ownership: "self" }],
        },
      ],
    }),
  );
  assert.equal(problem?.code, "SCHEMA_VIOLATION");
});

test("a handle segment outside [a-z][a-z0-9-]* is rejected", () => {
  for (const bad of ["Read", "read_all", "2fa", "read all", ""]) {
    const problem = checkSecurityDesign(
      PATH,
      doc({
        permissions: [
          {
            resource: "claims",
            component: "expense-api",
            actions: [{ handle: bad, ownership: "any" }],
          },
        ],
      }),
    );
    assert.equal(problem?.code, "SCHEMA_VIOLATION", `expected "${bad}" to be refused as an action handle`);
  }
});

test("a resource name outside [a-z][a-z0-9-]* is rejected", () => {
  const problem = checkSecurityDesign(
    PATH,
    doc({
      permissions: [
        { resource: "Claims", component: "expense-api", actions: [{ handle: "read", ownership: "own" }] },
      ],
    }),
  );
  assert.equal(problem?.code, "SCHEMA_VIOLATION");
});

test("a grant that is not a resource:action handle is rejected", () => {
  for (const bad of ["claims", "claims:read:all", "claims:", ":read"]) {
    const problem = checkSecurityDesign(
      PATH,
      doc({
        roles: [
          {
            name: "Employee",
            description: "Submits and follows their own claims.",
            stories: [1],
            grants: [bad],
            assignTo: ["Employees"],
          },
        ],
        testUsers: [],
      }),
    );
    assert.equal(problem?.code, "SCHEMA_VIOLATION", `expected "${bad}" to be refused as a grant`);
  }
});

test("a screen requiring something that is neither a handle, null nor \"public\" is rejected", () => {
  for (const bad of ["signed-in", "claims", "Public", "claims:read:all"]) {
    const problem = checkSecurityDesign(
      PATH,
      doc({ screens: [{ component: "expense-webapp", screen: "My Claims", requires: bad }] }),
    );
    assert.equal(problem?.code, "SCHEMA_VIOLATION", `expected requires "${bad}" to be refused`);
  }
});

test("a screen may require null (any signed-in user) or the literal public", () => {
  assert.equal(
    checkSecurityDesign(
      PATH,
      doc({
        screens: [
          { component: "expense-webapp", screen: "Home", requires: null },
          { component: "expense-webapp", screen: "Landing", requires: "public" },
        ],
      }),
    ),
    null,
  );
});

test("a role must grant at least one handle, and a permission must carry at least one action", () => {
  assert.equal(
    checkSecurityDesign(
      PATH,
      doc({
        roles: [{ name: "Employee", description: "d", stories: [1], grants: [], assignTo: ["Employees"] }],
        testUsers: [],
      }),
    )?.code,
    "SCHEMA_VIOLATION",
  );
  assert.equal(
    checkSecurityDesign(
      PATH,
      doc({ permissions: [{ resource: "claims", component: "expense-api", actions: [] }] }),
    )?.code,
    "SCHEMA_VIOLATION",
  );
});

test("enrolment and kind accept only the declared values", () => {
  assert.equal(checkSecurityDesign(PATH, fixture("clinic")), null); // enrolment: self-service
  assert.equal(checkSecurityDesign(PATH, fixture("vendor")), null); // kind: service
  const problem = checkSecurityDesign(
    PATH,
    doc({
      roles: [
        {
          name: "Employee",
          description: "d",
          stories: [1],
          grants: ["claims:read"],
          enrolment: "invite",
        },
      ],
      testUsers: [],
    }),
  );
  assert.equal(problem?.code, "SCHEMA_VIOLATION");
});

// --- referential (the minimum this task carries; task 1.2 widens it) --------

test("a grant naming a handle the catalog does not declare is rejected", () => {
  const problem = checkSecurityDesign(
    PATH,
    doc({
      roles: [
        {
          name: "Employee",
          description: "d",
          stories: [1],
          grants: ["claims:archive"],
          assignTo: ["Employees"],
        },
      ],
      testUsers: [],
    }),
  );
  assert.match(problem!.message, /permissions\[\] does not declare/);
});

test("a duplicate role name is rejected, case-insensitively", () => {
  const problem = checkSecurityDesign(
    PATH,
    doc({
      roles: [
        { name: "Employee", description: "a", stories: [1], grants: ["claims:read"], assignTo: ["Employees"] },
        { name: "employee", description: "b", stories: [2], grants: ["claims:read"], assignTo: ["Employees"] },
      ],
      testUsers: [],
    }),
  );
  assert.match(problem!.message, /declared twice/);
});

test("a test user naming an undeclared role is rejected", () => {
  const problem = checkSecurityDesign(PATH, doc({ testUsers: [{ username: "test-admin", roles: ["Admin"] }] }));
  assert.match(problem!.message, /no roles\[\] entry declares/);
});

test("a username the directory cannot hold is rejected", () => {
  const problem = checkSecurityDesign(
    PATH,
    doc({ testUsers: [{ username: "Test Employee", roles: ["Employee"] }] }),
  );
  assert.match(problem!.message, /usable directory username/);
});

test("an empty testUsers list passes the gate — the build supplies the missing users", () => {
  assert.equal(checkSecurityDesign(PATH, doc({ testUsers: [] })), null);
});

test("the FileBundle refuses a bad security.json and stays byte-for-byte unchanged", () => {
  const bundle = new FileBundle({ [PATH]: fixture("expense-tracker") });
  const before = bundle.snapshot()[PATH];
  const res = bundle.editFile(PATH, '"version": 2', '"version": 9');
  assert.equal(res.ok, false);
  if (res.ok) throw new Error("expected rejection");
  assert.equal(res.code, "SCHEMA_VIOLATION");
  assert.equal(bundle.snapshot()[PATH], before);
});
