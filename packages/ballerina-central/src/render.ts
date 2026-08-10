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
 * `Library` → the Ballerina syntax string.
 *
 * The output is not a compilable module and is not meant to be: it is the
 * package's whole public surface written in the language the caller is about to
 * write, so a signature can be read straight off it instead of inferred from
 * prose. Function bodies are `;`, and a declaration the reader has no Ballerina
 * form for becomes a comment naming it.
 *
 * Two conventions carry meaning beyond syntax:
 *
 *   a name owned by another package is rendered with that package's module
 *   alias (`gmail:Message`), and
 *
 *   the declaration it came from is repeated in a trailing `// Special Agent
 *   Note:` so the caller knows which import to add.
 *
 * `renderTypeDef` switches over `kind` with a `never` fallthrough: adding a
 * Ballerina shape to the IR fails the build here until it has a rendering.
 */

import type {
  AnnotationDef,
  AttachmentPoint,
  ClientClass,
  Fn,
  Library,
  ListenerParam,
  Param,
  ReturnDef,
  Service,
  ServiceMethod,
  StandaloneFn,
  TypeDef,
  TypeRef,
} from "./model.js";

const ATTACHMENT_POINT_LABELS: Readonly<Record<AttachmentPoint, string>> = {
  SERVICE: "service",
  OBJECT_METHOD: "service_function",
};

