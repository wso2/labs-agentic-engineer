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
 * `ops <pkg> [path] [--client C] [--sigs]` — a client's operations, navigated or
 * dumped.
 *
 * This is the discovery verb, and the one the earlier design was missing. Without
 * it a 903-operation connector is reachable only by already knowing the path you
 * want; with it, `ballerinax/github`'s tree is 36 segments in 445 bytes and each
 * level names the next command.
 *
 * The REPORT register: Markdown headings for structure, and Ballerina only inside
 * fenced blocks. Signatures inside those fences are byte-identical to the ones
 * `api` prints, because both call `renderMemberFunction`.
 */

import { code, columns, count, Report } from "../report.js";
import type { LoadedPackage } from "../library.js";
import type { ClientClass } from "../model.js";
import { formatQualifiedName } from "../qualified.js";
import { renderMemberFunction } from "../render/signature.js";
import { err, ok, type Result } from "../result.js";
import {
  autoDescend,
  buildPathTree,
  operationsOf,
  operationsUnder,
  resolvePath,
  splitPath,
  type Operation,
  type PathNode,
} from "../symbols.js";

export interface OpsOptions {
  readonly path?: string;
  readonly client?: string;
  readonly sigs: boolean;
}

/** A signature at column zero — a report quotes declarations, it does not indent them. */
function signature(operation: Operation): string {
  return renderMemberFunction(operation.fn, "");
}

function pathOf(segments: readonly string[]): string {
  return segments.length === 0 ? "(root)" : segments.join("/");
}

/**
 * Which client's operations these are.
 *
 * Resource and remote functions live on a client, so `ops` has to resolve one.
 * Exactly one client with resource functions is used silently; more than one is a
 * failure NAMING them rather than a guess, which on this corpus fires only for
 * `ballerina/http`, where `Client`, `FailoverClient`, `LoadBalanceClient` and
 * `StatusCodeClient` each carry seven.
 *
 * Zero is not an error. A package can legitimately have no resource functions at
 * all — `ballerinax/kafka` is three clients of remote functions, and an
 * `io`-shaped package has no client — and the honest answer is an empty report
 * that says where the callable surface actually is.
 */
function resolveClient(loaded: LoadedPackage, requested: string | undefined): Result<ClientClass | undefined> {
  const clients = loaded.library.clients;
  const label = `${formatQualifiedName(loaded.qualified)}:${loaded.version}`;

  if (requested !== undefined) {
    const exact = clients.find((client) => client.name === requested);
    if (exact) return ok(exact);
    return err({
      kind: "validation",
      message: `${label} declares no client named '${requested}'.`,
      suggestion:
        clients.length === 0
          ? `${label} declares no clients at all. Its callable surface is module-level functions; read \`bal-library ${formatQualifiedName(loaded.qualified)}\`.`
          : `Pass one of: ${clients.map((client) => client.name).join(", ")}.`,
    });
  }

  const withOperations = clients.filter((client) => operationsOf(client).length > 0);
  if (withOperations.length === 1) return ok(withOperations[0]);
  if (withOperations.length === 0) return ok(undefined);
  return err({
    kind: "validation",
    message: `${label} has ${withOperations.length} clients with resource functions, so 'ops' cannot pick one.`,
    suggestion: `Pass --client with one of: ${withOperations.map((client) => client.name).join(", ")}.`,
  });
}

export function renderOpsView(loaded: LoadedPackage, options: OpsOptions): Result<string> {
  const resolved = resolveClient(loaded, options.client);
  if (!resolved.ok) return resolved;
  const client = resolved.value;
  const pkg = formatQualifiedName(loaded.qualified);

  if (client === undefined) return ok(noResourceFunctions(loaded, pkg));

  const tree = buildPathTree(operationsOf(client));
  const tokens = splitPath(options.path ?? "");
  const found = resolvePath(tree, tokens);
  if (found.kind === "missing") {
    return ok(pathNotFound(loaded, client, pkg, found.matched, found.token, found.available));
  }

  return ok(
    options.sigs
      ? signatureDump(loaded, client, pkg, found.node, found.path)
      : levelView(loaded, client, pkg, found.node, found.path),
  );
}

function header(report: Report, loaded: LoadedPackage, client: ClientClass, pkg: string): void {
  report.heading(1, `Operations — ${pkg} ${code(client.name)}`);
}

/**
 * One level of the tree: what can be called here, and where to go next.
 *
 * Operations at a path carry FULL signatures rather than an accessor plus a doc
 * line. Measured on github's `repos/{owner}/{repo}`, the three terminals with
 * their doc comments are 469 bytes, and they remove an entire turn because
 * `returns FullRepository|error` is the fact the agent came for.
 */
