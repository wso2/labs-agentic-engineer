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
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "../../../generated/aep-api";

type Component = components["schemas"]["Component"];

const PROJECT = "demo-shop";

// The web app's public URL is a read of its DEPLOYMENTS (#196), keyed by
// component — so the stub answers per component name, which is also how the
// "not deployed yet" case is expressed: no entry, no URL.
const endpointUrls = vi.hoisted(() => ({
  current: {} as Record<string, string | undefined>,
}));
const endpointCalls = vi.hoisted(() => [] as string[]);

vi.mock("../api/queries", () => ({
  useComponentEndpointUrl: (_projectName: string, componentName: string) => {
    endpointCalls.push(componentName);
    return { data: endpointUrls.current[componentName] };
  },
}));

// The contract dialog is a lazy read behind a Suspense-y query of its own; the
// list's job here is only to offer the row that opens it.
vi.mock("./ComponentOpenApiDialog", () => ({
  ComponentOpenApiDialog: () => null,
}));

import { ComponentsList } from "./ComponentsList";

const webApp: Component = {
  name: "storefront",
  displayName: "storefront",
  type: "web-application",
  description: "The shop",
};

const service: Component = {
  name: "catalog-api",
  displayName: "catalog-api",
  type: "service",
  description: "Products",
};

beforeEach(() => {
  endpointUrls.current = {};
  endpointCalls.length = 0;
});

describe("a web app's row", () => {
  it("opens the running app in a new tab once it has a URL", () => {
    endpointUrls.current = { storefront: "https://storefront.dev.example.io" };
    render(<ComponentsList projectName={PROJECT} items={[webApp]} />);

    const link = screen.getByRole("link", { name: /storefront/ });
    expect(link).toHaveAttribute("href", "https://storefront.dev.example.io");
    expect(link).toHaveAttribute("target", "_blank");
    // No opener handed to a page the platform deployed but does not control.
    expect(link.getAttribute("rel")).toContain("noreferrer");
  });

  // A link offered before the app is up is a link to a 404.
  it("goes nowhere while the app has not deployed", () => {
    render(<ComponentsList projectName={PROJECT} items={[webApp]} />);

    expect(screen.queryByRole("link")).toBeNull();
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.getByText("storefront")).toBeInTheDocument();
  });
});

describe("a service's row", () => {
  it("still opens its contract, and asks for no endpoint URL", () => {
    render(<ComponentsList projectName={PROJECT} items={[service]} />);

    expect(screen.getByRole("button", { name: /catalog-api/ })).toBeInTheDocument();
    expect(screen.queryByRole("link")).toBeNull();
    expect(endpointCalls).toEqual([]);
  });
});
