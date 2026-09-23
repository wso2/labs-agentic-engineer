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

import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ComponentType, ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { exampleSpecs } from "../../examples/index.js";
import { createGenUiView, type GenUiSpec } from "../adapter/index.js";
import type { GenUiDesignSystem } from "../design-system.js";

export interface GenUiConformanceOptions {
  /** Providers the design system needs around it, e.g. a theme provider. */
  wrapper?: ComponentType<{ children: ReactNode }>;
}

const dependencyApproval = exampleSpecs["Dependency approval"] as GenUiSpec;
const deliveryStatus = exampleSpecs["Delivery status"] as GenUiSpec;
const buildAndDeploy = exampleSpecs["Build and deploy"] as GenUiSpec;
const buildView = exampleSpecs["Build view (console page)"] as GenUiSpec;

/**
 * The behaviour every design system must show, whatever it looks like. A
 * design-system package calls this from one test file, so adding a design
 * system is held to the same bar as the ones already here. Assertions go by
 * text and role only: nothing here depends on how a design system styles
 * things.
 */
export function describeGenUiConformance(
  designSystem: GenUiDesignSystem,
  { wrapper }: GenUiConformanceOptions = {},
): void {
  const GenUiView = createGenUiView(designSystem);
  const show = (ui: ReactNode) => render(<>{ui}</>, wrapper ? { wrapper } : {});

  describe(`GenUI conformance: ${designSystem.name}`, () => {
    it.each(Object.entries(exampleSpecs))(
      "renders the %s example with no invalid elements",
      (_, spec) => {
        show(<GenUiView spec={spec as GenUiSpec} />);
        expect(screen.queryByText(/Could not display/)).not.toBeInTheDocument();
      },
    );

    it("renders a spec's text", () => {
      show(<GenUiView spec={dependencyApproval} />);
      expect(screen.getByText("Approve dependency: payments-db")).toBeInTheDocument();
      expect(screen.getByText("checkout-service")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "Approve" })).toBeInTheDocument();
    });

    it("routes a button press to the host handler with validated params", async () => {
      const approveDependency = vi.fn();
      const onActionOutcome = vi.fn();
      show(
        <GenUiView
          spec={dependencyApproval}
          handlers={{ approveDependency }}
          onActionOutcome={onActionOutcome}
        />,
      );
      fireEvent.click(screen.getByRole("button", { name: "Approve" }));
      await waitFor(() =>
        expect(approveDependency).toHaveBeenCalledWith({ dependencyId: "payments-db" }),
      );
      expect(onActionOutcome).toHaveBeenCalledWith({
        status: "handled",
        action: "approveDependency",
      });
    });

    it("reports a press whose action has no host handler", async () => {
      const onActionOutcome = vi.fn();
      show(<GenUiView spec={dependencyApproval} onActionOutcome={onActionOutcome} />);
      fireEvent.click(screen.getByRole("button", { name: "Reject" }));
      await waitFor(() =>
        expect(onActionOutcome).toHaveBeenCalledWith({
          status: "unhandled",
          action: "rejectDependency",
        }),
      );
    });

    it("resolves state bindings, with host state layered over the spec's", () => {
      const { unmount } = show(<GenUiView spec={deliveryStatus} />);
      expect(screen.getByText("60% of tasks merged")).toBeInTheDocument();
      unmount();
      show(<GenUiView spec={deliveryStatus} state={{ delivery: { percent: 80 } }} />);
      expect(screen.getByText("80% of tasks merged")).toBeInTheDocument();
    });

    it("shows platform states in the shared wording", () => {
      show(<GenUiView spec={buildAndDeploy} />);
      expect(screen.getByText("Tasks merged")).toBeInTheDocument();
      expect(screen.getByText("#44")).toBeInTheDocument();
      expect(screen.getByText("Blocked")).toBeInTheDocument();
      expect(screen.getByText("Not deployed")).toBeInTheDocument();
      expect(screen.getByText("Partial")).toBeInTheDocument();
    });

    it("opens deployment links in a new tab without handing over the opener", () => {
      show(<GenUiView spec={buildAndDeploy} />);
      const link = screen.getByRole("link", { name: "https://checkout-dev.example.com" });
      expect(link).toHaveAttribute("target", "_blank");
      expect(link).toHaveAttribute("rel", "noopener noreferrer");
    });

    it("renders a whole console page: sections, task details, the agent timeline", () => {
      show(<GenUiView spec={buildView} />);
      expect(screen.getByText("Build v2")).toBeInTheDocument();
      // Each section header is the toggle that collapses it.
      for (const title of ["Tasks", "External resources", "Coding agent log", "Build logs"]) {
        expect(
          screen.getByRole("button", { name: new RegExp(`^${title}`), expanded: true }),
        ).toBeInTheDocument();
      }
      expect(screen.getByText(/19 items · 18 done/)).toBeInTheDocument();
      expect(screen.getAllByText("Sep 10, 03:14 PM")).toHaveLength(2);
      expect(screen.getByText("lead agent")).toBeInTheDocument();
      expect(screen.getByText("34m26s across 5 agents")).toBeInTheDocument();
    });

    it("binds a per-item button to its own params", async () => {
      const editExternalResource = vi.fn();
      show(<GenUiView spec={buildView} handlers={{ editExternalResource }} />);
      fireEvent.click(screen.getByRole("button", { name: "Edit configuration" }));
      await waitFor(() =>
        expect(editExternalResource).toHaveBeenCalledWith({ resourceName: "sendgrid" }),
      );
    });

    it("shows a warning instead of crashing on props that fail the schema", () => {
      const spec: GenUiSpec = {
        root: "a",
        elements: { a: { type: "KeyValueList", props: { items: "oops" }, children: [] } },
      };
      show(<GenUiView spec={spec} />);
      expect(screen.getByText(/Could not display a KeyValueList/)).toBeInTheDocument();
    });

    it("renders nothing for incomplete props while the spec is still streaming", () => {
      const spec: GenUiSpec = {
        root: "a",
        elements: { a: { type: "Card", props: {}, children: [] } },
      };
      show(<GenUiView spec={spec} loading />);
      expect(screen.queryByText(/Could not display/)).not.toBeInTheDocument();
    });

    it("shows a warning for a component type outside the catalog", () => {
      const spec: GenUiSpec = {
        root: "a",
        elements: { a: { type: "Iframe", props: { src: "https://x" }, children: [] } },
      };
      show(<GenUiView spec={spec} />);
      expect(screen.getByText(/Could not display a "Iframe"/)).toBeInTheDocument();
    });
  });
}
