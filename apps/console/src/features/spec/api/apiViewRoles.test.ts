/**
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

import { describe, expect, it } from "vitest";

import { EXPENSE_TRACKER_TEXT } from "../components/security/testFixtures";
import { scopeRolesOf } from "./apiViewRoles";

describe("scopeRolesOf — the catalog as the API view reads it", () => {
  const roles = scopeRolesOf(EXPENSE_TRACKER_TEXT)!;

  it("keys every handle the catalog declares, and nothing else", () => {
    expect(Object.keys(roles).sort()).toEqual([
      "claims:approve",
      "claims:read",
      "claims:read-all",
      "claims:reject",
      "claims:submit",
      "reports:export",
      "reports:read",
    ]);
  });

  it("names the granting roles in declaration order", () => {
    expect(roles["claims:read-all"]).toEqual(["Approver"]);
    expect(roles["reports:read"]).toEqual(["Approver"]);
  });

  it("carries the ownership qualifier for an own-rows action", () => {
    expect(roles["claims:read"]).toEqual({
      roles: ["Employee", "Approver"],
      note: "own rows",
    });
    expect(roles["claims:submit"]).toEqual({
      roles: ["Employee"],
      note: "own rows",
    });
  });

  // "Declared and granted by nobody" is a real answer, and the API view draws
  // it as a row with no names beside its handle.
  it("answers an empty list for a handle no role grants", () => {
    expect(roles["reports:export"]).toEqual([]);
  });

  // `undefined` and `{}` are different: with no map the view renders exactly as
  // it did before scopes existed, and with an empty one it starts labelling
  // every row. A project with no readable catalog must get the first.
  it("answers undefined when there is no readable catalog", () => {
    expect(scopeRolesOf(null)).toBeUndefined();
    expect(scopeRolesOf("")).toBeUndefined();
    expect(scopeRolesOf("{}")).toBeUndefined();
    expect(scopeRolesOf('{"version": 2,')).toBeUndefined();
  });
});
