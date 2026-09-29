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

import { screen } from "@testing-library/react";
import { renderWithPermissions } from "../auth/testing";
import { permissionsOf } from "../auth/permissions";
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

const heldPermissions = new Set<string>();

import { SettingsIndexPage, resolveSettingsLandingPath } from "./settings.index";

// Real permission keys through the real predicates, so each case names the
// grant a caller would actually hold rather than a pre-resolved boolean per
// section. Which key admits which section is now part of what this covers —
// it used to be stated only inside the component, untested.
const holding = (...permissions: string[]) => permissionsOf(new Set(permissions));

describe("resolveSettingsLandingPath — priority order", () => {
  it("lands on Credentials first when it's reachable at all", () => {
    expect(
      resolveSettingsLandingPath(
        holding("ae:github-config", "ae:skill-view", "ae:usage-view"),
      ),
    ).toBe("/settings/credentials");
  });

  it("falls through to Skills when Credentials has nothing to show", () => {
    expect(
      resolveSettingsLandingPath(holding("ae:skill-view", "ae:usage-view")),
    ).toBe("/settings/skills");
  });

  it("falls through to Usage when neither Credentials nor Skills has anything", () => {
    expect(resolveSettingsLandingPath(holding("ae:usage-view"))).toBe(
      "/settings/usage",
    );
  });

  it("returns null when none of the three has anything to show", () => {
    expect(resolveSettingsLandingPath(holding())).toBeNull();
  });

  // Credentials is the one section admitted by EITHER of two permissions,
  // because the BFF redacts each of its cards separately.
  it("lands on Credentials on the model half alone", () => {
    expect(resolveSettingsLandingPath(holding("ae:model-config"))).toBe(
      "/settings/credentials",
    );
  });

  // Skills is exact-match on ae:skill-view, NOT OR'd with ae:skill-config:
  // holding only the write half does not admit you to the section.
  it("does not land on Skills for ae:skill-config alone", () => {
    expect(resolveSettingsLandingPath(holding("ae:skill-config"))).toBeNull();
  });
});

describe("SettingsIndexPage", () => {
  afterEach(() => heldPermissions.clear());

  it("navigates to Credentials when the caller holds ae:github-config", () => {
    heldPermissions.add("ae:github-config");
    renderWithPermissions(<SettingsIndexPage />, heldPermissions);
    expect(screen.getByTestId("navigate")).toHaveAttribute(
      "data-to",
      "/settings/credentials",
    );
  });

  it("navigates to Skills when only ae:skill-view is held", () => {
    heldPermissions.add("ae:skill-view");
    renderWithPermissions(<SettingsIndexPage />, heldPermissions);
    expect(screen.getByTestId("navigate")).toHaveAttribute("data-to", "/settings/skills");
  });

  // Exact-match ae:skill-view, NOT OR'd with ae:skill-config: a config-only
  // holder (no separate view grant) has nothing reachable and sees the
  // blocked page, same rule as SettingsLayout's tab gating.
  it("does NOT navigate to Skills when only ae:skill-config is held", () => {
    heldPermissions.add("ae:skill-config");
    renderWithPermissions(<SettingsIndexPage />, heldPermissions);
    expect(screen.queryByTestId("navigate")).not.toBeInTheDocument();
    expect(
      screen.getByText(
        "You don't have permission to change settings that affect the entire organization.",
      ),
    ).toBeInTheDocument();
  });

  it("navigates to Usage when only ae:usage-view is held", () => {
    heldPermissions.add("ae:usage-view");
    renderWithPermissions(<SettingsIndexPage />, heldPermissions);
    expect(screen.getByTestId("navigate")).toHaveAttribute("data-to", "/settings/usage");
  });

  it("shows the blocked page with no permissions at all — no Navigate, no settings content", () => {
    renderWithPermissions(<SettingsIndexPage />, heldPermissions);
    expect(screen.queryByTestId("navigate")).not.toBeInTheDocument();
    expect(
      screen.getByText(
        "You don't have permission to change settings that affect the entire organization.",
      ),
    ).toBeInTheDocument();
  });
});
