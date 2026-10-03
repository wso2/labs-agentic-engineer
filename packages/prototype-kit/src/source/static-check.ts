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
 * The static half of the source check: what can be refused without running
 * anything. It reads the module's syntax tree, so a word inside a string or a
 * mock record ("window", "fetch") is never a finding. A guard rail that
 * teaches the rules — the sandboxed frame and the isolated render check are
 * the boundary.
 */

import type { Node } from "acorn";
import { ancestor } from "acorn-walk";
import { SOURCE_FILE, type Finding } from "../findings.js";
import { syntaxFinding } from "./transpile.js";
import { byLine, collectBoundNames, isIdentifier, parseSourceTree, sourceFinding, stringLiteral, type IdentifierNode } from "./tree.js";

/** The modules a prototype may import. */
export const PROTOTYPE_IMPORTS = ["react", "@wso2/prototype-kit"] as const;

/** What the transpiled module requires besides its imports: the automatic JSX runtime. */
export const PROTOTYPE_RUNTIME_IMPORTS = [...PROTOTYPE_IMPORTS, "react/jsx-runtime"] as const;

/** The largest source a prototype may be, in UTF-16 code units. */
export const MAX_SOURCE_LENGTH = 256 * 1024;

/** Globals the frame does not offer: the page, storage, the network, workers, code generation, the host runtime. */
const FORBIDDEN_GLOBALS = new Set([
  "window", "self", "globalThis", "document", "top", "parent", "frames", "opener", "location", "history",
  "navigator", "localStorage", "sessionStorage", "indexedDB", "caches", "cookieStore", "fetch", "XMLHttpRequest",
  "WebSocket", "EventSource", "Request", "Worker", "SharedWorker", "importScripts", "postMessage", "eval",
  "Function", "process", "global", "Proxy", "Reflect", "WebAssembly",
]);

/** Member names that reach behind an object to its constructor or prototype chain. */
const FORBIDDEN_MEMBERS = new Set(["constructor", "__proto__", "prototype", "createElement"]);

/** Nondeterminism: what a reviewer sees must not change under them. */
const NONDETERMINISTIC = new Map([
  ["Math", new Set(["random"])],
  ["Date", new Set(["now"])],
]);

const IMPORT_RULE = "a prototype imports only react and @wso2/prototype-kit, with static import declarations";

export function checkSource(source: string): Finding[] {
  if (source.length > MAX_SOURCE_LENGTH) {
    return [
      {
        code: "SOURCE_TOO_LARGE",
        file: SOURCE_FILE,
        location: "(file)",
        message: `prototype.tsx is ${source.length} characters; a prototype is at most ${MAX_SOURCE_LENGTH}. Split long mock data or remove unused screens.`,
      },
    ];
  }
  let tree: Node;
  try {
    tree = parseSourceTree(source);
  } catch (e) {
    return [syntaxFinding(e)];
  }

  const bound = collectBoundNames(tree);
  const findings: Finding[] = [];
  const report = (code: Finding["code"], node: Node, message: string) => findings.push(sourceFinding(code, node, message));

  // The local names the JSX runtime's element factories are bound to.
  const factories = new Set<string>();
  ancestor(tree, {
    ImportDeclaration(node) {
      const decl = node as unknown as ImportNode;
      const from = decl.source.value;
      if (!(PROTOTYPE_RUNTIME_IMPORTS as readonly string[]).includes(from)) {
        report("FORBIDDEN_IMPORT", node, `import of ${JSON.stringify(from)}: ${IMPORT_RULE}`);
        return;
      }
      if (from !== "react/jsx-runtime") return;
      for (const spec of decl.specifiers) {
        const imported = spec.imported?.name;
        if (imported === "jsx" || imported === "jsxs") factories.add(spec.local.name);
      }
    },
  });

  ancestor(tree, {
    ImportExpression(node) {
      report("FORBIDDEN_IMPORT", node, `a dynamic import(): ${IMPORT_RULE}`);
    },
    MetaProperty(node) {
      report("FORBIDDEN_API", node, "import.meta is not available to a prototype");
    },
    CallExpression(node) {
      const call = node as unknown as { callee: Node; arguments: Node[] };
      if (!isIdentifier(call.callee) || !factories.has(call.callee.name)) return;
      const tag = stringLiteral(call.arguments[0]);
      if (tag !== undefined) {
        report("FORBIDDEN_ELEMENT", node, `<${tag}> is a raw HTML element: draw screens with @wso2/prototype-kit components only`);
      }
    },
    Identifier(node, _state, ancestors) {
      const name = (node as unknown as IdentifierNode).name;
      if (!isReference(node, ancestors)) return;
      if (name === "require" && !bound.has(name)) {
        report("FORBIDDEN_IMPORT", node, `require: ${IMPORT_RULE}`);
      } else if (FORBIDDEN_GLOBALS.has(name) && !bound.has(name)) {
        report("FORBIDDEN_API", node, `${name} is not available to a prototype: it renders kit components over its own mock data`);
      }
    },
    MemberExpression(node) {
      const member = node as unknown as MemberNode;
      const name = member.computed ? stringLiteral(member.property) : (member.property as IdentifierNode).name;
      if (name === undefined) return;
      if (FORBIDDEN_MEMBERS.has(name)) {
        report("FORBIDDEN_API", node, `.${name} is not available to a prototype`);
        return;
      }
      if (isIdentifier(member.object) && NONDETERMINISTIC.get(member.object.name)?.has(name)) {
        report("FORBIDDEN_API", node, `${member.object.name}.${name}() is nondeterministic: use fixed literals, and useToday() for today's date`);
      }
    },
    Property(node) {
      const prop = node as unknown as { key: Node; computed: boolean };
      const key = prop.computed ? undefined : isIdentifier(prop.key) ? prop.key.name : stringLiteral(prop.key);
      if (key === "dangerouslySetInnerHTML") report("FORBIDDEN_API", node, "dangerouslySetInnerHTML is not available to a prototype");
    },
    NewExpression(node) {
      const expr = node as unknown as { callee: Node; arguments: Node[] };
      if (isIdentifier(expr.callee, "Date") && expr.arguments.length === 0) {
        report("FORBIDDEN_API", node, "new Date() is the current time: use fixed literals, and useToday() for today's date");
      }
    },
  });
  return byLine(findings);
}

interface ImportNode {
  source: { value: string };
  specifiers: { imported?: { name: string }; local: { name: string } }[];
}

interface MemberNode {
  object: Node;
  property: Node;
  computed: boolean;
}

/**
 * Whether an identifier reads a binding — not a member's spelled-out name
 * (`a.window`), an object literal's key (`{ document: … }`), a class member's
 * name, or part of an import declaration.
 */
function isReference(node: Node, ancestors: Node[]): boolean {
  const parent = ancestors[ancestors.length - 2] as unknown as Record<string, unknown> | undefined;
  if (!parent) return true;
  const type = parent["type"];
  if (type === "MemberExpression" && parent["property"] === node && !parent["computed"]) return false;
  if (type === "Property" && parent["key"] === node && !parent["computed"] && !parent["shorthand"]) return false;
  if (type === "MethodDefinition" || type === "PropertyDefinition") return parent["key"] !== node;
  if (typeof type === "string" && type.startsWith("Import")) return false;
  return true;
}
