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
import { checkComponentDesign } from "@aep/agent-stream";

const PATH = "specs/design/components/checkout/design.json";

/** A minimal, schema-valid ComponentDesign for `checkout`. */
function baseDoc(): Record<string, unknown> {
  return {
    name: "checkout",
    type: "service",
    version: "0.1.0",
    language: "Go",
    buildpack: "docker",
    appPath: "checkout",
    entrypoint: "deployment/service",
    exposure: "internet",
    dependencies: [],
    description: "Owns the checkout flow.",
  };
}

function check(doc: unknown, path = PATH) {
  return checkComponentDesign(path, JSON.stringify(doc));
}

// --- path gating -----------------------------------------------------------

test("non design.json path is not gated (null)", () => {
  assert.equal(checkComponentDesign("specs/notes/x.json", "{ not json"), null);
});

test("malformed JSON at a design.json path is INVALID_JSON", () => {
  const r = checkComponentDesign(PATH, "{ \"name\": ");
  assert.equal(r?.code, "INVALID_JSON");
});

// --- the four dependency kinds all validate --------------------------------

test("dependencies: kind=component (minimal) is accepted", () => {
  const doc = baseDoc();
  doc.dependencies = [{ kind: "component", name: "cart", description: "reads the cart" }];
  assert.equal(check(doc), null);
});

test("dependencies: kind=org-service is accepted", () => {
  const doc = baseDoc();
  doc.dependencies = [{ kind: "org-service", name: "billing" }];
  assert.equal(check(doc), null);
});

test("dependencies: kind=external is a bare reference — its definition lives in the dependency file", () => {
  const doc = baseDoc();
  doc.dependencies = [{ kind: "external", name: "stripe", description: "charges shipping" }];
  assert.equal(check(doc), null);
});

test("dependencies: kind=platform-resource with resourceType + parameters is accepted", () => {
  const doc = baseDoc();
  doc.dependencies = [
    {
      kind: "platform-resource",
      name: "orders-db",
      resourceType: "postgres",
      parameters: { size: "small", version: "16" },
    },
  ];
  assert.equal(check(doc), null);
});

test("dependencies: all four kinds together validate", () => {
  const doc = baseDoc();
  doc.dependencies = [
    { kind: "component", name: "cart" },
    { kind: "org-service", name: "billing" },
    { kind: "external", name: "stripe" },
    { kind: "platform-resource", name: "orders-db", resourceType: "postgres" },
  ];
  assert.equal(check(doc), null);
});

// --- the definition fields moved to specs/design/dependencies/<name>/dependency.json

for (const [field, value] of [
  ["style", "rest-api"],
  ["package", "npm:stripe@^14"],
  ["specPath", "dependencies/stripe.openapi.yaml"],
  ["candidates", [{ name: "sendgrid-rest", style: "rest-api" }, { name: "resend-sdk", style: "sdk" }]],
  ["config", [{ key: "STRIPE_API_KEY", secret: true }]],
] as const) {
  test(`dependencies: ${field} on kind=external -> SCHEMA_VIOLATION pointing at the dependency file`, () => {
    const doc = baseDoc();
    doc.dependencies = [{ kind: "external", name: "stripe", [field]: value }];
    const r = check(doc);
    assert.equal(r?.code, "SCHEMA_VIOLATION");
    assert.match(r!.message, /specs\/design\/dependencies\/stripe\/dependency\.json/);
    assert.match(r!.message, new RegExp(`"${field}"`));
  });
}

test("dependencies: needsSpec is retired -> unknown key SCHEMA_VIOLATION", () => {
  const doc = baseDoc();
  doc.dependencies = [{ kind: "external", name: "stripe", needsSpec: true }];
  assert.equal(check(doc)?.code, "SCHEMA_VIOLATION");
});

// specUrl (URL hint) and sources (provenance array) were removed from the
// schema long before the definition moved out — both still reject as unknown
// keys (strictObject).
test("dependencies: retired specUrl -> unknown key SCHEMA_VIOLATION", () => {
  const doc = baseDoc();
  doc.dependencies = [{ kind: "external", name: "stripe", specUrl: "https://example.com/stripe.yaml" }];
  assert.equal(check(doc)?.code, "SCHEMA_VIOLATION");
});

test("dependencies: retired sources -> unknown key SCHEMA_VIOLATION", () => {
  const doc = baseDoc();
  doc.dependencies = [{ kind: "external", name: "stripe", sources: ["https://stripe.com/docs/api"] }];
  assert.equal(check(doc)?.code, "SCHEMA_VIOLATION");
});

// The moved fields are unknown keys on every OTHER kind too — no pointer, just
// the strict schema.
for (const [field, value] of [
  ["style", "sdk"],
  ["package", "npm:stripe@^14"],
  ["specPath", "dependencies/stripe.openapi.yaml"],
] as const) {
  test(`dependencies: ${field} is an unknown key on kind=org-service -> SCHEMA_VIOLATION`, () => {
    const doc = baseDoc();
    doc.dependencies = [{ kind: "org-service", name: "identity", [field]: value }];
    const r = check(doc);
    assert.equal(r?.code, "SCHEMA_VIOLATION");
    assert.match(r!.message, new RegExp(field));
  });
}

