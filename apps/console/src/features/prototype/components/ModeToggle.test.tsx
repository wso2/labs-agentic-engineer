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
import { afterEach, describe, expect, it, vi } from "vitest";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import type { PrototypeMode } from "../model/viewState";
import { ModeToggle } from "./ModeToggle";

afterEach(cleanup);

function renderToggle(mode: PrototypeMode, disabled = false) {
  const onChange = vi.fn();
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <ModeToggle mode={mode} onChange={onChange} disabled={disabled} />
    </OxygenUIThemeProvider>,
  );
  return onChange;
}

const button = (name: string) => screen.getByRole("button", { name });

describe("ModeToggle", () => {
  it("marks the active mode pressed, inside a Mode group", () => {
    renderToggle("annotate");
    expect(screen.getByRole("group", { name: "Mode" })).toBeInTheDocument();
    expect(button("Annotate")).toHaveAttribute("aria-pressed", "true");
    expect(button("Preview")).toHaveAttribute("aria-pressed", "false");
  });

  it("switches mode on clicking the other one, and ignores the active one", () => {
    const onChange = renderToggle("preview");
    fireEvent.click(button("Preview"));
    expect(onChange).not.toHaveBeenCalled();
    fireEvent.click(button("Annotate"));
    expect(onChange).toHaveBeenCalledExactlyOnceWith("annotate");
  });

  it("is inert while disabled", () => {
    const onChange = renderToggle("preview", true);
    expect(button("Preview")).toBeDisabled();
    expect(button("Annotate")).toBeDisabled();
    fireEvent.click(button("Annotate"));
    expect(onChange).not.toHaveBeenCalled();
  });
});
