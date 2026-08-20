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
import { describe, expect, it, vi } from "vitest";
import type { components } from "../../../generated/aep-api";
import { BuildDependencyDrawer } from "./BuildDependencyDrawer";

type PreflightItem = components["schemas"]["PreflightItem"];

const AMBIGUOUS_ITEM: PreflightItem = {
  component: "checkout-api",
  dependency: "crm",
  kind: "external-ambiguous",
  description: "More than one candidate fits.",
};

const UNRESOLVED_ITEM: PreflightItem = {
  component: "checkout-api",
  dependency: "weather-api",
  kind: "external-unresolved",
  description: "Needs information only you can provide.",
};

const EXTERNAL_SPEC_ITEM: PreflightItem = {
  component: "checkout-api",
  dependency: "partner-api",
  kind: "external-spec",
  description: "Provide the partner API specification.",
};

const ORG_SERVICE_ITEM: PreflightItem = {
  component: "checkout-api",
  dependency: "billing-service",
  kind: "org-service",
  description: "Choose the owning project endpoint.",
};

const EXTERNAL_CONFIG_ITEM: PreflightItem = {
  component: "checkout-api",
  dependency: "stripe-config",
  kind: "external-config",
  description: "Stripe credentials",
  config: [{ key: "STRIPE_API_KEY", secret: true }],
};

const PLATFORM_RESOURCE_ITEM: PreflightItem = {
  component: "checkout-api",
  dependency: "postgres",
  kind: "platform-resource",
  description: "Postgres database",
  resourceType: "postgres",
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

describe("BuildDependencyDrawer resolution behavior", () => {
  it("does not reintroduce pre-build credential/resource panels when resolution items are mixed in", () => {
    setup([
      EXTERNAL_CONFIG_ITEM,
      PLATFORM_RESOURCE_ITEM,
      AMBIGUOUS_ITEM,
    ]);

    expect(screen.queryByText("stripe-config")).not.toBeInTheDocument();
    expect(screen.queryByText("postgres")).not.toBeInTheDocument();
    expect(screen.getByText("crm")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /resolve via chat/i }),
    ).toBeEnabled();
  });

  it("renders unresolved and ambiguous reasons without local inputs", () => {
    setup([AMBIGUOUS_ITEM, UNRESOLVED_ITEM]);

    expect(screen.getByText(/more than one candidate fits/i)).toBeInTheDocument();
    expect(
      screen.getByText(/needs information only you can provide/i),
    ).toBeInTheDocument();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /continue/i })).not.toBeInTheDocument();
  });

  it("resolves an ambiguous dependency through chat", () => {
    const { onResolveDependency } = setup([AMBIGUOUS_ITEM]);

    fireEvent.click(
      screen.getByRole("button", { name: /resolve via chat/i }),
    );

    expect(onResolveDependency).toHaveBeenCalledWith(AMBIGUOUS_ITEM);
  });

  it("dedupes a shared blocker and shows every consumer", () => {
    setup([
      { ...AMBIGUOUS_ITEM, component: "checkout-web" },
      { ...AMBIGUOUS_ITEM, component: "checkout-api" },
    ]);

    expect(screen.getAllByText("crm")).toHaveLength(1);
    expect(screen.getByText("checkout-api")).toBeInTheDocument();
    expect(screen.getByText("checkout-web")).toBeInTheDocument();
    expect(
      screen.getAllByRole("button", { name: /resolve via chat/i }),
    ).toHaveLength(1);
  });

  it("keeps org-service resolution chat-only", () => {
    const { onResolveDependency } = setup([ORG_SERVICE_ITEM]);

    expect(screen.getByText("billing-service")).toBeInTheDocument();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /continue/i })).not.toBeInTheDocument();

    fireEvent.click(
      screen.getByRole("button", { name: /resolve via chat/i }),
    );
    expect(onResolveDependency).toHaveBeenCalledWith(ORG_SERVICE_ITEM);
  });
});

describe("BuildDependencyDrawer local external-spec resolution", () => {
  it("enables Continue only after a spec URL or content is supplied", () => {
    setup([EXTERNAL_SPEC_ITEM]);

    const continueButton = screen.getByRole("button", { name: /continue/i });
    expect(continueButton).toBeDisabled();

    fireEvent.change(screen.getByLabelText(/spec url/i), {
      target: { value: "https://partner.example/openapi.yaml" },
    });

    expect(continueButton).toBeEnabled();
  });

  it("submits only locally resolved external-spec inputs", () => {
    const { onContinue } = setup([
      EXTERNAL_CONFIG_ITEM,
      PLATFORM_RESOURCE_ITEM,
      EXTERNAL_SPEC_ITEM,
    ]);

    fireEvent.change(screen.getByLabelText(/spec content/i), {
      target: { value: "openapi: 3.1.0" },
    });
    fireEvent.click(screen.getByRole("button", { name: /continue/i }));

    expect(onContinue).toHaveBeenCalledWith([
      {
        component: "checkout-api",
        dependency: "partner-api",
        kind: "external-spec",
        specContent: "openapi: 3.1.0",
      },
    ]);
  });

  it("trims external-spec values before submitting them", () => {
    const { onContinue } = setup([EXTERNAL_SPEC_ITEM]);

    fireEvent.change(screen.getByLabelText(/spec url/i), {
      target: { value: "  https://partner.example/openapi.yaml  " },
    });
    fireEvent.click(screen.getByRole("button", { name: /continue/i }));

    expect(onContinue).toHaveBeenCalledWith([
      {
        component: "checkout-api",
        dependency: "partner-api",
        kind: "external-spec",
        specUrl: "https://partner.example/openapi.yaml",
      },
    ]);
  });

  it("keeps Resolve via chat alongside the local external-spec form", () => {
    const { onResolveDependency } = setup([EXTERNAL_SPEC_ITEM]);

    fireEvent.click(
      screen.getByRole("button", { name: /resolve via chat/i }),
    );

    expect(onResolveDependency).toHaveBeenCalledWith(EXTERNAL_SPEC_ITEM);
  });

  it("keeps Continue disabled while another resolution blocker remains", () => {
    setup([EXTERNAL_SPEC_ITEM, UNRESOLVED_ITEM]);

    fireEvent.change(screen.getByLabelText(/spec url/i), {
      target: { value: "https://partner.example/openapi.yaml" },
    });

    expect(screen.getByRole("button", { name: /continue/i })).toBeDisabled();
  });

  it("locks Cancel and Continue while the build is being submitted", () => {
    setup([EXTERNAL_SPEC_ITEM], true);

    expect(screen.getByRole("button", { name: /continue/i })).toBeDisabled();
    expect(screen.getByRole("button", { name: /cancel/i })).toBeDisabled();
    expect(screen.getByRole("progressbar")).toBeInTheDocument();
  });
});
