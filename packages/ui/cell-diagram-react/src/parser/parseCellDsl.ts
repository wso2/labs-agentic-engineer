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

import {
  BoundaryDirection,
  Diagnostic,
  EdgeDirection,
  ParsedCellDocument,
  ParsedComponent,
  ParsedEdge,
  ParsedExternal,
  ParseResult
} from "../domain/cellModel";
import { stripFrontmatter } from "./frontmatter";
import { splitLabel } from "./labels";

const boundaryDirections = new Set<BoundaryDirection>(["north", "east", "south", "west"]);
const reservedKeywords = new Set(["title", "version", "component", "as", ...boundaryDirections]);

function tokenize(statement: string): string[] {
  const tokens: string[] = [];
  const pattern = /"([^"]*)"|(\S+)/g;
  let match: RegExpExecArray | null;

  while ((match = pattern.exec(statement)) !== null) {
    tokens.push(match[1] !== undefined ? match[1] : match[2]);
  }

  return tokens;
}

function edgeId(direction: EdgeDirection, source: string, target: string, line: number) {
  return `${direction}-${source}-${target}-${line}`;
}

function parseTypedDeclaration(tokens: string[]) {
  const id = tokens[0];

  if (!id) {
    return null;
  }

  if (tokens[1] === "as") {
    const labelAndTypeTokens = tokens.slice(2);

    if (labelAndTypeTokens.length === 0) {
      return null;
    }

    if (labelAndTypeTokens.length === 1) {
      return { id, label: labelAndTypeTokens[0], type: undefined };
    }

    return {
      id,
      label: labelAndTypeTokens.slice(0, -1).join(" "),
      type: labelAndTypeTokens.at(-1)
    };
  }

  if (tokens.length === 1) {
    return { id, label: undefined, type: undefined };
  }

  return { id, label: undefined, type: tokens.slice(1).join(" ") };
}

function parseExternalDeclaration(statement: string, line: number): ParsedExternal | null {
  const tokens = tokenize(statement);
  const direction = tokens[0] as BoundaryDirection;

  if (!boundaryDirections.has(direction) || tokens.length < 2) {
    return null;
  }

  const id = tokens[1];

  if (tokens[2] === "as") {
    const labelAndTypeTokens = tokens.slice(3);

    if (labelAndTypeTokens.length === 0) {
      return null;
    }

    const label =
      labelAndTypeTokens.length === 1 ? labelAndTypeTokens[0] : labelAndTypeTokens.slice(0, -1).join(" ");
    const type = labelAndTypeTokens.length === 1 ? undefined : labelAndTypeTokens.at(-1);

    return {
      id,
      direction,
      label,
      type,
      line
    };
  }

  return {
    id,
    direction,
    label: undefined,
    type: tokens.length > 2 ? tokens.slice(2).join(" ") : undefined,
    line
  };
}

function parseArrow(statement: string, line: number): ParsedEdge | null {
  const { body, label } = splitLabel(statement);
  const parts = body.split(/\s*->\s*/);

  if (parts.length !== 2 || !parts[0] || !parts[1]) {
    return null;
  }

  const leftTokens = parts[0].trim().split(/\s+/);
  const rightTokens = parts[1].trim().split(/\s+/);

  if (leftTokens.length === 1 && boundaryDirections.has(leftTokens[0] as BoundaryDirection)) {
    const direction = leftTokens[0] as BoundaryDirection;
    const target = rightTokens.join(" ");
    return {
      id: edgeId(direction, direction, target, line),
      source: direction,
      target,
      direction,
      kind: "exposure",
      label,
      line
    };
  }

  if (rightTokens.length === 1 && boundaryDirections.has(rightTokens[0] as BoundaryDirection)) {
    const direction = rightTokens[0] as BoundaryDirection;
    const source = leftTokens.join(" ");
    return {
      id: edgeId(direction, source, direction, line),
      source,
      target: direction,
      direction,
      kind: "exposure",
      label,
      line
    };
  }

  if (leftTokens.length === 2 && boundaryDirections.has(leftTokens[0] as BoundaryDirection)) {
    const direction = leftTokens[0] as BoundaryDirection;
    const source = leftTokens[1];
    const target = rightTokens.join(" ");
    return {
      id: edgeId(direction, source, target, line),
      source,
      target,
      direction,
      kind: "inbound",
      label,
      line
    };
  }

  if (rightTokens.length >= 2 && boundaryDirections.has(rightTokens[0] as BoundaryDirection)) {
    const direction = rightTokens[0] as BoundaryDirection;
    const source = leftTokens.join(" ");
    const target = rightTokens.slice(1).join(" ");
    return {
      id: edgeId(direction, source, target, line),
      source,
      target,
      direction,
      kind: "outbound",
      label,
      line
    };
  }

  const source = leftTokens.join(" ");
  const target = rightTokens.join(" ");
  return {
    id: edgeId("internal", source, target, line),
    source,
    target,
    direction: "internal",
    kind: "internal",
    label,
    line
  };
}

