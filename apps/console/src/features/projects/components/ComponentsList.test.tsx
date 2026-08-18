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

// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { components } from "../../../generated/aep-api";

let mockUrl: string | undefined = "https://storefront.dev.example.com/";

vi.mock("../api/queries", () => ({
  useComponentEndpointUrl: () => ({ data: mockUrl }),
}));

vi.mock("./ComponentOpenApiDialog", () => ({
  ComponentOpenApiDialog: () => null,
}));

import { ComponentsList } from "./ComponentsList";

type Component = components["schemas"]["Component"];

const webapp: Component = {
  name: "storefront",
  displayName: "Storefront",
  description: "SPA",
  type: "web-application",
  status: "active",
};

describe("ComponentsList — Open app", () => {
  it("links a deployed web-application to its public URL", () => {
    mockUrl = "https://storefront.dev.example.com/";
    render(<ComponentsList projectName="shop" items={[webapp]} />);
    const link = screen.getByRole("link", { name: "Open Storefront" });
    expect(link).toHaveAttribute("href", "https://storefront.dev.example.com/");
  });

  it("omits Open when the web-application has no public URL yet", () => {
    mockUrl = undefined;
    render(<ComponentsList projectName="shop" items={[webapp]} />);
    expect(screen.queryByRole("link", { name: "Open Storefront" })).toBeNull();
  });
});
