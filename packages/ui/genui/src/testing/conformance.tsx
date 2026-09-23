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
import type { ComponentType, ReactElement, ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { componentExamples, exampleSpecs } from "../../examples/index.js";
import { createGenUiView, type GenUiSpec } from "../adapter/index.js";
import { GenUiActionError } from "../catalog/index.js";
import type { GenUiDesignSystem } from "../design-system.js";

export interface GenUiConformanceOptions {
  /** Providers the design system needs around it, e.g. a theme provider. */
  wrapper?: ComponentType<{ children: ReactNode }>;
}

const dependencyApproval = exampleSpecs["Dependency approval"] as GenUiSpec;
const deliveryStatus = exampleSpecs["Delivery status"] as GenUiSpec;
const buildAndDeploy = exampleSpecs["Build and deploy"] as GenUiSpec;
const buildView = exampleSpecs["Build view (console page)"] as GenUiSpec;
const createCustomer = exampleSpecs["Create customer (form)"] as GenUiSpec;
const customers = exampleSpecs["Customers (list + form)"] as GenUiSpec;

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
  // Render the view as the root (no fragment around it) so rerender() updates
  // the same tree instead of remounting it, which would hide state bugs.
  const show = (ui: ReactElement) => render(ui, wrapper ? { wrapper } : {});

  describe(`GenUI conformance: ${designSystem.name}`, () => {
    it.each(Object.entries(exampleSpecs))(
      "renders the %s example with no invalid elements",
      (_, spec) => {
        show(<GenUiView spec={spec as GenUiSpec} />);
        expect(screen.queryByText(/Could not display/)).not.toBeInTheDocument();
      },
    );

    it.each(Object.entries(componentExamples))(
      "renders the %s component sample with no invalid elements",
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

    describe("a form (Create customer)", () => {
      const field = (name: RegExp) => screen.getByRole("textbox", { name });
      const type = (name: RegExp, value: string) =>
        fireEvent.change(field(name), { target: { value } });
      const submit = () =>
        fireEvent.click(screen.getByRole("button", { name: "Create customer" }));
      const fillValid = () => {
        type(/^Name/, "Acme");
        type(/^Contact name/, "Jane Doe");
        type(/^Contact email/, "jane@acme.test");
      };

      it("sends what was typed as the action's params", async () => {
        const createCustomer_ = vi.fn();
        show(<GenUiView spec={createCustomer} handlers={{ createCustomer: createCustomer_ }} />);
        fillValid();
        type(/^Contact phone/, "+1 555 0100");
        submit();
        await waitFor(() =>
          expect(createCustomer_).toHaveBeenCalledWith({
            name: "Acme",
            contactName: "Jane Doe",
            contactEmail: "jane@acme.test",
            contactPhone: "+1 555 0100",
          }),
        );
        expect(await screen.findByText("Customer created")).toBeInTheDocument();
      });

      it("stops invalid input before the request and says why beside each field", async () => {
        const createCustomer_ = vi.fn();
        show(<GenUiView spec={createCustomer} handlers={{ createCustomer: createCustomer_ }} />);
        type(/^Contact email/, "not-an-email");
        submit();
        expect(await screen.findByText("Check the highlighted fields.")).toBeInTheDocument();
        expect(screen.getByText("Enter the customer's name.")).toBeInTheDocument();
        expect(screen.getByText("Enter a contact name.")).toBeInTheDocument();
        expect(screen.getByText(/Enter a valid email address/)).toBeInTheDocument();
        expect(createCustomer_).not.toHaveBeenCalled();
        expect(screen.queryByText("Customer created")).not.toBeInTheDocument();
      });

      it("shows the server's refusal, with its field errors beside their fields", async () => {
        const createCustomer_ = vi.fn().mockRejectedValue(
          new GenUiActionError("A customer with this name already exists.", {
            name: "Pick a different name.",
          }),
        );
        show(<GenUiView spec={createCustomer} handlers={{ createCustomer: createCustomer_ }} />);
        fillValid();
        submit();
        expect(
          await screen.findByText("A customer with this name already exists."),
        ).toBeInTheDocument();
        expect(screen.getByText("Pick a different name.")).toBeInTheDocument();
      });

      it("still shows results after the view switches to it from another spec", async () => {
        const { rerender } = show(<GenUiView spec={dependencyApproval} />);
        rerender(<GenUiView spec={createCustomer} />);
        submit();
        expect(await screen.findByText("Check the highlighted fields.")).toBeInTheDocument();
      });

      it("keeps an unexpected error's details off the screen", async () => {
        const createCustomer_ = vi.fn().mockRejectedValue(new Error("ECONNREFUSED 127.0.0.1:2001"));
        show(<GenUiView spec={createCustomer} handlers={{ createCustomer: createCustomer_ }} />);
        fillValid();
        submit();
        expect(await screen.findByText("Something went wrong. Try again.")).toBeInTheDocument();
        expect(screen.queryByText(/ECONNREFUSED/)).not.toBeInTheDocument();
      });
    });

    describe("host data (Customers)", () => {
      const acme = {
        id: "c-1",
        name: "Acme",
        contactName: "Jane Doe",
        contactEmail: "jane@acme.test",
      };
      const globex = {
        id: "c-2",
        name: "Globex",
        contactName: "Hank Scorpio",
        contactEmail: "hank@globex.test",
      };

      it("renders the host's data where the spec binds it", () => {
        show(<GenUiView spec={customers} state={{ customers: { items: [acme], total: 1 } }} />);
        expect(screen.getByText("Acme")).toBeInTheDocument();
        expect(screen.getByText("1 in total")).toBeInTheDocument();
      });

      it("shows new host data without losing what the user typed", () => {
        const { rerender } = show(
          <GenUiView spec={customers} state={{ customers: { items: [acme], total: 1 } }} />,
        );
        fireEvent.change(screen.getByRole("textbox", { name: /^Name/ }), {
          target: { value: "Initech" },
        });
        rerender(
          <GenUiView spec={customers} state={{ customers: { items: [acme, globex], total: 2 } }} />,
        );
        expect(screen.getByText("Globex")).toBeInTheDocument();
        expect(screen.getByText("2 in total")).toBeInTheDocument();
        expect(screen.getByRole("textbox", { name: /^Name/ })).toHaveValue("Initech");
      });

      it("runs the spec's onSuccess (clearing the form) only when the action succeeds", async () => {
        const createCustomer_ = vi
          .fn()
          .mockRejectedValueOnce(new GenUiActionError("Name taken.", { name: "Pick another." }))
          .mockResolvedValueOnce(undefined);
        show(<GenUiView spec={customers} handlers={{ createCustomer: createCustomer_ }} />);
        const name = () => screen.getByRole("textbox", { name: /^Name/ });
        fireEvent.change(name(), { target: { value: "Acme" } });
        fireEvent.change(screen.getByRole("textbox", { name: /^Contact name/ }), {
          target: { value: "Jane Doe" },
        });
        fireEvent.change(screen.getByRole("textbox", { name: /^Contact email/ }), {
          target: { value: "jane@acme.test" },
        });
        fireEvent.click(screen.getByRole("button", { name: "Create customer" }));
        expect(await screen.findByText("Pick another.")).toBeInTheDocument();
        expect(name()).toHaveValue("Acme");
        fireEvent.click(screen.getByRole("button", { name: "Create customer" }));
        expect(await screen.findByText("Customer created")).toBeInTheDocument();
        await waitFor(() => expect(name()).toHaveValue(""));
      });
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
