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
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import type { SpecFileEntry } from "../api/mapping";
import { SpecFileList } from "./SpecFileList";
import { railSections, type RailInput, type RailSection } from "../lib/railSections";

// A settled project: the rail states are exercised in railSections.test.ts, so
// these render tests only need it out of the way.
afterEach(cleanup);

const RAIL_INPUT: RailInput = {
  hasRequirements: true,
  hasDesign: true,
  hasValidation: true,
  agentWorking: false,
  agentFlow: "",
  designOutdated: false,
  assumptions: 0,
  openQuestions: 0,
  planEntries: [],
  planWreckage: false,
};

/** The list as `SpecView` hands it over: deduped and sorted by path. */
function entries(...paths: string[]): SpecFileEntry[] {
  return paths
    .map((path) => ({ path, sha: "sha", group: "requirements" as const }))
    .sort((a, b) => a.path.localeCompare(b.path));
}

/** Design-group files — the existing `entries` helper marks everything requirements. */
function designEntries(...paths: string[]): SpecFileEntry[] {
  return paths
    .map((path) => ({ path, sha: "sha", group: "designs" as const }))
    .sort((a, b) => a.path.localeCompare(b.path));
}

function renderList(files: SpecFileEntry[], sections?: RailSection[], onReason = () => {}) {
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <SpecFileList
        files={files}
        selection={null}
        onSelect={() => {}}
        onRegenerateDesign={() => {}}
        sections={sections ?? railSections(RAIL_INPUT)}
        onReason={onReason}
      />
    </OxygenUIThemeProvider>,
  );
  // The Requirements group's own rows, in render order.
  const nav = screen.getByRole("navigation", { name: "Spec files" });
  return within(nav)
    .getAllByRole("button")
    .map((b) => b.textContent)
    .filter((t): t is string => Boolean(t) && t !== "");
}

describe("SpecFileList — the PRD leads Requirements", () => {
  it("puts the PRD first even though features/ sorts above it by path", () => {
    const rows = renderList(
      entries(
        "specs/requirements/features/approvals.md",
        "specs/requirements/prd.md",
        "specs/requirements/features/receipts.md",
      ),
    );
    expect(rows.slice(0, 3)).toEqual(["Product requirements", "approvals", "receipts"]);
  });

  it("keeps the rest in path order behind it", () => {
    const rows = renderList(
      entries(
        "specs/requirements/zebra.md",
        "specs/requirements/prd.md",
        "specs/requirements/alpha.md",
      ),
    );
    expect(rows.slice(0, 3)).toEqual(["Product requirements", "alpha", "zebra"]);
  });

  it("is untroubled by a project whose PRD has not been written yet", () => {
    const rows = renderList(entries("specs/requirements/features/receipts.md"));
    expect(rows[0]).toBe("receipts");
  });
});

