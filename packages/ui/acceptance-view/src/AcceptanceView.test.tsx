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

import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AcceptanceView, type AcceptanceFeatureSource } from "./AcceptanceView.js";

/**
 * The scenario list, apart from the toolbar above it.
 *
 * The outcome words now appear twice on the page — once as a filter segment and
 * once as a row's pill — so an unscoped query for "Blocked" is ambiguous by
 * construction rather than by accident.
 */
const list = () => within(screen.getByRole("region", { name: "Acceptance scenarios" }));

/**
 * A step's text is split into spans so its quoted literals can be emphasised,
 * which defeats getByText on the whole sentence. This matches the one element
 * whose own text is the sentence.
 */
function step(text: string) {
  return (_: string, element: Element | null) =>
    element?.tagName === "SPAN" && element.textContent === text;
}

const BOUGHT: AcceptanceFeatureSource = {
  path: "specs/acceptance/bought-items.feature",
  content: [
    "Feature: Bought items",
    "",
    "  @story-6",
    "  Rule: A bought item is locked from further edits",
    "",
    "    @negative",
    "    Scenario: Editing a bought item is refused",
    '      Given the shared list has a bought item named "Eggs"',
    '      When Dev tries to change the quantity of "Eggs" to "2"',
    '      Then the quantity of "Eggs" is still "1"',
    "",
    "    Scenario: Marking an item bought",
    '      When Priya marks "Eggs" as bought',
    '      Then the list shows "Eggs" as bought',
  ].join("\n"),
};

const ADDING: AcceptanceFeatureSource = {
  path: "specs/acceptance/adding-items.feature",
  content: [
    "Feature: Adding items",
    "",
    "  @story-2",
    "  Rule: An item is added with a name and a quantity",
    "",
    "    Scenario: Adding a new item",
    '      When Priya adds "Milk"',
    '      Then the list shows "Milk"',
  ].join("\n"),
};

function report(scenarios: unknown[]): string {
  return JSON.stringify({
    schemaVersion: 2,
    commit: "4f2ad1088c7e",
    baseUrl: "http://shopping-list.localhost:19080/",
    isolation: "Each scenario created the list it asserts on.",
    scenarios,
  });
}

const BLOCKED = {
  feature: "Bought items",
  featureFile: "specs/acceptance/bought-items.feature",
  rule: "A bought item is locked from further edits",
  scenario: "Editing a bought item is refused",
  outcome: "blocked",
  steps: [
    { text: 'the shared list has a bought item named "Eggs"', keyword: "Given", command: "POST /items", exit: 0 },
    {
      text: 'Dev tries to change the quantity of "Eggs" to "2"',
      keyword: "When",
      command: "agent-browser wait --fn '(() => row.querySelectorAll(\"button\").length === 0)()'",
      exit: 0,
      observed: "the Actions cell is a literal em dash and the row holds zero buttons",
    },
  ],
};

const PASSED = {
  feature: "Bought items",
  featureFile: "specs/acceptance/bought-items.feature",
  rule: "A bought item is locked from further edits",
  scenario: "Marking an item bought",
  outcome: "passed",
  steps: [
    { text: 'Priya marks "Eggs" as bought', keyword: "When", command: "agent-browser click", exit: 0 },
    { text: 'the list shows "Eggs" as bought', keyword: "Then", command: "agent-browser wait", exit: 0 },
  ],
};

