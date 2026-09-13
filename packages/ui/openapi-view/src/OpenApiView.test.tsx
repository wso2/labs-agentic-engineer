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
import { describe, expect, it } from "vitest";
import { OpenApiView } from "./OpenApiView.js";

// One spec carrying all three protection states, in the shape the build gate
// fixes for a component behind sign-in.
const SPEC = JSON.stringify({
  openapi: "3.0.3",
  info: { title: "Expense API", version: "1.0.0" },
  components: {
    securitySchemes: {
      oauth2: { type: "oauth2", flows: { authorizationCode: { scopes: {} } } },
    },
  },
  security: [{ oauth2: [] }],
  tags: [{ name: "claims", description: "Expense claims." }],
  paths: {
    "/health": { get: { tags: ["claims"], summary: "Health", security: [] } },
    "/me": { get: { tags: ["claims"], summary: "Who am I" } },
    "/claims": {
      get: { tags: ["claims"], summary: "List claims", security: [{ oauth2: ["claims:read"] }] },
    },
  },
});

describe("OpenApiView — protection on the operation row", () => {
  it("shows public, signed in, and the scope handle", () => {
    render(<OpenApiView spec={SPEC} />);
    expect(screen.getByText("public")).toBeInTheDocument();
    expect(screen.getByText("signed in")).toBeInTheDocument();
    expect(screen.getByText("claims:read")).toBeInTheDocument();
  });

  it("says nothing about who may reach a row when the roles map is absent", () => {
    render(<OpenApiView spec={SPEC} />);
    expect(screen.queryByText(/Employee/)).not.toBeInTheDocument();
    expect(screen.queryByText("any signed-in user")).not.toBeInTheDocument();
    expect(screen.queryByText(/no token needed/)).not.toBeInTheDocument();
  });

  it("names the fixed copy for the public and signed-in rows once a roles map is given", () => {
    render(<OpenApiView spec={SPEC} roles={{ "claims:read": ["Employee"] }} />);
    expect(screen.getByText("anyone · no token needed")).toBeInTheDocument();
  });

  // "everyone" sits one row from "anyone · no token needed" and beside a
  // padlock, where it reads as the opposite of what it means.
  it("spells the signed-in baseline out rather than calling it everyone", () => {
    render(<OpenApiView spec={SPEC} roles={{ "claims:read": ["Employee"] }} />);
    expect(screen.getByText("any signed-in user")).toBeInTheDocument();
    expect(screen.queryByText("everyone")).not.toBeInTheDocument();
  });

  it("names the granting roles when the roles map covers the handle", () => {
    render(<OpenApiView spec={SPEC} roles={{ "claims:read": ["Employee", "Approver"] }} />);
    expect(screen.getByText("Employee, Approver")).toBeInTheDocument();
  });

  it("appends the qualifier from the object form", () => {
    render(
      <OpenApiView
        spec={SPEC}
        roles={{ "claims:read": { roles: ["Employee", "Approver"], note: "own rows" } }}
      />,
    );
    expect(screen.getByText("Employee, Approver · own rows")).toBeInTheDocument();
  });

  it("names the resource server the scopes are granted on when given one", () => {
    render(
      <OpenApiView
        spec={SPEC}
        resourceServer="https://aep.wso2.com/orgs/acme/projects/expense-tracker"
      />,
    );
    expect(
      screen.getByText(
        "aud https://aep.wso2.com/orgs/acme/projects/expense-tracker",
      ),
    ).toBeInTheDocument();
  });

  it("says nothing about an audience the platform has no record of yet", () => {
    render(<OpenApiView spec={SPEC} />);
    expect(screen.queryByText(/^aud /)).not.toBeInTheDocument();
  });

  it("names no roles beside a handle the map does not cover", () => {
    render(<OpenApiView spec={SPEC} roles={{ "reports:read": ["Approver"] }} />);
    expect(screen.getByText("claims:read")).toBeInTheDocument();
    expect(screen.queryByText(/Approver/)).not.toBeInTheDocument();
  });
});