function levelView(
  loaded: LoadedPackage,
  client: ClientClass,
  pkg: string,
  node: PathNode,
  path: readonly string[],
): string {
  const descent = autoDescend(node, path);
  const report = new Report("ops");
  header(report, loaded, client, pkg);

  const facts: [string, string][] = [["Source", loaded.provenance]];
  facts.push(
    descent.path.length === path.length
      ? ["Path", code(pathOf(descent.path))]
      : [
          "Path",
          `${code(pathOf(path))} → descended to ${code(pathOf(descent.path))} (${count(descent.node.total)} of ${count(node.total)})`,
        ],
  );
  for (const sibling of descent.skipped) {
    facts.push(["Sibling", `${code(pathOf(sibling.path))} (${count(sibling.total)})`]);
  }
  report.facts(facts);

  const here = descent.node;
  if (here.operations.length > 0) {
    report.heading(2, `${count(here.operations.length)} operation${here.operations.length === 1 ? "" : "s"} at this path`);
    report.ballerina(here.operations.map(signature));
  } else {
    report.heading(2, "No operations at this path");
    report.paragraph("Nothing is callable here. Descend to one of the segments below.");
  }

  if (here.children.length > 0) {
    report.heading(2, `${count(here.children.length)} child segment${here.children.length === 1 ? "" : "s"}`);
    report.literal(columns(here.children.map((child) => `${child.segment} ${count(child.total)}`)));
  }

  report.heading(2, "Next");
  const next: string[] = [];
  const first = here.children[0];
  if (first !== undefined) {
    next.push(`descend: ${code(`bal-library ops ${pkg} '${pathOf([...descent.path, first.segment])}'`)}`);
  }
  const under = operationsUnder(here);
  next.push(
    `signatures: ${code(`bal-library ops ${pkg} '${pathOf(descent.path)}' --sigs`)} — ${count(under.length)} operation${under.length === 1 ? "" : "s"}, ${count(bytesOf(under))} bytes`,
  );
  next.push(`a declaration named in a signature: ${code(`bal-library type ${pkg} <Name> [--deps]`)}`);
  report.bullets(next);

  return report.toString();
}

function bytesOf(operations: readonly Operation[]): number {
  return operations.reduce((sum, operation) => sum + Buffer.byteLength(signature(operation), "utf-8") + 1, 0);
}

/**
 * Every signature at or under a path.
 *
 * NO CAP AND NO REFUSAL. `ops <pkg> repos --sigs` on github hands back 90,465
 * bytes for 421 operations, and it should: the tree already showed `repos 421`,
 * the header leads with the count and the byte size, so the caller chose it with
 * its eyes open. A truncation that can drop the answer is worse than a large
 * document somebody asked for.
 */
function signatureDump(
  loaded: LoadedPackage,
  client: ClientClass,
  pkg: string,
  node: PathNode,
  path: readonly string[],
): string {
  const operations = operationsUnder(node);
  const report = new Report("ops");
  header(report, loaded, client, pkg);
  report.facts([
    ["Source", loaded.provenance],
    ["Path", code(pathOf(path))],
    ["Operations", `${count(operations.length)} at or under this path`],
    ["Size", `${count(bytesOf(operations))} bytes of signatures`],
  ]);

  if (operations.length === 0) {
    report.heading(2, "No operations");
    report.paragraph(
      `Nothing is declared at or under ${code(pathOf(path))}. Navigate without ${code("--sigs")} to see what is.`,
    );
    return report.toString();
  }

  report.heading(2, "Signatures");
  report.ballerina(operations.map(signature));
  return report.toString();
}

/**
 * A path that does not exist, answered with what does.
 *
 * Exit 0 with the available segments, not a failure: an empty path is a fact
 * about the tree rather than a bad argument, and the caller's next move is right
 * here in the document. A `symbol-not-found` at exit 2 would be defensible and is
 * how `type` treats a bad name — the split is deliberate, and named in the design
 * as the one part of the contract an agent is most likely to invert.
 */
function pathNotFound(
  loaded: LoadedPackage,
  client: ClientClass,
  pkg: string,
  matched: readonly string[],
  token: string,
  available: readonly string[],
): string {
  const report = new Report("ops");
  header(report, loaded, client, pkg);
  report.facts([
    ["Source", loaded.provenance],
    ["Requested", code(pathOf([...matched, token]))],
    ["Matched", matched.length === 0 ? "nothing; the first segment is already wrong" : code(pathOf(matched))],
  ]);
  report.heading(2, `No segment ${code(token)} under ${code(pathOf(matched))}`);
  report.paragraph(
    available.length === 0
      ? "That path is a leaf — it has no child segments. Drop the last segment to see what it declares."
      : `${count(available.length)} segment${available.length === 1 ? " is" : "s are"} declared there:`,
  );
  report.literal(columns([...available]));
  report.heading(2, "Next");
  report.bullets([
    `${code("*")} is a wildcard segment, so a path parameter can be written ${code("'repos/*/*'")}`,
    `start over from the top: ${code(`bal-library ops ${pkg}`)}`,
  ]);
  return report.toString();
}

/**
 * A package whose clients have no resource functions at all.
 *
 * Says where the callable surface is instead of implying there is none: kafka's
 * three clients carry 24 remote functions between them, and `overview` lists every
 * one.
 */
function noResourceFunctions(loaded: LoadedPackage, pkg: string): string {
  const report = new Report("ops");
  report.heading(1, `Operations — ${pkg}`);
  report.facts([
    ["Source", loaded.provenance],
    ["Resource functions", "none in any client"],
  ]);
  report.paragraph(
    `${code("ops")} navigates resource functions, which this package does not declare. Its callable surface is remote and normal functions, which ${code("overview")} lists in full.`,
  );

  const rows = loaded.library.clients.map((client) => {
    const remote = client.functions.filter((fn) => fn.kind === "remote").length;
    const normal = client.functions.filter((fn) => fn.kind === "normal").length;
    return `${code(client.name)} — ${count(remote)} remote, ${count(normal)} normal`;
  });
  if (rows.length > 0) {
    report.heading(2, "Clients");
    report.bullets(rows);
  }
  if (loaded.library.functions.length > 0) {
    report.heading(2, "Module-level functions");
    report.paragraph(`${count(loaded.library.functions.length)}, callable without a client.`);
  }

  report.heading(2, "Next");
  report.bullets([`${code(`bal-library ${pkg}`)} — the overview, with every signature`]);
  return report.toString();
}
