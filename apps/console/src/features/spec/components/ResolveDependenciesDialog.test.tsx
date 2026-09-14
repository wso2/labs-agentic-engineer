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
import { ResolveDependenciesDialog } from "./ResolveDependenciesDialog";

const DEPENDENCIES = [
  {
    name: "currency-service",
    description: "No provider chosen yet — choose which one to use.",
    usedBy: ["orders-api", "reports-web"],
  },
  {
    name: "tax-service",
    description: "No interface yet — provide the API document to continue.",
    usedBy: ["orders-api"],
  },
];

function open(over: Partial<Parameters<typeof ResolveDependenciesDialog>[0]> = {}) {
  const onResolve = vi.fn();
  render(
    <ResolveDependenciesDialog
      open
      version="v3"
      dependencies={DEPENDENCIES}
      onClose={vi.fn()}
      onResolve={onResolve}
      {...over}
    />,
  );
  return { onResolve };
}

describe("ResolveDependenciesDialog", () => {
  it("names every dependency the version waits on, and what it costs", () => {
    open();

    expect(
      screen.getByText(
        "These need a provider and an interface before v3 can be cut.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByText("currency-service")).toBeInTheDocument();
    expect(screen.getByText("tax-service")).toBeInTheDocument();
  });

  it("names who waits on a shared dependency, and stays quiet for one consumer", () => {
    open();

    expect(screen.getByText("reports-web")).toBeInTheDocument();
    // A single consumer is the dependency's own component — saying it back adds
    // nothing.
    expect(screen.getAllByText("orders-api")).toHaveLength(1);
  });

  // One action, by decision (ADR-0029): a per-row button would be a second way
  // in, and the rail already reaches every definition.
  it("offers exactly one way forward", () => {
    const { onResolve } = open();

    const buttons = screen.getAllByRole("button").map((b) => b.textContent);
    expect(buttons).toEqual(["Cancel", "Resolve"]);

    fireEvent.click(screen.getByRole("button", { name: "Resolve" }));
    expect(onResolve).toHaveBeenCalledTimes(1);
  });

  it("drops the version from the sentence when the project has none yet", () => {
    open({ version: "" });

    expect(
      screen.getByText(
        "These need a provider and an interface before a version can be cut.",
      ),
    ).toBeInTheDocument();
  });
});
