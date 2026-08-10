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
 * Central's docs payload → the `Library` IR.
 *
 * This is where Central's flag-bag encoding is decided once and for all: a type
 * node's meaning comes from which of a dozen booleans are set, a function's
 * from `isResource`/`isRemote`/`name === "init"`, and a record field's from
 * whether `inclusionType` is present. Everything downstream sees a union with a
 * `kind`, and never a flag.
 *
 * Central models several declarations it has no Ballerina rendering for —
 * string/int/decimal/array/boolean/simple-name-reference type aliases — and
 * they land as `other`. That is not a gap: the declaration exists, the output
 * says so, and inventing a rendering for it would be a change in what the
 * reader claims, not a bug fix.
 */

import type {
  AnnotationDef,
  AttachmentPoint,
  ClientClass,
  EnumMember,
  Fn,
  Library,
  Link,
  ListenerParam,
  Param,
  PathSegment,
  RecordField,
  ReturnDef,
  Service,
  ServiceMethod,
  StandaloneFn,
  TypeDef,
  TypeRef,
} from "./model.js";
import type {
  CentralDocs,
  CentralMethod,
  CentralModule,
  CentralParameter,
  CentralRecordField,
  CentralType,
} from "./central/schema.js";
import { formatQualifiedName, type QualifiedName } from "./qualified.js";
import { err, ok, SCHEMA_DRIFT_SUGGESTION, type Result } from "./result.js";

/** The module a name belongs to, for deciding whether it needs a prefix. */
interface Scope {
  readonly moduleId: string;
  readonly orgName: string;
}

// ---------------------------------------------------------------------------
// Type expressions
// ---------------------------------------------------------------------------

/**
 * Which of Central's three type encodings a node uses.
 *
 * `UNK_ORG` is Central's own placeholder for "no owning module", so it counts
 * as absent rather than as an org named UNK_ORG.
 */
function classify(type: CentralType): "external" | "basic" | "ref" {
  if (type.orgName && type.orgName !== "UNK_ORG" && type.moduleName) return "external";
  if (type.name && type.category) return "basic";
  return "ref";
}

/** `googleapis.gmail` → `gmail`: the alias an import statement puts in scope. */
function moduleNameSuffix(moduleName: string): string {
  const parts = moduleName.split(".");
  return parts[parts.length - 1] ?? moduleName;
}

/** Rebuild a ref, keeping links only when there are some (`exactOptionalPropertyTypes`). */
function ref(name: string, links?: readonly Link[]): TypeRef {
  return links && links.length > 0 ? { name, links } : { name };
}

function optional(type: TypeRef, isNullable: boolean | undefined): TypeRef {
  return isNullable ? ref(`${type.name}?`, type.links) : type;
}

function mergeMembers(types: readonly TypeRef[], separator: string): TypeRef {
  const links = types.flatMap((t) => t.links ?? []);
  return ref(types.map((t) => t.name).join(separator), links);
}

function transformExternal(type: CentralType, scope: Scope): TypeRef {
  const moduleName = type.moduleName ?? "";
  const orgName = type.orgName ?? "";
  const recordName = type.name ?? "";
  const sameModule = moduleName === scope.moduleId && orgName === scope.orgName;

  let name = recordName;
  let link: Link;
  if (sameModule) {
    link = { category: "internal", recordName };
  } else {
    // `client.config` is a reserved-word module path; Ballerina needs it quoted
    // in an import, so the link carries the quoted form.
    const libraryName = moduleName === "client.config" ? `${orgName}/'client.config` : `${orgName}/${moduleName}`;
    link = { category: "external", libraryName, recordName };
    name = `${moduleNameSuffix(moduleName)}:${name}`;
  }

  if (type.isArrayType) name += "[]";
  if (type.isNullable) name += "?";
  return ref(name, [link]);
}

function transformBasic(type: CentralType, scope: Scope): TypeRef {
  const name = type.name ?? "";

  if (name === "stream") {
    const members = (type.memberTypes ?? []).map((m) => transformType(m, scope));
    const inner = mergeMembers(members, ",");
    return ref(`stream<${inner.name}>`, inner.links);
  }

  if (name === "map") {
    if (type.constraint) return ref(`map<${transformType(type.constraint, scope).name}>`);
    return ref("map<any>");
  }

  let result = name;
  if (type.isArrayType) result += "[]";
  if (type.isNullable) result += "?";
  return ref(result);
}

