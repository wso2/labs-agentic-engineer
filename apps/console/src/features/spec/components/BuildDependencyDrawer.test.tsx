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

import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "../../../generated/aep-api";
import {
  BuildDependencyDrawer,
  groupPreflightItems,
} from "./BuildDependencyDrawer";

// Every existing test in this file assumes Continue is otherwise reachable —
// no dedicated "no permission" test exists here since SpecView.test.tsx
// already covers the gate end to end.
const hasBuildPermission = vi.hoisted(() => ({ current: true }));
vi.mock("../../../auth/permissions", () => ({
  useHasPermission: () => hasBuildPermission.current,
}));

beforeEach(() => {
  hasBuildPermission.current = true;
});

type PreflightItem = components["schemas"]["PreflightItem"];
type BuildInputItem = components["schemas"]["BuildInputItem"];

const AMBIGUOUS: PreflightItem = {
  component: "checkout-api",
  dependency: "crm",
  kind: "external-ambiguous",
  description: "More than one candidate fits — resolve which one to use.",
};
const UNRESOLVED: PreflightItem = {
  component: "checkout-api",
  dependency: "weather-api",
  kind: "external-unresolved",
  description: "Needs information only you can provide.",
};
const NEEDS_SPEC: PreflightItem = {
  component: "checkout-api",
  dependency: "partner-openapi-spec",
  kind: "external-spec",
  description: "No API spec yet — provide one to continue.",
};
const ORG_SERVICE: PreflightItem = {
  component: "checkout-api",
  dependency: "billing-service",
  kind: "org-service",
  description: "Billing service endpoint",
};
// The two kinds the drawer must never render any more: their values /
// approvals are settled elsewhere (Builds page + deploy gate), so they only
// ever ride along in the request Continue sends.
const EXTERNAL_CONFIG: PreflightItem = {
  component: "checkout-api",
  dependency: "stripe-config",
  kind: "external-config",
  description: "Stripe API credentials",
  config: [
    { key: "STRIPE_API_KEY", secret: true, description: "Your Stripe key" },
    { key: "STRIPE_WEBHOOK_ID", secret: false, defaultValue: "whsec" },
  ],
};
const PLATFORM_RESOURCE: PreflightItem = {
  component: "checkout-api",
  dependency: "postgres",
  kind: "platform-resource",
  description: "Postgres database",
  resourceType: "postgres-cnpg",
  parameters: { instances: 1 },
};

function setup(items: PreflightItem[], submitting = false) {
  const onClose = vi.fn();
  const onContinue = vi.fn();
  const onResolveDependency = vi.fn();
  render(
    <BuildDependencyDrawer
      open
      items={items}
      submitting={submitting}
      onClose={onClose}
      onContinue={onContinue}
      onResolveDependency={onResolveDependency}
    />,
  );
  return { onClose, onContinue, onResolveDependency };
}

// The drawer's whole job: the dependencies whose identity is unsettled. It is
// only ever opened for those, so it renders only those.
describe("BuildDependencyDrawer — renders resolution blockers only", () => {
  it("renders a panel per resolution blocker present in items", () => {
    setup([AMBIGUOUS, UNRESOLVED, NEEDS_SPEC, ORG_SERVICE]);

    expect(screen.getByText("crm")).toBeInTheDocument();
    expect(screen.getByText("weather-api")).toBeInTheDocument();
    expect(screen.getByText("partner-openapi-spec")).toBeInTheDocument();
    expect(screen.getByText("billing-service")).toBeInTheDocument();
  });

  it("renders each blocker's plain-language reason", () => {
    setup([AMBIGUOUS, UNRESOLVED]);

    expect(
      screen.getByText(/more than one candidate fits/i),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/needs information only you can provide/i),
    ).toBeInTheDocument();
  });

  it("never renders an external-config item — no key fields, no card", () => {
    setup([AMBIGUOUS, EXTERNAL_CONFIG]);

    expect(screen.queryByText("stripe-config")).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/STRIPE_API_KEY/i)).not.toBeInTheDocument();
    expect(
      screen.queryByLabelText(/STRIPE_WEBHOOK_ID/i),
    ).not.toBeInTheDocument();
  });

  it("never renders a platform-resource item — approving it is not this drawer's job", () => {
    setup([AMBIGUOUS, PLATFORM_RESOURCE]);

    expect(screen.queryByText("postgres")).not.toBeInTheDocument();
  });

  it("shows an all-clear message when nothing is left to resolve", () => {
    setup([EXTERNAL_CONFIG, PLATFORM_RESOURCE]);

    expect(screen.getByText(/everything is resolved/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /continue/i })).toBeEnabled();
  });

  it("calls onClose when Cancel is clicked", () => {
    const { onClose } = setup([AMBIGUOUS]);

    fireEvent.click(screen.getByRole("button", { name: /cancel/i }));

    expect(onClose).toHaveBeenCalledTimes(1);
  });
});

