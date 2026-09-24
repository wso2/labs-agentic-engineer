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
import type { SpecSelection } from "../api/designTree";
import { SpecFileList } from "./SpecFileList";
import {
  railSections,
  type RailInput,
  type RailPlanEntry,
  type RailSection,
} from "../lib/railSections";

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

/** The real paths a generated ai-agent project produces. */
function designFiles(...paths: string[]): SpecFileEntry[] {
  return paths.map((path, i) => ({ path, sha: `sha${i}`, group: "designs" }));
}

describe("SpecFileList — artifact labels", () => {
  it("names an ai-agent's agent.afm.md 'Agent spec' rather than its file name", () => {
    renderList(designFiles("specs/design/components/booking-agent/agent.afm.md"));

    expect(screen.getByText("Agent spec")).toBeInTheDocument();
    expect(screen.queryByText("agent.afm.md")).not.toBeInTheDocument();
  });

  // Only the ai-agent artifact is pinned here. The sibling labels ("API",
  // "Design") are upstream's own concern and are covered there — and the rail
  // now carries a "Design" section header too, so a bare text query for them
  // matches the header as readily as the file.
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

describe("SpecFileList — a dependency's group", () => {
  it("lists each dependency directory like a component, its files as rows and its state on the header", () => {
    const onSelect = vi.fn();
    render(
      <OxygenUIThemeProvider theme={OxygenTheme}>
        <SpecFileList
          files={designEntries(
            "specs/design/design.cell",
            "specs/design/dependencies/stripe/openapi.yaml",
            "specs/design/dependencies/stripe/dependency.json",
            "specs/design/dependencies/dhl/dependency.json",
          )}
          selection={null}
          onSelect={onSelect}
          onRegenerateDesign={() => {}}
          sections={railSections(RAIL_INPUT)}
          onReason={() => {}}
          dependencyStates={{
            dhl: {
              dependency: { kind: "external", name: "dhl", status: "unresolved", reason: "needs-contract" },
              usedBy: ["parcel-api"],
              blocking: true,
              todo: "Needs a contract",
              flags: [],
            },
            stripe: {
              dependency: { kind: "external", name: "stripe", status: "resolved", flags: ["assumed"] },
              usedBy: ["parcel-api"],
              blocking: false,
              todo: "",
              flags: ["Assumed"],
            },
          }}
        />
      </OxygenUIThemeProvider>,
    );
    const nav = screen.getByRole("navigation", { name: "Spec files" });
    // One group per dependency, headed like a component's.
    expect(within(nav).getByRole("button", { name: "Collapse dhl" })).toBeInTheDocument();
    expect(within(nav).getByRole("button", { name: "Collapse stripe" })).toBeInTheDocument();
    expect(screen.getByLabelText("dhl: Needs a contract")).toBeInTheDocument();
    expect(screen.getByText("Assumed")).toBeInTheDocument();
    // The files are rows, named for what they are — definition first.
    const labels = within(nav)
      .getAllByRole("button")
      .map((b) => b.textContent)
      .filter((t) => t === "Definition" || t === "API");
    expect(labels).toEqual(["Definition", "Definition", "API"]);
    fireEvent.click(within(nav).getAllByRole("button", { name: "API" })[0]!);
    expect(onSelect).toHaveBeenCalledWith({
      kind: "file",
      path: "specs/design/dependencies/stripe/openapi.yaml",
    });
  });
});

// One entry stands for every specs/validation/acceptance/*.feature, because the pane reads
// them as one set — so the rail cannot carry a row per capability, and the two
// things `row` derives from a single path have to be folded for a set.
describe("SpecFileList — Acceptance criteria is one entry", () => {
  function validationEntries(...paths: string[]): SpecFileEntry[] {
    return paths
      .map((path) => ({ path, sha: "sha", group: "validation" as const }))
      .sort((a, b) => a.path.localeCompare(b.path));
  }

  function renderValidation(
    files: SpecFileEntry[],
    plan?: RailPlanEntry[],
    onSelect: (sel: SpecSelection) => void = () => {},
  ) {
    render(
      <OxygenUIThemeProvider theme={OxygenTheme}>
        <SpecFileList
          files={files}
          selection={null}
          onSelect={onSelect}
          onRegenerateDesign={() => {}}
          sections={railSections(RAIL_INPUT)}
          {...(plan ? { plan } : {})}
          onReason={() => {}}
        />
      </OxygenUIThemeProvider>,
    );
    return screen.getByRole("navigation", { name: "Spec files" });
  }

  const THREE = validationEntries(
    "specs/validation/acceptance/adding-items.feature",
    "specs/validation/acceptance/bought-items.feature",
    "specs/validation/acceptance/shared-list-access.feature",
  );

  // Every path here is an acceptance capability: the retired criteria document
  // is the only other thing the validation folder holds, and the spec view drops
  // it before the rail ever sees it (mapping.ts). That the entry does not
  // swallow an ordinary validation file is `buildValidationSection`'s own
  // invariant, tested against it directly in designTree.test.ts.
  it("collapses every capability into one row", () => {
    const nav = renderValidation(THREE);
    const rows = within(nav)
      .getAllByRole("button")
      .map((b) => b.textContent);

    expect(rows).toContain("Acceptance criteria");
    for (const capability of ["Adding items", "Bought items", "Shared list access"]) {
      expect(rows, capability).not.toContain(capability);
    }
  });

  it("selects the set, not a file", () => {
    const onSelect = vi.fn();
    renderValidation(THREE, undefined, onSelect);
    fireEvent.click(screen.getByText("Acceptance criteria"));
    expect(onSelect).toHaveBeenCalledWith({ kind: "acceptance" });
  });

  it("is absent when the project has no acceptance criteria", () => {
    const nav = renderValidation([]);
    expect(within(nav).queryByText("Acceptance criteria")).not.toBeInTheDocument();
  });

  // `row` reads its plan status from ONE path and this stands for many, so the
  // pulse is folded: it marks the entry while ANY capability is being written.
  it("pulses while the agent writes any capability", () => {
    renderValidation(
      validationEntries("specs/validation/acceptance/adding-items.feature"),
      [
        { path: "specs/validation/acceptance/adding-items.feature", status: "done", section: "validation" },
        { path: "specs/validation/acceptance/bought-items.feature", status: "writing", section: "validation" },
      ],
    );
    // The pulse is the console's one "an agent is working" dot, rendered
    // beside the row whose status is `writing`.
    const entry = screen.getByText("Acceptance criteria").closest("div[role], li, a, button");
    expect(entry?.querySelector("svg, span")).not.toBeNull();
    expect(screen.getByText("Acceptance criteria")).toBeInTheDocument();
  });

  // A ghost row is disabled, and a set is only a ghost when NOT ONE of its
  // documents exists — with two written and a third planned the entry is real.
  it("stays live while one capability is still planned", () => {
    const nav = renderValidation(
      validationEntries("specs/validation/acceptance/adding-items.feature"),
      [
        { path: "specs/validation/acceptance/adding-items.feature", status: "done", section: "validation" },
        { path: "specs/validation/acceptance/bought-items.feature", status: "planned", section: "validation" },
      ],
    );
    const entry = within(nav)
      .getAllByRole("button", { hidden: true })
      .find((b) => b.textContent === "Acceptance criteria");
    expect(
      entry!.hasAttribute("disabled") || entry!.getAttribute("aria-disabled") === "true",
    ).toBe(false);
  });

  it("is a disabled ghost before any capability has been written", () => {
    const nav = renderValidation(
      [],
      [{ path: "specs/validation/acceptance/bought-items.feature", status: "planned", section: "validation" }],
    );
    // The attribute, not a click: MUI disables a ListItemButton with
    // `pointer-events: none`, which jsdom does not enforce — so asserting on a
    // click would pass whatever the row did.
    const entry = within(nav)
      .getAllByRole("button", { hidden: true })
      .find((b) => b.textContent === "Acceptance criteria");
    expect(entry).toBeDefined();
    expect(
      entry!.hasAttribute("disabled") || entry!.getAttribute("aria-disabled") === "true",
    ).toBe(true);
  });
});

