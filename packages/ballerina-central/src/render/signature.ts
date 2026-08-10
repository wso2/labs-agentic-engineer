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
 * How one callable is written, and how a foreign name inside it is qualified.
 *
 * This is the module the views share with the API document, and sharing it is
 * what makes their agreement structural rather than tested: `overview` and
 * `ops --sigs` quote `renderMemberFunction`, so a signature they show cannot
 * differ from the one `api` shows. `test/views-agree.test.ts` still asserts it,
 * because the cheap way to break the guarantee is for a view to hand-roll a
 * line rather than call this.
 *
 * Two conventions carry meaning beyond syntax:
 *
 *   a name owned by another package is rendered with that package's module
 *   alias (`gmail:Message`), and
 *
 *   the declaration it came from is repeated in a trailing `// Special Agent
 *   Note:` so the caller knows which import to add.
 */

import type { Fn, Param, ReturnDef, StandaloneFn, TypeRef } from "../model.js";

/** One external name and the alias its module gets in an import. */
export interface ExternalLink {
  readonly recordName: string;
  readonly libraryName: string;
  readonly modulePrefix: string;
}

/** `ballerinax/googleapis.gmail` → `gmail`, the alias an import puts in scope. */
export function deriveModulePrefix(libraryName: string): string {
  const parts = libraryName.split(/[/.]/);
  return parts[parts.length - 1] ?? libraryName;
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

export function collectExternalLinks(type: TypeRef | undefined): ExternalLink[] {
  return (type?.links ?? []).flatMap((link) =>
    link.category === "external" && link.libraryName !== ""
      ? [
          {
            recordName: link.recordName,
            libraryName: link.libraryName,
            modulePrefix: deriveModulePrefix(link.libraryName),
          },
        ]
      : [],
  );
}

/**
 * Qualify every foreign name inside a type expression.
 *
 * Done textually because the expression is already a string by this point — a
 * union or an array of a foreign record has no structure left to walk. The
 * `prev === ":"` guard keeps an already-qualified name from gaining a second
 * prefix when two links resolve to the same word.
 */
export function applyPrefixToTypeName(typeName: string, links: readonly ExternalLink[]): string {
  let result = typeName;
  for (const link of links) {
    const pattern = new RegExp(`\\b${escapeRegExp(link.recordName)}\\b`, "g");
    result = result.replace(pattern, (match, offset: number, source: string) =>
      (offset > 0 ? source[offset - 1] : "") === ":" ? match : `${link.modulePrefix}:${match}`,
    );
  }
  return result;
}

/** The trailing note naming which package each foreign type came from. */
export function buildSpecialAgentNote(links: readonly ExternalLink[]): string {
  if (links.length === 0) return "";
  const grouped = new Map<string, string[]>();
  for (const link of links) {
    const names = grouped.get(link.libraryName);
    if (names) names.push(link.recordName);
    else grouped.set(link.libraryName, [link.recordName]);
  }
  const parts = [...grouped].map(([libraryName, names]) => `${names.join(", ")} FROM ${libraryName} package`);
  return ` // Special Agent Note: ${parts.join(", ")}`;
}

/** Ballerina doc comments, one `#` per line, with the trailing newline callers splice in. */
export function renderDescription(description: string): string {
  if (description.trim() === "") return "";
  return `${description
    .split("\n")
    .map((line) => `# ${line}`)
    .join("\n")}\n`;
}

/** Every foreign name a signature mentions, deduplicated, params before return. */
function collectSignatureLinks(params: readonly Param[], returns: TypeRef | undefined): ExternalLink[] {
  const links = [...params.flatMap((param) => collectExternalLinks(param.type)), ...collectExternalLinks(returns)];
  const seen = new Set<string>();
  return links.filter((link) => {
    const key = `${link.recordName}::${link.libraryName}`;
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

function renderParam(param: Param): string {
  const links = collectExternalLinks(param.type);
  const defaultValue = param.default === undefined ? "" : ` = ${param.default}`;
  return `${applyPrefixToTypeName(param.type.name, links)} ${param.name}${defaultValue}`;
}

function renderReturns(returns: ReturnDef, links: readonly ExternalLink[]): string {
  return ` returns ${applyPrefixToTypeName(returns.type.name, links)}`;
}

function renderDocComment(description: string, indent: string): string {
  if (!description) return "";
  return `${indent}# ${description.split("\n").join(`\n${indent}# `)}\n`;
}

/**
 * A resource function's path, as the caller types it.
 *
 * Path parameters keep the `[string owner]` spelling rather than a display form
 * like `{owner}`, because this string is a declaration and that is what goes in
 * the source. `ops` prints the display form in its Markdown prose and this form
 * inside its fenced blocks, which is the whole reason the two registers are
 * separate.
 */
export function renderResourcePath(fn: Extract<Fn, { kind: "resource" }>): string {
  return fn.paths
    .map((segment) => (segment.kind === "literal" ? segment.text : `[${segment.type} ${segment.name}]`))
    .join("/");
}

export function renderMemberFunction(fn: Fn, indent: string): string {
  const links = collectSignatureLinks(fn.params, fn.returns.type);
  const params = fn.params.map(renderParam).join(", ");
  const note = buildSpecialAgentNote(links);

  switch (fn.kind) {
    case "constructor":
      return `${indent}function init(${params})${renderReturns(fn.returns, links)};${note}`;
    case "remote":
      return (
        `${renderDocComment(fn.description, indent)}${indent}remote function ${fn.name}` +
        `(${params})${renderReturns(fn.returns, links)};${note}`
      );
    case "resource": {
      // Path parameters are declared in the path, so repeating them in the
      // parameter list would be a signature no caller can write.
      const inPath = new Set(fn.paths.flatMap((segment) => (segment.kind === "param" ? [segment.name] : [])));
      const rest = fn.params.filter((param) => !inPath.has(param.name));
      return (
        `${renderDocComment(fn.description, indent)}${indent}resource function ${fn.accessor} ${renderResourcePath(fn)}` +
        `(${rest.map(renderParam).join(", ")})${renderReturns(fn.returns, links)};${note}`
      );
    }
    case "normal":
      return `${renderDocComment(fn.description, indent)}${indent}function ${fn.name}(${params})${renderReturns(fn.returns, links)};${note}`;
    default: {
      const exhaustive: never = fn;
      return exhaustive;
    }
  }
}

/**
 * A module-level function, documented rather than merely declared: standalone
 * functions are usually utilities whose parameters are not self-describing, and
 * this is the only place the reader spends lines on `# +` doc rows.
 */
export function renderStandaloneFunction(fn: StandaloneFn): string {
  const links = collectSignatureLinks(fn.params, fn.returns.type);
  const lines: string[] = [];
  if (fn.description) lines.push(...fn.description.split("\n").map((line) => `# ${line}`));
  for (const param of fn.params) {
    if (param.description) lines.push(`# + ${param.name} - ${param.description}`);
  }
  if (fn.returns.description) lines.push(`# + return - ${fn.returns.description}`);
  const params = fn.params.map(renderParam).join(", ");
  lines.push(`function ${fn.name}(${params})${renderReturns(fn.returns, links)};${buildSpecialAgentNote(links)}`);
  return lines.join("\n");
}
