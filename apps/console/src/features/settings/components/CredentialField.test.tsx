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

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { CredentialField } from "./CredentialField";

afterEach(cleanup);

function renderField(ui: React.ReactElement) {
  render(<OxygenUIThemeProvider theme={OxygenTheme}>{ui}</OxygenUIThemeProvider>);
}

describe("CredentialField", () => {
  it("a stored key reads Set with no characters of the key", () => {
    renderField(<CredentialField label="API key" set />);
    expect(screen.getByText("API key")).toBeInTheDocument();
    expect(screen.getByText("Set ••••••••")).toBeInTheDocument();
  });

  it("no key reads Not set", () => {
    renderField(<CredentialField label="API key" set={false} />);
    expect(screen.getByText("Not set")).toBeInTheDocument();
    expect(screen.queryByText(/••••/)).not.toBeInTheDocument();
  });

  it("offers Replace while onReplace is set, and nothing named rotate", () => {
    const onReplace = vi.fn();
    renderField(<CredentialField label="API key" set onReplace={onReplace} />);
    fireEvent.click(screen.getByRole("button", { name: "Replace" }));
    expect(onReplace).toHaveBeenCalledOnce();
    expect(screen.queryByRole("button", { name: /rotate/i })).not.toBeInTheDocument();
  });
});
