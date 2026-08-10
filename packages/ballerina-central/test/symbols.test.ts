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
 * The two indexes, and the discovery corpus.
 *
 * The path corpus at the bottom is built from the lookup terms the nine recorded
 * playground runs actually used, each pinned with its hit count. That makes a
 * regression a reviewable diff — and, more importantly, makes a ZERO-hit pin
 * impossible to mistake for a working index, which is how the earlier `.bala`
 * grep loop failed silently for five runs in a row.
 */

import assert from "node:assert/strict";
import { test } from "node:test";
import { matchName, nearMisses, normalise, MAX_CANDIDATES } from "../src/match.js";
import type { ClientClass, Fn } from "../src/model.js";
import {
  autoDescend,
  buildPathTree,
  indexDeclarations,
  operationsOf,
  operationsUnder,
  readableSegment,
  resolvePath,
  splitPath,
} from "../src/symbols.js";
import { libraryFor } from "./corpus.js";

function clientOf(slug: string, name?: string): ClientClass {
  const clients = libraryFor(slug).clients;
  const client = name === undefined ? clients[0] : clients.find((candidate) => candidate.name === name);
  assert.ok(client, `${slug} has no client ${name ?? "(first)"}`);
  return client;
}

const github = () => buildPathTree(operationsOf(clientOf("ballerinax__github")));

// ---------------------------------------------------------------------------
// Ballerina's identifier escaping
// ---------------------------------------------------------------------------

test("a segment reads in prose without the escaping it needs in source", () => {
  // Central publishes github's paths as Ballerina writes them. Those spellings are
  // right inside a fence and unusable as a shell argument, which is where the agent
  // has to type them.
  assert.equal(readableSegment("code\\-scanning"), "code-scanning");
  assert.equal(readableSegment("'import"), "import");
  assert.equal(readableSegment("app\\-manifests"), "app-manifests");
  assert.equal(readableSegment("rate_limit"), "rate_limit", "an underscore needs no escaping and keeps none");
  assert.equal(readableSegment("repos"), "repos");
});

test("both spellings address the same node, because an agent may copy either", () => {
  const tree = github();
  for (const spelling of ["repos/*/*/code-scanning", "repos/*/*/code\\-scanning"]) {
    const resolution = resolvePath(tree, splitPath(spelling));
    assert.equal(resolution.kind, "found", spelling);
  }
});

// ---------------------------------------------------------------------------
// The tree
// ---------------------------------------------------------------------------

test("the tree accounts for every operation exactly once", () => {
  for (const slug of ["ballerinax__github", "ballerinax__slack", "ballerinax__googleapis.gmail"]) {
    for (const client of libraryFor(slug).clients) {
      const operations = operationsOf(client);
      if (operations.length === 0) continue;
      const tree = buildPathTree(operations);
      assert.equal(tree.total, operations.length, `${slug} ${client.name}: total`);
      assert.equal(operationsUnder(tree).length, operations.length, `${slug} ${client.name}: walk`);
    }
  }
});

test("github's 903 operations reduce to 36 top-level segments", () => {
  // The measurement the whole discovery argument rests on: a substring search for
  // `repos` returns 484 hits, and this is 36 lines.
  const tree = github();
  assert.equal(tree.total, 903);
  assert.equal(tree.children.length, 36);
  assert.equal(tree.children[0]?.segment, "repos");
  assert.equal(tree.children[0]?.total, 421, "busiest subtree first, because the tree is read to choose");
});

test("slack's paths do not nest, and the tree says so rather than pretending", () => {
  // 174 operations across 174 distinct top segments: its paths are RPC-style dotted
  // names, so level 1 IS the complete list. An agent told to descend a flat list
  // spends a turn discovering there is nowhere to go.
  const tree = buildPathTree(operationsOf(clientOf("ballerinax__slack")));
  assert.equal(tree.total, 174);
  assert.equal(tree.children.length, 174);
  assert.ok(tree.children.every((child) => child.children.length === 0));
});

test("children are ordered by operation count, then alphabetically", () => {
  const tree = github();
  for (let i = 1; i < tree.children.length; i++) {
    const previous = tree.children[i - 1];
    const current = tree.children[i];
    assert.ok(previous && current);
    assert.ok(
      previous.total > current.total || (previous.total === current.total && previous.segment <= current.segment),
      `${previous.segment}(${previous.total}) before ${current.segment}(${current.total})`,
    );
  }
});

// ---------------------------------------------------------------------------
// Anchoring
// ---------------------------------------------------------------------------

