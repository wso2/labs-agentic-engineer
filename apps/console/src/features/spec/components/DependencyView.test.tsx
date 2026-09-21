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

/** The file's `resource` block, which is all the definition says about the thing. */
function withResource(resource: Record<string, unknown>, rest: Record<string, unknown> = {}) {
  return { resource: { name: "dhl-courier", ...resource }, ...rest };
}

function renderView(
  definition: Record<string, unknown>,
  state: DependencyState | undefined,
  busyReason = "",
) {
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
        busyReason={busyReason}
      />
    </OxygenUIThemeProvider>,
  );
  return { onResolve, onReconsider, onOpenFile, onCommitted };
}

describe("DependencyView", () => {
  it("renders the file like a component's design: name once, the provider in its section, and Resolve", () => {
    const { onResolve } = renderView(
      withResource({ provider: "DHL", description: "Shipment tracking." }),
      stateOf({ status: "unresolved", reason: "needs-contract" }, { blocking: true, todo: "Needs a contract" }),
    );
    expect(screen.getByRole("heading", { name: "dhl-courier" })).toBeInTheDocument();
    expect(screen.getByText("DHL")).toBeInTheDocument();
    expect(screen.getByText("Shipment tracking.")).toBeInTheDocument();
    expect(screen.getByText("Needs a contract")).toBeInTheDocument();
    expect(screen.getByText("No interface on file yet.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Resolve" }));
    expect(onResolve).toHaveBeenCalledWith("dhl-courier");
  });

  // Style is not on the file any more: how a component talks to the system is
  // the contract's type, said in the user's words.
  it("says how the dependency is consumed, computed from the contract's type", () => {
    renderView(
      withResource({ provider: "DHL", contract: { type: "openapi", path: "openapi.yaml" } }),
      stateOf({ status: "resolved" }),
    );
    expect(screen.getByText("Consumed as")).toBeInTheDocument();
    expect(screen.getByText("REST API")).toBeInTheDocument();
    expect(screen.queryByText("Style")).not.toBeInTheDocument();
  });

  it("provides the interface through a modal opened beside the Interface section", async () => {
    const { onCommitted } = renderView(
      withResource({ provider: "DHL" }),
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
      withResource({
        provider: "DHL",
        contract: { type: "openapi", path: "openapi.yaml", origin: "assumed" },
      }),
      stateOf(
        { status: "unresolved", reason: "needs-acceptance", contract: "openapi.yaml", contractAssumed: true },
        { blocking: true, todo: "Needs your acceptance" },
      ),
    );
    expect(screen.getByText(/the agent wrote this interface from research/i)).toBeInTheDocument();
    expect(screen.getByText("written by the agent, not yet accepted")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /accept the assumption/i }));
    expect(acceptMutate).toHaveBeenCalledWith({ depName: "dhl-courier" }, expect.anything());
    acceptMutate.mock.calls.at(-1)![1].onSuccess();
    expect(onCommitted).toHaveBeenCalledWith("dhl-courier");
  });

  it("links the interface file in place with its origin and provenance, and offers Reconsider and Replace once resolved", () => {
    const { onReconsider, onOpenFile } = renderView(
      withResource(
        {
          provider: "DHL",
          contract: {
            type: "openapi",
            path: "openapi.yaml",
            origin: "assumed",
            accepted: { by: "admin", at: "2026-09-08T10:00:00Z" },
          },
          config: [{ key: "DHL_API_KEY", secret: true, description: "Developer API key" }],
        },
        { provenance: { sourceUrl: "https://developer.dhl.com", readOn: "2026-09-08" } },
      ),
      stateOf({ status: "resolved", contract: "openapi.yaml", flags: ["assumed"] }, { flags: ["Assumed"] }),
    );
    expect(screen.getByText("Resolved")).toBeInTheDocument();
    expect(screen.getAllByText("Assumed").length).toBeGreaterThan(0);
    expect(screen.getByText("written by the agent, accepted by admin on 2026-09-08")).toBeInTheDocument();
    expect(screen.getByText("https://developer.dhl.com")).toBeInTheDocument();
    expect(screen.getByText("2026-09-08")).toBeInTheDocument();
    // A contract is a whole document now — nothing is sliced out of it.
    expect(screen.queryByText("Kept")).not.toBeInTheDocument();
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
      { resource: { name: "dhl-courier" }, suggestions: [{ name: "sendgrid", style: "rest-api", description: "Mail API" }, { name: "postmark" }] },
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
    renderView(withResource({ provider: "DHL" }), undefined);
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
          busyReason=""
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
          busyReason=""
        />
      </OxygenUIThemeProvider>,
    );
    expect(screen.getByText(/couldn.t parse this dependency.s definition/i)).toBeInTheDocument();
  });
});