// A chat-only blocker has no local form: Continue stays disabled until the
// parent's preflight refetch drops it from `items`.
describe("BuildDependencyDrawer — chat-only blockers gate Continue", () => {
  it("keeps Continue disabled while a blocker item is present", () => {
    setup([AMBIGUOUS]);

    expect(screen.getByRole("button", { name: /continue/i })).toBeDisabled();
  });

  it("never renders a local input for a blocker item (no textbox)", () => {
    setup([AMBIGUOUS, UNRESOLVED]);

    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  });

  it("fires onResolveDependency with the item and the RESOLVE intent from 'Resolve via chat'", () => {
    const { onResolveDependency } = setup([AMBIGUOUS]);

    fireEvent.click(screen.getByRole("button", { name: /resolve via chat/i }));

    expect(onResolveDependency).toHaveBeenCalledTimes(1);
    expect(onResolveDependency).toHaveBeenCalledWith(AMBIGUOUS, "resolve");
  });

  it("re-enables Continue once the blocker is no longer in items (a resolved refetch)", () => {
    const onClose = vi.fn();
    const onContinue = vi.fn();
    const { rerender } = render(
      <BuildDependencyDrawer
        open
        items={[AMBIGUOUS]}
        onClose={onClose}
        onContinue={onContinue}
      />,
    );
    expect(screen.getByRole("button", { name: /continue/i })).toBeDisabled();

    // The parent refetched preflight after the chat resolved it — the item is
    // simply gone from the next `items` array.
    rerender(
      <BuildDependencyDrawer
        open
        items={[]}
        onClose={onClose}
        onContinue={onContinue}
      />,
    );

    expect(screen.getByRole("button", { name: /continue/i })).toBeEnabled();
  });

  it("never renders a hamburger — every item here is unresolved, so chat is the only handoff", () => {
    setup([AMBIGUOUS, NEEDS_SPEC, ORG_SERVICE]);

    expect(
      screen.queryByRole("button", { name: /actions for/i }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("menuitem", { name: /discuss in chat/i }),
    ).not.toBeInTheDocument();
  });

  it("without onResolveDependency wired: no chat button renders", () => {
    render(
      <BuildDependencyDrawer
        open
        items={[AMBIGUOUS, NEEDS_SPEC, ORG_SERVICE]}
        onClose={vi.fn()}
        onContinue={vi.fn()}
      />,
    );

    expect(
      screen.queryByRole("button", { name: /resolve via chat/i }),
    ).not.toBeInTheDocument();
  });
});

// external-spec is the one blocker with a local form: the pasted URL/content
// rides along with the build request, which the BFF commits to HEAD before
// cutting the tag (InputsCoordinator.ApplyPreTag).
describe("BuildDependencyDrawer — the external-spec paste path", () => {
  it("keeps Continue disabled until a spec URL or pasted content is supplied", () => {
    setup([NEEDS_SPEC]);

    const continueButton = screen.getByRole("button", { name: /continue/i });
    expect(continueButton).toBeDisabled();

    fireEvent.change(screen.getByLabelText(/spec url/i), {
      target: { value: "https://example.com/openapi.json" },
    });

    expect(continueButton).toBeEnabled();
  });

  it("emits an external-spec input carrying the URL", () => {
    const { onContinue } = setup([NEEDS_SPEC]);

    fireEvent.change(screen.getByLabelText(/spec url/i), {
      target: { value: "https://example.com/openapi.json" },
    });
    fireEvent.click(screen.getByRole("button", { name: /continue/i }));

    expect(onContinue).toHaveBeenCalledTimes(1);
    const inputs = onContinue.mock.calls[0]?.[0] as BuildInputItem[];
    expect(inputs).toEqual([
      {
        component: "checkout-api",
        dependency: "partner-openapi-spec",
        kind: "external-spec",
        specUrl: "https://example.com/openapi.json",
      },
    ]);
  });

  it("accepts a pasted spec via the content field and emits specContent (not specUrl)", () => {
    const { onContinue } = setup([NEEDS_SPEC]);

    const continueButton = screen.getByRole("button", { name: /continue/i });
    expect(continueButton).toBeDisabled();

    fireEvent.change(screen.getByLabelText(/spec content/i), {
      target: { value: "openapi: 3.0.0\ninfo:\n  title: Partner API" },
    });

    expect(continueButton).toBeEnabled();
    fireEvent.click(continueButton);

    const inputs = onContinue.mock.calls[0]?.[0] as BuildInputItem[];
    const specInput = inputs.find(
      (i) => i.dependency === "partner-openapi-spec",
    );
    expect(specInput).toMatchObject({
      kind: "external-spec",
      specContent: "openapi: 3.0.0\ninfo:\n  title: Partner API",
    });
    expect(specInput).not.toHaveProperty("specUrl");
  });

  it("renders 'Resolve via chat' alongside the paste form", () => {
    const { onResolveDependency } = setup([NEEDS_SPEC]);

    expect(screen.getByLabelText(/spec url/i)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /resolve via chat/i }));

    expect(onResolveDependency).toHaveBeenCalledWith(NEEDS_SPEC, "resolve");
  });

  it("clears a pasted spec when the drawer is reopened", () => {
    const onClose = vi.fn();
    const onContinue = vi.fn();
    const { rerender } = render(
      <BuildDependencyDrawer
        open
        items={[NEEDS_SPEC]}
        onClose={onClose}
        onContinue={onContinue}
      />,
    );
    fireEvent.change(screen.getByLabelText(/spec url/i), {
      target: { value: "https://example.com/openapi.json" },
    });

    for (const open of [false, true]) {
      rerender(
        <BuildDependencyDrawer
          open={open}
          items={[NEEDS_SPEC]}
          onClose={onClose}
          onContinue={onContinue}
        />,
      );
    }

    expect(screen.getByLabelText(/spec url/i)).toHaveValue("");
  });
});

