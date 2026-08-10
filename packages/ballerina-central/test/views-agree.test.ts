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
 * THE test that makes the addressed verbs safe, and a reviewer should refuse them
 * without it.
 *
 * The risk the verbs introduce is not that a document looks wrong — it is that one
 * of them shows a signature the package does not have, while `api` shows the right
 * one, and nothing in either document says they disagree. An agent then writes
 * code against a signature that came from a summariser's shortcut. The committed
 * `api` snapshots are the oracle: they are byte-exact against the recorded
 * payloads, so anything a view emits has to be findable in them verbatim.
 *
 * Five properties, over every fixture:
 *
 *   1. every signature a view prints appears in that fixture's `api` snapshot;
 *   2. every `type <Name>` body is `renderTypeDef` of that declaration exactly;
 *   3. every declaration resolves through `type`, and every name `type` resolves
 *      is in the index — in both directions;
 *   4. every path the tree offers is reachable by `ops`, and every `ops` path is
 *      in the tree;
 *   5. `--deps` closures terminate.
 */

import assert from "node:assert/strict";
import { test } from "node:test";
import { describeProvenance, type LoadedPackage } from "../src/library.js";
import { matchName } from "../src/match.js";
import { renderTypeDef } from "../src/render/typedef.js";
import { buildPathTree, indexDeclarations, operationsOf, resolvePath, splitPath } from "../src/symbols.js";
import { renderOpsView } from "../src/views/ops.js";
import { renderOverview } from "../src/views/overview.js";
import { renderTypeView } from "../src/views/type.js";
import { libraryFor, listFixtures, loadFixture, qualifiedForSlug, readSnapshot } from "./corpus.js";
import type { PathNode } from "../src/symbols.js";
import { collectReadmes } from "../src/readme.js";
import type { Version } from "../src/qualified.js";

/**
 * The IR every view renders from, under a FIXED provenance so a snapshot is
 * deterministic. The real header is run-order-dependent — the same command prints
 * `central` then `cache` — and pretending otherwise would make these tests fail on
 * a second run rather than on a regression.
 */
function loaded(slug: string): LoadedPackage {
  return {
    qualified: qualifiedForSlug(slug),
    version: "0.0.0-fixture" as Version,
    library: libraryFor(slug),
    readmes: collectReadmes(loadFixture(slug)),
    provenance: describeProvenance("central", false),
  };
}

/**
 * The document down to but not including its Guide.
 *
 * The guide is the package author's own Markdown, embedded verbatim, and its
 * ```ballerina samples are USAGE — `github:Client github = check new (config);` —
 * not declarations. They are the reason the guide is worth carrying and they have
 * no business in an oracle that checks signatures against the API document.
 */
