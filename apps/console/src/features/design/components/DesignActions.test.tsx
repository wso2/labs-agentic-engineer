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
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import type { DesignArtifact, DesignModel } from "../api/designModel";

// The design card's header offers the design turn once something is designed
// (before that, the card's body offers it). The platform's design model
// tracks no revisions (useDesignModel: `revision: null`), so "designed" is
// read from what both the platform and the mock have: the design's artifacts.

let model: DesignModel;
vi.mock("../useDesignModel", () => ({ useDesignModel: () => ({ data: model }) }));

let label: string | null;
vi.mock("../../spec/useSpecWorkspace", () => ({
  useSpecWorkspace: () => ({ workspace: { design: { label, toDesign: ["F1"], outOfDate: ["F1"] } } }),
}));

const design = vi.fn();
vi.mock("../useDesignTurns", () => ({
  useDesignTurns: () => ({ design, addressComments: vi.fn(), ready: true }),
}));
vi.mock("../../prototype/components/MakePrototypeButton", () => ({ MakePrototypeButton: () => null }));
vi.mock("../../builds/components/BuildButton", () => ({ BuildButton: () => null }));

const { DesignActions } = await import("./DesignWorkspace");

const contract: DesignArtifact = {
  id: "greeter",
  title: "greeter",
  depth: "technical",
  features: ["F1"],
  addedIn: 0,
  changedIn: 0,
  source: { kind: "acceptance", path: "specs/validation/acceptance/F1.feature", content: "" },
};

/** The model as the platform builds it (useDesignModel): no revisions. */
function platformModel(artifacts: DesignArtifact[]): DesignModel {
  return { revision: null, running: null, artifacts, dependencies: [], comments: [], taught: false, commenting: false };
}

function renderActions() {
  return render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <DesignActions projectName="s0" />
    </OxygenUIThemeProvider>,
  );
}

beforeEach(() => design.mockClear());
afterEach(cleanup);

describe("DesignActions, the design turn", () => {
  it("offers Update design for an out-of-date feature once a design exists on the platform", () => {
    model = platformModel([contract]);
    label = "Update design · 1 feature";
    renderActions();
    fireEvent.click(screen.getByRole("button", { name: "Update design · 1 feature" }));
    expect(design).toHaveBeenCalledOnce();
  });

  it("leaves the first design to the card's body", () => {
    model = platformModel([]);
    label = "Design 1 feature";
    renderActions();
    expect(screen.queryByRole("button", { name: "Design 1 feature" })).toBeNull();
  });

  it("offers nothing when no feature needs designing", () => {
    model = platformModel([contract]);
    label = null;
    renderActions();
    expect(screen.queryByRole("button")).toBeNull();
  });
});