// Old repos still hold the flat file — provider, style and contract at the top
// level. The platform lifts it on read; the view renders the FILE, so it lifts
// the same way rather than reporting a parse error.
describe("DependencyView — a definition written before the resource block", () => {
  it("reads the flat shape: provider, style as the contract's type, and the acceptance record", () => {
    renderView(
      {
        provider: "DHL",
        style: "rest-api",
        contract: "openapi.yaml",
        description: "Shipment tracking.",
        assumed: { by: "admin", at: "2026-09-08T10:00:00Z" },
        provenance: { sourceUrl: "https://developer.dhl.com", sliced: true, fetchedAt: "2026-09-08" },
        config: [{ key: "DHL_API_KEY", secret: true }],
      },
      stateOf({ status: "resolved", contract: "openapi.yaml" }),
    );
    expect(screen.getByText("DHL")).toBeInTheDocument();
    expect(screen.getByText("REST API")).toBeInTheDocument();
    expect(screen.getByText("Shipment tracking.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "openapi.yaml" })).toBeInTheDocument();
    expect(screen.getByText("written by the agent, accepted by admin on 2026-09-08")).toBeInTheDocument();
    // `fetchedAt` is `readOn` now, and `sliced` is gone with sliced contracts.
    expect(screen.getByText("2026-09-08")).toBeInTheDocument();
    expect(screen.queryByText("Kept")).not.toBeInTheDocument();
    expect(screen.getByText("DHL_API_KEY")).toBeInTheDocument();
  });

  it("reads a flat org-registered dependency as a copy of the registered resource", () => {
    renderView(
      { source: "org", provider: "DHL", style: "rest-api", contract: "openapi.yaml" },
      stateOf({ status: "resolved", resourceRef: "dhl-courier", source: "org" }),
    );
    expect(screen.getByText("Organization registry")).toBeInTheDocument();
    expect(screen.getAllByText("from the organization").length).toBeGreaterThan(0);
  });
});

describe("DependencyView — a copy of a Registered External resource", () => {
  const copy = (resource: Record<string, unknown> = {}) =>
    withResource({
      ref: "dhl-courier",
      provider: "DHL",
      consumptionInstructions: "Use the shared courier account; never open your own.",
      config: [{ key: "DHL_API_KEY", secret: true }],
      contract: { type: "openapi", path: "openapi.yaml", origin: "registry" },
      ...resource,
    });

  it("names the provider and the registry, marks what was copied, and still allows a replacement", () => {
    renderView(copy(), stateOf({ status: "resolved", resourceRef: "dhl-courier", source: "org", contract: "openapi.yaml" }));
    expect(screen.getByText("Organization registry")).toBeInTheDocument();
    // The provider is the SYSTEM, never "Registered by the organization".
    expect(screen.getByText("DHL")).toBeInTheDocument();
    expect(screen.queryByText(/registered by the organization/i)).not.toBeInTheDocument();
    expect(screen.getByText("How the organization uses it")).toBeInTheDocument();
    expect(screen.getByText(/never open your own/)).toBeInTheDocument();
    expect(screen.getByText("copied from the organization's registry")).toBeInTheDocument();
    // Keys, instructions and the document are all marked as the organization's.
    expect(screen.getAllByText("from the organization").length).toBe(3);
    // The organization's document is a starting point, not a cage.
    expect(screen.getByRole("button", { name: "Replace interface" })).toBeInTheDocument();
  });

  // After Replace interface the copy keeps its `ref`, but the document on disk
  // is the project's own. Marking it "from the organization" would contradict
  // the origin shown right under it.
  it("stops calling the interface the organization's once it has been replaced", () => {
    renderView(
      copy({ contract: { type: "openapi", path: "openapi.yaml", origin: "provider" } }),
      stateOf({
        status: "resolved",
        resourceRef: "dhl-courier",
        source: "org",
        contract: "openapi.yaml",
        contractOrigin: "provider",
      }),
    );
    // The keys and the instructions are still the organization's; the document is not.
    expect(screen.getAllByText("from the organization").length).toBe(2);
    expect(
      screen.queryByText("copied from the organization's registry"),
    ).not.toBeInTheDocument();
  });

  it("says when the organization has no resource of that name, and offers the way out", () => {
    const { onResolve } = renderView(
      copy(),
      stateOf(
        { status: "unresolved", reason: "needs-input", resourceRef: "dhl-courier", source: "org" },
        { blocking: true, todo: "Choose a provider" },
      ),
    );
    expect(
      screen.getByText("The organization has no registered resource with this name"),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Select a provider" }));
    expect(onResolve).toHaveBeenCalledWith("dhl-courier");
  });

  it("says when the organization's document has moved on since the copy", () => {
    renderView(
      copy(),
      stateOf({ status: "resolved", resourceRef: "dhl-courier", source: "org", flags: ["stale"] }, { flags: ["Stale"] }),
    );
    expect(
      screen.getByText("The organization's document changed since this copy was made"),
    ).toBeInTheDocument();
  });
});

describe("DependencyView — an interface derived from the provider's documentation", () => {
  it("says where it came from, links the file, and asks nothing", () => {
    renderView(
      withResource(
        { provider: "Star", contract: { type: "openapi", path: "openapi.yaml", origin: "derived" } },
        { provenance: { sourceUrl: "https://star.example/docs" } },
      ),
      stateOf({ status: "resolved", contract: "openapi.yaml", contractDerived: true, flags: ["derived"] }, { flags: ["Derived from docs"] }),
    );
    expect(screen.getByText("derived from the provider's documentation")).toBeInTheDocument();
    expect(screen.getByText("Derived from docs")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "openapi.yaml" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /accept the assumption/i })).not.toBeInTheDocument();
  });
});

describe("DependencyView — when a document is not the next step", () => {
  it("offers no upload before a system is identified", () => {
    renderView({ resource: { name: "dhl-courier" } }, stateOf({ status: "unresolved", reason: "needs-input" }, { blocking: true, todo: "Choose a service" }));
    expect(screen.queryByRole("button", { name: /interface/i })).not.toBeInTheDocument();
  });

  it("offers no upload for a GraphQL schema, which the platform does not fetch", () => {
    renderView(
      withResource({ provider: "Star", contract: { type: "graphql", path: "schema.graphql" } }),
      stateOf({ status: "resolved", contract: "schema.graphql" }),
    );
    expect(screen.getByText("GraphQL")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /interface/i })).not.toBeInTheDocument();
  });

  it("offers no Reconsider for a resolved dependency nothing references", () => {
    renderView(
      withResource({ provider: "DHL", contract: { type: "openapi", path: "openapi.yaml" } }),
      stateOf({ status: "resolved" }, { usedBy: [] }),
    );
    expect(screen.queryByRole("button", { name: "Reconsider" })).not.toBeInTheDocument();
  });
});

