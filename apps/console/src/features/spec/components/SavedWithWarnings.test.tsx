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

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SavedWithWarnings } from "./SavedWithWarnings";

// The Room's last save landed, with warnings: each file and the platform's
// message for it, as given, until dismissed or the next save replaces them.

afterEach(cleanup);

const renderIt = (ui: React.ReactNode) =>
  render(<OxygenUIThemeProvider theme={OxygenTheme}>{ui}</OxygenUIThemeProvider>);

describe("SavedWithWarnings", () => {
  it("says the save happened and lists each file's warning", () => {
    renderIt(
      <SavedWithWarnings
        warnings={[
          { path: "specs/requirements/prd.md", message: "front matter dropped" },
          { path: "specs/design/design.cell", message: "not valid YAML" },
        ]}
        onDismiss={() => undefined}
      />,
    );
    expect(screen.getByText("Saved with warnings")).toBeInTheDocument();
    expect(screen.getByText(/specs\/requirements\/prd\.md/)).toHaveTextContent("specs/requirements/prd.md: front matter dropped");
    expect(screen.getByText(/specs\/design\/design\.cell/)).toHaveTextContent("specs/design/design.cell: not valid YAML");
  });

  it("is dismissible", () => {
    const onDismiss = vi.fn();
    renderIt(<SavedWithWarnings warnings={[{ path: "specs/a.md", message: "x" }]} onDismiss={onDismiss} />);
    fireEvent.click(screen.getByRole("button", { name: /close/i }));
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it("shows nothing without warnings", () => {
    const { container } = renderIt(<SavedWithWarnings warnings={[]} onDismiss={() => undefined} />);
    expect(container).toBeEmptyDOMElement();
  });
});