test("a path is matched from the FIRST segment, never as a suffix", () => {
  const tree = github();
  const anchored = resolvePath(tree, splitPath("repos/*/*"));
  assert.equal(anchored.kind, "found");
  if (anchored.kind !== "found") return;
  assert.equal(anchored.node.operations.length, 3);

  // The counter-measurements, which are why this walks the tree instead of filtering
  // strings. Two unrelated subtrees END in the same three segments, so a SUFFIX match
  // returns nine operations for a path that has three — and a SUBSTRING match returns
  // 426. In both cases the caller asked for one path, got other subtrees mixed in,
  // and has nothing in the output to tell them apart.
  const all = operationsUnder(tree);
  const target = "repos/{owner}/{repo}";
  const bySuffix = all.filter((operation) => operation.segments.join("/").endsWith(target));
  const bySubstring = all.filter((operation) => operation.segments.join("/").includes(target));
  assert.equal(bySuffix.length, 9);
  assert.equal(bySubstring.length, 426);

  const strangers = new Set(
    bySuffix
      .map((operation) => operation.segments.join("/"))
      .filter((path) => !path.startsWith(target)),
  );
  assert.deepEqual(
    [...strangers].sort(),
    ["orgs/{org}/teams/{teamSlug}/repos/{owner}/{repo}", "teams/{teamId}/repos/{owner}/{repo}"],
    "these two are what an unanchored match mixes in, and they are about team access, not repositories",
  );
});

test("a path that runs out names what was there instead of failing blankly", () => {
  const resolution = resolvePath(github(), splitPath("repos/*/*/nonesuch"));
  assert.equal(resolution.kind, "missing");
  if (resolution.kind !== "missing") return;
  assert.deepEqual(resolution.matched, ["repos", "{owner}", "{repo}"]);
  assert.equal(resolution.token, "nonesuch");
  assert.equal(resolution.available.length, 63);
});

test("a first segment that is wrong reports nothing matched, not a partial path", () => {
  const resolution = resolvePath(github(), splitPath("nonesuch/repos"));
  assert.equal(resolution.kind, "missing");
  if (resolution.kind === "missing") assert.deepEqual(resolution.matched, []);
});

test("an empty path is the root, which is every operation", () => {
  const resolution = resolvePath(github(), splitPath(""));
  assert.equal(resolution.kind, "found");
  if (resolution.kind === "found") assert.equal(resolution.node.total, 903);
});

// ---------------------------------------------------------------------------
// Auto-descent
// ---------------------------------------------------------------------------

test("descent steps through parameter-only levels and names the branch it did not take", () => {
  const tree = github();
  const repos = resolvePath(tree, ["repos"]);
  assert.equal(repos.kind, "found");
  if (repos.kind !== "found") return;

  const descent = autoDescend(repos.node, repos.path);
  assert.deepEqual(descent.path, ["repos", "{owner}", "{repo}"]);
  assert.equal(descent.node.operations.length, 3);

  // Naming the sibling is not cosmetic: `repos` really has two parameter children
  // with different spellings, and collapsing silently to the dominant one hides an
  // operation permanently, because nothing downstream would mention it again.
  assert.equal(descent.skipped.length, 1);
  assert.deepEqual(descent.skipped[0]?.path, ["repos", "{templateOwner}", "{templateRepo}", "generate"]);
  assert.equal(descent.skipped[0]?.total, 1);
});

test("descent stops at a level with a literal child, because that is a real choice", () => {
  const tree = github();
  const user = resolvePath(tree, ["user"]);
  assert.equal(user.kind, "found");
  if (user.kind !== "found") return;
  const descent = autoDescend(user.node, user.path);
  // `user` has operations of its own AND literal children; choosing for the caller
  // there would be guessing which resource they meant.
  assert.deepEqual(descent.path, ["user"]);
  assert.equal(descent.skipped.length, 0);
});

test("descent terminates on a leaf", () => {
  const tree = github();
  const zen = resolvePath(tree, ["zen"]);
  assert.equal(zen.kind, "found");
  if (zen.kind !== "found") return;
  const descent = autoDescend(zen.node, zen.path);
  assert.deepEqual(descent.path, ["zen"]);
  assert.equal(descent.node.operations.length, 1);
});

// ---------------------------------------------------------------------------
// The declaration index
// ---------------------------------------------------------------------------

test("a duplicate name keeps the first declaration, so a collision cannot pass unnoticed", () => {
  const index = indexDeclarations([
    { kind: "error", name: "ClientError", description: "the correction", isDistinct: false },
    { kind: "other", name: "ClientError", description: "Central's placeholder" },
  ]);
  assert.deepEqual(index.names, ["ClientError"]);
  assert.equal(index.get("ClientError")?.kind, "error");
});

test("github's declaration roster is 1,224 names and its operations are not in it", () => {
  const index = indexDeclarations(libraryFor("ballerinax__github").typeDefs);
  assert.equal(index.names.length, 1224);
  // Operations are addressed by path, not by name, which is why the oracle's
  // bijection is scoped to declarations.
  assert.equal(index.get("repos"), undefined);
});

// ---------------------------------------------------------------------------
// Name matching
// ---------------------------------------------------------------------------

test("normalisation is what STATUS_ACCEPTED and StatusAccepted have in common", () => {
  assert.equal(normalise("STATUS_ACCEPTED"), "statusaccepted");
  assert.equal(normalise("StatusAccepted"), "statusaccepted");
  assert.equal(normalise("client_id"), "clientid");
  assert.equal(normalise("clientId"), "clientid");
  assert.equal(normalise("'import"), "import");
});

