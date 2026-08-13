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
import { VerdictTile, verdictCounts, verdictSentence } from "./VerdictTile";

function tally(
  total: number,
  states: Record<string, number> = {},
): CriterionTally {
  return {
    total,
    states: Object.entries(states).map(([status, count]) => ({ status, count })),
  };
}

describe("verdictSentence", () => {
  // `passed` now REQUIRES full coverage, so its sentence must say so — claiming
  // only "everything passed" is what let a green banner sit over criteria nobody
  // had checked.
  it("passed names coverage, not just the result", () => {
    expect(verdictSentence("passed", tally(40, { pass: 40 }))).toBe(
      "All 40 validation criteria were covered by a test and passed.",
    );
  });

  // The pair the vocabulary exists for. The numbers are the whole point: without
  // them "Validated*" leaves the reader asking which part.
  it("partial counts the uncovered criteria against the authored total", () => {
    const t = tally(40, { pass: 35, manual: 3, not_run: 2 });
    expect(verdictSentence("partial", t)).toBe(
      "Everything that ran passed, but 5 of 40 validation criteria couldn't be automated — please validate them manually.",
    );
  });

  it("partial inflects for a single uncovered criterion", () => {
    expect(verdictSentence("partial", tally(40, { pass: 39, manual: 1 }))).toContain(
      "1 of 40 validation criteria couldn't be automated — please validate it manually",
    );
  });

  // A failing verdict now ENDS the run, which is a consequence no chip can state —
  // so the sentence has to carry it.
  it("failed counts the failures and states what it did to the run", () => {
    const s = verdictSentence("failed", tally(40, { fail: 2, pass: 38 }));
    expect(s).toContain("2 of 40 criteria failed");
    expect(s).toContain("they are marked below");
    expect(s).toContain("the milestone stays open for the fix");
  });

  it("failed inflects for a single failure", () => {
    expect(verdictSentence("failed", tally(40, { fail: 1, pass: 39 }))).toContain(
      "it is marked below",
    );
  });

  // The consequence clause is the half that goes STALE mid-loop: a fatal verdict on
  // a live run is not the run's answer, and "the run stopped here" would tell a
  // reader the version was abandoned while the platform is repairing it. The
  // evidence half — what the report found — is unchanged, because that much is
  // still true.
  it("failed keeps its evidence but not its ending while a repair is in flight", () => {
    const t = tally(40, { fail: 2, pass: 38 });
    const s = verdictSentence("failed", t, "awaiting-fix");
    expect(s).toContain("2 of 40 criteria failed");
    expect(s).not.toContain("The run stopped here");
    expect(s).toContain("validates again once the fix is deployed");
    // The run is the subject here as it is in every other run sentence a reader
    // meets — a second actor would read as a second thing acting.
    expect(s).toContain("The run filed each failure as an issue");
  });

  it("failed says a repeat attempt is running while one is", () => {
    const s = verdictSentence("failed", tally(40, { fail: 2, pass: 38 }), "running");
    expect(s).toContain("2 of 40 criteria failed");
    expect(s).not.toContain("The run stopped here");
    expect(s).toContain("A new validation attempt is running");
  });

  // `unreported` gets its OWN mid-loop sentence rather than the failed one's ending:
  // the platform files nothing for it (there is no failing criterion to turn into
  // work), so promising a fix would name work that does not exist.
  it("unreported promises a retry, never a fix, while the loop repeats it", () => {
    const s = verdictSentence("unreported", undefined, "awaiting-fix");
    expect(s).toContain("the run is validating again");
    expect(s).not.toContain("fix");
    expect(s).not.toContain("there are no results to show for this run");
  });

  // A state is only honoured for the two verdicts the loop repeats. Everything else
  // is final the moment it is written, so its copy must not acquire a mid-loop
  // ending from a lifecycle value that could only be stale.
  it("leaves a final verdict's sentence alone whatever state it is given", () => {
    for (const v of ["passed", "partial", "inconclusive"]) {
      const t = tally(40, { pass: 35, manual: 5 });
      expect(verdictSentence(v, t, "awaiting-fix"), `${v} changed mid-loop`).toBe(
        verdictSentence(v, t),
      );
    }
  });

  it("inconclusive asks for manual validation", () => {
    expect(verdictSentence("inconclusive", tally(12, { manual: 12 }))).toBe(
      "None of the 12 validation criteria could be automated — please validate them manually.",
    );
  });

  // Not a test outcome but a reporting failure, so the sentence says so instead of
  // reading as a failing suite — and it must never quote the terminal reason, which
  // is a wire value, not something to hand a reader.
  it("unreported names the reporting failure, never the terminal reason", () => {
    const s = verdictSentence("unreported", undefined);
    expect(s).toContain("generating the validation report");
    expect(s).not.toContain("validation-unreported");
  });

  // The tile renders before the report loads, and `unreported` has no report at
  // all — every sentence must still read as a whole sentence.
  it("every verdict degrades to a count-free sentence", () => {
    for (const v of ["passed", "partial", "failed", "inconclusive", "unreported"]) {
      const s = verdictSentence(v, undefined);
      expect(s, `no sentence for ${v}`).not.toBe("");
      expect(s, `${v} leaked a count`).not.toMatch(/\d+ of \d+|All 0|the 0 /);
    }
  });

  // A total of one would force verb agreement on every numbered form, so the
  // numbered forms are gated on total > 1 rather than inflected six ways.
  it("skips the numbers for a single-criterion oracle", () => {
    expect(verdictSentence("passed", tally(1, { pass: 1 }))).toBe(
      "Every validation criterion was covered by a test and passed.",
    );
  });

  it("is empty for a verdict it does not speak for", () => {
    expect(verdictSentence("skipped", tally(0))).toBe("");
    expect(verdictSentence("", undefined)).toBe("");
  });
});

describe("verdictCounts", () => {
  it("reads as a run-on line, lowercased", () => {
    expect(verdictCounts(tally(40, { fail: 2, pass: 35, manual: 3 }))).toBe(
      "2 failed · 35 passed · 3 manual",
    );
  });

  it("names an unknown status verbatim rather than dropping it", () => {
    expect(verdictCounts(tally(1, { quarantined: 1 }))).toBe("1 quarantined");
  });

  it("is empty with no report and with no tally", () => {
    expect(verdictCounts(tally(40))).toBe("");
    expect(verdictCounts(undefined)).toBe("");
  });
});

describe("VerdictTile", () => {
  it("leads with the shared mapper's label as its headline", () => {
    render(<VerdictTile verdict="partial" tally={tally(40, { pass: 35, manual: 5 })} />);
    // "validated" in the mapper (green since #401; the tile's copy carries
    // the uncovered-criteria hedge); a headline leads.
    expect(screen.getByText("Validated")).toBeInTheDocument();
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
