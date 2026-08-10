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
 * Per-package corrections, applied after the IR is built.
 *
 * Every one of these exists because Central's docs disagree with what the
 * package actually compiles against, and an agent that trusts the docs writes
 * code that fails to build. They are deliberately narrow — keyed on the exact
 * library name, no pattern matching beyond the `ballerina/ai*` family — so a
 * package nobody has had trouble with passes through untouched.
 *
 * The service injectors are the other half: `ballerina/http` and friends are
 * written as services rather than called as clients, and Central describes no
 * service at all for them. Without this the most common thing an agent does
 * with `ballerina/http` has no entry in the output.
 *
 * Each patch takes a `Library` and returns one; nothing here mutates.
 */

import type { ClientClass, Library, ListenerParam, RecordField, Service, TypeDef } from "./model.js";

type Patch = (library: Library) => Library;

/** Rewrite one record's fields, leaving every other declaration identical. */
function mapRecord(library: Library, name: string, fn: (fields: readonly RecordField[]) => RecordField[]): Library {
  return {
    ...library,
    typeDefs: library.typeDefs.map((typeDef) =>
      typeDef.kind === "record" && typeDef.name === name ? { ...typeDef, fields: fn(typeDef.fields) } : typeDef,
    ),
  };
}

/**
 * `ballerinax/googleapis.sheets` — the Range record's values field.
 *
 * Central types it one dimension short. A sheet range is rows of cells, so the
 * value a caller assigns is a 2D array; the docs say 1D and the assignment
 * fails to compile.
 */
const fixSheets2DArray: Patch = (library) => {
  if (library.name !== "ballerinax/googleapis.sheets") return library;
  return mapRecord(library, "Range", (fields) =>
    fields.map((field, index) => (index === 1 ? { ...field, type: { name: "(int|string|decimal)[][]" } } : field)),
  );
};

/**
 * `ballerinax/sap` — two type names the module re-exports from `ballerina/http`
 * without Central listing them. Both appear in the client's signatures, so
 * without the declarations the output references names it never defines.
 */
const addLibsToSap: Patch = (library) => {
  if (library.name !== "ballerinax/sap") return library;
  const injected: TypeDef[] = [
    { kind: "error", name: "ClientError", description: "Defines the possible client error types." },
    {
      kind: "other",
      name: "RequestMessage",
      description: "The types of messages that are accepted by HTTP client when sending out the outbound request.",
    },
  ];
  return { ...library, typeDefs: [...injected, ...library.typeDefs] };
};

/**
 * `ballerinax/slack` — Slack's schema models "this call succeeded" as a
 * single-member type. Ballerina spells that as the literal `true`, and naming
 * the alias instead sends the agent looking for a type that is not worth
 * finding.
 */
const removeOkTrueDef: Patch = (library) => {
  if (library.name !== "ballerinax/slack") return library;

  const typeDefs = library.typeDefs.map((typeDef) => {
    if (typeDef.kind !== "record") return typeDef;
    return {
      ...typeDef,
      fields: typeDef.fields.map((field) =>
        field.type.name === "OkTrueDef" ? { ...field, type: { name: "true" } } : field,
      ),
    };
  });

  // The alias is gone from the signatures, so the trailing "comes from package
  // X" note must stop pointing at it.
  const clients: ClientClass[] = library.clients.map((client) => ({
    ...client,
    functions: client.functions.map((fn) => {
      const links = fn.returns.type.links?.filter((link) => link.recordName !== "OkTrueDef");
      if (!links || links.length === fn.returns.type.links?.length) return fn;
      return {
        ...fn,
        returns: {
          ...fn.returns,
          type: links.length > 0 ? { ...fn.returns.type, links } : { name: fn.returns.type.name },
        },
      };
    }),
  }));

  return { ...library, typeDefs, clients };
};

/**
 * `ballerinax/client.config` — `config` is not a reserved word but the module
 * path needs quoting in an import, and the unquoted form does not parse.
 */
const changeClientConfigName: Patch = (library) =>
  library.name === "ballerinax/client.config" ? { ...library, name: "ballerinax/'client.config" } : library;

/**
 * `ballerina/graphql` — ErrorDetail.locations.
 *
 * Central resolves it through the GraphQL parser's internal Location type,
 * which is not public API. `json[]` is what a caller can actually write.
 */
const simplifyGraphQLErrorDetail: Patch = (library) => {
  if (library.name !== "ballerina/graphql") return library;
  return mapRecord(library, "ErrorDetail", (fields) =>
    fields.map((field) => (field.name === "locations" ? { ...field, type: { name: "json[]" } } : field)),
  );
};

/**
 * `ballerina/ai*` — ChatClient is an internal detail of the agent runtime, not
 * something to construct directly, and it is the largest client in the module.
 */
const removeChatClient: Patch = (library) =>
  library.name.startsWith("ballerina/ai")
    ? { ...library, clients: library.clients.filter((client) => client.name !== "ChatClient") }
    : library;

/**
 * Replace whatever service Central described with the one-listener form these
 * packages are actually written against.
 */
function genericService(library: Library, parameter: ListenerParam): Library {
  const service: Service = {
    kind: "generic",
    instructions: "",
    listener: { name: "Listener", parameters: [parameter] },
  };
  return { ...library, services: [service] };
}

const addHttpService: Patch = (library) =>
  library.name === "ballerina/http" ? genericService(library, { name: "port", type: { name: "int" } }) : library;

const addGraphQLService: Patch = (library) =>
  library.name === "ballerina/graphql"
    ? genericService(library, { name: "listenTo", type: { name: "int" } })
    : library;

const addAiService: Patch = (library) =>
  library.name === "ballerina/ai" ? genericService(library, { name: "listenOn", type: { name: "int" } }) : library;

/**
 * Order matters in one place: `changeClientConfigName` and the service
 * injectors both key on `library.name`, so the rename runs after every patch
 * that could have matched the original name.
 */
const PATCHES: readonly Patch[] = [
  fixSheets2DArray,
  addLibsToSap,
  removeOkTrueDef,
  simplifyGraphQLErrorDetail,
  removeChatClient,
  addHttpService,
  addGraphQLService,
  addAiService,
  changeClientConfigName,
];

export function applyPatches(library: Library): Library {
  return PATCHES.reduce<Library>((current, patch) => patch(current), library);
}
