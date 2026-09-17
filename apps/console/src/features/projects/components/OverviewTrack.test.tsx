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

import type { ElementType } from "react";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "../../../generated/aep-api";

// Router replaced so each leg renders as a plain anchor whose href is the
// resolved route path — no RouterProvider needed (mirrors DeploymentsPage).
vi.mock("@tanstack/react-router", () => ({
  createLink: (Component: ElementType) =>
    function MockLink({
      to,
      params,
      ...rest
    }: {
      to: string;
      params?: Record<string, unknown>;
    } & Record<string, unknown>) {
      let href = to;
      for (const [key, value] of Object.entries(params ?? {})) {
        href = href.replace(`$${key}`, String(value));
      }
      return <Component component="a" href={href} {...rest} />;
    },
}));

// A per-test permission set, read by useHasAnyPermission (permissions.ts),
// which itself reads useSession().permissions — mocking it here, rather than
// the permissions module, keeps one mock doing the whole job. Defaults to
// holding both design-view and build-view so every pre-existing test below
// (written before the track's legs could lock) keeps seeing unlocked legs;
// the "locked legs" tests further down override it per case.
const permissions = vi.hoisted(
  () => ({ current: new Set(["ae:design-view", "ae:build-view"]) }),
);
vi.mock("../../../auth/SessionContext", () => ({
  useSession: () => ({ orgHandle: "default", permissions: permissions.current }),
}));

// The track reads the LOCAL chat log for the one state no server field can
// produce: a turn that ended on a question. The log's fetch is not what this
// file is about, and `engaged` is a per-test input. A spy, not a plain stub,
// so the "gated on ae:design-view" tests can pin exactly what project (if
// any) OverviewTrack asks it to rehydrate.
const useConversationLogSpy = vi.hoisted(() => vi.fn());
vi.mock("../../agent-chat/useConversationLog", () => ({
  useConversationLog: (...args: unknown[]) => useConversationLogSpy(...args),
}));
const engaged = vi.hoisted(() => ({ current: false }));
vi.mock("../../agent-chat/useAgentEngaged", () => ({
  useAgentEngaged: () => engaged.current,
}));

import { OverviewTrack } from "./OverviewTrack";

type ProjectStatus = components["schemas"]["ProjectStatus"];

function status(over: {
  spec?: Partial<ProjectStatus["spec"]>;
  build?: Partial<ProjectStatus["build"]>;
  deploy?: Partial<ProjectStatus["deploy"]>;
}): ProjectStatus {
  return {
    phase: "spec",
    repoStatus: "ready",
    repoUrl: "",
    hasSpec: true,
    hasDesign: false,
    hasTasks: false,
    specStatus: "",
    spec: { exists: true, version: "v1", dirty: false, design: false, agent: "", ...over.spec },
    build: { version: "", status: "idle", ...over.build },
    deploy: {
      version: "",
      status: "none",
      components: { total: 0, ready: 0 },
      validation: "none",
      ...over.deploy,
    },
  };
}

const draw = (s: ProjectStatus) =>
  render(<OverviewTrack projectName="demo-shop" status={s} />);

/** A leg by its stage name, whatever line it happens to be carrying. */
const legFor = (name: string): HTMLElement =>
  screen
    .getAllByRole("link")
    .find((a) => a.getAttribute("aria-label")?.startsWith(`${name}:`))!;

/**
 * Whether the leg carries the lit ring.
 *
 * Read out of the injected Emotion rule rather than via `getComputedStyle`,
 * which does not resolve Emotion classes under jsdom and returns "" for both
 * the lit and the quiet case — an assertion that passes on a broken component.
 */
function isLit(el: HTMLElement): boolean {
  const css = [...document.querySelectorAll("style")]
    .map((s) => s.textContent ?? "")
    .join("");
  return (el.getAttribute("class") ?? "")
    .split(" ")
    .some(
      (c) =>
        c.startsWith("css-") &&
        new RegExp(`\\.${c}\\{[^}]*inset 0 0 0 1px`).test(css),
    );
}

beforeEach(() => {
  engaged.current = false;
  permissions.current = new Set(["ae:design-view", "ae:build-view"]);
  useConversationLogSpy.mockClear();
});