function transformRef(type: CentralType, scope: Scope): TypeRef {
  const members = type.memberTypes ?? [];

  if (type.isAnonymousUnionType) {
    return optional(mergeMembers(members.map((m) => transformType(m, scope)), "|"), type.isNullable);
  }
  if (type.isIntersectionType) {
    return optional(mergeMembers(members.map((m) => transformType(m, scope)), "&"), type.isNullable);
  }

  if (type.category === "inline_record" || type.category === "inline_closed_record") {
    const first = members[0];
    // Named-field form: each member is a field, not a type. The alternative
    // form carries bare types and collapses to an opaque `record {}`.
    if (first && first.name !== undefined && first.elementType) {
      let body = "record {";
      for (const member of members) {
        if (!member.elementType) continue;
        const fieldType = transformType(member.elementType, scope);
        body += `${fieldType.name}${member.isOptional ? "?" : ""} ${member.name}; `;
      }
      body += "}";
      return optional(ref(body), type.isNullable);
    }
    return optional(inlineRecord(members, scope), type.isNullable);
  }

  if (type.elementType) {
    let result = transformType(type.elementType, scope);
    if (type.isParenthesisedType) result = ref(`(${result.name})`, result.links);
    if (type.isArrayType) result = ref(`${result.name}[]`, result.links);
    if (type.isTypeDesc) result = ref(`typedesc<${result.name}>`, result.links);
    return optional(result, type.isNullable);
  }

  return optional(inlineRecord(members, scope), type.isNullable);
}

/** An anonymous record whose fields Central did not describe; only its links survive. */
function inlineRecord(members: readonly CentralType[], scope: Scope): TypeRef {
  const links = members.flatMap((m) => transformType(m, scope).links ?? []);
  return ref("record {}", links);
}

export function transformType(type: CentralType, scope: Scope): TypeRef {
  const encoding = classify(type);
  switch (encoding) {
    case "external":
      return transformExternal(type, scope);
    case "basic":
      return transformBasic(type, scope);
    case "ref":
      return transformRef(type, scope);
    default: {
      const exhaustive: never = encoding;
      return exhaustive;
    }
  }
}

// ---------------------------------------------------------------------------
// Functions
// ---------------------------------------------------------------------------

function transformParams(parameters: readonly CentralParameter[] | undefined, scope: Scope): Param[] {
  return (parameters ?? []).map((parameter) => {
    const defaultValue = (parameter.defaultValue ?? "").trim();
    return {
      name: parameter.name,
      description: (parameter.description ?? "").trim(),
      type: transformType(parameter.type, scope),
      ...(defaultValue === "" ? {} : { default: defaultValue }),
    };
  });
}

function transformReturn(method: CentralMethod, scope: Scope): ReturnDef {
  const first = (method.returnParameters ?? [])[0];
  // A function Central gives no return parameters for returns nothing. `nil` is
  // Ballerina's name for that, and saying it beats an empty `returns` clause.
  if (!first) return { type: { name: "nil" } };
  const description = (first.description ?? "").trim();
  return {
    type: transformType(first.type, scope),
    ...(description === "" ? {} : { description }),
  };
}

/**
 * Split a resource path into its segments, keeping a path parameter's type and
 * name apart. Central writes them as `[string owner]`; a bracketed segment with
 * no space inside is not a parameter and stays a literal.
 */
export function createPaths(resourcePath: string | undefined): PathSegment[] {
  if (!resourcePath) return [];
  return resourcePath.split("/").map((segment): PathSegment => {
    if (segment.startsWith("[") && segment.endsWith("]")) {
      const inner = segment.slice(1, -1);
      const space = inner.indexOf(" ");
      if (space !== -1) {
        return { kind: "param", type: inner.slice(0, space), name: inner.slice(space + 1) };
      }
    }
    return { kind: "literal", text: segment };
  });
}

export function transformMethod(method: CentralMethod, scope: Scope): Fn {
  const params = transformParams(method.parameters, scope);
  const returns = transformReturn(method, scope);
  const description = (method.description ?? "").trim();

  if (method.name === "init") return { kind: "constructor", description, params, returns };
  if (method.isResource) {
    return {
      kind: "resource",
      accessor: method.accessor ?? "",
      paths: createPaths(method.resourcePath),
      description,
      params,
      returns,
    };
  }
  if (method.isRemote) return { kind: "remote", name: method.name, description, params, returns };
  return { kind: "normal", name: method.name, description, params, returns };
}

// ---------------------------------------------------------------------------
// Records
// ---------------------------------------------------------------------------

/**
 * Fields of one record, with inclusions (`*Other;`) spliced in.
 *
 * Keyed by name as it goes: an included field that the record also declares
 * itself must resolve to the declaration, and Central lists them in that order.
 */
