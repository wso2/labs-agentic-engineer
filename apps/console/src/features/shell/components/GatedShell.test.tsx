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

import type { ReactNode } from "react";
import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

// The gates run outside in: sign-in, then onboarding (GitHub and a model),
// then AE Studio, then the shell. The AE Studio gate sits inside onboarding:
// an org that is not set up gets the wizard, never an AE Studio hold.

function gate(name: string) {
  return ({ children }: { children?: ReactNode }) => <div data-gate={name}>{children}</div>;
}
vi.mock("../../../auth/AuthGuard", () => ({ AuthGuard: gate("auth") }));
vi.mock("../../onboarding/components/OnboardingGate", () => ({ OnboardingGate: gate("onboarding") }));
vi.mock("../../ae-studio/components/AeStudioGate", () => ({ AeStudioGate: gate("ae-studio") }));
vi.mock("./Shell", () => ({ Shell: () => <div data-gate="shell" /> }));

const { GatedShell } = await import("./GatedShell");

afterEach(cleanup);

describe("GatedShell", () => {
  it("wraps the shell in AuthGuard > OnboardingGate > AeStudioGate", () => {
    const { container } = render(<GatedShell />);
    const order: string[] = [];
    let el: Element | null = container.querySelector("[data-gate]");
    while (el) {
      order.push(el.getAttribute("data-gate")!);
      el = el.querySelector(":scope > [data-gate]");
    }
    expect(order).toEqual(["auth", "onboarding", "ae-studio", "shell"]);
  });
});
