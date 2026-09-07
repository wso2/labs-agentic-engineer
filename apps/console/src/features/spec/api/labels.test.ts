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
    expect(fileLabel("specs/validation/validation-criteria.json")).toBe("Validation criteria");
  });

  it("names a component's artifacts without repeating the component", () => {
    expect(fileLabel("specs/design/components/orders/design.json")).toBe("Design");
    expect(fileLabel("specs/design/components/orders/openapi.yaml")).toBe("API");
  });

  it("falls back to the slug for feature and flow files", () => {
    expect(fileLabel("specs/requirements/features/checkout.md")).toBe("checkout");
    expect(fileLabel("specs/design/flows/checkout.md")).toBe("checkout");
  });
});
