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
 * `Library` → the whole-package Ballerina document the `api` verb prints.
 *
 * The output is not a compilable module and is not meant to be: it is the
 * package's whole public surface written in the language the caller is about to
 * write, so a signature can be read straight off it instead of inferred from
 * prose. Function bodies are `;`, and a declaration the reader has no Ballerina
 * form for becomes a comment naming it.
 *
 * Since the addressed verbs landed, this is the fallback rather than the default:
 * `overview`, `ops` and `type` answer by name or by path, and `api` exists for
 * the question none of them answered, and so that a stale instruction telling an
 * agent to grep one file is recoverable rather than fatal.
 */

import type {
  AnnotationDef,
  AttachmentPoint,
  ClientClass,
  Library,
  ListenerParam,
  Service,
  ServiceMethod,
} from "../model.js";
import { renderDescription, renderMemberFunction, renderStandaloneFunction } from "./signature.js";
import { renderTypeDef } from "./typedef.js";

const ATTACHMENT_POINT_LABELS: Readonly<Record<AttachmentPoint, string>> = {
  SERVICE: "service",
  OBJECT_METHOD: "service_function",
};

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
      const lines = [
        "// --- Service (generic) ---",
        `// Listener: ${service.listener.name}(${params})`,
        "// Instructions:",
      ];
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
 * functions, services, annotations. Reordering it was proposed and rejected —
 * it moves every declaration in all nine snapshots and does not solve the
 * motivating case, since `ballerinax/github`'s client section is 2,715 lines on
 * its own. The addressed verbs are the answer to "the client is at the bottom".
 */
export function toSyntaxString(library: Library): string {
  const output: string[] = ["// ============================================================", `// Library: ${library.name}`];
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
