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

const navigate = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigate,
  Link: ({ to, children }: { to: string; children?: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));

import { RegisterComposerPage } from "./RegisterComposerPage";

const STRIPE_PROMPT =
  "Register Stripe as a payments integration. Runtime config: API credentials and webhook signing.";

beforeEach(() => {
  vi.clearAllMocks();
});

describe("RegisterComposerPage", () => {
  it("navigates to the form with the typed prompt when Start is clicked", () => {
    render(<RegisterComposerPage />);

    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "Register Acme as an invoicing API." },
    });
    fireEvent.click(screen.getByRole("button", { name: "Start" }));

    expect(navigate).toHaveBeenCalledWith({
      to: "/resources/register/form",
      state: { registerPrompt: "Register Acme as an invoicing API." },
    });
  });

  it("navigates to the form with the example prompt when an example is clicked", () => {
    render(<RegisterComposerPage />);

    fireEvent.click(screen.getByText("Stripe"));

    expect(navigate).toHaveBeenCalledWith({
      to: "/resources/register/form",
      state: { registerPrompt: STRIPE_PROMPT },
    });
  });
});
