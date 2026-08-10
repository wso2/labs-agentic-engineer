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
 * The intermediate representation: what a Ballerina package's public API is,
 * independent of how Central describes it or how it gets rendered.
 *
 * The pipeline is fetch → parse → transform → render and no stage mutates its
 * input, which is why everything here is `readonly`. Two shapes are unions
 * rather than optional-field bags, and both are load-bearing:
 *
 *   `Fn`      — a resource function's accessor and its path are separate
 *               fields, so the mistake the upstream reader warns about in prose
 *               ("never merge them into one string") has no field to live in.
 *   `TypeDef` — the renderer switches over `kind` with a `never` fallthrough,
 *               so a Ballerina shape nobody renders is a compile error rather
 *               than a silently dropped declaration.
 */

/**
 * Where a type name came from. `external` names are re-prefixed with the
 * owning module at render time and gathered into the trailing agent note;
 * `internal` ones are already in scope.
 */
export type Link =
  | { readonly category: "internal"; readonly recordName: string }
  | { readonly category: "external"; readonly libraryName: string; readonly recordName: string };

/** A rendered type expression plus the packages its names came from. */
export interface TypeRef {
  readonly name: string;
  readonly links?: readonly Link[];
}

export interface Param {
  readonly name: string;
  readonly description: string;
  readonly type: TypeRef;
  readonly default?: string;
}

export interface ReturnDef {
  readonly type: TypeRef;
  readonly description?: string;
}

export interface RecordField {
  readonly name: string;
  readonly description: string;
  readonly type: TypeRef;
  readonly default?: string;
  readonly optional?: boolean;
}

export interface EnumMember {
  readonly name: string;
}

/**
 * One segment of a resource path. `literal` carries the raw segment — including
 * the odd bracketed form Central emits without a type (`[\"quoted\"]`) — while
 * `param` keeps the declared type and name apart.
 */
export type PathSegment =
  | { readonly kind: "literal"; readonly text: string }
  | { readonly kind: "param"; readonly type: string; readonly name: string };

/**
 * A function on a client (or at module scope).
 *
 * `accessor` is a plain string rather than a closed set of HTTP methods:
 * Ballerina's resource accessor is an identifier, and `subscribe` (websub,
 * graphql) is as legal as `get`. Constraining it would reject real packages
 * without buying anything — the guarantee that matters is structural, and that
 * comes from `paths` being its own field.
 */
export type Fn =
  | {
      readonly kind: "constructor";
      readonly description: string;
      readonly params: readonly Param[];
      readonly returns: ReturnDef;
    }
  | {
      readonly kind: "remote";
      readonly name: string;
      readonly description: string;
      readonly params: readonly Param[];
      readonly returns: ReturnDef;
    }
  | {
      readonly kind: "normal";
      readonly name: string;
      readonly description: string;
      readonly params: readonly Param[];
      readonly returns: ReturnDef;
    }
  | {
      readonly kind: "resource";
      readonly accessor: string;
      readonly paths: readonly PathSegment[];
      readonly description: string;
      readonly params: readonly Param[];
      readonly returns: ReturnDef;
    };

/**
 * A module-level type declaration.
 *
 * `other` is the honest name for what Central calls a string/int/decimal/array/
 * boolean/simple-name-reference type: the renderer has no Ballerina form for
 * them and emits a placeholder comment, which is what the upstream reader does
 * too. Keeping them in the union rather than dropping them preserves the
 * declaration's existence in the output.
 */
export type TypeDef =
  | {
      readonly kind: "record";
      readonly name: string;
      readonly description: string;
      readonly fields: readonly RecordField[];
    }
  | {
      readonly kind: "enum";
      readonly name: string;
      readonly description: string;
      readonly members: readonly EnumMember[];
    }
  | {
      readonly kind: "union";
      readonly name: string;
      readonly description: string;
      readonly members: readonly TypeRef[];
    }
  | {
      readonly kind: "constant";
      readonly name: string;
      readonly description: string;
      readonly value: string;
      readonly varType: TypeRef;
    }
  | { readonly kind: "class"; readonly name: string; readonly description: string }
  | { readonly kind: "error"; readonly name: string; readonly description: string }
  | { readonly kind: "other"; readonly name: string; readonly description: string };

/**
 * A function at module scope. A constructor belongs to a class and a resource
 * function to a client, so neither can appear here — which is why `Library`
 * names this type rather than `Fn`.
 */
export type StandaloneFn = Extract<Fn, { kind: "normal" | "remote" }>;

export interface ClientClass {
  readonly name: string;
  readonly description: string;
  readonly functions: readonly Fn[];
}

export interface ListenerParam {
  readonly name: string;
  readonly type: TypeRef;
  readonly default?: string;
}

export interface ServiceMethod {
  readonly name: string;
  readonly description: string;
  readonly isDeprecated: boolean;
  readonly params: readonly ListenerParam[];
  readonly returns: ReturnDef;
}

/**
 * How a package expects a service to be written against it.
 *
 * `fixed` is derived from the package's own listener and service types — the
 * exact `service X on new Y(...)` block plus the remote contract it must
 * implement. `generic` is attached by a patch for the few packages whose
 * service story Central does not describe at all (`ballerina/http` and
 * friends), and renders as a comment block rather than a template.
 */
export type Service =
  | {
      readonly kind: "generic";
      readonly instructions: string;
      readonly listener: { readonly name: string; readonly parameters: readonly ListenerParam[] };
    }
  | {
      readonly kind: "fixed";
      readonly name: string;
      readonly isDeprecated: boolean;
      readonly listener: { readonly name: string; readonly parameters: readonly ListenerParam[] };
      readonly methods: readonly ServiceMethod[];
    };

/** Attachment points this reader renders. Central reports more; the rest are dropped. */
export type AttachmentPoint = "SERVICE" | "OBJECT_METHOD";

export interface AnnotationDef {
  readonly name: string;
  readonly description: string;
  readonly attachmentPoint: AttachmentPoint;
}

export interface Library {
  /** `org/module-id` — the string an `import` statement takes. */
  readonly name: string;
  readonly description: string;
  readonly typeDefs: readonly TypeDef[];
  readonly clients: readonly ClientClass[];
  readonly functions: readonly StandaloneFn[];
  readonly services: readonly Service[];
  readonly annotations: readonly AnnotationDef[];
}
