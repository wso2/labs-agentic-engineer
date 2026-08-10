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
 * `type <pkg> <Name>... [--deps]` — declarations, addressed by name.
 *
 * The CODE register: nothing but real declarations and a two-line provenance
 * comment. This is the verb that replaces the trace's grep-then-sed loop, and it
 * replaces it by removing the thing being navigated rather than by navigating
 * better — a record is read whole, so there is no closing brace to probe for and
 * no extent to get wrong. Reading `FullRepository` whole is 8,701 bytes against
 * the same 8,701 bytes plus two dead probe turns the golden trace paid.
 *
 * A large response is the right answer here. The agent needs the whole shape, and
 * the alternative (single-field addressing) needs an elision convention that is
 * itself a small lie about the declaration.
 */

import { matchName } from "../match.js";
import type { LoadedPackage } from "../library.js";
import type { TypeDef, TypeRef } from "../model.js";
import { formatQualifiedName } from "../qualified.js";
import { renderTypeDef } from "../render/typedef.js";
import { err, ok, type Result } from "../result.js";
import { indexDeclarations } from "../symbols.js";

/** Names that are Ballerina, not declarations, and are never in the index anyway. */
const IDENTIFIER = /[A-Za-z_][A-Za-z0-9_]*/g;

/** One reference to a declaration in another package, as written. */
export interface ExternalRef {
  readonly prefix: string;
  readonly name: string;
}

/**
 * Every type expression a declaration mentions.
 *
 * Read off the rendered expression rather than off `links`, because links are
 * lossy in exactly the places that matter here: `transformBasic` drops them for a
 * `map<Foo>` constraint, and `fromCentral` drops them for union and intersection
 * members. Adding them back would change what `buildSpecialAgentNote` prints and
 * move snapshots for a reason that has nothing to do with dependencies.
 */
function expressionsOf(typeDef: TypeDef): readonly TypeRef[] {
  switch (typeDef.kind) {
    case "record":
      return typeDef.fields.map((field) => field.type);
    case "union":
      return typeDef.members;
    case "constant":
      return [typeDef.varType];
    case "error":
      return typeDef.base === undefined ? [] : [typeDef.base];
    case "enum":
    case "class":
    case "other":
      return [];
    default: {
      const exhaustive: never = typeDef;
      return exhaustive;
    }
  }
}

/**
 * Split an expression's identifiers into same-package names and foreign ones.
 *
 * A token preceded by `:` is foreign and a token followed by `:` is the module
 * alias itself, which is how `http:Response` yields one external reference and no
 * local one. Everything else is looked up in the index, so builtins (`string`,
 * `map`, `anydata`) fall out for free by not being declarations.
 */
