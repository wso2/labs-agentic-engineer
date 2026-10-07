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
 * The source's references into the manifest that can be read without running
 * it: every literal `to="…"` on a kit component and every literal
 * `go("…")`. Each must be one of the manifest's screens. Computed targets,
 * and whether the viewing role reaches a target, are the render check's.
 */

import type { Node } from "acorn";
import { ancestor } from "acorn-walk";
import type { Finding } from "../findings.js";
import type { PrototypeManifest } from "../manifest/types.js";
import { byLine, isIdentifier, parseSourceTree, sourceFinding, stringLiteral } from "./tree.js";

interface Target {
  screenId: string;
  node: Node;
  how: string;
}

/** Literal navigation targets in a source that already passed `checkSource`. */
function navigationTargets(source: string): Target[] {
  const tree = parseSourceTree(source);
  const factories = new Set<string>();
  const targets: Target[] = [];
  ancestor(tree, {
    ImportDeclaration(node) {
      const decl = node as unknown as { source: { value: string }; specifiers: { imported?: { name: string }; local: { name: string } }[] };
      if (decl.source.value !== "react/jsx-runtime") return;
      for (const spec of decl.specifiers) if (spec.imported?.name === "jsx" || spec.imported?.name === "jsxs") factories.add(spec.local.name);
    },
  });
  ancestor(tree, {
    CallExpression(node) {
      const call = node as unknown as { callee: Node & { property?: Node; computed?: boolean }; arguments: Node[] };
      // jsx(Component, { to: "screen.x", … })
      if (isIdentifier(call.callee) && factories.has(call.callee.name)) {
        const props = call.arguments[1] as (Node & { properties?: Node[] }) | undefined;
        for (const prop of props?.type === "ObjectExpression" ? (props.properties ?? []) : []) {
          const p = prop as unknown as { type: string; key: Node; value: Node; computed: boolean };
          if (p.type !== "Property" || p.computed || !isIdentifier(p.key, "to")) continue;
          const screenId = stringLiteral(p.value);
          if (screenId !== undefined) targets.push({ screenId, node: prop, how: `to=${JSON.stringify(screenId)}` });
        }
        return;
      }
      // nav.go("screen.x", …)
      if (call.callee.type === "MemberExpression" && !call.callee.computed && isIdentifier(call.callee.property, "go")) {
        const screenId = stringLiteral(call.arguments[0]);
        if (screenId !== undefined) targets.push({ screenId, node, how: `go(${JSON.stringify(screenId)})` });
      }
    },
  });
  return targets;
}

export function sourceReferenceFindings(source: string, manifest: PrototypeManifest): Finding[] {
  const screens = new Set(manifest.screens.map((s) => s.id));
  return byLine(
    navigationTargets(source)
      .filter((t) => !screens.has(t.screenId))
      .map((t) => sourceFinding("UNKNOWN_NAV_TARGET", t.node, `${t.how} is not one of the manifest's screens (${[...screens].join(", ")})`)),
  );
}