describe("the specification alone", () => {
  it("groups by feature, and counts rules, scenarios and refusals", () => {
    render(<AcceptanceView features={[BOUGHT, ADDING]} />);
    expect(screen.getByText("Bought items")).toBeInTheDocument();
    expect(screen.getByText("Adding items")).toBeInTheDocument();
    expect(screen.getByText("1 rule · 2 scenarios · 1 refusal")).toBeInTheDocument();
    expect(screen.getByText("1 rule · 1 scenario")).toBeInTheDocument();
    expect(screen.getByText("2 capabilities · 2 rules · 3 scenarios · 1 refusal")).toBeInTheDocument();
  });

  it("opens nothing by itself, and opens on a click", () => {
    render(<AcceptanceView features={[BOUGHT]} />);
    expect(screen.queryByText(step('Dev tries to change the quantity of "Eggs" to "2"'))).not.toBeInTheDocument();
    fireEvent.click(screen.getByText("Editing a bought item is refused"));
    expect(screen.getByText(step('Dev tries to change the quantity of "Eggs" to "2"'))).toBeInTheDocument();
    expect(screen.getByText("Given")).toBeInTheDocument();
  });

  it("carries no outcome chip where there is no run", () => {
    render(<AcceptanceView features={[BOUGHT]} />);
    expect(screen.queryByText("Passed")).not.toBeInTheDocument();
    expect(list().queryByText("No result")).not.toBeInTheDocument();
  });

  // It was an inline glyph with visually-hidden text; the reference design draws
  // tags as pills, and `@negative` is a tag — so it renders as one and becomes
  // filterable, which a glyph never was.
  it("marks a refusal with its own tag, which is also a filter", () => {
    render(<AcceptanceView features={[BOUGHT]} />);
    expect(list().getByText("@negative")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "@negative" })).toBeInTheDocument();
  });

  // @negative inherits Feature -> Rule -> Scenario, so a scenario under a
  // prohibition rule HAS the property without carrying the tag. Reading the raw
  // tags showed 22 of this repo's 77 refusals nothing at all — on the one mark
  // whose job is "are the refusals covered".
  it("marks a refusal that inherits the tag from its rule", () => {
    const inherited: AcceptanceFeatureSource = {
      path: "specs/acceptance/locked.feature",
      content: [
        "Feature: Locked items",
        "",
        "  @story-6 @negative",
        "  Rule: A bought item cannot be changed",
        "",
        "    Scenario: Changing a bought item is refused",
        "      Then the quantity is unchanged",
      ].join("\n"),
    };
    render(<AcceptanceView features={[inherited]} />);

    // Once, on the row — although the scenario's own tag list is empty, and
    // although it is the RULE that carries the tag in the file.
    expect(list().getAllByText("@negative")).toHaveLength(1);
    // The rule band keeps its reference and drops the property.
    expect(list().getByText("@story-6")).toBeInTheDocument();
  });

  // The step count held the flush-right column the outcome pill wants, answered
  // no question a reader has, and on a blocked scenario counted steps that never
  // ran.
  it("prints no step count on a row", () => {
    render(<AcceptanceView features={[BOUGHT]} report={report([BLOCKED, PASSED])} />);
    expect(screen.queryByText(/\d+ steps?$/)).not.toBeInTheDocument();
  });

  // Emphasis, not hue: every hue on this page is spoken for, and amber
  // especially would put a refusal SCENARIO in the same bucket as a BLOCKED one.
  it("tells a refusal apart from a story reference", () => {
    const tagged: AcceptanceFeatureSource = {
      path: "specs/acceptance/tagged.feature",
      content: [
        "Feature: Tagged",
        "",
        "  @story-6",
        "  Rule: A rule",
        "",
        "    @negative",
        "    Scenario: A refusal",
        "      Then nothing changes",
      ].join("\n"),
    };
    render(<AcceptanceView features={[tagged]} />);

    const negative = list().getByText("@negative");
    const story = list().getByText("@story-6");
    expect(getComputedStyle(negative).backgroundColor).not.toBe(
      getComputedStyle(story).backgroundColor,
    );
  });
});

