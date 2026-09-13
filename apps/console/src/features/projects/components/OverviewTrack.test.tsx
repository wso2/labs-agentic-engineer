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

vi.mock("../../../auth/SessionContext", () => ({
  useSession: () => ({ orgHandle: "default" }),
}));

// The track reads the LOCAL chat log for the one state no server field can
// produce: a turn that ended on a question. The log's fetch is not what this
// file is about, and `engaged` is a per-test input.
vi.mock("../../agent-chat/useConversationLog", () => ({
  useConversationLog: () => undefined,
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
});