// org-service is informational plus a chat handoff: preflight only raises one
// while it is unresolved/blocked/ambiguous, so it is never already-resolved.
describe("BuildDependencyDrawer — org-service", () => {
  it("renders the cross-project-endpoint label and does not block Continue", () => {
    setup([ORG_SERVICE]);

    expect(screen.getByText(/cross-project endpoint/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /continue/i })).toBeEnabled();
  });

  it("uses the RESOLVE chat affordance", () => {
    const { onResolveDependency } = setup([ORG_SERVICE]);

    fireEvent.click(screen.getByRole("button", { name: /resolve via chat/i }));

    expect(onResolveDependency).toHaveBeenCalledWith(ORG_SERVICE, "resolve");
  });
});

// Continue still starts the build, so the request it sends must carry the
// approvals preflight raised — derived from the FULL items list, not from the
// blockers this drawer rendered.
describe("BuildDependencyDrawer — the request Continue sends", () => {
  it("carries platform-resource and org-service approvals alongside the pasted spec", () => {
    const { onContinue } = setup([
      NEEDS_SPEC,
      ORG_SERVICE,
      EXTERNAL_CONFIG,
      PLATFORM_RESOURCE,
    ]);

    fireEvent.change(screen.getByLabelText(/spec url/i), {
      target: { value: "https://example.com/openapi.json" },
    });
    fireEvent.click(screen.getByRole("button", { name: /continue/i }));

    const inputs = onContinue.mock.calls[0]?.[0] as BuildInputItem[];
    expect(inputs.find((i) => i.dependency === "postgres")).toEqual({
      component: "checkout-api",
      dependency: "postgres",
      kind: "platform-resource",
      approved: true,
      parameters: { instances: 1 },
    });
    expect(inputs.find((i) => i.dependency === "billing-service")).toEqual({
      component: "checkout-api",
      dependency: "billing-service",
      kind: "org-service",
      approved: true,
    });
  });

  it("never sends an external-config input — values are not a build concern", () => {
    const { onContinue } = setup([ORG_SERVICE, EXTERNAL_CONFIG]);

    fireEvent.click(screen.getByRole("button", { name: /continue/i }));

    const inputs = onContinue.mock.calls[0]?.[0] as BuildInputItem[];
    expect(inputs.some((i) => i.kind === "external-config")).toBe(false);
  });

  it("never sends a blocker item — there is no input that represents one", () => {
    const { onContinue } = setup([AMBIGUOUS, ORG_SERVICE]);

    // Continue is disabled while the blocker stands; the parent's refetch
    // drops it once chat resolves it.
    expect(screen.getByRole("button", { name: /continue/i })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: /continue/i }));
    expect(onContinue).not.toHaveBeenCalled();
  });

  it("shows a spinner on Continue and disables both buttons while submitting", () => {
    const { onContinue } = setup([ORG_SERVICE], true);

    const continueButton = screen.getByRole("button", { name: /continue/i });
    // Disabled despite nothing being required — the in-flight build is what
    // blocks it, so the request can't be double-submitted.
    expect(continueButton).toBeDisabled();
    expect(screen.getByRole("progressbar")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /cancel/i })).toBeDisabled();

    fireEvent.click(continueButton);
    expect(onContinue).not.toHaveBeenCalled();
  });
});