describe("joined against a run", () => {
  it("chips each scenario with the report's own word", () => {
    render(<AcceptanceView features={[BOUGHT]} report={report([BLOCKED, PASSED])} />);
    expect(list().getByText("Blocked")).toBeInTheDocument();
    expect(list().getByText("Passed")).toBeInTheDocument();
  });

  it("renders an outcome word it does not know rather than mislabelling it", () => {
    render(
      <AcceptanceView features={[BOUGHT]} report={report([{ ...BLOCKED, outcome: "abandoned" }, PASSED])} />,
    );
    expect(list().getByText("Abandoned")).toBeInTheDocument();
  });

  it("says why a non-passed scenario is not passed without being opened", () => {
    render(<AcceptanceView features={[BOUGHT]} report={report([BLOCKED, PASSED])} />);
    expect(
      screen.getByText("the Actions cell is a literal em dash and the row holds zero buttons"),
    ).toBeInTheDocument();
  });

  it("stops a blocked scenario at the step the run reached", () => {
    render(<AcceptanceView features={[BOUGHT]} report={report([BLOCKED, PASSED])} />);
    fireEvent.click(screen.getByText("Editing a bought item is refused"));
    // The report has two steps; the feature file declares three, and the third
    // is specification the run never got to.
    expect(screen.getByText(step('the quantity of "Eggs" is still "1"'))).toBeInTheDocument();
    expect(screen.getByText("not reached")).toBeInTheDocument();
  });

  it("marks a nonzero exit and leaves a zero one unmarked", () => {
    const failed = {
      ...PASSED,
      outcome: "failed",
      steps: [
        { text: 'Priya marks "Eggs" as bought', keyword: "When", command: "agent-browser click", exit: 0 },
        {
          text: 'the list shows "Eggs" as bought',
          keyword: "Then",
          command: 'agent-browser get count "tbody tr"',
          exit: 1,
          observed: "2 — the list holds two rows",
        },
      ],
    };
    render(<AcceptanceView features={[BOUGHT]} report={report([BLOCKED, failed])} />);
    fireEvent.click(screen.getByText("Marking an item bought"));
    expect(screen.getByText("exit 1")).toBeInTheDocument();
    expect(screen.queryByText("exit 0")).not.toBeInTheDocument();
  });

  it("says No result for a scenario written since the run", () => {
    render(<AcceptanceView features={[BOUGHT, ADDING]} report={report([BLOCKED, PASSED])} />);
    expect(list().getByText("No result")).toBeInTheDocument();
  });

  it("withholds No result while an attempt is still in flight", () => {
    render(
      <AcceptanceView features={[BOUGHT, ADDING]} report={report([BLOCKED, PASSED])} awaitingReport />,
    );
    expect(list().queryByText("No result")).not.toBeInTheDocument();
  });

  it("keeps a scenario the run answered that the specification no longer declares", () => {
    const gone = { ...PASSED, scenario: "Clearing bought items", rule: "All bought items can be cleared" };
    render(<AcceptanceView features={[BOUGHT]} report={report([BLOCKED, PASSED, gone])} />);
    expect(screen.getByText("NOT IN THE CURRENT SPECIFICATION")).toBeInTheDocument();
    expect(screen.getByText("Clearing bought items")).toBeInTheDocument();
  });

  // The tally belongs to the verdict that explains it, which the consumer
  // renders above — a second copy here says the same numbers twice. The run's
  // identity (commit, deploy URL, timestamp) and its isolation statement go with
  // it: the run is still HELD to isolation by check-report.mjs, the reader is
  // simply not shown it.
  it("says nothing about the run itself — that belongs to the page", () => {
    render(<AcceptanceView features={[BOUGHT]} report={report([BLOCKED, PASSED])} />);

    expect(screen.queryByText(/4f2ad10/)).not.toBeInTheDocument();
    expect(screen.queryByText(/How the scenarios were kept apart/)).not.toBeInTheDocument();
    expect(screen.queryByText(/Each scenario created the list/)).not.toBeInTheDocument();
    // And no tally: the page's verdict tile owns those numbers.
    expect(screen.queryByText(/1 of 2 passed/)).not.toBeInTheDocument();
  });

  it("drops to the specification with a warning when the report cannot be read", () => {
    render(<AcceptanceView features={[BOUGHT]} report="{ not json" />);
    expect(screen.getByText(/could not be read/)).toBeInTheDocument();
    expect(screen.getByText("Editing a bought item is refused")).toBeInTheDocument();
    expect(list().queryByText("No result")).not.toBeInTheDocument();
  });
});

describe("with nothing to show", () => {
  it("says the criteria have not been written yet", () => {
    render(<AcceptanceView features={[]} />);
    expect(screen.getByText("No acceptance criteria yet")).toBeInTheDocument();
  });
});