// --- connections is GONE ---------------------------------------------------

test("connections key is now an unknown key -> SCHEMA_VIOLATION", () => {
  const doc = baseDoc();
  delete doc.dependencies;
  doc.connections = [{ to: "cart", type: "http" }];
  const r = check(doc);
  assert.equal(r?.code, "SCHEMA_VIOLATION");
  assert.match(r!.message, /connections|dependencies/);
});

// --- status/reason are read-time computed, never authored ------------------

test("status inside a dependency -> SCHEMA_VIOLATION", () => {
  const doc = baseDoc();
  doc.dependencies = [{ kind: "external", name: "stripe", status: "unresolved" }];
  const r = check(doc);
  assert.equal(r?.code, "SCHEMA_VIOLATION");
  assert.match(r!.message, /status/);
});

test("reason inside a dependency -> SCHEMA_VIOLATION", () => {
  const doc = baseDoc();
  doc.dependencies = [{ kind: "external", name: "stripe", reason: "needs-spec" }];
  const r = check(doc);
  assert.equal(r?.code, "SCHEMA_VIOLATION");
  assert.match(r!.message, /reason/);
});

// --- dependency-entry strictness (mirrors the Go codec) --------------------

test("dependency missing kind -> SCHEMA_VIOLATION", () => {
  const doc = baseDoc();
  doc.dependencies = [{ name: "cart" }];
  assert.equal(check(doc)?.code, "SCHEMA_VIOLATION");
});

test("dependency missing name -> SCHEMA_VIOLATION", () => {
  const doc = baseDoc();
  doc.dependencies = [{ kind: "component" }];
  assert.equal(check(doc)?.code, "SCHEMA_VIOLATION");
});

test("dependency with unknown kind -> SCHEMA_VIOLATION", () => {
  const doc = baseDoc();
  doc.dependencies = [{ kind: "sidecar", name: "x" }];
  const r = check(doc);
  assert.equal(r?.code, "SCHEMA_VIOLATION");
  assert.match(r!.message, /kind/);
});

test("unknown key inside a dependency -> SCHEMA_VIOLATION", () => {
  const doc = baseDoc();
  doc.dependencies = [{ kind: "component", name: "cart", to: "cart" }];
  assert.equal(check(doc)?.code, "SCHEMA_VIOLATION");
});

test("unknown key inside a config entry -> SCHEMA_VIOLATION", () => {
  const doc = baseDoc();
  doc.dependencies = [{ kind: "external", name: "stripe", config: [{ key: "K", env: "K" }] }];
  assert.equal(check(doc)?.code, "SCHEMA_VIOLATION");
});

// --- kind-field policy is LENIENT, mirroring the Go single-struct codec -----

test("kind-specific fields are lenient: resourceType on an external dep is accepted (mirrors Go)", () => {
  const doc = baseDoc();
  doc.dependencies = [{ kind: "external", name: "stripe", resourceType: "postgres" }];
  assert.equal(check(doc), null);
});

// --- platform-owned passthrough blocks (Go-written, agent-visible) ----------

test("platform blocks exposesAPI/componentAgentInstructions are accepted", () => {
  const doc = baseDoc();
  doc.exposesAPI = { managed: true, auth: "end-user-required", userContext: "X-User-Id", orgPublished: true };
  doc.componentAgentInstructions = "Prefer the v2 endpoints.";
  assert.equal(check(doc), null);
});

test("empty exposesAPI block is accepted (Go emits all-optional fields)", () => {
  const doc = baseDoc();
  doc.exposesAPI = {};
  assert.equal(check(doc), null);
});

test("unknown key inside exposesAPI -> SCHEMA_VIOLATION", () => {
  const doc = baseDoc();
  doc.exposesAPI = { managed: true, bogus: 1 };
  assert.equal(check(doc)?.code, "SCHEMA_VIOLATION");
});

// The retired caller-identity field is gone from the schema, so it is now an
// unknown top-level key like any other — a design.json authored before the
// thunder-app dependency replaced it is rejected, not silently tolerated.
test("retired callerIdentity block -> SCHEMA_VIOLATION", () => {
  const doc = baseDoc();
  doc.callerIdentity = { mode: "end-user" };
  assert.equal(check(doc)?.code, "SCHEMA_VIOLATION");
});

// --- top-level strictness / name==dir --------------------------------------

test("unknown top-level key -> SCHEMA_VIOLATION", () => {
  const doc = baseDoc();
  doc.extra = true;
  assert.equal(check(doc)?.code, "SCHEMA_VIOLATION");
});

test("name must equal the component directory", () => {
  const doc = baseDoc();
  doc.name = "other";
  const r = check(doc);
  assert.equal(r?.code, "SCHEMA_VIOLATION");
  assert.match(r!.message, /name.*checkout/);
});

test("bad exposure value -> SCHEMA_VIOLATION", () => {
  const doc = baseDoc();
  doc.exposure = "public";
  assert.equal(check(doc)?.code, "SCHEMA_VIOLATION");
});
