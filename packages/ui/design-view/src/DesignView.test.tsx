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

import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { DesignView } from "./DesignView.js";

function designJson(deps: unknown[]): string {
  return JSON.stringify({
    name: "checkout-api",
    type: "service",
    version: "0.1.0",
    dependencies: deps,
  });
}

describe("DesignView — dependency status cards (#252 Task 9)", () => {
  it("renders without dependencyStatus/onResolveDependency — existing callers unaffected", () => {
    render(<DesignView design={designJson([{ kind: "external", name: "stripe" }])} />);
    expect(screen.getByText("stripe")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /resolve in chat/i }),
    ).not.toBeInTheDocument();
    // No status chip either — none of the known labels should appear.
    expect(screen.queryByText("Resolved")).not.toBeInTheDocument();
  });

  it("resolved: shows a Resolved chip, no Resolve in chat button, and a hamburger instead", () => {
    render(
      <DesignView
        design={designJson([{ kind: "component", name: "orders-api" }])}
        dependencyStatus={{ "orders-api": { status: "resolved" } }}
        onResolveDependency={vi.fn()}
      />,
    );
    expect(screen.getByText("Resolved")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /resolve in chat/i }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /actions for orders-api/i }),
    ).toBeInTheDocument();
  });

  it("ambiguous: shows an Ambiguous chip, a concise multiple-candidates note naming the candidates (no detailed rows), and Resolve in chat fires the callback", () => {
    const onResolve = vi.fn();
    render(
      <DesignView
        design={designJson([
          {
            kind: "external",
            name: "payments",
            candidates: [
              {
                name: "stripe",
                style: "sdk",
                package: "npm:stripe",
              },
              {
                name: "adyen",
                style: "rest-api",
              },
            ],
          },
        ])}
        dependencyStatus={{ payments: { status: "ambiguous" } }}
        onResolveDependency={onResolve}
      />,
    );

    expect(screen.getByText("Ambiguous")).toBeInTheDocument();

    // Concise note names the candidates as examples; the old per-candidate
    // detail rows + Docs links are gone (resolution happens via chat).
    const note = screen.getByText(/multiple candidates/i);
    expect(note.textContent).toMatch(/stripe/);
    expect(note.textContent).toMatch(/adyen/);
    expect(screen.queryAllByRole("link", { name: "Docs" })).toHaveLength(0);

    fireEvent.click(screen.getByRole("button", { name: /resolve in chat/i }));
    expect(onResolve).toHaveBeenCalledTimes(1);
    expect(onResolve).toHaveBeenCalledWith("payments", "resolve");
  });

  it("unresolved + reason: shows an Unresolved chip, the mapped reason, and the button", () => {
    render(
      <DesignView
        design={designJson([{ kind: "external", name: "sms-gateway", style: "sdk" }])}
        dependencyStatus={{
          "sms-gateway": { status: "unresolved", reason: "needs-input" },
        }}
        onResolveDependency={vi.fn()}
      />,
    );
    expect(screen.getByText("Unresolved")).toBeInTheDocument();
    expect(screen.getByText("needs input")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /resolve in chat/i }),
    ).toBeInTheDocument();
  });

  it("blocked (org-service access-required): shows a Blocked chip, the reason, and the button", () => {
    render(
      <DesignView
        design={designJson([{ kind: "org-service", name: "identity-api" }])}
        dependencyStatus={{
          "identity-api": { status: "blocked", reason: "access-required" },
        }}
        onResolveDependency={vi.fn()}
      />,
    );
    expect(screen.getByText("Blocked")).toBeInTheDocument();
    expect(screen.getByText("access required")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /resolve in chat/i }),
    ).toBeInTheDocument();
  });

  it("no status entry for a dependency (map wired but not yet loaded for this one): no chip, no button", () => {
    render(
      <DesignView
        design={designJson([{ kind: "external", name: "unknown-dep" }])}
        dependencyStatus={{}}
        onResolveDependency={vi.fn()}
      />,
    );
    expect(screen.queryByText("Resolved")).not.toBeInTheDocument();
    expect(screen.queryByText("Ambiguous")).not.toBeInTheDocument();
    expect(screen.queryByText("Unresolved")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /resolve in chat/i }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /actions/i }),
    ).not.toBeInTheDocument();
  });

  it("renders config keys, marking exactly the secret one with the secret chip's icon", () => {
    render(
      <DesignView
        design={designJson([
          {
            kind: "external",
            name: "stripe",
            config: [
              { key: "STRIPE_API_KEY", secret: true },
              { key: "STRIPE_REGION" },
            ],
          },
        ])}
      />,
    );
    expect(screen.getByText("STRIPE_API_KEY")).toBeInTheDocument();
    expect(screen.getByText("STRIPE_REGION")).toBeInTheDocument();
    expect(screen.getAllByTestId("secret-icon")).toHaveLength(1);
  });
});

