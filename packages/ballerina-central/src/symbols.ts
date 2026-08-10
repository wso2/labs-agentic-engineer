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
 * The two indexes the addressed verbs read: declarations by name, and operations
 * by path.
 *
 * Discovery is what this file exists for. The recorded golden trace greps for
 * `repos/[string owner]/[string repo]` because the model already had GitHub's
 * REST API memorised — measured, an agent without that knowledge has nowhere to
 * go, since the roster says `repos(421)` and a substring search for `repos`
 * returns 484 hits. A path tree is the answer that does not need the answer:
 * `ballerinax/github`'s 903 operations reduce to 36 top-level segments in 445
 * bytes, and each level names the next.
 */

import type { ClientClass, Fn, PathSegment, TypeDef } from "./model.js";

// ---------------------------------------------------------------------------
// Declarations
// ---------------------------------------------------------------------------

export interface DeclarationIndex {
  /** Every declaration name, in the package's own order. */
  readonly names: readonly string[];
  get(name: string): TypeDef | undefined;
}

/**
 * Declarations by name.
 *
 * A duplicate name keeps the FIRST declaration, because `patches.ts` prepends its
 * injections and those are corrections of what Central got wrong. There should be
 * no duplicates left — the sap `ClientError` collision was the only one and it is
 * fixed — but a silent last-wins would make the next one impossible to notice.
 */
export function indexDeclarations(typeDefs: readonly TypeDef[]): DeclarationIndex {
  const byName = new Map<string, TypeDef>();
  const names: string[] = [];
  for (const typeDef of typeDefs) {
    if (byName.has(typeDef.name)) continue;
    byName.set(typeDef.name, typeDef);
    names.push(typeDef.name);
  }
  return { names, get: (name) => byName.get(name) };
}

// ---------------------------------------------------------------------------
// Operations
// ---------------------------------------------------------------------------

export type ResourceFn = Extract<Fn, { kind: "resource" }>;

/** A resource function plus its path in the display spelling the tree navigates by. */
export interface Operation {
  readonly fn: ResourceFn;
  readonly segments: readonly string[];
}

/**
 * A literal segment with Ballerina's identifier escaping removed.
 *
 * Central publishes github's paths as Ballerina writes them: `code\\-scanning`,
 * because `-` needs escaping in an identifier, and `'import`, because `import` is
 * a reserved word. Those are correct INSIDE a fence, where the line is a
 * quotation of source — and unusable in prose or as a shell argument, which is
 * where an agent has to type them. Unescaping here and matching tolerantly below
 * is the same two-registers split the whole design rests on.
 */