function transformRecordFields(declared: readonly CentralRecordField[] | undefined, scope: Scope): RecordField[] {
  const fields = new Map<string, RecordField>();
  for (const field of declared ?? []) {
    if ("inclusionType" in field) {
      for (const member of field.inclusionType.memberTypes ?? []) {
        if (member.name === undefined || !member.elementType) continue;
        fields.set(member.name, {
          name: member.name,
          description: (member.description ?? "").trim(),
          type: transformType(member.elementType, scope),
        });
      }
      continue;
    }
    const defaultValue = (field.defaultValue ?? "").trim();
    fields.set(field.name, {
      name: field.name,
      description: (field.description ?? "").trim(),
      type: transformType(field.type, scope),
      ...(defaultValue === "" ? {} : { default: defaultValue }),
      ...(field.type.isOptional ? { optional: true } : {}),
    });
  }
  return [...fields.values()];
}

// ---------------------------------------------------------------------------
// Services
// ---------------------------------------------------------------------------

function listenerParams(parameters: readonly Param[]): ListenerParam[] {
  // The service template shows a listener's shape, not its docs, so parameters
  // are reduced to type + name (+ default) here.
  return parameters.map((parameter) => ({
    name: parameter.name,
    type: { name: parameter.type.name },
    ...(parameter.default === undefined ? {} : { default: parameter.default }),
  }));
}

/**
 * Pair every service type the module declares with its listener, producing the
 * `service X on new Y(...)` template plus the remote contract the service must
 * implement.
 *
 * Connectors expose a single listener, so the first one is the listener. A
 * module with service types but no listener (or the reverse) describes no
 * service worth templating.
 */
function buildServices(module: CentralModule, scope: Scope): Service[] {
  const listener = module.listeners[0];
  if (!listener || module.serviceTypes.length === 0) return [];
  const parameters = listenerParams(transformParams(listener.initMethod?.parameters, scope));

  return module.serviceTypes.map((serviceType): Service => {
    const methods: ServiceMethod[] = [];
    for (const method of serviceType.methods ?? []) {
      const fn = transformMethod(method, scope);
      // Resource functions have no name, only an accessor and a path, and the
      // template renders methods as `remote function <name>`. Dropping them
      // beats emitting a nameless declaration — a listener service type's
      // contract is remote methods.
      if (fn.kind === "resource" || fn.kind === "constructor") continue;
      methods.push({
        name: fn.name,
        description: fn.description,
        isDeprecated: method.isDeprecated ?? false,
        params: listenerParams(fn.params),
        returns: fn.returns,
      });
    }
    return {
      kind: "fixed",
      name: serviceType.name,
      isDeprecated: serviceType.isDeprecated ?? false,
      listener: { name: `${scope.moduleId}:${listener.name}`, parameters },
      methods,
    };
  });
}

/**
 * Service- and resource-level annotations.
 *
 * Central reports attachment points as a comma-separated string. Only the two
 * points a service author writes are rendered; an annotation that attaches only
 * to parameters, returns, record fields or types is not something the agent
 * reaches for from a service, and listing it costs context.
 */
function buildAnnotations(module: CentralModule): AnnotationDef[] {
  const annotations: AnnotationDef[] = [];
  for (const annotation of module.annotations) {
    const points = (annotation.attachmentPoints ?? "").split(",").map((point) => point.trim());
    const attachmentPoint: AttachmentPoint | undefined = points.includes("service")
      ? "SERVICE"
      : points.includes("object function")
        ? "OBJECT_METHOD"
        : undefined;
    if (!attachmentPoint) continue;
    annotations.push({
      name: annotation.name,
      description: (annotation.description ?? "").trim(),
      attachmentPoint,
    });
  }
  return annotations;
}

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

/**
 * The module of the payload that the caller actually asked for.
 *
 * Reading `modules[0]` instead — which is what this did — is untested by
 * construction, because every fixture in the corpus is single-module: a
 * multi-module package would render whichever module Central happened to put
 * first, under the name the caller typed. It is also what makes the cache's
 * coordinate check meaningful; verifying one module and then rendering another
 * verifies nothing.
 *
 * `id === name` is the package's default module — the one `import org/name;`
 * puts in scope. `id.startsWith(name + ".")` catches the submodule form
 * (`googleapis.gmail` under `googleapis`), which Central names the same way.
 */
