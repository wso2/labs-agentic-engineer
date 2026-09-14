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
 * Write-gate behavior for `specs/design/dependencies/<name>/dependency.json`
 * and its `sdk.json` — the one definition of an external dependency. These
 * assert the zod source of truth directly; the Go fold gate has its own parity
 * tests, and the two must agree.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { checkDependencyDesign, preserveAssumption } from "../src/dependency-design-schema.ts";

const PATH = "specs/design/dependencies/payment-provider/dependency.json";
const SDK_PATH = "specs/design/dependencies/payment-provider/sdk.json";

function dep(overrides: Record<string, unknown> = {}): string {
  return JSON.stringify({
    name: "payment-provider",
    description: "Charges the customer for shipping.",
    provider: "Stripe",
    style: "rest-api",
    contract: "openapi.yaml",
    config: [{ key: "PAYMENT_API_KEY", secret: true }],
    ...overrides,
  });
}

/** A bundle reader over a fixed file map. */
const bundle = (files: Record<string, string>) => ({ read: (p: string) => files[p] });

test("ignores paths that are not a dependency file", () => {
  assert.equal(checkDependencyDesign("specs/design/components/api/design.json", "{nope"), null);
  assert.equal(checkDependencyDesign("specs/design/dependencies/payment-provider/openapi.yaml", "openapi: 3"), null);
});

test("accepts a resolved REST dependency", () => {
  assert.equal(checkDependencyDesign(PATH, dep()), null);
});

test("accepts the need alone: suggestions, no provider, no config", () => {
  const open = dep({
    provider: undefined,
    style: undefined,
    contract: undefined,
    config: undefined,
    suggestions: [
      { name: "sendgrid", style: "rest-api" },
      { name: "postmark" },
    ],
  });
  assert.equal(checkDependencyDesign(PATH, open), null);
  // One suggestion is fine — it is a starting point, not a choice.
  assert.equal(
    checkDependencyDesign(PATH, dep({ provider: undefined, style: undefined, contract: undefined, config: undefined, suggestions: [{ name: "sendgrid" }] })),
    null,
  );
});

test("config keys follow the chosen service — none before a provider", () => {
  const problem = checkDependencyDesign(
    PATH,
    dep({ provider: undefined, style: undefined, contract: undefined }),
  );
  assert.match(problem!.message, /derived from the chosen service/);
  // A registered org copy carries the org's keys without a project-chosen provider.
  assert.equal(checkDependencyDesign(PATH, JSON.stringify({ name: "payment-provider", source: "org", config: [{ key: "K" }] })), null);
});

test("accepts a registered-org stub — the platform fills the rest at save", () => {
  assert.equal(
    checkDependencyDesign(PATH, JSON.stringify({ name: "payment-provider", source: "org" })),
    null,
  );
});

test("accepts a provider chosen but no contract yet — the needs-contract state", () => {
  assert.equal(checkDependencyDesign(PATH, dep({ contract: undefined })), null);
});

test("rejects invalid JSON with a re-emit instruction", () => {
  const problem = checkDependencyDesign(PATH, "{nope");
  assert.equal(problem?.code, "INVALID_JSON");
});

test("rejects a name that is not the directory", () => {
  const problem = checkDependencyDesign(PATH, dep({ name: "stripe" }));
  assert.equal(problem?.code, "SCHEMA_VIOLATION");
  assert.match(problem!.message, /"payment-provider"/);
});

test("rejects read-time state and unknown keys", () => {
  for (const extra of [{ status: "resolved" }, { reason: "needs-spec" }, { specPath: "https://…" }]) {
    const problem = checkDependencyDesign(PATH, dep(extra));
    assert.equal(problem?.code, "SCHEMA_VIOLATION", JSON.stringify(extra));
  }
});

test("rejects the retired candidates field", () => {
  const problem = checkDependencyDesign(
    PATH,
    dep({ provider: undefined, style: undefined, contract: undefined, config: undefined, candidates: [{ name: "sendgrid", style: "rest-api" }, { name: "postmark", style: "rest-api" }] }),
  );
  assert.equal(problem?.code, "SCHEMA_VIOLATION");
  assert.match(problem!.message, /candidates/);
});

test("rejects suggestions alongside a chosen provider, and alongside a style", () => {
  const both = dep({ suggestions: [{ name: "a" }, { name: "b" }] });
  assert.match(checkDependencyDesign(PATH, both)!.message, /never coexist/);
  const styled = dep({
    provider: undefined,
    contract: undefined,
    config: undefined,
    suggestions: [{ name: "a" }, { name: "b" }],
  });
  assert.match(checkDependencyDesign(PATH, styled)!.message, /stay unset/);
});

test("the contract file name must fit the style", () => {
  assert.match(checkDependencyDesign(PATH, dep({ contract: "schema.graphql" }))!.message, /rest-api/);
  assert.equal(checkDependencyDesign(PATH, dep({ style: "graphql", contract: "schema.graphql" })), null);
  assert.match(checkDependencyDesign(PATH, dep({ contract: "specs/x/openapi.yaml" }))!.message, /not a path/);
  assert.match(checkDependencyDesign(PATH, dep({ style: undefined }))!.message, /"style" is required/);
});