describe("OverviewTrack", () => {
  it("links each leg to the section that runs it", () => {
    draw(status({}));
    expect(legFor("Spec")).toHaveAttribute("href", "/projects/demo-shop/spec");
    expect(legFor("Build")).toHaveAttribute("href", "/projects/demo-shop/builds");
    expect(legFor("Deploy")).toHaveAttribute(
      "href",
      "/projects/demo-shop/deployments",
    );
  });

  // The chat panel's questions pointer navigates to the spec page, which is
  // exactly this leg's destination — so the leg IS the button, and saying so is
  // the whole fix. It must stay a link: a real button nested inside the leg's
  // anchor is a broken target.
  describe("the agent's questions", () => {
    it("names the action on the leg, in the chat's own words", () => {
      engaged.current = true;
      draw(status({}));
      const spec = legFor("Spec");
      expect(spec).toHaveTextContent("The agent has questions for you");
      expect(spec).toHaveTextContent("Answer them");
      expect(spec).toHaveAttribute("href", "/projects/demo-shop/spec");
    });

    it("renders no nested button inside the leg", () => {
      engaged.current = true;
      const { container } = draw(status({}));
      expect(container.querySelectorAll("a button")).toHaveLength(0);
    });

    // A link announced as "Spec: the agent has questions for you" gives a
    // screen reader user no clue that following it is how they answer.
    it("puts the action in the accessible name too", () => {
      engaged.current = true;
      draw(status({}));
      expect(legFor("Spec")).toHaveAccessibleName(
        "Spec: The agent has questions for you. Answer them",
      );
    });

    it("says nothing extra on a settled leg", () => {
      draw(status({}));
      expect(legFor("Spec")).toHaveAccessibleName("Spec: Published");
      expect(legFor("Spec")).not.toHaveTextContent("Answer them");
    });
  });

  // Oxygen is a CSS-variable theme, so a colour computed inside an `sx`
  // callback bakes the DEFAULT (light) palette and never follows the active
  // scheme — `theme.palette.text.primary` returns #40404B while the page paints
  // #efefef. The seam was drawn in dark grey on a near-black ground: in the DOM,
  // invisible on screen. The fix is to read the channel variable, and this
  // guards it, because the symptom is invisible to any DOM assertion.
  it("draws the seam from the theme variable, not a baked palette value", () => {
    draw(status({}));
    const css = [...document.querySelectorAll("style")]
      .map((s) => s.textContent ?? "")
      .join("");
    expect(css).toContain("--oxygen-palette-text-primaryChannel");
    // rgb(64,64,75) is what the light palette bakes in; seeing it means the
    // callback resolved the static palette again.
    expect(css).not.toContain("64, 64, 75");
  });

  // The lit ring is the "this stage is unsettled" signal. `done` and `waiting`
  // stay quiet on purpose: if every leg glows, the glow means nothing.
  describe("which legs are lit", () => {
    it("lights the deploy leg while validation is still running", () => {
      draw(
        status({
          build: { version: "v1", status: "succeeded" },
          deploy: { version: "v1", status: "deployed", validation: "running" },
        }),
      );
      expect(legFor("Deploy")).toHaveTextContent("validating");
      expect(isLit(legFor("Deploy"))).toBe(true);
    });

    it("leaves a settled deploy quiet", () => {
      draw(
        status({
          build: { version: "v1", status: "succeeded" },
          deploy: { version: "v1", status: "deployed", validation: "passed" },
        }),
      );
      expect(isLit(legFor("Deploy"))).toBe(false);
    });
  });

  // Spec gates on its own permission pair; Build and Deploy share ONE
  // (ae:build-view/ae:build) — there is no separate deployment permission in
  // this console, so the two always lock and unlock together.
  describe("locked legs", () => {
    it("locks Spec without ae:design-view/ae:design, leaves Build and Deploy open", () => {
      permissions.current = new Set(["ae:build-view"]);
      draw(status({}));
      // TanStack Router's real Link strips `href` for a disabled link
      // (link.js's getHrefOption) — this file's router mock renders a plain
      // anchor and does not replicate that, so `aria-disabled` (which MUI's
      // ButtonBase itself sets for a non-native-button disabled element) and
      // the accessible name are the two things this test can actually pin.
      expect(legFor("Spec")).toHaveAttribute("aria-disabled", "true");
      expect(legFor("Spec")).toHaveAccessibleName(
        "Spec: You don't have permission to view the spec.",
      );
      expect(legFor("Build")).toHaveAttribute("href", "/projects/demo-shop/builds");
      expect(legFor("Deploy")).toHaveAttribute(
        "href",
        "/projects/demo-shop/deployments",
      );
    });

    it("locks Build and Deploy together without ae:build-view/ae:build, leaves Spec open", () => {
      permissions.current = new Set(["ae:design-view"]);
      draw(status({}));
      expect(legFor("Build")).toHaveAttribute("aria-disabled", "true");
      expect(legFor("Build")).toHaveAccessibleName(
        "Build: You don't have permission to view builds.",
      );
      expect(legFor("Deploy")).toHaveAttribute("aria-disabled", "true");
      expect(legFor("Deploy")).toHaveAccessibleName(
        "Deploy: You don't have permission to view deployments.",
      );
      expect(legFor("Spec")).toHaveAttribute("href", "/projects/demo-shop/spec");
    });

    // Exact-match, not OR'd: SpecView's own page gate is exact-match
    // ae:design-view alone, so holding only ae:design (a real, if unusual,
    // role shape) must still lock this leg — an unlocked leg that lands on
    // SpecViewRestricted is the exact dead end the lock exists to prevent.
    it("does NOT unlock Spec on ae:design alone", () => {
      permissions.current = new Set(["ae:design"]);
      draw(status({}));
      expect(legFor("Spec")).toHaveAttribute("aria-disabled", "true");
    });

    it("unlocks Build and Deploy together on either ae:build-view or ae:build alone", () => {
      permissions.current = new Set(["ae:build"]);
      draw(status({}));
      expect(legFor("Build")).toHaveAttribute("href", "/projects/demo-shop/builds");
      expect(legFor("Deploy")).toHaveAttribute(
        "href",
        "/projects/demo-shop/deployments",
      );
    });
  });

  // The chat-log rehydrate (useConversationLog/useAgentEngaged) is a passive
  // read, gated on ae:design-view alone — unlike the interactive chat panel
  // itself (AppLayout's own hasDesignAccess), which needs the stronger
  // ae:design. This is independent of the Spec leg's own click-lock, which
  // stays OR'd on design-view/design.
  describe("chat-log rehydrate", () => {
    it("rehydrates the real project when the caller holds ae:design-view", () => {
      permissions.current = new Set(["ae:design-view", "ae:build-view"]);
      draw(status({}));
      expect(useConversationLogSpy).toHaveBeenCalledWith("default", "demo-shop");
    });

    it("withholds the rehydrate without ae:design-view, even holding ae:design", () => {
      permissions.current = new Set(["ae:design", "ae:build-view"]);
      draw(status({}));
      expect(useConversationLogSpy).toHaveBeenCalledWith("default", undefined);
    });
  });
});