// The same gate as the PRD's lenses: while a turn holds the room nothing here
// may fire another or write the dependency's directory, and the reason is on
// the button. Reading is never gated.
describe("DependencyView — while a turn holds the room", () => {
  const BUSY = "An agent is still working — this is available once it finishes";

  it("disables Reconsider and Replace interface, says why, and keeps the file link live", () => {
    const { onReconsider, onOpenFile } = renderView(
      withResource({ provider: "DHL", contract: { type: "openapi", path: "openapi.yaml", origin: "provider" } }),
      stateOf({ status: "resolved", contract: "openapi.yaml" }),
      BUSY,
    );
    const reconsider = screen.getByRole("button", { name: "Reconsider" });
    expect(reconsider).toBeDisabled();
    fireEvent.click(reconsider);
    expect(onReconsider).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Replace interface" })).toBeDisabled();
    // MUI puts a string title on the wrapped span as its accessible label.
    expect(screen.getAllByLabelText(BUSY)).toHaveLength(2);
    fireEvent.click(screen.getByRole("button", { name: "openapi.yaml" }));
    expect(onOpenFile).toHaveBeenCalledWith("specs/design/dependencies/dhl-courier/openapi.yaml");
  });

  it("disables Resolve, Replace interface and Accept the assumption on an unresolved dependency", () => {
    const { onResolve } = renderView(
      withResource({ provider: "DHL", contract: { type: "openapi", path: "openapi.yaml", origin: "assumed" } }),
      stateOf({ status: "unresolved", reason: "needs-acceptance", contract: "openapi.yaml" }),
      BUSY,
    );
    for (const name of ["Resolve", "Replace interface", "Accept the assumption"]) {
      expect(screen.getByRole("button", { name }), name).toBeDisabled();
    }
    fireEvent.click(screen.getByRole("button", { name: "Resolve" }));
    expect(onResolve).not.toHaveBeenCalled();
    expect(screen.getAllByLabelText(BUSY)).toHaveLength(3);
    // Read it first is a link into the file, so it stays live.
    expect(screen.getByRole("button", { name: "Read it first" })).toBeEnabled();
  });

  it("disables Select a provider while none is chosen", () => {
    const { onResolve } = renderView(
      { resource: { name: "dhl-courier" }, suggestions: [{ name: "DHL" }] },
      stateOf({ status: "unresolved", reason: "needs-input" }),
      BUSY,
    );
    const select = screen.getByRole("button", { name: "Select a provider" });
    expect(select).toBeDisabled();
    fireEvent.click(select);
    expect(onResolve).not.toHaveBeenCalled();
    expect(screen.getByLabelText(BUSY)).toBeInTheDocument();
  });

  it("with an empty reason the buttons are live and carry no tooltip", () => {
    const { onResolve } = renderView(
      withResource({ provider: "DHL" }),
      stateOf({ status: "unresolved", reason: "needs-contract" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Resolve" }));
    expect(onResolve).toHaveBeenCalledWith("dhl-courier");
    expect(screen.queryByLabelText(BUSY)).not.toBeInTheDocument();
  });
});