export function selectModule(docs: CentralDocs, qualified: QualifiedName): Result<CentralModule> {
  const wanted = docs.docsData.modules.find(
    (module) =>
      module.orgName === qualified.org && (module.id === qualified.name || module.id.startsWith(`${qualified.name}.`)),
  );
  if (wanted) return ok(wanted);
  return err({
    kind: "schema-drift",
    qualified: formatQualifiedName(qualified),
    issues: [
      {
        path: "docsData.modules",
        message: `no module matches; Central returned ${docs.docsData.modules
          .map((module) => `${module.orgName}/${module.id}`)
          .join(", ")}`,
      },
    ],
    suggestion: SCHEMA_DRIFT_SUGGESTION,
  });
}

export function fromCentral(module: CentralModule): Library {
  const scope: Scope = { moduleId: module.id, orgName: module.orgName };

  const typeDefs: TypeDef[] = [];
  const describe = (value: { description?: string | undefined }): string => (value.description ?? "").trim();

  for (const record of module.records) {
    typeDefs.push({
      kind: "record",
      name: record.name,
      description: describe(record),
      fields: transformRecordFields(record.fields, scope),
    });
  }

  // Ordering is part of the output contract — a reader greps this file and the
  // section a name lands in is how they find it. Aliases Central models as
  // dedicated node kinds all render the same way, so they are grouped by the
  // order Central lists them rather than merged.
  for (const alias of module.stringTypes) typeDefs.push({ kind: "other", name: alias.name, description: describe(alias) });
  for (const alias of module.integerTypes) typeDefs.push({ kind: "other", name: alias.name, description: describe(alias) });
  for (const alias of module.decimalTypes) typeDefs.push({ kind: "other", name: alias.name, description: describe(alias) });
  for (const alias of module.arrayTypes) typeDefs.push({ kind: "other", name: alias.name, description: describe(alias) });
  for (const error of module.errors) {
    typeDefs.push({
      kind: "error",
      name: error.name,
      description: describe(error),
      isDistinct: error.isDistinct,
      ...(error.detailType === undefined ? {} : { base: transformType(error.detailType, scope) }),
    });
  }

  for (const constant of module.constants) {
    typeDefs.push({
      kind: "constant",
      name: constant.name,
      description: describe(constant),
      value: constant.value,
      varType: transformType(constant.type, scope),
    });
  }

  for (const enumeration of module.enums) {
    const members: EnumMember[] = (enumeration.members ?? []).map((member) => ({ name: member.name }));
    typeDefs.push({ kind: "enum", name: enumeration.name, description: describe(enumeration), members });
  }

  // Central describes a class's methods, but the reader does not render them:
  // a class is shown as evidence that the name exists, and the clients section
  // is where callable surface lives.
  for (const cls of module.classes) typeDefs.push({ kind: "class", name: cls.name, description: "" });
  for (const objectType of module.objectTypes) typeDefs.push({ kind: "class", name: objectType.name, description: "" });

  for (const union of module.unionTypes) {
    typeDefs.push({
      kind: "union",
      name: union.name,
      description: describe(union),
      members: (union.memberTypes ?? []).map((member) => ({ name: transformType(member, scope).name })),
    });
  }
  // An intersection renders through the union form. Ballerina spells the two
  // differently (`&` vs `|`), and correcting that is a change in output, not a
  // port — it belongs to a snapshot-moving change, not this one.
  for (const intersection of module.intersectionTypes) {
    typeDefs.push({
      kind: "union",
      name: intersection.name,
      description: describe(intersection),
      members: (intersection.memberTypes ?? []).map((member) => ({ name: transformType(member, scope).name })),
    });
  }

  for (const alias of module.simpleNameReferenceTypes) {
    typeDefs.push({ kind: "other", name: alias.name, description: describe(alias) });
  }
  for (const alias of module.booleanTypes) {
    typeDefs.push({ kind: "other", name: alias.name, description: describe(alias) });
  }

  const clients: ClientClass[] = module.clients.map((client) => ({
    name: client.name,
    description: describe(client),
    functions: (client.methods ?? []).map((method) => transformMethod(method, scope)),
  }));

  // Module-level resource functions and constructors are not callable as
  // free functions, so only the two forms that are get listed.
  const functions = module.functions
    .map((method) => transformMethod(method, scope))
    .filter((fn): fn is StandaloneFn => fn.kind === "normal" || fn.kind === "remote");

  return {
    name: `${module.orgName}/${module.id}`,
    description: (module.summary ?? "").trim(),
    typeDefs,
    clients,
    functions,
    services: buildServices(module, scope),
    annotations: buildAnnotations(module),
  };
}