describe("the toolbar", () => {
  const search = () => screen.getByRole("textbox", { name: "Filter scenarios" });

  it("narrows on a scenario name", () => {
    render(<AcceptanceView features={[BOUGHT, ADDING]} />);
    fireEvent.change(search(), { target: { value: "marking" } });
    expect(list().getByText("Marking an item bought")).toBeInTheDocument();
    expect(list().queryByText("Adding a new item")).not.toBeInTheDocument();
  });

  // The step text is the point: a reader looking for where "Eggs" is asserted
  // will not remember which scenario name it lives under.
  it("narrows on the text of a step, not just the name", () => {
    render(<AcceptanceView features={[BOUGHT, ADDING]} />);
    // "Eggs" appears in no scenario name, no rule and no tag — only in steps.
    fireEvent.change(search(), { target: { value: "eggs" } });
    expect(list().getByText("Editing a bought item is refused")).toBeInTheDocument();
    expect(list().queryByText("Adding a new item")).not.toBeInTheDocument();
  });

  // The rule is part of the haystack too: it is on screen above the scenario,
  // so a reader who searches for a phrase they can see expects a hit.
  it("narrows on the rule a scenario sits under", () => {
    render(<AcceptanceView features={[BOUGHT, ADDING]} />);
    fireEvent.change(search(), { target: { value: "locked from further edits" } });
    expect(list().getByText("Editing a bought item is refused")).toBeInTheDocument();
    expect(list().queryByText("Adding a new item")).not.toBeInTheDocument();
  });

  it("says how much it is hiding, and shows the total when it is hiding nothing", () => {
    render(<AcceptanceView features={[BOUGHT, ADDING]} />);
    expect(screen.getByText("3 scenarios")).toBeInTheDocument();
    fireEvent.change(search(), { target: { value: "marking" } });
    expect(screen.getByText("1 of 3 scenarios match")).toBeInTheDocument();
  });

  it("clears the query from the field's own button", () => {
    render(<AcceptanceView features={[BOUGHT, ADDING]} />);
    fireEvent.change(search(), { target: { value: "marking" } });
    fireEvent.click(screen.getByRole("button", { name: "Clear the filter" }));
    expect(list().getByText("Adding a new item")).toBeInTheDocument();
  });

  it("narrows on a tag", () => {
    render(<AcceptanceView features={[BOUGHT, ADDING]} />);
    fireEvent.click(screen.getByRole("button", { name: "@negative" }));
    expect(list().getByText("Editing a bought item is refused")).toBeInTheDocument();
    expect(list().queryByText("Marking an item bought")).not.toBeInTheDocument();
  });

  it("resets every filter at once", () => {
    render(<AcceptanceView features={[BOUGHT, ADDING]} />);
    fireEvent.change(search(), { target: { value: "refused" } });
    fireEvent.click(screen.getByRole("button", { name: "@negative" }));
    fireEvent.click(screen.getByRole("button", { name: "Reset" }));
    expect(screen.getByText("3 scenarios")).toBeInTheDocument();
    expect(list().getByText("Adding a new item")).toBeInTheDocument();
  });

  it("offers a way back when the filters match nothing", () => {
    render(<AcceptanceView features={[BOUGHT, ADDING]} />);
    fireEvent.change(search(), { target: { value: "nothing matches this" } });
    expect(screen.getByText("No matching scenarios")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Clear filters" }));
    expect(list().getByText("Adding a new item")).toBeInTheDocument();
  });

  it("opens and closes every scenario at once", () => {
    render(<AcceptanceView features={[BOUGHT, ADDING]} />);
    const open = () => list().queryAllByRole("button", { expanded: true }).length;
    const shut = () => list().queryAllByRole("button", { expanded: false }).length;

    // Three scenarios, and the two group headers are open from the start.
    expect(open()).toBe(2);
    expect(shut()).toBe(3);

    fireEvent.click(screen.getByRole("button", { name: /Expand all/ }));
    expect(shut()).toBe(0);
    expect(
      screen.getByText(step('Dev tries to change the quantity of "Eggs" to "2"')),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /Collapse all/ }));
    expect(shut()).toBe(3);
  });

  // Without a run there is nothing to filter outcomes by, so the control is
  // absent rather than present and inert.
  it("offers no outcome filter where there is no run", () => {
    render(<AcceptanceView features={[BOUGHT, ADDING]} />);
    expect(screen.queryByRole("group", { name: "Filter by outcome" })).not.toBeInTheDocument();
  });

  it("offers only the outcomes the run produced, and narrows on them", () => {
    render(<AcceptanceView features={[BOUGHT, ADDING]} report={report([BLOCKED, PASSED])} />);
    const group = within(screen.getByRole("group", { name: "Filter by outcome" }));
    expect(group.getByRole("button", { name: "Blocked" })).toBeInTheDocument();
    expect(group.queryByRole("button", { name: "Failed" })).not.toBeInTheDocument();

    fireEvent.click(group.getByRole("button", { name: "Blocked" }));
    expect(list().getByText("Editing a bought item is refused")).toBeInTheDocument();
    expect(list().queryByText("Marking an item bought")).not.toBeInTheDocument();
  });
});

// The shell's width contract, which a restyle silently broke: `fullWidth` means
// the PAGE owns its width, and capping it anyway left the report card short of
// the verdict tile above it — an Alert, and uncapped.
//
// Testable without a layout engine, unlike most geometry here: emotion injects
// its rules into document.head and jsdom's getComputedStyle resolves them.
describe("how wide the view lets itself be", () => {
  it("takes the width it is given when the page owns it", () => {
    const { container } = render(<AcceptanceView features={[BOUGHT]} fullWidth />);
    const inner = container.firstElementChild?.firstElementChild as HTMLElement;
    expect(getComputedStyle(inner).maxWidth).toBe("");
  });

  it("centres a reading column when it owns its own width", () => {
    const { container } = render(<AcceptanceView features={[BOUGHT]} />);
    const inner = container.firstElementChild?.firstElementChild as HTMLElement;
    expect(getComputedStyle(inner).maxWidth).toBe("1080px");
    expect(getComputedStyle(inner).marginLeft).toBe("auto");
  });
});

