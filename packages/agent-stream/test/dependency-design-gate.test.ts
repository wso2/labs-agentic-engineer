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
 * tests (agentfold/dependencygate_test.go), and the two must agree.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { checkDependencyDesign, preservePlatformFields } from "../src/dependency-design-schema.ts";

const PATH = "specs/design/dependencies/payment-provider/dependency.json";
const SDK_PATH = "specs/design/dependencies/payment-provider/sdk.json";

const CONTRACT = { type: "openapi", path: "openapi.yaml", origin: "provider" };

/** A resolved REST dependency.json; top-level overrides by key, resource-block overrides under `resource`. */
function dep(overrides: Record<string, unknown> = {}, resourceOverrides: Record<string, unknown> = {}): string {
  const { resource: _ignored, ...top } = overrides as { resource?: unknown };
  void _ignored;
  return JSON.stringify({
    name: "payment-provider",
    resource: {
      name: "payment-provider",
      description: "Charges the customer for shipping.",
      provider: "Stripe",
      config: [{ key: "PAYMENT_API_KEY", secret: true }],
      contract: CONTRACT,
      ...resourceOverrides,
    },
    ...top,
  });
}

/** The need alone: no provider, no contract, no config. */
const OPEN = { provider: undefined, contract: undefined, config: undefined };

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
  assert.equal(checkDependencyDesign(PATH, dep({ suggestions: [{ name: "sendgrid", style: "rest-api" }, { name: "postmark" }] }, OPEN)), null);
  // One suggestion is fine — it is a starting point, not a choice.
  assert.equal(checkDependencyDesign(PATH, dep({ suggestions: [{ name: "sendgrid" }] }, OPEN)), null);
  assert.equal(checkDependencyDesign(PATH, dep({}, OPEN)), null);
});

test("config keys follow the chosen service — none before a provider", () => {
  const problem = checkDependencyDesign(PATH, dep({}, { provider: undefined, contract: undefined }));
  assert.match(problem!.message, /derived from the chosen service/);
  // A copy of a registered resource carries the org's keys without a project-chosen provider.
  assert.equal(
    checkDependencyDesign(PATH, JSON.stringify({ name: "payment-provider", resource: { ref: "payment-provider", name: "payment-provider", config: [{ key: "K" }] } })),
    null,
  );
});

test("accepts the registry stub — the agent asks by name and the platform fills the block at save", () => {
  assert.equal(checkDependencyDesign(PATH, JSON.stringify({ name: "payment-provider", resource: { ref: "payment-provider", name: "payment-provider" } })), null);
  assert.match(
    checkDependencyDesign(PATH, JSON.stringify({ name: "payment-provider", resource: { ref: "stripe", name: "payment-provider" } }))!.message,
    /"resource.ref" must equal/,
  );
});

test("accepts a provider chosen but no contract yet — the needs-contract state", () => {
  assert.equal(checkDependencyDesign(PATH, dep({}, { contract: undefined })), null);
});

test("rejects invalid JSON with a re-emit instruction", () => {
  assert.equal(checkDependencyDesign(PATH, "{nope")?.code, "INVALID_JSON");
});

test("rejects a name that is not the directory, and a resource block named otherwise", () => {
  const problem = checkDependencyDesign(PATH, dep({ name: "stripe" }, { name: "stripe" }));
  assert.equal(problem?.code, "SCHEMA_VIOLATION");
  assert.match(problem!.message, /"payment-provider"/);
  assert.match(checkDependencyDesign(PATH, dep({}, { name: "stripe" }))!.message, /"resource.name" must equal/);
  assert.match(checkDependencyDesign(PATH, JSON.stringify({ name: "payment-provider" }))!.message, /"resource" is required/);
});

test("rejects read-time state and unknown keys", () => {
  for (const extra of [{ status: "resolved" }, { reason: "needs-spec" }, { specPath: "https://…" }]) {
    assert.equal(checkDependencyDesign(PATH, dep(extra))?.code, "SCHEMA_VIOLATION", JSON.stringify(extra));
  }
  assert.match(checkDependencyDesign(PATH, dep({}, { style: "rest-api" }))!.message, /style/);
});

test("rejects the retired flat shape by name", () => {
  for (const [k, v] of [["provider", "Stripe"], ["style", "rest-api"], ["source", "org"], ["contract", "openapi.yaml"], ["assumed", { by: "a", at: "b" }], ["candidates", [{ name: "x" }]]] as const) {
    const problem = checkDependencyDesign(PATH, dep({ [k]: v }));
    assert.equal(problem?.code, "SCHEMA_VIOLATION", k);
    assert.match(problem!.message, /not a top-level field/, k);
  }
});