function unknownStatement(line: number): Diagnostic {
  return {
    severity: "error",
    message: "Unknown statement. Expected title, version, component, or dependency arrow.",
    line,
    column: 1
  };
}

export function parseCellDsl(source: string): ParseResult {
  const components: ParsedComponent[] = [];
  const externals: ParsedExternal[] = [];
  const edges: ParsedEdge[] = [];
  const diagnostics: Diagnostic[] = [];
  const componentNames = new Set<string>();
  const externalNames = new Set<string>();
  let title: string | undefined;
  let version: string | undefined;

  stripFrontmatter(source).split(/\r?\n/).forEach((rawLine, index) => {
    const line = index + 1;
    const statement = rawLine.trim();

    if (!statement || statement.startsWith("#") || statement.startsWith("//")) {
      return;
    }

    if (statement.startsWith("title ")) {
      title = statement.slice("title ".length).trim() || undefined;
      return;
    }

    if (statement.startsWith("version ")) {
      version = statement.slice("version ".length).trim() || undefined;
      return;
    }

    if (statement.startsWith("component ")) {
      // Story citations left the grammar (stories live in each component's
      // design.json now). Reject the retired suffix loudly — silently folding
      // it into the type would render a legacy cell as a wrongly-typed
      // component instead of a visible error.
      if (/\[\s*stories\s*:/i.test(statement)) {
        diagnostics.push({
          severity: "error",
          message: "Story citations were removed from the cell — list the stories in the component's design.json instead.",
          line,
          column: 1
        });
        return;
      }
      const declaration = parseTypedDeclaration(tokenize(statement).slice(1));

      if (!declaration) {
        diagnostics.push({
          severity: "error",
          message: "Component statements must use: component <id> [as <label>] [type].",
          line,
          column: 1
        });
        return;
      }

      const { id } = declaration;

      if (reservedKeywords.has(id)) {
        diagnostics.push({
          severity: "error",
          message: `"${id}" is a reserved keyword and cannot be used as a component id.`,
          line,
          column: rawLine.indexOf(id) + 1
        });
        return;
      }

      if (componentNames.has(id)) {
        diagnostics.push({
          severity: "error",
          message: `Component "${id}" is already defined.`,
          line,
          column: rawLine.indexOf(id) + 1
        });
        return;
      }

      componentNames.add(id);
      components.push({ ...declaration, line });
      return;
    }

    if (!statement.includes("->") && boundaryDirections.has(tokenize(statement)[0] as BoundaryDirection)) {
      const external = parseExternalDeclaration(statement, line);

      if (!external) {
        diagnostics.push({
          severity: "error",
          message: "External statements must use: <direction> <id> [as <label>] [type].",
          line,
          column: 1
        });
        return;
      }

      if (reservedKeywords.has(external.id)) {
        diagnostics.push({
          severity: "error",
          message: `"${external.id}" is a reserved keyword and cannot be used as an external id.`,
          line,
          column: rawLine.indexOf(external.id) + 1
        });
        return;
      }

      if (externalNames.has(external.id)) {
        diagnostics.push({
          severity: "error",
          message: `External "${external.id}" is already defined.`,
          line,
          column: rawLine.indexOf(external.id) + 1
        });
        return;
      }

      externalNames.add(external.id);
      externals.push(external);
      return;
    }

    if (statement.includes("->")) {
      const edge = parseArrow(statement, line);
      if (edge) {
        edges.push(edge);
        return;
      }
    }

    diagnostics.push(unknownStatement(line));
  });

  const document: ParsedCellDocument = {
    title,
    version,
    components,
    externals,
    edges
  };

  return { document, diagnostics };
}

export function isCellDslStatement(statement: string): boolean {
  const trimmed = statement.trim();
  if (!trimmed || trimmed.startsWith("#") || trimmed.startsWith("//")) {
    return false;
  }
  return parseCellDsl(trimmed).diagnostics.length === 0;
}
