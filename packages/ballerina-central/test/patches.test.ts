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
 * The per-package corrections, pinned against the fixtures they correct.
 *
 * A patch is a hand-maintained claim about a package, which is exactly the kind
 * of thing that rots silently: Central starts publishing the field, or the
 * package renames the type, and the correction goes on overriding reality. So
 * each one is pinned in BOTH directions where it has a negative case — what it
 * must change, and what it must leave alone.
 */

import assert from "node:assert/strict";
import { test } from "node:test";
import type { TypeDef } from "../src/model.js";
import { libraryFor } from "./corpus.js";

function errorsOf(slug: string): Map<string, Extract<TypeDef, { kind: "error" }>> {
  const errors = new Map<string, Extract<TypeDef, { kind: "error" }>>();
  for (const typeDef of libraryFor(slug).typeDefs) {
    if (typeDef.kind === "error") errors.set(typeDef.name, typeDef);
  }
  return errors;
}

test("the http detail type is restored at the intersection, on exactly the three declarations that carry it", () => {
  const errors = errorsOf("ballerina__http");
  const withDetail = [...errors.values()].filter((error) => error.base?.name.includes("error<Detail>"));
  assert.deepEqual(
    withDetail.map((error) => error.name).sort(),
    ["ApplicationResponseError", "ClientRequestError", "RemoteServerError"],
    "Central publishes the intersection shape for these three and no others",
  );
});

test("an error whose base is a plain reference is left alone, so it cannot claim a detail record it lacks", () => {
  const errors = errorsOf("ballerina__http");
  // The negative half of the pin. Attaching `Detail` at the root `Error` is the
  // intuitive reading of Central's `detailType`, and it would make roughly 50 of
  // http's 56 errors advertise an `int statusCode` they do not have.
  for (const name of ["SslError", "ListenerError", "ClientError", "AllRetryAttemptsFailed"]) {
    const error = errors.get(name);
    assert.ok(error, `${name} should be published as an error`);
    assert.doesNotMatch(error.base?.name ?? "error", /Detail/, `${name} must not mention Detail`);
  }
  assert.equal(errors.get("Error")?.base, undefined, "the root error narrows nothing");
});

test("ClientRequestError's chain reaches Detail, and Detail is a declaration in the same package", () => {
  const library = libraryFor("ballerina__http");
  const errors = errorsOf("ballerina__http");
  // Following the chain rather than asserting one line: this is the lookup the
  // recorded corpus came for, and it is only useful if every hop resolves.
  assert.equal(errors.get("ClientRequestError")?.base?.name, "(ApplicationResponseError&error<Detail>)");
  assert.equal(errors.get("ApplicationResponseError")?.base?.name, "(ClientError&error<Detail>)");
  assert.equal(errors.get("ClientError")?.base?.name, "Error");
  assert.equal(errors.get("Error")?.isDistinct, true);

  const detail = library.typeDefs.find((typeDef) => typeDef.kind === "record" && typeDef.name === "Detail");
  assert.ok(detail?.kind === "record");
  assert.deepEqual(
    detail.fields.map((field) => field.name),
    ["statusCode", "headers", "body"],
  );
});

test("every error carries the distinctness it declared, including the one that declares none", () => {
  // The whole point of promoting the field: before this, all 74 error
  // declarations across the corpus rendered identically as `type X error;`.
  //
  // 55 of http's 56 are distinct and `StatusCodeResponseDataBindingError` is not,
  // which is what keeps the non-distinct half of the rendering table from being
  // dead code nobody has ever exercised.
  const errors = errorsOf("ballerina__http");
  assert.equal(errors.size, 56);
  const plain = [...errors.values()].filter((error) => !error.isDistinct);
  assert.deepEqual(
    plain.map((error) => error.name),
    ["StatusCodeResponseDataBindingError"],
  );
  // And it is not distinct for a reason worth keeping visible: it is an alias for
  // a union of three other errors, which is exactly the shape `distinct` cannot
  // apply to.
  assert.equal(
    plain[0]?.base?.name,
    "MediaTypeBindingStatusCodeClientError|PayloadBindingStatusCodeClientError|HeaderBindingStatusCodeClientError",
  );
});

test("sap declares ClientError exactly once, not as both an injection and a placeholder", () => {
  const named = libraryFor("ballerinax__sap").typeDefs.filter((typeDef) => typeDef.name === "ClientError");
  assert.equal(named.length, 1, "a duplicate declaration is an ambiguity a name-addressed lookup cannot arbitrate");
  assert.equal(named[0]?.kind, "error");
});

test("the sap injection claims no distinctness, because Central never reported any", () => {
  const injected = libraryFor("ballerinax__sap").typeDefs.find(
    (typeDef): typeDef is Extract<TypeDef, { kind: "error" }> =>
      typeDef.kind === "error" && typeDef.name === "ClientError",
  );
  assert.equal(injected?.isDistinct, false);
  assert.equal(injected?.base, undefined);
});