test("an sdk dependency names its manifest and may carry the API slice beside it", () => {
  assert.equal(checkDependencyDesign(PATH, dep({ style: "sdk", sdk: "sdk.json" })), null);
  assert.equal(checkDependencyDesign(PATH, dep({ style: "sdk", sdk: "sdk.json", contract: undefined })), null);
  assert.match(checkDependencyDesign(PATH, dep({ style: "sdk", contract: undefined }))!.message, /sdk\.json/);
  assert.match(checkDependencyDesign(PATH, dep({ sdk: "sdk.json" }))!.message, /only meaningful on style "sdk"/);
  assert.match(checkDependencyDesign(PATH, dep({ style: "sdk", sdk: "manifest.json" }))!.message, /"sdk\.json"/);
});

test("a secret config key cannot carry a default", () => {
  const problem = checkDependencyDesign(PATH, dep({ config: [{ key: "K", secret: true, defaultValue: "x" }] }));
  assert.match(problem!.message, /secret/);
});

test("the assumption record is echoed, never authored", () => {
  const assumed = { by: "admin", at: "2026-09-08T10:15:00Z", note: "auth guessed" };
  // Introducing it against a bundle where the file has none → refused.
  const noPrior = bundle({});
  assert.match(checkDependencyDesign(PATH, dep({ assumed }), noPrior)!.message, /permission record/);
  // Echoing the record the file already carries → fine.
  const prior = bundle({ [PATH]: dep({ assumed }) });
  assert.equal(checkDependencyDesign(PATH, dep({ assumed, description: "edited" }), prior), null);
  // Altering it → refused; dropping it → allowed (a real contract replaced it).
  assert.match(checkDependencyDesign(PATH, dep({ assumed: { ...assumed, by: "agent" } }), prior)!.message, /permission record/);
  assert.equal(checkDependencyDesign(PATH, dep(), prior), null);
  // Without a bundle (the BFF save-gate) the record is accepted as given.
  assert.equal(checkDependencyDesign(PATH, dep({ assumed })), null);
});

test("sdk.json: languages are lower-case and packages ecosystem-prefixed", () => {
  const ok = JSON.stringify({ packages: { typescript: "npm:stripe@^14", go: "go:github.com/stripe/stripe-go/v79" }, calls: ["paymentIntents.create"] });
  assert.equal(checkDependencyDesign(SDK_PATH, ok), null);
  assert.match(checkDependencyDesign(SDK_PATH, JSON.stringify({ packages: {} }))!.message, /at least one/);
  assert.match(checkDependencyDesign(SDK_PATH, JSON.stringify({ packages: { TypeScript: "npm:stripe" } }))!.message, /lower-case/);
  assert.match(checkDependencyDesign(SDK_PATH, JSON.stringify({ packages: { go: "stripe-go" } }))!.message, /ecosystem-prefixed/);
  assert.equal(checkDependencyDesign(SDK_PATH, "{nope")?.code, "INVALID_JSON");
});

test("config keys: an optional description, and a defaultValue on a non-secret key", () => {
  assert.equal(
    checkDependencyDesign(PATH, dep({ config: [{ key: "PAYMENT_API_KEY", secret: true, description: "Your Stripe secret API key" }] })),
    null,
  );
  assert.equal(checkDependencyDesign(PATH, dep({ config: [{ key: "AWS_REGION", defaultValue: "us-east-1" }] })), null);
  assert.equal(
    checkDependencyDesign(PATH, dep({ config: [{ key: "PAYMENT_API_KEY", credentialClass: "secret" }] }))?.code,
    "SCHEMA_VIOLATION",
  );
});

test("an echoed assumption record compares by value, whatever the key order", () => {
  const prior = bundle({ [PATH]: dep({ assumed: { by: "admin", at: "2026-09-08T10:15:00Z", note: "n" } }) });
  assert.equal(checkDependencyDesign(PATH, dep({ assumed: { note: "n", at: "2026-09-08T10:15:00Z", by: "admin" } }), prior), null);
});

test("the user's assumed record rides through a write that leaves it out", () => {
  const record = { by: "admin", at: "2026-09-08T10:15:00Z" };
  const prior = dep({ assumed: record });
  // Left out → put back; carried → untouched; other paths and unparseable writes → untouched.
  assert.deepEqual(JSON.parse(preserveAssumption(PATH, dep({ description: "edited" }), prior)).assumed, record);
  const carried = dep({ assumed: record, description: "edited" });
  assert.equal(preserveAssumption(PATH, carried, prior), carried);
  assert.equal(preserveAssumption("specs/design/components/api/design.json", "{}", prior), "{}");
  assert.equal(preserveAssumption(PATH, "{nope", prior), "{nope");
  assert.equal(preserveAssumption(PATH, dep(), dep()), dep());
  // And the gate then reads the put-back record as an echo, not an authoring.
  const restored = preserveAssumption(PATH, dep({ description: "edited" }), prior);
  assert.equal(checkDependencyDesign(PATH, restored, bundle({ [PATH]: prior })), null);
});