// #252 Task 15: Task 14 lifted the ComponentType!=service preflight guard, so
// a project-scoped shared dependency now surfaces one PreflightItem PER
// consuming component. groupPreflightItems re-collapses those into one card.
describe("groupPreflightItems (#252 Task 15)", () => {
  const CRM_WEB: PreflightItem = {
    component: "web-frontend",
    dependency: "crm",
    kind: "external-ambiguous",
    description: "More than one candidate fits.",
  };
  const CRM_SERVICE: PreflightItem = {
    component: "auth-service",
    dependency: "crm",
    kind: "external-ambiguous",
    description: "More than one candidate fits.",
  };

  it("merges same-kind, same-name entries across components into one group", () => {
    const groups = groupPreflightItems([CRM_WEB, CRM_SERVICE]);

    expect(groups).toHaveLength(1);
    expect(groups[0]!.usedBy).toEqual(["auth-service", "web-frontend"]);
    expect(groups[0]!.items).toHaveLength(2);
    // Representative is deterministic (sorted by component), not "whichever
    // arrived first" — auth-service < web-frontend alphabetically.
    expect(groups[0]!.representative.component).toBe("auth-service");
  });

  it("does not merge entries with a different dependency name", () => {
    const groups = groupPreflightItems([
      CRM_WEB,
      { ...CRM_SERVICE, dependency: "weather-api" },
    ]);

    expect(groups).toHaveLength(2);
    expect(groups.every((g) => g.usedBy.length === 1)).toBe(true);
  });

  it("does not merge entries with the same name but a different kind", () => {
    const groups = groupPreflightItems([
      CRM_WEB,
      { ...CRM_SERVICE, kind: "org-service" },
    ]);

    expect(groups).toHaveLength(2);
  });
});

describe("BuildDependencyDrawer cross-component 'Used by' rendering (#252 Task 15)", () => {
  const CRM_WEB: PreflightItem = {
    component: "web-frontend",
    dependency: "crm",
    kind: "external-ambiguous",
    description: "More than one candidate fits — resolve which one to use.",
  };
  const CRM_SERVICE: PreflightItem = {
    ...CRM_WEB,
    component: "auth-service",
  };

  it("renders a shared blocker ONCE with a 'Used by' listing every consumer, and one chat handoff", () => {
    const { onResolveDependency } = setup([CRM_WEB, CRM_SERVICE]);

    // The regression: before Task 15 this rendered TWICE (once per component).
    expect(screen.getAllByText("crm")).toHaveLength(1);
    expect(screen.getByText("web-frontend")).toBeInTheDocument();
    expect(screen.getByText("auth-service")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /continue/i })).toBeDisabled();

    fireEvent.click(screen.getByRole("button", { name: /resolve via chat/i }));

    expect(onResolveDependency).toHaveBeenCalledTimes(1);
    expect(onResolveDependency).toHaveBeenCalledWith(
      expect.objectContaining({ dependency: "crm", component: "auth-service" }),
      "resolve",
    );
  });

  it("does not render a 'Used by' line for a component-local dependency", () => {
    setup([AMBIGUOUS]);

    expect(screen.queryByText(/used by/i)).not.toBeInTheDocument();
  });

  it("collects a shared external-spec ONCE and fans it out to every consumer on Continue", () => {
    const specWeb: PreflightItem = {
      component: "web-frontend",
      dependency: "partner-api",
      kind: "external-spec",
      description: "No API spec yet.",
    };
    const specService: PreflightItem = {
      ...specWeb,
      component: "auth-service",
    };
    const { onContinue } = setup([specWeb, specService]);

    // Exactly one form is rendered — the merged, collect-once card.
    expect(screen.getAllByLabelText(/spec url/i)).toHaveLength(1);
    fireEvent.change(screen.getByLabelText(/spec url/i), {
      target: { value: "https://example.com/partner.json" },
    });
    fireEvent.click(screen.getByRole("button", { name: /continue/i }));

    const inputs = onContinue.mock.calls[0]?.[0] as BuildInputItem[];
    expect(inputs).toHaveLength(2);
    for (const consumer of ["web-frontend", "auth-service"]) {
      expect(inputs.find((i) => i.component === consumer)).toMatchObject({
        component: consumer,
        dependency: "partner-api",
        kind: "external-spec",
        specUrl: "https://example.com/partner.json",
      });
    }
  });
});
