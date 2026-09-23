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
import { derivePrototypeModel } from "./derivePrototype";

const DSL = 'screen Login\n  button "Sign in" -> Home\nscreen Home\n';

describe("derivePrototypeModel", () => {
  it("compiles a wireframes .dsl into a prototype model", () => {
    const m = derivePrototypeModel("specs/design/components/shop/wireframes.dsl", DSL);
    expect(m?.screens.map((s) => s.name)).toEqual(["Login", "Home"]);
    expect(m?.screens[0]?.hotspots[0]?.target).toBe("Home");
  });
  it("returns null for domain-model sources", () => {
    expect(derivePrototypeModel("specs/design/erd.dsl", "entity User\n")).toBeNull();
  });
  it("returns null when the DSL does not compile", () => {
    expect(derivePrototypeModel("specs/design/components/shop/wireframes.dsl", "")).toBeNull();
  });
});
