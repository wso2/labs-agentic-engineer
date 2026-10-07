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

/** Shared syntax-tree helpers for the static checks (acorn over the analysis transpile). */

import { parse, type Node } from "acorn";
import { simple } from "acorn-walk";
import { SOURCE_FILE, type Finding, type FindingCode } from "../findings.js";
import { transpileForAnalysis } from "./transpile.js";

export type SourceTree = Node;

/** The module's tree; throws (with a `loc`) when the source does not parse. */
export function parseSourceTree(source: string): SourceTree {
  return parse(transpileForAnalysis(source), { ecmaVersion: "latest", sourceType: "module", locations: true });
}

export function lineOf(node: Node): number {
  return (node as Node & { loc: { start: { line: number } } }).loc.start.line;
}

export function sourceFinding(code: FindingCode, node: Node, message: string): Finding {
  return { code, file: SOURCE_FILE, location: `line ${lineOf(node)}`, message };
}

/** Findings sorted by their line, stable within a line. */
export function byLine(findings: Finding[]): Finding[] {
  const line = (f: Finding) => Number(/^line (\d+)$/.exec(f.location)?.[1] ?? 0);
  return [...findings].sort((a, b) => line(a) - line(b));
}

export interface IdentifierNode extends Node {
  name: string;
}

export interface LiteralNode extends Node {
  value: unknown;
}

export function isIdentifier(node: Node | null | undefined, name?: string): node is IdentifierNode {
  return node?.type === "Identifier" && (name === undefined || (node as IdentifierNode).name === name);
}

export function stringLiteral(node: Node | null | undefined): string | undefined {
  if (node?.type !== "Literal") return undefined;
  const value = (node as LiteralNode).value;
  return typeof value === "string" ? value : undefined;
}

/** Every name the module binds anywhere (declarations, parameters, catch and import bindings): a module-wide set, not scope analysis. */
export function collectBoundNames(tree: SourceTree): Set<string> {
  const names = new Set<string>();
  const bind = (pattern: Node | null | undefined): void => {
    const p = pattern as unknown as Record<string, unknown> | null | undefined;
    if (!p) return;
    switch (p["type"]) {
      case "Identifier":
        names.add(p["name"] as string);
        break;
      case "ObjectPattern":
        for (const prop of p["properties"] as Node[]) bind((prop as unknown as { value?: Node; argument?: Node }).value ?? (prop as unknown as { argument?: Node }).argument);
        break;
      case "ArrayPattern":
        for (const element of p["elements"] as (Node | null)[]) bind(element);
        break;
      case "RestElement":
        bind(p["argument"] as Node);
        break;
      case "AssignmentPattern":
        bind(p["left"] as Node);
        break;
    }
  };
  const bindFunction = (node: Node): void => {
    const fn = node as unknown as { id?: Node | null; params: Node[] };
    bind(fn.id);
    fn.params.forEach(bind);
  };
  const bindClass = (node: Node): void => bind((node as unknown as { id?: Node | null }).id);
  simple(tree, {
    VariableDeclarator: (node) => bind((node as unknown as { id: Node }).id),
    FunctionDeclaration: bindFunction,
    FunctionExpression: bindFunction,
    ArrowFunctionExpression: bindFunction,
    ClassDeclaration: bindClass,
    ClassExpression: bindClass,
    CatchClause: (node) => bind((node as unknown as { param?: Node | null }).param),
    ImportSpecifier: (node) => bind((node as unknown as { local: Node }).local),
    ImportDefaultSpecifier: (node) => bind((node as unknown as { local: Node }).local),
    ImportNamespaceSpecifier: (node) => bind((node as unknown as { local: Node }).local),
  });
  return names;
}
