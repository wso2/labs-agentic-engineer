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
import { describe, expect, it } from "vitest";
import { ValidationView } from "./ValidationView.js";

const AGENT = "Validated automatically by the agent.";
const HUMAN = "Requires manual validation.";

// REQ-002 is absent on purpose: ids are stable by contract, so a deleted
// requirement leaves a gap, and the numbers have to read as names rather than as
// list positions.
const CRITERIA = JSON.stringify({
  requirements: [
    {
      id: "REQ-001",
      statement: "A visitor can reset a forgotten password.",
      criteria: [
        {
          id: "AC-001-a",
          must: "A registered email receives a reset link",
          method: "e2e",
        },
        {
          id: "AC-001-b",
          must: "The reset confirmation reads clearly",
          method: "manual",
        },
      ],
    },
    {
      id: "REQ-003",
      statement: "Passwords meet the complexity policy.",
      criteria: [
        { id: "AC-003-a", must: "A short password is rejected", method: "e2e" },
        { id: "AC-003-b", must: "The policy copy is accurate", method: "scenario" },
      ],
    },
  ],
});

function reportOf(criteria: unknown[]): string {
  return JSON.stringify({ schemaVersion: 1, criteria });
}

/** Every criterion answered, so no row drifts. */
const FULL_REPORT = reportOf([
  { id: "AC-001-a", status: "pass" },
  { id: "AC-001-b", status: "manual" },
  { id: "AC-003-a", status: "fail", failure: { message: "boom", location: "" } },
  { id: "AC-003-b", status: "not_validated" },
]);

describe("ValidationView — the Spec view's pane (no run attached)", () => {
  it("marks who checks each criterion, with no run signal anywhere", () => {
    render(<ValidationView criteria={CRITERIA} />);
    // Two e2e criteria take the agent glyph; manual AND the legacy `scenario`
    // both fall to the person, because neither is ever automated.
    expect(screen.getAllByText(AGENT)).toHaveLength(2);
    expect(screen.getAllByText(HUMAN)).toHaveLength(2);
    // Nothing here may name a run that does not exist.
    for (const word of ["Passed", "Failed", "Pending", "Manual", "No result"]) {
      expect(screen.queryByText(word)).not.toBeInTheDocument();
    }
  });

  it("shortens both id levels to what the card does not already say", () => {
    render(<ValidationView criteria={CRITERIA} />);
    expect(screen.getByText("1")).toBeInTheDocument();
    expect(screen.getByText("3")).toBeInTheDocument();
    expect(screen.getAllByText("a")).toHaveLength(2);
    expect(screen.getAllByText("b")).toHaveLength(2);
    // The full id is the hover handle, so it is not on the page as text.
    expect(screen.queryByText("AC-001-a")).not.toBeInTheDocument();
    expect(screen.queryByText("REQ-001")).not.toBeInTheDocument();
  });

  it("drops the summary line, the method words and the criteria count", () => {
    render(<ValidationView criteria={CRITERIA} />);
    expect(screen.queryByText(/requirements ·/)).not.toBeInTheDocument();
    expect(screen.queryByText("2 criteria")).not.toBeInTheDocument();
    // The solid AUTO / MANUAL badges are gone with it.
    expect(screen.queryByText("auto")).not.toBeInTheDocument();
    expect(screen.queryByText("manual")).not.toBeInTheDocument();
  });

  it("explains a mark on hover", async () => {
    render(<ValidationView criteria={CRITERIA} />);

    // Tooltip listens on the glyph's wrapper — the hidden phrase's parent. Asserted
    // through the tooltip's own role rather than a text count: the phrase is
    // already on the page as hidden text, so findAllByText would resolve on that
    // and never wait for the popper.
    fireEvent.mouseOver(screen.getAllByText(AGENT)[0]!.parentElement!);
    expect(await screen.findByRole("tooltip")).toHaveTextContent(AGENT);
  });

  it("keeps the full id one hover away", async () => {
    render(<ValidationView criteria={CRITERIA} />);

    fireEvent.mouseOver(screen.getAllByText("a")[0]!);
    expect(await screen.findByRole("tooltip")).toHaveTextContent("AC-001-a");
  });

  it("renders an id that breaks the convention in full", () => {
    render(
      <ValidationView
        criteria={JSON.stringify({
          requirements: [
            {
              id: "legacy-req",
              statement: "An older oracle, hand-edited.",
              criteria: [{ id: "check-one", must: "It still renders", method: "e2e" }],
            },
          ],
        })}
      />,
    );
    expect(screen.getByText("legacy-req")).toBeInTheDocument();
    expect(screen.getByText("check-one")).toBeInTheDocument();
  });
});