/** One external name and the alias its module gets in an import. */
interface ExternalLink {
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
function renderDescription(description: string): string {
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

// ---------------------------------------------------------------------------
// Type definitions
// ---------------------------------------------------------------------------

function renderRecord(typeDef: Extract<TypeDef, { kind: "record" }>): string {
  const lines = [renderDescription(typeDef.description), `type ${typeDef.name} record {`];
  for (const field of typeDef.fields) {
    const links = collectExternalLinks(field.type);
    const typeName = applyPrefixToTypeName(field.type.name, links);
    const optional = field.optional ? "?" : "";
    const defaultValue = field.default === undefined ? "" : ` = ${field.default}`;
    const description = field.description ? `    # ${field.description}\n` : "";
    lines.push(`${description}    ${typeName} ${field.name}${optional}${defaultValue};${buildSpecialAgentNote(links)}`);
  }
  lines.push("};");
  return lines.join("\n");
}

function renderEnum(typeDef: Extract<TypeDef, { kind: "enum" }>): string {
  const members = typeDef.members.map((member) => member.name).join(",\n    ");
  // Joined with "" rather than "\n": renderDescription already ends in a
  // newline, and an enum with no description must not gain a leading blank line.
  return [renderDescription(typeDef.description), `enum ${typeDef.name} {\n    ${members}\n}`].join("");
}

function renderUnion(typeDef: Extract<TypeDef, { kind: "union" }>): string {
  const description = renderDescription(typeDef.description);
  if (typeDef.members.length === 0) return `${description}type ${typeDef.name};`;
  return `${description}type ${typeDef.name} ${typeDef.members.map((member) => member.name).join("|")};`;
}

function renderConstant(typeDef: Extract<TypeDef, { kind: "constant" }>): string {
  const value = typeDef.varType.name === "string" ? `"${typeDef.value}"` : typeDef.value;
  return `${renderDescription(typeDef.description)}const ${typeDef.varType.name} ${typeDef.name} = ${value};`;
}

/**
 * An error declaration, as the four combinations of the two facts Central
 * publishes about it:
 *
 *   distinct + base   `type SslError distinct ClientError;`
 *   distinct only     `type Error distinct error;`
 *   base only         `type X ClientError;`
 *   neither           `type X error;`
 *
 * Every one of `ballerina/http`'s 56 errors used to render as the last line,
 * which made the subtype hierarchy — and therefore `e is http:ClientRequestError`
 * — unlearnable from the document. The absent-base default is `error` rather
 * than nothing because an error at the top of its own hierarchy narrows the
 * language's `error` and that is what its declaration says.
 */
function renderError(typeDef: Extract<TypeDef, { kind: "error" }>): string {
  const links = collectExternalLinks(typeDef.base);
  const base = typeDef.base === undefined ? "error" : applyPrefixToTypeName(typeDef.base.name, links);
  const distinct = typeDef.isDistinct ? "distinct " : "";
  return (
    `${renderDescription(typeDef.description)}type ${typeDef.name} ${distinct}${base};` +
    buildSpecialAgentNote(links)
  );
}

export function renderTypeDef(typeDef: TypeDef): string {
  switch (typeDef.kind) {
    case "record":
      return renderRecord(typeDef);
    case "enum":
      return renderEnum(typeDef);
    case "union":
      return renderUnion(typeDef);
    case "constant":
      return renderConstant(typeDef);
    case "class":
      return `${renderDescription(typeDef.description)}class ${typeDef.name} {\n}`;
    case "error":
      return renderError(typeDef);
    case "other":
      // Named but not rendered: the declaration exists and the caller may need
      // to reference it, so saying so beats omitting it silently.
      return `// Unknown type: ${typeDef.name}`;
    default: {
      const exhaustive: never = typeDef;
      return exhaustive;
    }
  }
}

// ---------------------------------------------------------------------------
// Functions
// ---------------------------------------------------------------------------

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

function renderMemberFunction(fn: Fn, indent: string): string {
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
      const path = fn.paths
        .map((segment) => (segment.kind === "literal" ? segment.text : `[${segment.type} ${segment.name}]`))
        .join("/");
      // Path parameters are declared in the path, so repeating them in the
      // parameter list would be a signature no caller can write.
      const inPath = new Set(fn.paths.flatMap((segment) => (segment.kind === "param" ? [segment.name] : [])));
      const rest = fn.params.filter((param) => !inPath.has(param.name));
      return (
        `${renderDocComment(fn.description, indent)}${indent}resource function ${fn.accessor} ${path}` +
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

function renderClient(client: ClientClass): string {
  const lines = [`${renderDescription(client.description)}client class ${client.name} {`];
  for (const fn of client.functions) {
    // The constructor sits directly under the class header; every other member
    // gets a blank line so a long client stays scannable.
    if (fn.kind !== "constructor") lines.push("");
    lines.push(renderMemberFunction(fn, "    "));
  }
  lines.push("}");
  return lines.join("\n");
}

/**
 * A module-level function, documented rather than merely declared: standalone
 * functions are usually utilities whose parameters are not self-describing, and
 * this is the only place the reader spends lines on `# +` doc rows.
 */
function renderStandaloneFunction(fn: StandaloneFn): string {
  const links = collectSignatureLinks(fn.params, fn.returns.type);
  const lines: string[] = [];
  if (fn.description) lines.push(...fn.description.split("\n").map((line) => `# ${line}`));
  for (const param of fn.params) {
    if (param.description) lines.push(`# + ${param.name} - ${param.description}`);
  }
  if (fn.returns.description) lines.push(`# + return - ${fn.returns.description}`);
  const params = fn.params.map(renderParam).join(", ");
  lines.push(
    `function ${fn.name}(${params})${renderReturns(fn.returns, links)};${buildSpecialAgentNote(links)}`,
  );
  return lines.join("\n");
}

// ---------------------------------------------------------------------------
// Services and annotations
// ---------------------------------------------------------------------------

function renderListenerParam(param: ListenerParam, withDefault: boolean): string {
  const defaultValue = withDefault && param.default !== undefined ? ` = ${param.default}` : "";
  return `${param.type.name} ${param.name}${defaultValue}`;
}

function renderServiceMethod(method: ServiceMethod): string {
  const description = method.description ? `    # ${method.description}\n` : "";
  const deprecated = method.isDeprecated ? "    @deprecated\n" : "";
  const params = method.params
    .map((param) => `${param.type.name}${param.name ? ` ${param.name}` : ""}`)
    .join(", ");
  return `${description}${deprecated}    remote function ${method.name}(${params}) returns ${method.returns.type.name};`;
}

/** `kafka:Listener` → `kafka`, the alias the service type needs too. */
function deriveListenerAlias(listenerName: string): string | undefined {
  const index = listenerName.indexOf(":");
  return index > 0 ? listenerName.slice(0, index) : undefined;
}

function renderService(service: Service): string {
  switch (service.kind) {
    case "generic": {
      const params = service.listener.parameters.map((param) => renderListenerParam(param, false)).join(", ");
      const lines = ["// --- Service (generic) ---", `// Listener: ${service.listener.name}(${params})`, "// Instructions:"];
      if (service.instructions) lines.push(service.instructions);
      return lines.join("\n");
    }
    case "fixed": {
      const params = service.listener.parameters.map((param) => renderListenerParam(param, true)).join(", ");
      const lines: string[] = [];
      if (service.isDeprecated) lines.push("@deprecated");
      const alias = deriveListenerAlias(service.listener.name);
      const prefix = service.name && alias ? `${alias}:${service.name} ` : "";
      lines.push(`service ${prefix}on new ${service.listener.name}(${params}) {`);
      for (const method of service.methods) {
        lines.push(renderServiceMethod(method));
        lines.push("");
      }
      if (lines[lines.length - 1] === "") lines.pop();
      lines.push("}");
      return lines.join("\n");
    }
    default: {
      const exhaustive: never = service;
      return exhaustive;
    }
  }
}

function renderAnnotation(annotation: AnnotationDef): string {
  const lines: string[] = [];
  if (annotation.description) {
    lines.push(
      annotation.description
        .split("\n")
        .map((line) => `# ${line}`)
        .join("\n"),
    );
  }
  lines.push(`public annotation ${annotation.name} on ${ATTACHMENT_POINT_LABELS[annotation.attachmentPoint]};`);
  return lines.join("\n");
}

// ---------------------------------------------------------------------------
// Document
// ---------------------------------------------------------------------------

/**
 * Section order is the output's contract with the caller: types, clients,
 * functions, services, annotations. Callers grep this file rather than read it
 * — `ballerinax/github` is over 20,000 lines — so the section banners are the
 * navigation, and moving one is a change to how the skill tells an agent to
 * search.
 */
export function toSyntaxString(library: Library): string {
  const output: string[] = [
    "// ============================================================",
    `// Library: ${library.name}`,
  ];
  if (library.description) output.push(`// ${library.description.split("\n")[0] ?? ""}`);
  output.push("// ============================================================", `import ${library.name};`);

  const section = (title: string, rendered: readonly string[]): void => {
    if (rendered.length === 0) return;
    output.push("", title);
    for (const item of rendered) output.push("", item);
  };

  section("// --- Types ---", library.typeDefs.map(renderTypeDef));
  section("// --- Client ---", library.clients.map(renderClient));
  section("// --- Functions ---", library.functions.map(renderStandaloneFunction));
  section("// --- Service ---", library.services.map(renderService));
  section("// --- Annotations ---", library.annotations.map(renderAnnotation));

  output.push("");
  return output.join("\n");
}