test("rejects suggestions alongside a chosen provider, and alongside a contract or a ref", () => {
  assert.match(checkDependencyDesign(PATH, dep({ suggestions: [{ name: "a" }, { name: "b" }] }))!.message, /never coexist/);
  assert.match(checkDependencyDesign(PATH, dep({ suggestions: [{ name: "a" }] }, { provider: undefined, config: undefined }))!.message, /stay unset/);
  assert.match(
    checkDependencyDesign(PATH, JSON.stringify({ name: "payment-provider", resource: { ref: "payment-provider", name: "payment-provider" }, suggestions: [{ name: "a" }] }))!.message,
    /stay unset/,
  );
});

test("the contract is { type, path }: the path fits the type, is a file name, and has no URL form", () => {
  assert.match(checkDependencyDesign(PATH, dep({}, { contract: { ...CONTRACT, path: "schema.graphql" } }))!.message, /for type "openapi"/);
  assert.equal(checkDependencyDesign(PATH, dep({}, { contract: { type: "graphql", path: "schema.graphql" } })), null);
  assert.match(checkDependencyDesign(PATH, dep({}, { contract: { ...CONTRACT, path: "specs/x/openapi.yaml" } }))!.message, /not a path/);
  assert.match(checkDependencyDesign(PATH, dep({}, { contract: { type: "openapi", url: "https://x/openapi.yaml" } }))!.message, /no "url" form/);
  assert.match(checkDependencyDesign(PATH, dep({}, { contract: { type: "asyncapi", path: "asyncapi.yaml" } }))!.message, /not an allowed value/);
  assert.match(checkDependencyDesign(PATH, dep({}, { contract: { ...CONTRACT, origin: "guessed" } }))!.message, /origin/);
  assert.equal(checkDependencyDesign(PATH, dep({}, { contract: { ...CONTRACT, origin: "derived" } })), null);
});

test("an sdk contract is the manifest", () => {
  assert.equal(checkDependencyDesign(PATH, dep({}, { contract: { type: "sdk", path: "sdk.json" } })), null);
  assert.match(checkDependencyDesign(PATH, dep({}, { contract: { type: "sdk", path: "manifest.json" } }))!.message, /"sdk\.json"/);
});

test("a secret config key cannot carry a default", () => {
  assert.match(checkDependencyDesign(PATH, dep({}, { config: [{ key: "K", secret: true, defaultValue: "x" }] }))!.message, /secret/);
});

test("provenance: a hash, a source url or a registry path — never a url in the registry field, and the retired fields are named", () => {
  const sha = "ab".repeat(32);
  assert.equal(checkDependencyDesign(PATH, dep({ provenance: { sourceUrl: "https://x", sha256: sha, readOn: "2026-09-17" } })), null);
  assert.equal(checkDependencyDesign(PATH, dep({ provenance: { registry: "payment-provider/openapi.yaml", sha256: sha } })), null);
  assert.match(checkDependencyDesign(PATH, dep({ provenance: { registry: "https://x/openapi.yaml" } }))!.message, /never a URL/);
  assert.match(checkDependencyDesign(PATH, dep({ provenance: { sha256: "nope" } }))!.message, /SHA-256/);
  assert.equal(checkDependencyDesign(PATH, dep({ provenance: { sliced: true } }))?.code, "SCHEMA_VIOLATION");
  assert.equal(checkDependencyDesign(PATH, dep({ provenance: { fetchedAt: "x" } }))?.code, "SCHEMA_VIOLATION");
});

test("the acceptance record and the organization's instructions are echoed, never authored", () => {
  const accepted = { by: "admin", at: "2026-09-08T10:15:00Z", note: "auth guessed" };
  const assumed = { ...CONTRACT, origin: "assumed", accepted };
  // Introducing the record against a bundle where the file has none → refused.
  assert.match(checkDependencyDesign(PATH, dep({}, { contract: assumed }), bundle({}))!.message, /permission record/);
  // Echoing the record the file already carries → fine.
  const prior = bundle({ [PATH]: dep({}, { contract: assumed }) });
  assert.equal(checkDependencyDesign(PATH, dep({}, { contract: assumed, description: "edited" }), prior), null);
  // Altering it → refused; dropping it → allowed (a real contract replaced it).
  assert.match(checkDependencyDesign(PATH, dep({}, { contract: { ...assumed, accepted: { ...accepted, by: "agent" } } }), prior)!.message, /permission record/);
  assert.equal(checkDependencyDesign(PATH, dep(), prior), null);
  // Without a bundle (the BFF save-gate) the record is accepted as given.
  assert.equal(checkDependencyDesign(PATH, dep({}, { contract: assumed })), null);
  // The organization's instructions ride only on the platform's copy.
  const copy = dep({}, { ref: "payment-provider", consumptionInstructions: "Never log the key." });
  assert.match(checkDependencyDesign(PATH, copy, bundle({}))!.message, /consumptionInstructions/);
  assert.equal(checkDependencyDesign(PATH, copy, bundle({ [PATH]: copy })), null);
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
  assert.equal(checkDependencyDesign(PATH, dep({}, { config: [{ key: "PAYMENT_API_KEY", secret: true, description: "Your Stripe secret API key" }] })), null);
  assert.equal(checkDependencyDesign(PATH, dep({}, { config: [{ key: "AWS_REGION", defaultValue: "us-east-1" }] })), null);
  assert.equal(checkDependencyDesign(PATH, dep({}, { config: [{ key: "PAYMENT_API_KEY", credentialClass: "secret" }] }))?.code, "SCHEMA_VIOLATION");
});

