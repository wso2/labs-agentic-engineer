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

import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import type { DependencyState } from "../lib/dependencyStates";
import { DependencyView } from "./DependencyView";

const provideMutate = vi.fn();
const acceptMutate = vi.fn();
vi.mock("../api/queries", () => ({
  useProvideDependencyContract: () => ({
    mutate: provideMutate,
    reset: vi.fn(),
    isPending: false,
    isError: false,
    isSuccess: false,
    error: null,
  }),
  useAcceptDependencyAssumption: () => ({
    mutate: acceptMutate,
    isPending: false,
    isError: false,
    error: null,
  }),
}));

const stateOf = (over: Partial<DependencyState["dependency"]>, extra: Partial<DependencyState> = {}): DependencyState => {
  const dependency = { kind: "external", name: "dhl-courier", ...over } as DependencyState["dependency"];
  return { dependency, usedBy: ["parcel-api"], blocking: false, todo: "", flags: [], ...extra };
};

function renderView(definition: Record<string, unknown>, state: DependencyState | undefined) {
  const onResolve = vi.fn();
  const onReconsider = vi.fn();
  const onOpenFile = vi.fn();
  const onCommitted = vi.fn();
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <DependencyView
        projectName="proj1"
        name="dhl-courier"
        definition={JSON.stringify({ name: "dhl-courier", ...definition })}
        state={state}
        onOpenFile={onOpenFile}
        onResolve={onResolve}
        onReconsider={onReconsider}
        onCommitted={onCommitted}
      />
    </OxygenUIThemeProvider>,
  );
  return { onResolve, onReconsider, onOpenFile, onCommitted };
}

