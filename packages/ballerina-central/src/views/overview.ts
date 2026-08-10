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
 * `bal-library <pkg>` — the default document, and the only one an agent cannot
 * skip.
 *
 * It carries the readme, every client's constructor and functions, the
 * module-level functions, and the error declarations. NO OTHER TYPES: they are 738
 * of `ballerinax/github`'s 927KB, and excluding them is the whole reason one
 * document is viable. Every other type arrives named in a return type or a
 * parameter, which is the ordering that cannot go wrong — github's own readme
 * example returns `Repository` while the operation returns `FullRepository`, and
 * eleven records in the package carry a `stargazersCount`, four of them optional.
 *
 * ERRORS ARE THE ONE TYPE FAMILY THAT STAYS, because they are the only
 * declarations unreachable from a signature. `ballerinax/github` declares zero
 * errors and all 903 of its operations return the language-level `error`, so
 * nothing in the API document names `http:ClientRequestError` — which is exactly
 * the lookup eight of nine recorded runs came for. They are also cheap: 3,089
 * bytes for http's 56, at most 334 for anything else, and the section is omitted
 * entirely for the four packages that declare none.
 */

import { code, columns, count, Report } from "../report.js";
import type { LoadedPackage } from "../library.js";
import type { ClientClass, Fn, Library, TypeDef } from "../model.js";
import { formatQualifiedName } from "../qualified.js";
import { demoteHeadings } from "../readme.js";
import { renderMemberFunction, renderStandaloneFunction } from "../render/signature.js";
import { renderTypeDef } from "../render/typedef.js";
import { buildPathTree, operationsOf, type PathNode } from "../symbols.js";

/**
 * When a client's resource functions are replaced by a path tree.
 *
 * Both halves matter and they are not redundant. The COUNT is the readable limit;
 * the BYTE guard is what stops a verbose 80-operation connector from producing a
 * 40KB document that passes the count. At gmail's measured ~480 bytes per
 * operation, 100 operations is about 48KB, which is far too large for a document
 * meant to be read from the top. On the nine-fixture corpus both halves agree —
 * github and slack take the tree, gmail at 32 operations and 15.3KB stays inline —
 * so the guard costs nothing today and is there for the package that has not been
 * recorded yet.
 */
export const MAX_INLINE_OPERATIONS = 100;
export const MAX_INLINE_SIGNATURE_BYTES = 20_000;

export interface OverviewOptions {
  /** Narrow to one client. Everything else about the document is unchanged. */
  readonly client?: string;
}

function bytesOf(text: string): number {
  return Buffer.byteLength(text, "utf-8");
}

/**
 * How the declarations break down, for the one line that says they are not here.
 *
 * Errors are excluded from the total because they ARE listed, in their own
 * section; counting them under "not listed here" would make the line false by
 * exactly the number of errors.
 */
function describeTypes(typeDefs: readonly TypeDef[]): string {
  const kinds = typeDefs.filter((typeDef) => typeDef.kind !== "error");
  if (kinds.length === 0) return "none declared";
  const records = kinds.filter((typeDef) => typeDef.kind === "record").length;
  const unions = kinds.filter((typeDef) => typeDef.kind === "union").length;
  const other = kinds.length - records - unions;
  const parts = [`${count(records)} records`, `${count(unions)} unions`, `${count(other)} other`].filter(
    (part) => !part.startsWith("0 "),
  );
  return `${count(kinds.length)} declarations (${parts.join(", ")}), not listed here — read one with \`type\``;
}

function errorsOf(library: Library): readonly Extract<TypeDef, { kind: "error" }>[] {
  return library.typeDefs.flatMap((typeDef) => (typeDef.kind === "error" ? [typeDef] : []));
}

/** A signature at column zero — a report quotes declarations, it does not indent them. */
function signature(fn: Fn): string {
  return renderMemberFunction(fn, "");
}

/**
 * One client's section.
 *
 * Functions are GROUPED BY CALL SYNTAX, and the heading states it rather than
 * leaving it to be inferred from a keyword. ADR-0001 already had to fix one
 * rendering bug of exactly this kind — upstream declared `remote function close()`
 * on a database client, and `dbClient->close()` does not compile — so "remote
 * means `->`, normal means `.`" is a fact worth writing down where the signature
 * is read.
 */
function clientSection(report: Report, client: ClientClass, pkg: string): void {
  report.heading(2, `Client ${code(client.name)}`);
  if (client.description.trim() !== "") report.paragraph(client.description.split("\n")[0] ?? "");

  const constructor = client.functions.find((fn) => fn.kind === "constructor");
  if (constructor) {
    report.heading(3, "Constructor");
    report.ballerina([signature(constructor)]);
  }

  const remote = client.functions.filter((fn) => fn.kind === "remote");
  if (remote.length > 0) {
    report.heading(3, `Remote functions — ${count(remote.length)}, call with ${code("->")}`);
    report.ballerina(remote.map(signature));
  }

  const normal = client.functions.filter((fn) => fn.kind === "normal");
  if (normal.length > 0) {
    report.heading(3, `Normal functions — ${count(normal.length)}, call with ${code(".")}`);
    report.ballerina(normal.map(signature));
  }

  resourceSection(report, client, pkg);
}