// A file written before the nested shape carries its acceptance at the top
// level as `assumed`. The gate reads the RAW prior, so it must lift that too:
// otherwise the first nested write over a flat file drops what the user accepted.
test("a flat prior's acceptance survives the first nested write", () => {
  const accepted = { by: "admin", at: "2026-09-08T10:15:00Z", note: "proceed" };
  const flatPrior = JSON.stringify({
    name: "payment-provider",
    provider: "Stripe",
    style: "rest-api",
    contract: "openapi.yaml",
    assumed: accepted,
  });
  const restored = preservePlatformFields(PATH, dep({}, { contract: { ...CONTRACT, origin: "assumed" } }), flatPrior);
  const got = JSON.parse(restored) as { resource: { contract: { accepted?: unknown } } };
  assert.deepEqual(got.resource.contract.accepted, accepted);
  // A flat prior with nothing accepted still leaves the write alone.
  const plainFlat = JSON.stringify({ name: "payment-provider", provider: "Stripe", contract: "openapi.yaml" });
  assert.equal(preservePlatformFields(PATH, dep(), plainFlat), dep());
});

test("an echoed record compares by value, whatever the key order", () => {
  const accepted = { by: "admin", at: "2026-09-08T10:15:00Z", note: "n" };
  const prior = bundle({ [PATH]: dep({}, { contract: { ...CONTRACT, origin: "assumed", accepted } }) });
  assert.equal(
    checkDependencyDesign(PATH, dep({}, { contract: { ...CONTRACT, origin: "assumed", accepted: { note: "n", at: "2026-09-08T10:15:00Z", by: "admin" } } }), prior),
    null,
  );
});

test("the platform's fields ride through a write that leaves them out", () => {
  const accepted = { by: "admin", at: "2026-09-08T10:15:00Z" };
  const prior = dep({}, { ref: "payment-provider", consumptionInstructions: "Never log the key.", contract: { ...CONTRACT, origin: "assumed", accepted } });
  const restored = preservePlatformFields(PATH, dep({}, { ref: "payment-provider", description: "edited", contract: { ...CONTRACT, origin: "assumed" } }), prior);
  const got = JSON.parse(restored) as { resource: { ref?: string; consumptionInstructions?: string; description?: string; contract: { accepted?: unknown } } };
  assert.equal(got.resource.ref, "payment-provider");
  assert.equal(got.resource.consumptionInstructions, "Never log the key.");
  assert.equal(got.resource.description, "edited");
  assert.deepEqual(got.resource.contract.accepted, accepted);
  // Carried → untouched; other paths and unparseable writes → untouched; no prior → untouched.
  const carried = dep({}, { ref: "payment-provider", consumptionInstructions: "Never log the key.", contract: { ...CONTRACT, origin: "assumed", accepted }, description: "edited" });
  assert.equal(preservePlatformFields(PATH, carried, prior), carried);
  assert.equal(preservePlatformFields("specs/design/components/api/design.json", "{}", prior), "{}");
  assert.equal(preservePlatformFields(PATH, "{nope", prior), "{nope");
  assert.equal(preservePlatformFields(PATH, dep(), dep()), dep());
  // And the gate then reads the put-back fields as echoes, not an authoring.
  assert.equal(checkDependencyDesign(PATH, restored, bundle({ [PATH]: prior })), null);
  // A write that DROPS the ref is the agent replacing the copy with an inline
  // resource: neither the ref nor the instructions come back; the acceptance does.
  const dropped = JSON.parse(preservePlatformFields(PATH, dep({}, { provider: "Adyen", contract: { ...CONTRACT, origin: "assumed" } }), prior)) as {
    resource: { ref?: string; consumptionInstructions?: string; contract: { accepted?: unknown } };
  };
  assert.equal(dropped.resource.ref, undefined);
  assert.equal(dropped.resource.consumptionInstructions, undefined);
  assert.deepEqual(dropped.resource.contract.accepted, accepted);
});

test("an explicit null is never an optional field", () => {
  for (const body of [
    { name: "payment-provider", resource: { name: "payment-provider" }, provenance: null },
    { name: "payment-provider", resource: { name: "payment-provider" }, suggestions: null },
    { name: "payment-provider", resource: { name: "payment-provider", provider: "Stripe", contract: null } },
    { name: "payment-provider", resource: { name: "payment-provider", provider: "Stripe", contract: { ...CONTRACT, accepted: null } } },
  ]) {
    assert.equal(checkDependencyDesign(PATH, JSON.stringify(body))?.code, "SCHEMA_VIOLATION", JSON.stringify(body));
  }
});