test("an exact match wins before normalisation is even tried", () => {
  // github's ManifestConversions declares BOTH clientId and client_id, so the exact
  // spelling has to be honoured or one of them is unreachable.
  const names = ["clientId", "client_id"];
  assert.deepEqual(matchName("clientId", names), { kind: "found", name: "clientId" });
  assert.deepEqual(matchName("client_id", names), { kind: "found", name: "client_id" });
});

test("a normalised collision is reported with every match, never resolved silently", () => {
  const match = matchName("CLIENTID", ["clientId", "client_id"]);
  assert.equal(match.kind, "ambiguous");
  if (match.kind === "ambiguous") assert.deepEqual([...match.candidates].sort(), ["clientId", "client_id"]);
});

test("a miss suggests names by the longest run of characters they share", () => {
  const names = ["FullRepository", "MinimalRepository", "NullableRepository", "Repository", "SimpleUser", "Issue"];
  const candidates = nearMisses("FullRepo", names);
  assert.equal(candidates[0], "FullRepository", "the closest first");
  for (const expected of ["MinimalRepository", "NullableRepository", "Repository"]) {
    assert.ok(candidates.includes(expected), `${expected} shares 'Repo'`);
  }
  assert.ok(!candidates.includes("Issue"), "four characters of overlap is the floor, and 'Issue' shares none");
});

test("candidates are capped, because the alternative is the whole roster on stderr", () => {
  const index = indexDeclarations(libraryFor("ballerinax__github").typeDefs);
  // 33,431 bytes for github's 1,224 names, inside a JSON object an agent has to read.
  assert.ok(nearMisses("Repository", index.names).length <= MAX_CANDIDATES);
});

test("a request that resembles nothing gets no candidates rather than noise", () => {
  const match = matchName("zzzzzz", ["FullRepository", "SimpleUser"]);
  assert.equal(match.kind, "missing");
  if (match.kind === "missing") assert.deepEqual(match.candidates, []);
});

// ---------------------------------------------------------------------------
// The discovery corpus
// ---------------------------------------------------------------------------

/**
 * Every distinct lookup the nine recorded runs made, as this design addresses it.
 *
 * Each row is pinned with its hit count so a regression is a diff. A row whose
 * count is zero would mean the index cannot answer a question a real run asked,
 * which is the failure mode the `.bala` grep loop had and nobody noticed.
 */
const RECORDED_LOOKUPS: readonly { fixture: string; path: string; operations: number }[] = [
  // The golden run: a repository's star count.
  { fixture: "ballerinax__github", path: "repos", operations: 421 },
  { fixture: "ballerinax__github", path: "repos/*/*", operations: 420 },
  { fixture: "ballerinax__github", path: "repos/*/*/issues", operations: 30 },
  { fixture: "ballerinax__github", path: "repos/*/*/pulls", operations: 31 },
  { fixture: "ballerinax__github", path: "repos/*/*/stargazers", operations: 1 },
  { fixture: "ballerinax__github", path: "user", operations: 93 },
  { fixture: "ballerinax__github", path: "orgs", operations: 200 },
  { fixture: "ballerinax__github", path: "search", operations: 7 },
  // Connectors the other runs reached for.
  { fixture: "ballerinax__googleapis.gmail", path: "users", operations: 32 },
  { fixture: "ballerinax__slack", path: "chat.postMessage", operations: 1 },
  { fixture: "ballerinax__slack", path: "conversations.list", operations: 1 },
];

for (const lookup of RECORDED_LOOKUPS) {
  test(`the recorded lookup ${lookup.fixture} '${lookup.path}' resolves to ${lookup.operations}`, () => {
    const tree = buildPathTree(operationsOf(clientOf(lookup.fixture)));
    const resolution = resolvePath(tree, splitPath(lookup.path));
    assert.equal(resolution.kind, "found", `${lookup.path} is unreachable`);
    if (resolution.kind !== "found") return;
    const found = operationsUnder(resolution.node).length;
    assert.equal(found, lookup.operations, `${lookup.path} moved from ${lookup.operations} to ${found}`);
    assert.ok(found > 0, "a zero-hit pin cannot masquerade as a working index");
  });
}

test("every operation in the corpus carries a return type, which is what the lookup came for", () => {
  // Two of the golden run's twelve turns went to learning what an operation returns.
  // If a signature can render without one, the verb that replaces those turns is
  // sometimes silently useless.
  for (const slug of ["ballerinax__github", "ballerinax__slack", "ballerinax__googleapis.gmail"]) {
    for (const client of libraryFor(slug).clients) {
      for (const fn of client.functions as readonly Fn[]) {
        assert.ok(fn.returns.type.name.length > 0, `${slug} ${client.name}: a signature with no return type`);
      }
    }
  }
});