function partitionIdentifiers(expression: string): { local: string[]; external: ExternalRef[] } {
  const local: string[] = [];
  const external: ExternalRef[] = [];
  for (const match of expression.matchAll(IDENTIFIER)) {
    const token = match[0];
    const start = match.index;
    const before = start > 0 ? expression[start - 1] : "";
    const after = expression[start + token.length] ?? "";
    if (after === ":") continue;
    if (before === ":") {
      // Walk back over the alias to name it.
      const aliasEnd = start - 1;
      let aliasStart = aliasEnd;
      while (aliasStart > 0 && /[A-Za-z0-9_'.]/.test(expression[aliasStart - 1] ?? "")) aliasStart--;
      external.push({ prefix: expression.slice(aliasStart, aliasEnd), name: token });
      continue;
    }
    local.push(token);
  }
  return { local, external };
}

export interface TypeOptions {
  readonly names: readonly string[];
  readonly deps: boolean;
}

/**
 * The declarations, or one failure naming every name that did not resolve.
 *
 * ALL-OR-NOTHING across names. If any one fails, stdout gets nothing: "exit 0
 * means stdout is complete" is what every redirecting caller relies on, and a
 * partial document under exit 0 quietly breaks it.
 */
export function renderTypeView(loaded: LoadedPackage, options: TypeOptions): Result<string> {
  const index = indexDeclarations(loaded.library.typeDefs);
  const label = `${formatQualifiedName(loaded.qualified)}:${loaded.version}`;

  const roots: string[] = [];
  const unresolved: string[] = [];
  const candidates = new Set<string>();
  for (const requested of options.names) {
    const match = matchName(requested, index.names);
    if (match.kind === "found") {
      roots.push(match.name);
      continue;
    }
    unresolved.push(requested);
    for (const candidate of match.candidates) candidates.add(candidate);
  }

  if (unresolved.length > 0) {
    const ambiguous = unresolved.length === 1 && candidates.size > 1;
    return err({
      kind: "symbol-not-found",
      qualified: label,
      requested: unresolved,
      candidates: [...candidates],
      suggestion:
        candidates.size === 0
          ? `No declaration matched, and nothing in ${label} is close. Check the name against \`bal-library ${formatQualifiedName(loaded.qualified)}\`; add --refresh if you believe it was published since.`
          : `${ambiguous ? "Several declarations match" : "No declaration matched"}. Re-run \`type\` with one of the candidates; add --refresh if you believe the name exists and is newer than the cached copy.`,
    });
  }

  const selected = options.deps ? withDependencies(roots, index) : roots;
  const external = new Map<string, ExternalRef>();
  const blocks: string[] = [`// ${label}\n// Source: ${loaded.provenance}`];

  for (const name of selected) {
    const typeDef = index.get(name);
    if (typeDef === undefined) continue;
    blocks.push(renderTypeDef(typeDef));
    for (const expression of expressionsOf(typeDef)) {
      for (const reference of partitionIdentifiers(expression.name).external) {
        external.set(`${reference.prefix}:${reference.name}`, reference);
      }
    }
  }

  const footer = externalFooter([...external.values()], loaded);
  if (footer !== undefined) blocks.push(footer);

  return ok(`${blocks.join("\n\n")}\n`);
}

/**
 * The transitive same-package closure, depth-first from each root.
 *
 * Depth-first rather than breadth-first so a chain reads as a chain: an error's
 * `distinct` hierarchy comes out in the order a reader follows it, and the detail
 * record it eventually reaches comes after, not wedged into the middle.
 *
 * `visited` makes it terminate on a cycle, which real packages have — a record
 * whose field is an array of itself is ordinary.
 */
function withDependencies(roots: readonly string[], index: ReturnType<typeof indexDeclarations>): readonly string[] {
  const order: string[] = [];
  const visited = new Set<string>();

  const walk = (name: string): void => {
    if (visited.has(name)) return;
    const typeDef = index.get(name);
    if (typeDef === undefined) return;
    visited.add(name);
    order.push(name);
    for (const expression of expressionsOf(typeDef)) {
      for (const token of partitionIdentifiers(expression.name).local) {
        if (index.get(token) !== undefined) walk(token);
      }
    }
  };

  for (const root of roots) walk(root);
  return order;
}

/**
 * Names from other packages, and the command that reads them.
 *
 * `--deps` stays same-package on purpose. `ballerina/http:ConnectionConfig` has a
 * local closure of ONE and fifteen external edges — `BearerTokenConfig`,
 * `HttpVersion`, `ClientHttp1Settings` and the rest all live in another payload —
 * so crossing the boundary would hide a five-second cold fetch inside an answer
 * the caller expects to be warm. Naming the edge plus the exact follow-up gets
 * the same fact predictably.
 *
 * A `//` comment rather than prose because this is the code register, where a
 * comment annotates real declarations instead of impersonating them.
 */
function externalFooter(references: readonly ExternalRef[], loaded: LoadedPackage): string | undefined {
  if (references.length === 0) return undefined;

  const byPrefix = new Map<string, string[]>();
  for (const reference of references) {
    const names = byPrefix.get(reference.prefix);
    if (names) names.push(reference.name);
    else byPrefix.set(reference.prefix, [reference.name]);
  }

  // The alias is all Central gives at a use site, so the package it stands for is
  // recovered from the links the same payload published elsewhere.
  const libraries = new Map<string, string>();
  for (const typeDef of loaded.library.typeDefs) {
    for (const expression of expressionsOf(typeDef)) {
      for (const link of expression.links ?? []) {
        if (link.category !== "external") continue;
        const alias = link.libraryName.split(/[/.]/).pop();
        if (alias !== undefined) libraries.set(alias, link.libraryName);
      }
    }
  }

  const lines = ["// Declared in other packages, not included above:"];
  for (const [prefix, names] of [...byPrefix].sort(([a], [b]) => a.localeCompare(b))) {
    const library = libraries.get(prefix) ?? prefix;
    lines.push(`//   ${[...new Set(names)].sort().join(", ")}  <-  ${library}`);
    lines.push(`//   bal-library type ${library} ${[...new Set(names)].sort().join(" ")} --deps`);
  }
  return lines.join("\n");
}