// #252 Task 17: state-based affordance — a non-resolved dependency keeps its
// "Resolve in chat" button; a resolved one gets a hamburger → "Discuss in
// chat & modify" instead. Never both for the same dependency, uniform across
// every resolved kind.
describe("DesignView — resolved-dependency hamburger (#252 Task 17)", () => {
  it.each(["component", "org-service", "external", "platform-resource"])(
    "resolved %s dependency: hamburger, not the chat button",
    (kind) => {
      render(
        <DesignView
          design={designJson([{ kind, name: "thing" }])}
          dependencyStatus={{ thing: { status: "resolved" } }}
          onResolveDependency={vi.fn()}
        />,
      );
      expect(
        screen.getByRole("button", { name: /actions for thing/i }),
      ).toBeInTheDocument();
      expect(
        screen.queryByRole("button", { name: /resolve in chat/i }),
      ).not.toBeInTheDocument();
    },
  );

  it.each([
    ["ambiguous", undefined],
    ["unresolved", "needs-input"],
    ["blocked", "access-required"],
  ] as const)(
    "%s dependency: the chat button, not the hamburger",
    (status, reason) => {
      render(
        <DesignView
          design={designJson([{ kind: "external", name: "thing" }])}
          dependencyStatus={{ thing: { status, reason } }}
          onResolveDependency={vi.fn()}
        />,
      );
      expect(
        screen.getByRole("button", { name: /resolve in chat/i }),
      ).toBeInTheDocument();
      expect(
        screen.queryByRole("button", { name: /actions/i }),
      ).not.toBeInTheDocument();
    },
  );

  it('hamburger → "Discuss in chat & modify" fires the RECONSIDER intent', () => {
    const onResolveDependency = vi.fn();
    render(
      <DesignView
        design={designJson([{ kind: "external", name: "stripe" }])}
        dependencyStatus={{ stripe: { status: "resolved" } }}
        onResolveDependency={onResolveDependency}
      />,
    );

    fireEvent.click(
      screen.getByRole("button", { name: /actions for stripe/i }),
    );
    fireEvent.click(
      screen.getByRole("menuitem", { name: /discuss in chat & modify/i }),
    );

    expect(onResolveDependency).toHaveBeenCalledTimes(1);
    expect(onResolveDependency).toHaveBeenCalledWith("stripe", "reconsider");
  });

  it('chat button fires the RESOLVE intent', () => {
    const onResolveDependency = vi.fn();
    render(
      <DesignView
        design={designJson([{ kind: "external", name: "stripe" }])}
        dependencyStatus={{ stripe: { status: "unresolved" } }}
        onResolveDependency={onResolveDependency}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: /resolve in chat/i }));

    expect(onResolveDependency).toHaveBeenCalledTimes(1);
    expect(onResolveDependency).toHaveBeenCalledWith("stripe", "resolve");
  });

  // The console passes the reason its spec view computes for every other
  // turn-firing affordance; this package only renders it.
  describe("while a turn holds the room (busyReason)", () => {
    const BUSY = "An agent is still working — this is available once it finishes";

    it("disables the chat button with the reason as its tooltip", () => {
      const onResolveDependency = vi.fn();
      render(
        <DesignView
          design={designJson([{ kind: "external", name: "stripe" }])}
          dependencyStatus={{ stripe: { status: "unresolved" } }}
          onResolveDependency={onResolveDependency}
          busyReason={BUSY}
        />,
      );
      const button = screen.getByRole("button", { name: /resolve in chat/i });
      expect(button).toBeDisabled();
      fireEvent.click(button);
      expect(onResolveDependency).not.toHaveBeenCalled();
      // MUI puts a string title on the wrapped span as its accessible label.
      expect(screen.getByLabelText(BUSY)).toBeInTheDocument();
    });

    it("disables the resolved card's hamburger with the reason as its tooltip", () => {
      render(
        <DesignView
          design={designJson([{ kind: "external", name: "stripe" }])}
          dependencyStatus={{ stripe: { status: "resolved" } }}
          onResolveDependency={vi.fn()}
          busyReason={BUSY}
        />,
      );
      const button = screen.getByRole("button", { name: /actions for stripe/i });
      expect(button).toBeDisabled();
      fireEvent.click(button);
      expect(screen.queryByRole("menuitem")).not.toBeInTheDocument();
      expect(screen.getByLabelText(BUSY)).toBeInTheDocument();
    });

    it("closes a menu already open when the turn starts, and its item no longer fires", async () => {
      const onResolveDependency = vi.fn();
      const view = (busyReason: string) => (
        <DesignView
          design={designJson([{ kind: "external", name: "stripe" }])}
          dependencyStatus={{ stripe: { status: "resolved" } }}
          onResolveDependency={onResolveDependency}
          busyReason={busyReason}
        />
      );
      const { rerender } = render(view(""));
      fireEvent.click(screen.getByRole("button", { name: /actions for stripe/i }));
      const item = screen.getByRole("menuitem", { name: /discuss in chat & modify/i });

      rerender(view(BUSY));

      // The item may linger through the close transition; clicking it does nothing.
      fireEvent.click(item);
      expect(onResolveDependency).not.toHaveBeenCalled();
      await waitFor(() => expect(screen.queryByRole("menuitem")).not.toBeInTheDocument());
      expect(screen.getByRole("button", { name: /actions for stripe/i })).toBeDisabled();

      // The gate lifts: the menu stays closed until the next click.
      rerender(view(""));
      expect(screen.queryByRole("menuitem")).not.toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: /actions for stripe/i }));
      fireEvent.click(screen.getByRole("menuitem", { name: /discuss in chat & modify/i }));
      expect(onResolveDependency).toHaveBeenCalledWith("stripe", "reconsider");
    });

    it("an empty reason leaves both live, and callers that pass none are unaffected", () => {
      const onResolveDependency = vi.fn();
      render(
        <DesignView
          design={designJson([{ kind: "external", name: "stripe" }])}
          dependencyStatus={{ stripe: { status: "unresolved" } }}
          onResolveDependency={onResolveDependency}
          busyReason=""
        />,
      );
      fireEvent.click(screen.getByRole("button", { name: /resolve in chat/i }));
      expect(onResolveDependency).toHaveBeenCalledWith("stripe", "resolve");
      expect(screen.queryByLabelText(BUSY)).not.toBeInTheDocument();
    });
  });

  it("without onResolveDependency: a resolved dependency renders no hamburger", () => {
    render(
      <DesignView
        design={designJson([{ kind: "external", name: "stripe" }])}
        dependencyStatus={{ stripe: { status: "resolved" } }}
      />,
    );
    expect(
      screen.queryByRole("button", { name: /actions/i }),
    ).not.toBeInTheDocument();
  });
});