// The rail is the flow (#575): the sections carry state, and an amber one
// explains itself in rows rather than a hover.
describe("SpecFileList — the rail carries state", () => {
  function renderWith(over: Partial<RailInput>, onReason = vi.fn()) {
    const files = entries("specs/requirements/prd.md");
    render(
      <OxygenUIThemeProvider theme={OxygenTheme}>
        <SpecFileList
          files={files}
          selection={null}
          onSelect={() => {}}
            onRegenerateDesign={() => {}}
          sections={railSections({ ...RAIL_INPUT, ...over })}
          onReason={onReason}
        />
      </OxygenUIThemeProvider>,
    );
    return onReason;
  }

  // One design, written across several documents.
  it("names the design section in the singular", () => {
    renderWith({});
    expect(screen.getByText("Design")).toBeInTheDocument();
    expect(screen.queryByText("Designs")).not.toBeInTheDocument();
  });

  // The old note claimed agents were "being derived…" over sections nobody had
  // asked for yet — stating something untrue about what the platform was doing.
  it("says a section is not created rather than being derived", () => {
    renderWith({ hasDesign: false, hasValidation: false });
    expect(screen.getAllByText("Not created yet").length).toBeGreaterThan(0);
    expect(screen.queryByText("Being derived…")).not.toBeInTheDocument();
  });

  // Work in progress is the app's existing pulse, not a second animation.
  it("pulses the section an agent is working on", () => {
    renderWith({ hasDesign: false, agentWorking: true, agentFlow: "design" });
    expect(screen.getAllByTestId("working-pulse").length).toBeGreaterThan(0);
  });

  // An agent re-deriving a stale design is already resolving it, so warning
  // about the thing being fixed while it is being fixed reads as a fault. The
  // model still CARRIES the reasons here — SpecView reads them for the design
  // warning — so the rail has to gate on the state rather than on their count.
  it("shows no warning chip on the section an agent is working on", () => {
    renderWith({ designOutdated: true, agentWorking: true, agentFlow: "design" });

    expect(screen.getAllByTestId("working-pulse").length).toBeGreaterThan(0);
    expect(screen.queryByLabelText(/Design: \d+ to resolve/)).not.toBeInTheDocument();
  });

  // The same rule on the other section, reached by a different flow: settling
  // an assumption must not leave the requirements looking unattended.
  it("shows no warning chip while the requirements are being settled", () => {
    renderWith({ assumptions: 3, agentWorking: true, agentFlow: "settle" });

    expect(screen.queryByLabelText(/Requirements: \d+ to resolve/)).not.toBeInTheDocument();
  });

  // A count, not just a mark: three assumptions and one would otherwise look
  // identical, and "how much" is what a glance is for.
  it("counts what a section has to resolve", () => {
    renderWith({ assumptions: 2, openQuestions: 1 });
    expect(
      screen.getByRole("button", { name: "Requirements: 3 to resolve" }),
    ).toBeInTheDocument();
  });

  it("opens the problems in a dialog, and each one carries its fix", () => {
    const onReason = renderWith({ assumptions: 2, openQuestions: 1 });

    fireEvent.click(screen.getByRole("button", { name: "Requirements: 3 to resolve" }));
    expect(screen.getByText("1 question only you can answer")).toBeInTheDocument();
    expect(screen.getByText("2 decisions marked assumed")).toBeInTheDocument();

    fireEvent.click(screen.getAllByRole("button", { name: "Open the document" })[0]!);
    expect(onReason).toHaveBeenCalledWith("document");
  });

  // The validation criteria are written against the same stories, so they go
  // stale with the design and clear with it — two amber sections, one reason
  // each, one repair.
  it("marks design and validation together", () => {
    renderWith({ designOutdated: true });
    expect(screen.getByRole("button", { name: "Design: 1 to resolve" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Validation: 1 to resolve" })).toBeInTheDocument();
  });

  it("offers the re-derivation from the design's dialog", () => {
    const onReason = renderWith({ designOutdated: true });

    fireEvent.click(screen.getByRole("button", { name: "Design: 1 to resolve" }));
    fireEvent.click(screen.getByRole("button", { name: "Update the design" }));
    expect(onReason).toHaveBeenCalledWith("update-design");
  });
});

// The declared plan (#576): ghosts hold the coming files' places, the header
// carries the count, and a ghost is disabled — a control that selects nothing
// is worse than prose.
describe("SpecFileList — the declared plan", () => {
  const plan = [
    { path: "specs/design/domain-model.md", status: "writing" as const, section: "design" as const },
    {
      path: "specs/design/components/portal/design.json",
      status: "planned" as const,
      section: "design" as const,
    },
  ];

  function renderWithPlan() {
    render(
      <OxygenUIThemeProvider theme={OxygenTheme}>
        <SpecFileList
          files={entries("specs/requirements/prd.md")}
          selection={null}
          onSelect={() => {}}
          onRegenerateDesign={() => {}}
          sections={railSections({ ...RAIL_INPUT, agentWorking: true, planEntries: plan })}
          plan={plan}
          onReason={() => {}}
        />
      </OxygenUIThemeProvider>,
    );
    return screen.getByRole("navigation", { name: "Spec files" });
  }

  it("renders a planned-but-unwritten path as a disabled ghost row in its group", () => {
    const nav = renderWithPlan();
    // The ghost is the row under the COMPONENT (portal), which the plan lists
    // but nothing has written; the domain-model row beside it is being
    // written and must stay live. Asserting "some row is disabled" passed for
    // the wrong reasons, so each row is now identified and checked on its own.
    const rows = within(nav).getAllByRole("button", { hidden: true });
    const disabled = (b: HTMLElement) =>
      b.hasAttribute("disabled") || b.getAttribute("aria-disabled") === "true";
    const ghostRows = rows.filter((b) => disabled(b));
    expect(ghostRows).toHaveLength(1);
    expect(ghostRows[0]!.textContent).toBe("Design");
  });

  it("shows the section count from the plan", () => {
    const nav = renderWithPlan();
    expect(within(nav).getByText("0 of 2")).toBeTruthy();
  });

  // The entry a dead turn stopped on has no file behind it any more than one it
  // never reached, so it must not offer a click that selects nothing.
  it("disables an errored row that never became a file", () => {
    const wreck = [
      {
        path: "specs/design/components/portal/design.json",
        status: "error" as const,
        section: "design" as const,
      },
    ];
    render(
      <OxygenUIThemeProvider theme={OxygenTheme}>
        <SpecFileList
          files={entries("specs/requirements/prd.md")}
          selection={null}
          onSelect={() => {}}
          onRegenerateDesign={() => {}}
          sections={railSections({ ...RAIL_INPUT, planWreckage: true, planEntries: wreck })}
          plan={wreck}
          onReason={() => {}}
        />
      </OxygenUIThemeProvider>,
    );
    const nav = screen.getByRole("navigation", { name: "Spec files" });
    const rows = within(nav).getAllByRole("button", { hidden: true });
    const errored = rows.find((b) => b.textContent === "Design");
    expect(errored).toBeTruthy();
    expect(
      errored!.hasAttribute("disabled") || errored!.getAttribute("aria-disabled") === "true",
    ).toBe(true);
  });
});

describe("SpecFileList — Security rail from security.json", () => {
  it("hides Security when only the domain model exists", () => {
    renderList(designEntries("specs/design/domain-model.md"));
    expect(screen.queryByRole("button", { name: "Security" })).not.toBeInTheDocument();
  });

  it("shows a Security button for security.json, not a security.json filename row", () => {
    renderList(
      designEntries("specs/design/domain-model.md", "specs/design/security.json"),
    );
    expect(screen.getByRole("button", { name: "Security" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "security.json" })).not.toBeInTheDocument();
  });

  it("does not show Security for leftover security.md and roles.json alone", () => {
    renderList(
      designEntries(
        "specs/design/domain-model.md",
        "specs/design/security.md",
        "specs/design/roles.json",
      ),
    );
    expect(screen.queryByRole("button", { name: /^Security$/ })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^security$/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "roles.json" })).toBeInTheDocument();
  });
});

describe("SpecFileList — the design reads as its parts (#686)", () => {
  it("lists the documents as rows, then the Flows group, then one group per component", () => {
    const rows = renderList(
      designEntries(
        "specs/design/components/api/design.json",
        "specs/design/components/api/openapi.yaml",
        "specs/design/flows/checkout.md",
        "specs/design/flows/view-order.md",
        "specs/design/domain-model.md",
        "specs/design/security.json",
      ),
    );
    expect(rows).toEqual([
      "Domain model",
      "Security",
      "Flows",
      "checkout",
      "view-order",
      "api",
      "Design",
      "API",
    ]);
  });

  it("shows no Flows group until a flow exists", () => {
    renderList(designEntries("specs/design/domain-model.md", "specs/design/components/api/design.json"));
    expect(screen.queryByRole("button", { name: /Flows$/ })).not.toBeInTheDocument();
  });

  it("collapses and expands the Flows group like a component group", async () => {
    renderList(designEntries("specs/design/flows/checkout.md"));
    const header = screen.getByRole("button", { name: "Collapse Flows" });
    expect(header).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByRole("button", { name: "checkout" })).toBeInTheDocument();
    fireEvent.click(header);
    const collapsed = screen.getByRole("button", { name: "Expand Flows" });
    expect(collapsed).toHaveAttribute("aria-expanded", "false");
    // The group unmounts its rows once the collapse transition ends.
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "checkout" })).not.toBeInTheDocument(),
    );
  });

  it("holds a planned flow as a ghost row inside the Flows group", () => {
    const plan = [{ path: "specs/design/flows/checkout.md", status: "planned" as const, section: "design" as const }];
    render(
      <OxygenUIThemeProvider theme={OxygenTheme}>
        <SpecFileList
          files={designEntries("specs/design/domain-model.md")}
          selection={null}
          onSelect={() => {}}
          onRegenerateDesign={() => {}}
          sections={railSections({ ...RAIL_INPUT, agentWorking: true, planEntries: plan })}
          plan={plan}
          onReason={() => {}}
        />
      </OxygenUIThemeProvider>,
    );
    const nav = screen.getByRole("navigation", { name: "Spec files" });
    expect(within(nav).getByRole("button", { name: "Collapse Flows" })).toBeInTheDocument();
    const ghost = within(nav).getAllByRole("button", { hidden: true }).find((b) => b.textContent === "checkout");
    expect(ghost).toBeTruthy();
    expect(ghost!.hasAttribute("disabled") || ghost!.getAttribute("aria-disabled") === "true").toBe(true);
  });
});
