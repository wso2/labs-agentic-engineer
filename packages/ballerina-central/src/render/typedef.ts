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
 * One module-level declaration, as Ballerina.
 *
 * `renderTypeDef` switches over `kind` with a `never` fallthrough: adding a
 * Ballerina shape to the IR fails the build here until it has a rendering. It is
 * also the whole of the `type` verb's output — that verb selects declarations
 * and prints them, so a declaration read by name is byte-identical to the same
 * declaration inside the API document.
 *
 * Spacing differs by kind and is load-bearing: an undescribed record still opens
 * with a blank line and an undescribed enum does not. Neither is an accident,
 * and changing either moves every snapshot.
 */

import type { TypeDef } from "../model.js";
import {
  applyPrefixToTypeName,
  buildSpecialAgentNote,
  collectExternalLinks,
  renderDescription,
} from "./signature.js";

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
 *   base only         `type X A|B|C;`
 *   neither           `type X error;`
 *
 * Every one of `ballerina/http`'s 56 errors used to render as the last line,
 * which made the subtype hierarchy — and therefore `e is http:ClientRequestError`
 * — unlearnable from the document. The absent-base default is `error` rather
 * than nothing because an error at the top of its own hierarchy narrows the
 * language's `error`, and that is what its declaration says.
 */
function renderError(typeDef: Extract<TypeDef, { kind: "error" }>): string {
  const links = collectExternalLinks(typeDef.base);
  const base = typeDef.base === undefined ? "error" : applyPrefixToTypeName(typeDef.base.name, links);
  const distinct = typeDef.isDistinct ? "distinct " : "";
  return (
    `${renderDescription(typeDef.description)}type ${typeDef.name} ${distinct}${base};` + buildSpecialAgentNote(links)
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
