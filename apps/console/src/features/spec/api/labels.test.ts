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
import { fileLabel } from "./labels";

describe("fileLabel — a document's name, never its filename", () => {
  it("names the rail's documents", () => {
    expect(fileLabel("specs/requirements/prd.md")).toBe("Product requirements");
    expect(fileLabel("specs/design/domain-model.md")).toBe("Domain model");
    expect(fileLabel("specs/design/security.json")).toBe("Security");
  });

  it("names a component's artifacts without repeating the component", () => {
    expect(fileLabel("specs/design/components/orders/design.json")).toBe("Design");
    expect(fileLabel("specs/design/components/orders/openapi.yaml")).toBe("API");
  });

  it("names a dependency's files without repeating the dependency", () => {
    expect(fileLabel("specs/design/dependencies/stripe/dependency.json")).toBe("Definition");
    expect(fileLabel("specs/design/dependencies/stripe/openapi.yaml")).toBe("API");
    expect(fileLabel("specs/design/dependencies/shop/schema.graphql")).toBe("API");
    expect(fileLabel("specs/design/dependencies/s3/sdk.json")).toBe("SDK");
  });

  it("falls back to the slug for feature and flow files", () => {
    expect(fileLabel("specs/requirements/features/checkout.md")).toBe("checkout");
    expect(fileLabel("specs/design/flows/checkout.md")).toBe("checkout");
  });

  // A `.feature` slug is a capability name the agent chose, and the rail reads as
  // a document tree — so it is cased like one. First word only: a capability is a
  // phrase, and Title Casing Every Word reads as a product name.
  it("title-cases an acceptance capability", () => {
    expect(fileLabel("specs/validation/acceptance/bought-items.feature")).toBe("Bought items");
    expect(fileLabel("specs/validation/acceptance/checkout.feature")).toBe("Checkout");
    expect(fileLabel("specs/validation/acceptance/shared_list_access.feature")).toBe(
      "Shared list access",
    );
  });

  // A requirement's depth document keeps its verbatim name: the PRD references
  // those by name, and re-casing one would stop the two matching.
  it("leaves a requirement's own feature document uncased", () => {
    expect(fileLabel("specs/requirements/features/bought-items.md")).toBe("bought-items");
  });
});