export function readableSegment(text: string): string {
  return text.replace(/^'/, "").replace(/\\(?=[^A-Za-z0-9_])/g, "");
}

/**
 * How a path segment reads in prose: `repos`, or `{owner}` for a parameter.
 *
 * Deliberately NOT the `[string owner]` declaration spelling. That form belongs
 * inside a fenced block where it is a quotation of source; in prose and in a
 * command argument it is three characters of shell escaping that cost the golden
 * trace two turns when `\b` after `]` never matched.
 */
export function displaySegment(segment: PathSegment): string {
  if (segment.kind === "literal") return readableSegment(segment.text);
  return segment.type.endsWith("...") ? `{...${segment.name}}` : `{${segment.name}}`;
}

export function operationsOf(client: ClientClass): readonly Operation[] {
  return client.functions.flatMap((fn) =>
    fn.kind === "resource" ? [{ fn, segments: fn.paths.map(displaySegment) }] : [],
  );
}

export interface PathNode {
  /** The display segment this node is reached by; `""` at the root. */
  readonly segment: string;
  readonly isParam: boolean;
  /** Operations that END here — the ones a caller at this path can invoke. */
  readonly operations: readonly Operation[];
  readonly children: readonly PathNode[];
  /** Operations at or under this node. What the tree prints beside each segment. */
  readonly total: number;
}

interface MutableNode {
  segment: string;
  isParam: boolean;
  operations: Operation[];
  children: Map<string, MutableNode>;
}

function emptyNode(segment: string, isParam: boolean): MutableNode {
  return { segment, isParam, operations: [], children: new Map() };
}

/**
 * Children ordered by how many operations they lead to, then alphabetically.
 *
 * Descending count because the tree is read to choose where to go next, and the
 * busiest subtree is the likeliest answer. Alphabetical ties because the order has
 * to be stable enough to snapshot.
 */
function freeze(node: MutableNode): PathNode {
  const children = [...node.children.values()]
    .map(freeze)
    .sort((a, b) => b.total - a.total || a.segment.localeCompare(b.segment));
  return {
    segment: node.segment,
    isParam: node.isParam,
    operations: node.operations,
    children,
    total: node.operations.length + children.reduce((sum, child) => sum + child.total, 0),
  };
}

export function buildPathTree(operations: readonly Operation[]): PathNode {
  const root = emptyNode("", false);
  for (const operation of operations) {
    let node = root;
    for (const [index, segment] of operation.segments.entries()) {
      const isParam = operation.fn.paths[index]?.kind === "param";
      let child = node.children.get(segment);
      if (!child) {
        child = emptyNode(segment, isParam);
        node.children.set(segment, child);
      }
      node = child;
    }
    node.operations.push(operation);
  }
  return freeze(root);
}

// ---------------------------------------------------------------------------
// Navigating it
// ---------------------------------------------------------------------------

/**
 * Does one path token address this node?
 *
 * `*` is the wildcard, so `repos/*` addresses a level whose segment is a
 * parameter without spelling the parameter's name. A parameter also answers to
 * its own name with or without braces, because an agent reading `{owner}` off a
 * tree will type either.
 */
function tokenMatches(token: string, node: PathNode): boolean {
  if (token === "*") return true;
  // The escaped and quoted spellings both answer, because an agent that copied a
  // path out of a fenced signature will type `code\\-scanning` or `'import`.
  if (token === node.segment || readableSegment(token) === node.segment) return true;
  if (!node.isParam) return false;
  const bare = node.segment.replace(/^\{\.{0,3}|\}$/g, "");
  return token === bare || token === `{${bare}}`;
}

export function splitPath(path: string): readonly string[] {
  return path.split("/").filter((token) => token !== "");
}

export type PathResolution =
  | { readonly kind: "found"; readonly node: PathNode; readonly path: readonly string[] }
  /** The prefix matched, then a token matched nothing. `available` is what was there. */
  | {
      readonly kind: "missing";
      readonly matched: readonly string[];
      readonly token: string;
      readonly available: readonly string[];
    };

/**
 * Walk a path from the FIRST segment, one token per level.
 *
 * Anchoring is the whole contract. An unanchored or suffix match for
 * `repos/{owner}/{repo}` on github returns NINE operations rather than three,
 * because `orgs/{org}/teams/{slug}/repos/{owner}/{repo}` and
 * `teams/{id}/repos/{owner}/{repo}` share the suffix and belong to unrelated
 * subtrees. A caller that asked for one path and got three others mixed in has no
 * way to tell — which makes substring matching not a convenience but a
 * correctness bug, and the reason this walks the tree instead of filtering
 * strings.
 */
export function resolvePath(root: PathNode, tokens: readonly string[]): PathResolution {
  let node = root;
  const matched: string[] = [];
  for (const token of tokens) {
    const next = node.children.find((child) => tokenMatches(token, child));
    if (!next) {
      return {
        kind: "missing",
        matched,
        token,
        available: node.children.map((child) => child.segment),
      };
    }
    node = next;
    matched.push(node.segment);
  }
  return { kind: "found", node, path: matched };
}

/** Every operation at or under a node, in tree order. */
export function operationsUnder(node: PathNode): readonly Operation[] {
  return [...node.operations, ...node.children.flatMap(operationsUnder)];
}

export interface Descent {
  readonly node: PathNode;
  readonly path: readonly string[];
  /** Levels stepped through, and the sibling branches not taken at each. */
  readonly skipped: readonly { readonly path: readonly string[]; readonly total: number }[];
}

/**
 * The full path of a sibling branch, followed through its own parameter-only
 * levels.
 *
 * `repos/{templateOwner}` on its own is not an address a caller can use — the
 * operation is at `repos/{templateOwner}/{templateRepo}`. Naming the branch
 * without naming where it goes makes the skipped operation harder to reach than
 * before it was mentioned.
 */
function fullSiblingPath(node: PathNode, path: readonly string[]): readonly string[] {
  const walked = [...path];
  let current = node;
  while (current.operations.length === 0 && current.children.length === 1 && current.children[0] !== undefined) {
    current = current.children[0];
    walked.push(current.segment);
  }
  return walked;
}

/**
 * Step down through levels that add no routing choice, and NAME what was skipped.
 *
 * `ops <pkg> repos` should land on the operations, not on a level whose only
 * content is "there is a parameter here". But the skipping cannot be silent:
 * `repos` genuinely has two parameter children with different spellings —
 * `{owner}` with 420 operations and `{templateOwner}` with 1 — and collapsing to
 * the dominant one without saying so hides an operation permanently, since
 * nothing downstream would ever mention it again.
 *
 * Only parameter-only levels are stepped through. A level with a literal child is
 * a real choice about which resource to address, and choosing for the caller
 * there would be guessing.
 */
export function autoDescend(node: PathNode, path: readonly string[]): Descent {
  const walked = [...path];
  const skipped: { path: readonly string[]; total: number }[] = [];
  let current = node;

  while (current.operations.length === 0 && current.children.length > 0 && current.children.every((c) => c.isParam)) {
    const [dominant, ...siblings] = current.children;
    if (!dominant) break;
    for (const sibling of siblings) {
      skipped.push({ path: fullSiblingPath(sibling, [...walked, sibling.segment]), total: sibling.total });
    }
    walked.push(dominant.segment);
    current = dominant;
  }

  return { node: current, path: walked, skipped };
}
