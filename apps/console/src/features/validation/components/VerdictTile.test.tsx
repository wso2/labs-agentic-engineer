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

import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { CriterionTally } from "@aep/ui-validation-view";
import { VerdictTile } from "./VerdictTile";

function tally(
  total: number,
  states: Record<string, number> = {},
): CriterionTally {
  return {
    total,
    states: Object.entries(states).map(([status, count]) => ({ status, count })),
  };
}

describe("VerdictTile", () => {
  it("leads with the shared mapper's label as its headline", () => {
    render(<VerdictTile verdict="partial" tally={tally(40, { pass: 35, manual: 5 })} />);
    // "validated*" in the mapper — the mark hedges what the sentence below spells
    // out; a headline leads, so it is capitalized here.
    expect(screen.getByText("Validated*")).toBeInTheDocument();
  });

  it("renders the counts under the sentence", () => {
    render(<VerdictTile verdict="passed" tally={tally(40, { pass: 40 })} />);
    expect(screen.getByText("40 passed")).toBeInTheDocument();
  });

  // Both green since #401: nothing about a partial run FAILED, and the tile's
  // own sentence + counts ("35 passed / 5 manual") carry the uncovered-criteria
  // hedge the old info tone and asterisk did.
  it("tones partial and passed as a success", () => {
    const { unmount } = render(<VerdictTile verdict="partial" />);
    expect(screen.getByRole("alert").className).toMatch(/Success/);
    unmount();
    render(<VerdictTile verdict="passed" />);
    expect(screen.getByRole("alert").className).toMatch(/Success/);
  });

  // A reporting failure that FAILS the run — so error, like a failing suite, and
  // distinctly worded from one.
  it("tones unreported as an error", () => {
    render(<VerdictTile verdict="unreported" />);
    expect(screen.getByRole("alert").className).toMatch(/Error/);
  });

  it("renders without a tally, before the report loads", () => {
    render(<VerdictTile verdict="failed" />);
    expect(screen.getByText("Validation failed")).toBeInTheDocument();
    expect(screen.getByText(/At least one criterion failed/)).toBeInTheDocument();
  });

  // The headline comes from `state` and the copy from `verdict`, because they answer
  // different questions mid-repair: what the platform is DOING, and what the last
  // attempt FOUND. Leading with the verdict is what made this tile announce a
  // terminal failure over a version the loop was actively repairing.
  it("leads with the loop's state, keeps the verdict's evidence, mid-repair", () => {
    render(
      <VerdictTile
        verdict="failed"
        state="awaiting-fix"
        tally={tally(40, { fail: 2, pass: 38 })}
      />,
    );
    expect(screen.getByText("Awaiting fix")).toBeInTheDocument();
    expect(screen.queryByText("Validation failed")).not.toBeInTheDocument();
    expect(screen.getByText(/2 of 40 criteria failed/)).toBeInTheDocument();
    // The counts stay: they are the evidence of what is being fixed.
    expect(screen.getByText("2 failed · 38 passed")).toBeInTheDocument();
  });

  // Warning, not error. The verdict is real but not final, and `error` here would
  // read as terminal — the same reason the shared mapper tones `awaiting-fix` this
  // way for the deployments board.
  it("tones a repair in flight as a warning, not an error", () => {
    render(<VerdictTile verdict="failed" state="awaiting-fix" />);
    expect(screen.getByRole("alert").className).toMatch(/Warning/);
  });

  // The tile still needs an ATTEMPT to speak for: `running` with no verdict yet has
  // no evidence to put a tile above, and the page shows the live log instead.
  it("renders nothing for a lifecycle state with no verdict behind it", () => {
    const { container } = render(<VerdictTile verdict="" state="awaiting-fix" />);
    expect(container.firstChild).toBeNull();
  });

  // skipped has its own empty state on the page — there is no report and no
  // criteria to put a tile above — and an unknown value must not render a shell.
  it("renders nothing for skipped, running, empty or unknown verdicts", () => {
    for (const v of ["skipped", "running", "", "something-new"]) {
      const { container, unmount } = render(<VerdictTile verdict={v} />);
      expect(container.firstChild, `rendered for ${v}`).toBeNull();
      unmount();
    }
  });

  // No Pass/Fail controls: the earlier design had them, and the no-human-gate
  // decision retired them. A verdict never waits on a person.
  it("offers no verdict controls", () => {
    render(<VerdictTile verdict="partial" tally={tally(40, { pass: 35, manual: 5 })} />);
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
});