// #252 Task 15: cross-component "Used by" — the console computes this map
// across every component's dependencies (this package has no notion of
// "other components" of its own) and passes only the slice for the
// currently-rendered design.
describe("DesignView — cross-component 'Used by' (#252 Task 15)", () => {
  it('renders a "Used by" line listing every consuming component when 2+ are present', () => {
    render(
      <DesignView
        design={designJson([
          { kind: "platform-resource", name: "thunder-app", resourceType: "auth" },
        ])}
        dependencyUsedBy={{ "thunder-app": ["auth-service", "web-frontend"] }}
      />,
    );
    expect(screen.getByText(/used by/i)).toBeInTheDocument();
    expect(screen.getByText("auth-service")).toBeInTheDocument();
    expect(screen.getByText("web-frontend")).toBeInTheDocument();
  });

  it('renders no "Used by" line when the dependency has no entry (component-local, the common case)', () => {
    render(
      <DesignView
        design={designJson([{ kind: "external", name: "stripe" }])}
        dependencyUsedBy={{}}
      />,
    );
    expect(screen.queryByText(/used by/i)).not.toBeInTheDocument();
  });

  it('renders no "Used by" line for a single-entry (self-only) usedBy list', () => {
    render(
      <DesignView
        design={designJson([{ kind: "external", name: "stripe" }])}
        dependencyUsedBy={{ stripe: ["checkout-api"] }}
      />,
    );
    expect(screen.queryByText(/used by/i)).not.toBeInTheDocument();
  });

  it("renders without dependencyUsedBy at all — existing callers unaffected", () => {
    render(
      <DesignView design={designJson([{ kind: "external", name: "stripe" }])} />,
    );
    expect(screen.queryByText(/used by/i)).not.toBeInTheDocument();
  });
});
