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
 * Rendering rules, one at a time.
 *
 * The corpus proves the whole pipeline against real packages; these pin the
 * individual decisions, so a snapshot diff has something to be explained by.
 */

import assert from "node:assert/strict";
import { test } from "node:test";
import type { Library, TypeDef } from "../src/model.js";
import {
  applyPrefixToTypeName,
  buildSpecialAgentNote,
  deriveModulePrefix,
  renderTypeDef,
  toSyntaxString,
} from "../src/render.js";

const EMPTY: Library = {
  name: "test/pkg",
  description: "",
  typeDefs: [],
  clients: [],
  functions: [],
  services: [],
  annotations: [],
};

test("a module alias is the last dotted segment of the package path", () => {
  assert.equal(deriveModulePrefix("ballerinax/googleapis.gmail"), "gmail");
  assert.equal(deriveModulePrefix("ballerina/http"), "http");
});

test("a foreign name is qualified once, never twice", () => {
  const links = [{ recordName: "Message", libraryName: "ballerinax/googleapis.gmail", modulePrefix: "gmail" }];
  assert.equal(applyPrefixToTypeName("Message", links), "gmail:Message");
  assert.equal(applyPrefixToTypeName("gmail:Message", links), "gmail:Message");
  // Inside a union and an array, both members still get qualified.
  assert.equal(applyPrefixToTypeName("Message[]|error", links), "gmail:Message[]|error");
  // A longer name that merely contains it is a different type.
  assert.equal(applyPrefixToTypeName("MessageList", links), "MessageList");
});

test("the agent note groups names by the package they come from", () => {
  const note = buildSpecialAgentNote([
    { recordName: "Message", libraryName: "ballerinax/googleapis.gmail", modulePrefix: "gmail" },
    { recordName: "Error", libraryName: "ballerina/sql", modulePrefix: "sql" },
    { recordName: "Draft", libraryName: "ballerinax/googleapis.gmail", modulePrefix: "gmail" },
  ]);
  assert.equal(
    note,
    " // Special Agent Note: Message, Draft FROM ballerinax/googleapis.gmail package, Error FROM ballerina/sql package",
  );
  assert.equal(buildSpecialAgentNote([]), "");
});

// One per `TypeDef` kind. Typed as a total record, so adding a kind to the IR
// fails to compile here until it has a case — the test-side half of the
// renderer's `never` check.
const ONE_OF_EACH: Readonly<Record<TypeDef["kind"], TypeDef>> = {
  record: {
    kind: "record",
    name: "Stars",
    description: "A star count.",
    fields: [
      { name: "owner", description: "", type: { name: "string" } },
      { name: "count", description: "", type: { name: "int" }, optional: true, default: "0" },
    ],
  },
  enum: { kind: "enum", name: "Colour", description: "", members: [{ name: "RED" }, { name: "GREEN" }] },
  union: { kind: "union", name: "Id", description: "", members: [{ name: "int" }, { name: "string" }] },
  constant: { kind: "constant", name: "NAME", description: "", value: "aep", varType: { name: "string" } },
  class: { kind: "class", name: "Engine", description: "" },
  error: { kind: "error", name: "ClientError", description: "" },
  other: { kind: "other", name: "Weird", description: "" },
};

test("every type kind renders", () => {
  assert.equal(
    renderTypeDef(ONE_OF_EACH.record),
    "# A star count.\n\ntype Stars record {\n    string owner;\n    int count? = 0;\n};",
  );
  // Spacing differs by kind and is load-bearing for the snapshots: an
  // undescribed record still opens with a blank line, an undescribed enum does
  // not. Neither is an accident, and changing either moves every snapshot.
  assert.equal(
    renderTypeDef({ ...ONE_OF_EACH.record, description: "" } as TypeDef),
    "\ntype Stars record {\n    string owner;\n    int count? = 0;\n};",
  );
  assert.equal(renderTypeDef(ONE_OF_EACH.enum), "enum Colour {\n    RED,\n    GREEN\n}");
  assert.equal(renderTypeDef(ONE_OF_EACH.union), "type Id int|string;");
  assert.equal(renderTypeDef(ONE_OF_EACH.constant), 'const string NAME = "aep";');
  assert.equal(renderTypeDef(ONE_OF_EACH.class), "class Engine {\n}");
  assert.equal(renderTypeDef(ONE_OF_EACH.error), "type ClientError error;");
  assert.equal(renderTypeDef(ONE_OF_EACH.other), "// Unknown type: Weird");
});

test("a description becomes a Ballerina doc comment", () => {
  assert.match(renderTypeDef(ONE_OF_EACH.record), /^# A star count\.\n/);
});

test("path parameters are declared in the path and not repeated in the parameter list", () => {
  const rendered = toSyntaxString({
    ...EMPTY,
    clients: [
      {
        name: "Client",
        description: "",
        functions: [
          {
            kind: "resource",
            accessor: "get",
            paths: [
              { kind: "literal", text: "repos" },
              { kind: "param", type: "string", name: "owner" },
            ],
            description: "",
            params: [
              { name: "owner", description: "", type: { name: "string" } },
              { name: "page", description: "", type: { name: "int" }, default: "1" },
            ],
            returns: { type: { name: "json" } },
          },
        ],
      },
    ],
  });
  assert.match(rendered, /resource function get repos\/\[string owner\]\(int page = 1\) returns json;/);
});

test("a service template names the listener and the contract it implies", () => {
  const rendered = toSyntaxString({
    ...EMPTY,
    services: [
      {
        kind: "fixed",
        name: "ConsumerService",
        isDeprecated: false,
        listener: {
          name: "kafka:Listener",
          parameters: [{ name: "config", type: { name: "ConsumerConfiguration" } }],
        },
        methods: [
          {
            name: "onConsumerRecord",
            description: "",
            isDeprecated: false,
            params: [{ name: "records", type: { name: "BytesConsumerRecord[]" } }],
            returns: { type: { name: "error?" } },
          },
        ],
      },
    ],
  });
  assert.match(rendered, /service kafka:ConsumerService on new kafka:Listener\(ConsumerConfiguration config\) \{/);
  assert.match(rendered, /remote function onConsumerRecord\(BytesConsumerRecord\[\] records\) returns error\?;/);
});

test("a section with nothing in it prints no banner", () => {
  const rendered = toSyntaxString(EMPTY);
  assert.equal(rendered.includes("// --- Types ---"), false);
  assert.equal(rendered.includes("// --- Client ---"), false);
  assert.match(rendered, /^\/\/ =+\n\/\/ Library: test\/pkg\n\/\/ =+\nimport test\/pkg;\n$/);
});
