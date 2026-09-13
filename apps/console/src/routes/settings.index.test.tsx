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
import { afterEach, describe, expect, it, vi } from "vitest";

// Navigate normally needs a live router context (it calls useNavigate() in an
// effect) — stubbed to a plain marker so SettingsIndexPage is renderable in
// isolation, the same way builds-routes.test.ts keeps `Route` real but never
// mounts a router. createFileRoute itself is left real: it only builds a
// route descriptor at import time, no router context required for that.
vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>();
  return {
    ...actual,
    Navigate: ({ to }: { to: string }) => <div data-testid="navigate" data-to={to} />,
  };
});

const heldPermissions = vi.hoisted(() => new Set<string>());
vi.mock("../auth/permissions", () => ({
  useHasPermission: (permission: string) => heldPermissions.has(permission),
  useHasAnyPermission: (permissions: string[]) =>
    permissions.some((p) => heldPermissions.has(p)),
}));

import { SettingsIndexPage, resolveSettingsLandingPath } from "./settings.index";

describe("resolveSettingsLandingPath — priority order", () => {
  it("lands on Credentials first when it's reachable at all", () => {
    expect(
      resolveSettingsLandingPath({ credentials: true, skills: true, usage: true }),
    ).toBe("/settings/credentials");
  });

  it("falls through to Skills when Credentials has nothing to show", () => {
    expect(
      resolveSettingsLandingPath({ credentials: false, skills: true, usage: true }),
    ).toBe("/settings/skills");
  });

  it("falls through to Usage when neither Credentials nor Skills has anything", () => {
    expect(
      resolveSettingsLandingPath({ credentials: false, skills: false, usage: true }),
    ).toBe("/settings/usage");
  });

  it("returns null when none of the three has anything to show", () => {
    expect(
      resolveSettingsLandingPath({ credentials: false, skills: false, usage: false }),
    ).toBeNull();
  });
});

describe("SettingsIndexPage", () => {
  afterEach(() => heldPermissions.clear());

  it("navigates to Credentials when the caller holds ae:github-config", () => {
    heldPermissions.add("ae:github-config");
    render(<SettingsIndexPage />);
    expect(screen.getByTestId("navigate")).toHaveAttribute(
      "data-to",
      "/settings/credentials",
    );
  });

  it("navigates to Skills when only ae:skill-view is held", () => {
    heldPermissions.add("ae:skill-view");
    render(<SettingsIndexPage />);
    expect(screen.getByTestId("navigate")).toHaveAttribute("data-to", "/settings/skills");
  });

  it("navigates to Usage when only ae:usage-view is held", () => {
    heldPermissions.add("ae:usage-view");
    render(<SettingsIndexPage />);
    expect(screen.getByTestId("navigate")).toHaveAttribute("data-to", "/settings/usage");
  });

  it("shows the blocked page with no permissions at all — no Navigate, no settings content", () => {
    render(<SettingsIndexPage />);
    expect(screen.queryByTestId("navigate")).not.toBeInTheDocument();
    expect(
      screen.getByText(
        "You don't have permission to change settings that affect the entire organization.",
      ),
    ).toBeInTheDocument();
  });
});