function resourceSection(report: Report, client: ClientClass, pkg: string): void {
  const operations = operationsOf(client);
  if (operations.length === 0) return;

  const rendered = operations.map((operation) => signature(operation.fn));
  const bytes = rendered.reduce((sum, line) => sum + bytesOf(line) + 1, 0);
  const tooMany = operations.length > MAX_INLINE_OPERATIONS;
  const tooBig = bytes > MAX_INLINE_SIGNATURE_BYTES;

  report.heading(3, `Resource functions — ${count(operations.length)}, call with ${code("->")} and a path`);

  if (!tooMany && !tooBig) {
    report.ballerina(rendered);
    return;
  }

  report.paragraph(
    `**Not listed here** — ${count(operations.length)} operations, ${count(bytes)} bytes of signatures, ` +
      `over the ${tooMany ? `${count(MAX_INLINE_OPERATIONS)}-operation` : `${count(MAX_INLINE_SIGNATURE_BYTES)}-byte`} limit. ` +
      `Top-level path segments, with the number of operations under each:`,
  );

  const tree = buildPathTree(operations);
  report.literal(columns(tree.children.map((child) => `${child.segment} ${count(child.total)}`)));
  report.paragraph(describeTree(tree));
  report.bullets([
    `navigate: ${code(`bal-library ops ${pkg} <segment>`)}`,
    `signatures under a path: ${code(`bal-library ops ${pkg} '<path>' --sigs`)}`,
  ]);
}

/**
 * An honest header for a "tree" that is not always one.
 *
 * `ballerinax/slack` has 174 operations across 174 distinct top-level segments —
 * its paths are RPC-style single names like `chat.postMessage` — so level 1 IS the
 * complete operation list. That needs no special case, only saying so, because an
 * agent told to descend a flat list will spend a turn discovering there is nowhere
 * to descend to.
 */
function describeTree(tree: PathNode): string {
  const flat = tree.children.every((child) => child.children.length === 0);
  return flat
    ? `${count(tree.total)} operations across ${count(tree.children.length)} top-level segments, and none of them nests — the list above is every operation. Nothing to descend into.`
    : `${count(tree.total)} operations across ${count(tree.children.length)} top-level segments.`;
}

export function renderOverview(loaded: LoadedPackage, options: OverviewOptions = {}): string {
  const pkg = formatQualifiedName(loaded.qualified);
  const { library } = loaded;
  const clients =
    options.client === undefined
      ? library.clients
      : library.clients.filter((client) => client.name === options.client);

  const report = new Report("overview");
  report.heading(1, `${pkg} ${loaded.version}`);

  const errors = errorsOf(library);
  report.facts([
    ["Source", loaded.provenance],
    [
      "Clients",
      library.clients.length === 0
        ? "none — the callable surface is module-level functions"
        : library.clients.map((client) => code(client.name)).join(", "),
    ],
    [
      "Module functions",
      library.functions.length === 0 ? "none" : `${count(library.functions.length)}, listed below`,
    ],
    [
      "Errors",
      errors.length === 0
        ? "none declared; operations return the language-level `error`"
        : `${count(errors.length)}, listed below`,
    ],
    ["Types", describeTypes(library.typeDefs)],
  ]);

  if (options.client !== undefined && clients.length === 0) {
    report.paragraph(
      `**No client named ${code(options.client)}.** The full document is ${code(`bal-library ${pkg}`)}.`,
    );
    return report.toString();
  }

  report.heading(2, "Next");
  report.bullets([
    `${code(`bal-library ops ${pkg} <path>`)} — navigate a client's operations`,
    `${code(`bal-library type ${pkg} <Name> [--deps]`)} — read a declaration whole`,
    `${code(`bal-library api ${pkg}`)} — every declaration, when nothing above answered`,
  ]);

  for (const client of clients) clientSection(report, client, pkg);

  if (library.functions.length > 0) {
    report.heading(2, `Module-level functions — ${count(library.functions.length)}, call with ${code(".")}`);
    report.ballerina(library.functions.map(renderStandaloneFunction));
  }

  if (errors.length > 0) {
    report.heading(2, `Errors — ${count(errors.length)}`);
    report.paragraph(
      `The subtype chain is what ${code("is")} tests against, so ${code("e is <Name>")} works off these lines directly.`,
    );
    report.ballerina(errors.map(renderTypeDef));
  }

  guideSection(report, loaded);
  return report.toString();
}

/**
 * The package's own guide, last, with its headings demoted two levels.
 *
 * Last because it is the largest section for most packages and the document is
 * meant to be read from the top with the API first. Demoted so the document keeps
 * ONE outline and `grep '^## '` returns the overview's sections rather than the
 * readme's. Otherwise verbatim, fences intact and copyable — it goes in as raw
 * Markdown rather than as commented-out text, which reads better and costs three
 * fewer bytes per line.
 */
function guideSection(report: Report, loaded: LoadedPackage): void {
  if (loaded.readmes.length === 0) {
    report.heading(2, "Guide");
    report.paragraph("This package publishes no guide. The signatures above are all Central holds.");
    return;
  }

  for (const readme of loaded.readmes) {
    report.heading(2, loaded.readmes.length === 1 ? "Guide" : `Guide — ${readme.module}`);
    report.paragraph("*The package's own readme, verbatim, with its headings demoted two levels.*");
    report.embedded(demoteHeadings(readme.markdown, 2));
  }
}