describe("DependencyView", () => {
  it("renders the file like a component's design: name once, the provider in its section, and Resolve", () => {
    const { onResolve } = renderView(
      { provider: "DHL", style: "rest-api", description: "Shipment tracking." },
      stateOf({ status: "unresolved", reason: "needs-contract" }, { blocking: true, todo: "Needs a contract" }),
    );
    expect(screen.getByRole("heading", { name: "dhl-courier" })).toBeInTheDocument();
    expect(screen.getByText("DHL")).toBeInTheDocument();
    expect(screen.getByText("REST API")).toBeInTheDocument();
    expect(screen.getByText("Shipment tracking.")).toBeInTheDocument();
    expect(screen.getByText("Needs a contract")).toBeInTheDocument();
    expect(screen.getByText("No interface on file yet.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Resolve" }));
    expect(onResolve).toHaveBeenCalledWith("dhl-courier");
  });

  it("provides the interface through a modal opened beside the Interface section", async () => {
    const { onCommitted } = renderView(
      { provider: "DHL", style: "rest-api" },
      stateOf({ status: "unresolved", reason: "needs-contract" }, { blocking: true, todo: "Needs a contract" }),
    );
    // The form is not in the document — the button is.
    expect(screen.queryByLabelText("OpenAPI document URL")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Provide interface" }));
    const dialog = screen.getByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("OpenAPI document URL"), { target: { value: "https://x/openapi.yaml" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Fetch" }));
    expect(provideMutate).toHaveBeenCalledWith({ depName: "dhl-courier", url: "https://x/openapi.yaml" }, expect.anything());
    // The document landed: the modal closes and the owner hears of the write.
    provideMutate.mock.calls.at(-1)![1].onSuccess();
    expect(onCommitted).toHaveBeenCalledWith("dhl-courier");
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("offers acceptance for an interface the agent wrote, and nothing else can accept it", () => {
    const { onCommitted } = renderView(
      { provider: "DHL", style: "rest-api", contract: "openapi.yaml" },
      stateOf(
        { status: "unresolved", reason: "needs-acceptance", contract: "openapi.yaml", contractAssumed: true },
        { blocking: true, todo: "Needs your acceptance" },
      ),
    );
    expect(screen.getByText(/the agent wrote this interface from research/i)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /accept the assumption/i }));
    expect(acceptMutate).toHaveBeenCalledWith({ depName: "dhl-courier" }, expect.anything());
    acceptMutate.mock.calls.at(-1)![1].onSuccess();
    expect(onCommitted).toHaveBeenCalledWith("dhl-courier");
  });

  it("links the interface file in place with its provenance, and offers Reconsider and Replace once resolved", () => {
    const { onReconsider, onOpenFile } = renderView(
      {
        provider: "DHL",
        style: "rest-api",
        contract: "openapi.yaml",
        assumed: { by: "admin", at: "2026-09-08T10:00:00Z" },
        provenance: { sourceUrl: "https://developer.dhl.com", sliced: true },
        config: [{ key: "DHL_API_KEY", secret: true, description: "Developer API key" }],
      },
      stateOf({ status: "resolved", contract: "openapi.yaml", flags: ["assumed"] }, { flags: ["Assumed"] }),
    );
    expect(screen.getByText("Resolved")).toBeInTheDocument();
    expect(screen.getAllByText("Assumed").length).toBeGreaterThan(0);
    expect(screen.getByText("https://developer.dhl.com")).toBeInTheDocument();
    expect(screen.getByText("DHL_API_KEY")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "openapi.yaml" }));
    expect(onOpenFile).toHaveBeenCalledWith("specs/design/dependencies/dhl-courier/openapi.yaml");
    expect(screen.getByRole("button", { name: "Replace interface" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Reconsider" }));
    expect(onReconsider).toHaveBeenCalledWith("dhl-courier");
    expect(screen.queryByRole("button", { name: "Resolve" })).not.toBeInTheDocument();
  });

  it("offers Select a provider while none is chosen — the flow's card asks which, with the suggestions", () => {
    const { onResolve } = renderView(
      { suggestions: [{ name: "sendgrid", style: "rest-api", description: "Mail API" }, { name: "postmark" }] },
      stateOf({ status: "unresolved", reason: "needs-input" }, { blocking: true, todo: "Choose a provider" }),
    );
    expect(screen.getByText("Provider")).toBeInTheDocument();
    expect(screen.getByText(/none chosen yet/i)).toBeInTheDocument();
    // Nothing about an interface applies before a provider is chosen; the
    // suggestions are the card's, not the view's; the header does not repeat
    // the section's button.
    expect(screen.queryByText("Interface")).not.toBeInTheDocument();
    expect(screen.queryByText(/sendgrid/)).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Resolve" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Select a provider" }));
    expect(onResolve).toHaveBeenCalledWith("dhl-courier");
  });

  it("renders the file alone when the read model does not know the name yet, Resolve still offered", () => {
    renderView({ provider: "DHL", style: "rest-api" }, undefined);
    expect(screen.getByRole("heading", { name: "dhl-courier" })).toBeInTheDocument();
    expect(screen.getByText(/no component references this dependency yet/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Resolve" })).toBeInTheDocument();
  });

  it("reads a file still carrying the retired candidates as suggestions", () => {
    render(
      <OxygenUIThemeProvider theme={OxygenTheme}>
        <DependencyView
          projectName="proj1"
          name="mail"
          definition={JSON.stringify({ name: "mail", candidates: [{ name: "sendgrid", style: "rest-api", package: "npm:x" }, { name: "postmark", style: "rest-api" }] })}
          state={undefined}
          onOpenFile={() => {}}
          onResolve={() => {}}
          onReconsider={() => {}}
        />
      </OxygenUIThemeProvider>,
    );
    expect(screen.getByRole("button", { name: "Select a provider" })).toBeInTheDocument();
    expect(screen.queryByText(/couldn.t parse/i)).not.toBeInTheDocument();
  });

  it("says what is wrong with a definition it cannot read", () => {
    render(
      <OxygenUIThemeProvider theme={OxygenTheme}>
        <DependencyView
          projectName="proj1"
          name="dhl-courier"
          definition="{not json"
          state={undefined}
          onOpenFile={() => {}}
          onResolve={() => {}}
          onReconsider={() => {}}
        />
      </OxygenUIThemeProvider>,
    );
    expect(screen.getByText(/couldn.t parse this dependency.s definition/i)).toBeInTheDocument();
  });
});

describe("DependencyView — an interface derived from the provider's documentation", () => {
  it("says so, links the file, and asks nothing", () => {
    renderView(
      { provider: "Star", style: "rest-api", contract: "openapi.yaml", provenance: { sourceUrl: "https://star.example/docs" } },
      stateOf({ status: "resolved", contract: "openapi.yaml", contractDerived: true, flags: ["derived"] }, { flags: ["Derived from docs"] }),
    );
    expect(screen.getByText(/derived from the provider.s documentation/i)).toBeInTheDocument();
    expect(screen.getByText("Derived from docs")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "openapi.yaml" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /accept the assumption/i })).not.toBeInTheDocument();
  });
});

describe("DependencyView — when a document is not the next step", () => {
  it("offers no upload before a system is identified, nor for an org-registered one", () => {
    renderView({}, stateOf({ status: "unresolved", reason: "needs-input" }, { blocking: true, todo: "Choose a service" }));
    expect(screen.queryByRole("button", { name: /interface/i })).not.toBeInTheDocument();
  });

  it("offers no Reconsider for a resolved dependency nothing references", () => {
    renderView({ provider: "DHL", style: "rest-api", contract: "openapi.yaml" }, stateOf({ status: "resolved" }, { usedBy: [] }));
    expect(screen.queryByRole("button", { name: "Reconsider" })).not.toBeInTheDocument();
  });
});