function beforeGuide(document: string): string {
  const guide = document.search(/^## Guide/m);
  return guide === -1 ? document : document.slice(0, guide);
}

/** Ballerina lines inside ```ballerina fences, which is the only place a report may hold any. */
function fencedBallerina(document: string): string[] {
  const lines: string[] = [];
  let inside = false;
  for (const line of document.split("\n")) {
    if (line.startsWith("```ballerina")) {
      inside = true;
      continue;
    }
    if (line.startsWith("```")) {
      inside = false;
      continue;
    }
    if (inside && line.trim() !== "") lines.push(line);
  }
  return lines;
}

const fixtures = listFixtures();

// ---------------------------------------------------------------------------
// 1. Signatures agree with the API document
// ---------------------------------------------------------------------------

for (const slug of fixtures) {
  test(`every signature the overview prints is in ${slug}'s api snapshot, verbatim`, () => {
    const snapshot = new Set(readSnapshot(slug).split("\n").map((line) => line.trimStart()));
    const quoted = fencedBallerina(beforeGuide(renderOverview(loaded(slug))));
    assert.ok(quoted.length > 0, "a fixture whose overview quotes nothing would pass vacuously");
    for (const line of quoted) {
      assert.ok(
        snapshot.has(line.trimStart()),
        `the overview quotes a line the API document does not contain:\n  ${line}`,
      );
    }
  });
}

for (const slug of fixtures) {
  test(`every signature ops --sigs prints is in ${slug}'s api snapshot, verbatim`, () => {
    const snapshot = new Set(readSnapshot(slug).split("\n").map((line) => line.trimStart()));
    const library = libraryFor(slug);
    let checked = 0;
    for (const client of library.clients) {
      if (operationsOf(client).length === 0) continue;
      const view = renderOpsView(loaded(slug), { sigs: true, client: client.name });
      assert.ok(view.ok);
      for (const line of fencedBallerina(view.value)) {
        assert.ok(snapshot.has(line.trimStart()), `ops --sigs quotes a line api does not:\n  ${line}`);
        checked++;
      }
    }
    // Four fixtures declare no resource functions; those are covered by the ops
    // tests in cli.test.ts rather than here.
    assert.ok(checked > 0 || library.clients.every((client) => operationsOf(client).length === 0));
  });
}

// ---------------------------------------------------------------------------
// 2 and 3. Declarations resolve by name, and print exactly
// ---------------------------------------------------------------------------

for (const slug of fixtures) {
  test(`every declaration in ${slug} resolves through type, and prints identically`, () => {
    const context = loaded(slug);
    const index = indexDeclarations(context.library.typeDefs);
    assert.ok(index.names.length > 0);

    for (const name of index.names) {
      const view = renderTypeView(context, { names: [name], deps: false });
      assert.ok(view.ok, `type could not resolve ${name}, which the index holds`);
      const typeDef = index.get(name);
      assert.ok(typeDef);
      // Byte-exact, not merely "contains": `type` IS `renderTypeDef` plus a header,
      // so anything else means a view started reformatting declarations.
      assert.ok(
        view.value.includes(`\n${renderTypeDef(typeDef)}\n`),
        `type ${name} did not print renderTypeDef's output exactly`,
      );
    }
  });
}

for (const slug of fixtures) {
  test(`nothing outside ${slug}'s declaration index resolves through type`, () => {
    const context = loaded(slug);
    const index = indexDeclarations(context.library.typeDefs);
    const held = new Set(index.names);
    // The other direction. Scoped to DECLARATIONS deliberately: operations are
    // addressed by path and `type` does not take one, so demanding a bijection over
    // all of github's 8,837 symbols would be unsatisfiable.
    for (const invented of ["NoSuchDeclarationAnywhere", "zzzz", "__", "Client Name"]) {
      if (held.has(invented)) continue;
      const view = renderTypeView(context, { names: [invented], deps: false });
      assert.equal(view.ok, false, `${invented} must not resolve`);
      if (view.ok) continue;
      assert.equal(view.error.kind, "symbol-not-found");
    }
  });
}

test("a name that normalises onto several declarations is a failure, never a silent pick", () => {
  // Real, not theoretical: `ballerina/http` has 61 constant-versus-class collisions
  // of the STATUS_ACCEPTED / StatusAccepted shape.
  const index = indexDeclarations(libraryFor("ballerina__http").typeDefs);
  const collisions = index.names.filter((name) => {
    const match = matchName(name.toLowerCase(), index.names);
    return match.kind === "ambiguous";
  });
  assert.ok(collisions.length > 0, "the corpus should contain at least one normalisation collision");

  const first = collisions[0];
  assert.ok(first !== undefined);
  const view = renderTypeView(loaded("ballerina__http"), { names: [first.toLowerCase()], deps: false });
  assert.equal(view.ok, false);
  if (!view.ok && view.error.kind === "symbol-not-found") {
    assert.ok(view.error.candidates.length > 1, "every colliding name has to be listed");
  }
});

// ---------------------------------------------------------------------------
// 4. Every path the tree offers is reachable
// ---------------------------------------------------------------------------

/** Every path in the tree, as token lists. */
function allPaths(node: PathNode, prefix: readonly string[] = []): string[][] {
  return node.children.flatMap((child) => {
    const path = [...prefix, child.segment];
    return [path, ...allPaths(child, path)];
  });
}

for (const slug of fixtures) {
  test(`every path ${slug}'s tree offers is reachable by ops`, () => {
    // Hoisted: `renderOpsView` takes the loaded package, and building it inside the
    // per-path loop re-derived a 12.4MB fixture once for each of github's 900-odd
    // tree paths.
    const context = loaded(slug);
    const library = context.library;
    for (const client of library.clients) {
      const operations = operationsOf(client);
      if (operations.length === 0) continue;
      const tree = buildPathTree(operations);
      for (const path of allPaths(tree)) {
        // A navigation affordance that dead-ends is a test failure, not a wasted
        // agent turn.
        const resolution = resolvePath(tree, path);
        assert.equal(resolution.kind, "found", `${client.name}: ${path.join("/")} is offered but unreachable`);
        const view = renderOpsView(context, { sigs: false, path: path.join("/"), client: client.name });
        assert.ok(view.ok, `${client.name}: ops could not render ${path.join("/")}`);
      }
    }
  });
}

test("a wildcard addresses a parameter level, and the anchored match is not a suffix match", () => {
  const tree = buildPathTree(operationsOf(libraryFor("ballerinax__github").clients[0]!));

  const anchored = resolvePath(tree, splitPath("repos/*/*"));
  assert.equal(anchored.kind, "found");
  if (anchored.kind !== "found") return;
  // THE correctness property. An unanchored match for this path returns NINE
  // operations rather than three, because
  // `orgs/{org}/teams/{slug}/repos/{owner}/{repo}` and `teams/{id}/repos/{owner}/{repo}`
  // share the suffix and belong to unrelated subtrees. A caller that asked for one
  // path and got three others mixed in has no way to tell.
  assert.equal(anchored.node.operations.length, 3);
  assert.deepEqual(anchored.path, ["repos", "{owner}", "{repo}"]);

  // The same three are reachable by naming the parameters, with or without braces.
  for (const spelling of ["repos/{owner}/{repo}", "repos/owner/repo", "repos/*/repo"]) {
    const resolution = resolvePath(tree, splitPath(spelling));
    assert.equal(resolution.kind, "found", spelling);
    if (resolution.kind === "found") assert.equal(resolution.node.operations.length, 3);
  }
});

// ---------------------------------------------------------------------------
// 5. --deps terminates
// ---------------------------------------------------------------------------

for (const slug of fixtures) {
  test(`--deps closures terminate for every declaration in ${slug}`, () => {
    const context = loaded(slug);
    const index = indexDeclarations(context.library.typeDefs);
    for (const name of index.names) {
      // A record whose field is an array of itself is ordinary, so this is a real
      // cycle risk rather than a hypothetical one. `renderTypeView` returning at all
      // is the assertion; an unguarded walk would not.
      const view = renderTypeView(context, { names: [name], deps: true });
      assert.ok(view.ok, `--deps failed on ${name}`);
      // And no declaration is printed twice inside one closure.
      // `const` puts the TYPE before the name, so a naive capture reads `string`
      // three times and reports a duplicate that is not one.
      const declared = [
        ...view.value.matchAll(/^(?:type|enum|class) ([A-Za-z0-9_']+)|^const \S+ ([A-Za-z0-9_']+)/gm),
      ].map((match) => match[1] ?? match[2]);
      assert.equal(new Set(declared).size, declared.length, `--deps repeated a declaration for ${name}`);
    }
  });
}

test("--deps follows a chain in order and stops at the package boundary", () => {
  const view = renderTypeView(loaded("ballerina__http"), { names: ["ClientRequestError"], deps: true });
  assert.ok(view.ok);
  const order = [...view.value.matchAll(/^type ([A-Za-z0-9_]+) /gm)].map((match) => match[1]);
  assert.deepEqual(order.slice(0, 4), ["ClientRequestError", "ApplicationResponseError", "ClientError", "Error"]);
  assert.ok(order.includes("Detail"), "the detail record A1 unlocked has to be reachable");
});

test("--deps names cross-package edges instead of fetching them", () => {
  // `http:ConnectionConfig` has a LOCAL closure of one and fifteen external edges;
  // crossing the boundary would hide a five-second cold fetch inside an answer the
  // caller expects to be warm.
  const view = renderTypeView(loaded("ballerinax__github"), { names: ["ConnectionConfig"], deps: true });
  assert.ok(view.ok);
  assert.match(view.value, /^\/\/ Declared in other packages, not included above:$/m);
  assert.match(view.value, /ballerina\/http/);
  assert.match(view.value, /^\/\/ {3}bal-library type ballerina\/http .*--deps$/m);
});