describe("ValidationView — the Validations page (a report joined in)", () => {
  it("carries the verdict as well as the method, not instead of it", () => {
    render(<ValidationView criteria={CRITERIA} report={FULL_REPORT} />);
    expect(screen.getByText("Passed")).toBeInTheDocument();
    expect(screen.getByText("Failed")).toBeInTheDocument();
    expect(screen.getByText("Manual")).toBeInTheDocument();
    expect(screen.getByText("Not validated")).toBeInTheDocument();
    // The glyphs survive a run: who CHECKS a criterion is a standing property of
    // it, and a reader scanning a page of results for their own work needs it in
    // the same fixed column it occupies with no run attached.
    expect(screen.getAllByText(AGENT)).toHaveLength(2);
    expect(screen.getAllByText(HUMAN)).toHaveLength(2);
  });

  it("chips a criterion the pinned report predates", () => {
    // The consumer reads criteria at the branch tip and the report at the merge
    // commit that wrote it, so a criterion authored since has no row.
    render(
      <ValidationView
        criteria={CRITERIA}
        report={reportOf([{ id: "AC-001-a", status: "pass" }])}
      />,
    );
    expect(screen.getAllByText("No result")).toHaveLength(3);
    expect(screen.getByText("Passed")).toBeInTheDocument();
  });

  it("falls back to method marks when the report will not parse", () => {
    // hasRun is read from the PARSED statuses, not from the prop: keying off the
    // prop would tell the reader all four criteria were authored after the last
    // run, when the truth is that the file is unreadable.
    render(<ValidationView criteria={CRITERIA} report="{ not json" />);
    expect(
      screen.getByText(/Couldn't parse the validation report/),
    ).toBeInTheDocument();
    expect(screen.getAllByText(AGENT)).toHaveLength(2);
    expect(screen.getAllByText(HUMAN)).toHaveLength(2);
    expect(screen.queryByText("No result")).not.toBeInTheDocument();
  });

  it("says what is about to happen while a first attempt is in flight", () => {
    render(<ValidationView criteria={CRITERIA} awaitingReport />);
    // Only the two e2e criteria are the run's to answer, so only they are told to
    // wait for it. The other two already have their final word — which is the
    // point of routing both through runAnswers: a `scenario` criterion marked as
    // a person's job on the Spec view cannot be promised an agent result here.
    expect(screen.getAllByText("Pending")).toHaveLength(2);
    expect(screen.getByText("Manual")).toBeInTheDocument();
    expect(screen.getByText("Not validated")).toBeInTheDocument();
    // Nothing has drifted — there is no report to have missed them.
    expect(screen.queryByText("No result")).not.toBeInTheDocument();
  });

  // The run works on a `scenario` criterion — explores it, authors a spec, runs
  // it — and still reports `not_validated` for it. So it emits progress for a row
  // it will never answer, and the console's run-wide line counts that row. A row
  // that refused the status would contradict the line directly above it.
  it("shows live progress for a criterion the run works on but cannot answer", () => {
    render(
      <ValidationView
        criteria={CRITERIA}
        report={FULL_REPORT}
        live={{ "AC-003-b": "running" }}
      />,
    );
    expect(screen.getByText("Running…")).toBeInTheDocument();
    // Not the pinned report's word for it, which the live status outranks.
    expect(screen.queryByText("Not validated")).not.toBeInTheDocument();
  });

  it("lets a live status outrank the previous attempt's verdict", () => {
    render(
      <ValidationView
        criteria={CRITERIA}
        report={FULL_REPORT}
        live={{ "AC-001-a": "healing", "AC-001-b": "planned" }}
      />,
    );
    expect(screen.getByText("Healing…")).toBeInTheDocument();
    // A manual criterion never takes a live status: the run will never answer it.
    expect(screen.queryByText("Planned")).not.toBeInTheDocument();
    expect(screen.getByText("Manual")).toBeInTheDocument();
  });
});

describe("ValidationView — the terminal verdicts carry an icon", () => {
  /** The chip element wrapping a label, and whether it renders an icon. */
  function hasIcon(label: string): boolean {
    const chip = screen.getByText(label).closest(".MuiChip-root");
    return chip?.querySelector("svg") != null;
  }

  it("separates pass from fail by more than colour", () => {
    // Outlined chips in success/error differ only in hue without these, which is
    // nothing to a red/green colour-blind reader — and pass/fail is the one pair
    // that has to be told apart at a glance.
    render(<ValidationView criteria={CRITERIA} report={FULL_REPORT} />);
    expect(hasIcon("Passed")).toBe(true);
    expect(hasIcon("Failed")).toBe(true);
  });

  it("leaves the non-answers unmarked", () => {
    // Everything else is the ABSENCE of a verdict rather than one of them, so a
    // mark would compete for the distinction the pair above needs.
    render(<ValidationView criteria={CRITERIA} report={FULL_REPORT} />);
    expect(hasIcon("Manual")).toBe(false);
    expect(hasIcon("Not validated")).toBe(false);
  });
});

describe("ValidationView — flaky and healed ride the verdict", () => {
  function noteFor(entry: Record<string, unknown>): string {
    render(
      <ValidationView criteria={CRITERIA} report={reportOf([{ id: "AC-001-a", ...entry }])} />,
    );
    return screen.getByText(/the test/).textContent ?? "";
  }

  it("marks a flaky pass", () => {
    expect(noteFor({ status: "pass", flaky: true })).toBe(
      "Passed, but the test was flaky.",
    );
    expect(screen.getByText("Passed*")).toBeInTheDocument();
  });

  it("marks a healed pass", () => {
    expect(noteFor({ status: "pass", healed: true })).toBe(
      "Passed after the agent repaired the test.",
    );
    expect(screen.getByText("Passed*")).toBeInTheDocument();
  });

  it("marks a pass that was both", () => {
    expect(noteFor({ status: "pass", flaky: true, healed: true })).toBe(
      "Passed, but the test was flaky, and the agent repaired it.",
    );
    expect(screen.getByText("Passed*")).toBeInTheDocument();
  });

  it("marks a failure the agent tried to repair", () => {
    // `healed` is set before the status is decided, so this row is real.
    expect(noteFor({ status: "fail", healed: true })).toBe(
      "Failed. The agent tried to repair the test.",
    );
    expect(screen.getByText("Failed*")).toBeInTheDocument();
  });

  it("leaves an unqualified verdict unmarked, and keeps no separate chips", () => {
    render(<ValidationView criteria={CRITERIA} report={FULL_REPORT} />);
    expect(screen.getByText("Passed")).toBeInTheDocument();
    expect(screen.queryByText("Passed*")).not.toBeInTheDocument();
    expect(screen.queryByText("flaky")).not.toBeInTheDocument();
    expect(screen.queryByText("healed")).not.toBeInTheDocument();
  });
});
