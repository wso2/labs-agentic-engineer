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

import { describe, expect, it } from "vitest";
import {
  mostSignificant,
  railSections,
  reasonCount,
  type RailInput,
  type RailSection,
} from "./railSections";

function input(over: Partial<RailInput> = {}): RailInput {
  return {
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
    ...over,
  };
}

const of = (sections: RailSection[], id: RailSection["id"]) =>
  sections.find((s) => s.id === id)!;

describe("railSections — the rail is the flow", () => {
  it("reads in journey order", () => {
    expect(railSections(input()).map((s) => s.id)).toEqual([
      "requirements",
      "design",
      "validation",
    ]);
  });

  // "Design", not "Designs" — one design, written across several documents.
  it("names the design section in the singular", () => {
    expect(of(railSections(input()), "design").title).toBe("Design");
  });

  it("a settled project is ready throughout", () => {
    for (const s of railSections(input())) {
      expect(s.state).toBe("ready");
      expect(s.reasons).toEqual([]);
    }
  });

  it("an empty section with nobody working has not started", () => {
    const sections = railSections(
      input({ hasRequirements: false, hasDesign: false, hasValidation: false }),
    );
    for (const s of sections) expect(s.state).toBe("not-started");
  });

  // The kickoff. Design and validation criteria have not begun and cannot until
  // requirements exist.
  it("pulses the section the running work targets", () => {
    const sections = railSections(
      input({
        hasRequirements: false,
        hasDesign: false,
        hasValidation: false,
        agentWorking: true,
        agentFlow: "start",
      }),
    );
    expect(of(sections, "requirements").state).toBe("active");
    expect(of(sections, "design").state).toBe("not-started");
    expect(of(sections, "validation").state).toBe("not-started");
  });

  // Guessing from emptiness lit Design here, because Design was the first empty
  // section — though the work was settling an assumption in the requirements.
  it("stays on requirements while an assumption is being settled", () => {
    const sections = railSections(
      input({ hasDesign: false, hasValidation: false, agentWorking: true, agentFlow: "settle" }),
    );
    expect(of(sections, "requirements").state).toBe("active");
    expect(of(sections, "design").state).toBe("not-started");
  });

  // And it jumped to Validation the moment a design run wrote its first file,
  // while the rest of the design was still being written.
  it("keeps pulsing the design after its first file lands", () => {
    const sections = railSections(
      input({ hasValidation: false, agentWorking: true, agentFlow: "design" }),
    );
    expect(of(sections, "design").state).toBe("active");
    expect(of(sections, "validation").state).toBe("not-started");
  });

  // An agent IS working, but nothing here can say where.
  it("pulses nothing for work it cannot place", () => {
    const sections = railSections(input({ agentWorking: true, agentFlow: "" }));
    for (const s of sections) expect(s.state).not.toBe("active");
  });

  // The turn that carries a member's interview answers is plain prose, not a
  // `/<skill>` command, so it has no flow — and it is the turn that writes the
  // first requirements document. Before that document exists a turn cannot be
  // writing anything else, so the rail claims Requirements by elimination
  // (#629). Unattributed, this turn left the workspace offering Retry against
  // work in flight.
  it("claims requirements for flowless work before requirements exist", () => {
    const sections = railSections(
      input({
        hasRequirements: false,
        hasDesign: false,
        hasValidation: false,
        agentWorking: true,
        agentFlow: "",
      }),
    );
    expect(of(sections, "requirements").state).toBe("active");
    expect(of(sections, "design").state).toBe("not-started");
    expect(of(sections, "validation").state).toBe("not-started");
  });

  // Same elimination for a flow this rail does not know — an org's own skill
  // kicking a project off is still requirements work while nothing exists.
  it("claims requirements for an unknown flow before requirements exist", () => {
    const sections = railSections(
      input({
        hasRequirements: false,
        hasDesign: false,
        hasValidation: false,
        agentWorking: true,
        agentFlow: "org-kickoff",
      }),
    );
    expect(of(sections, "requirements").state).toBe("active");
  });

  // The elimination argument needs the project to hold NOTHING. Design files
  // without a requirements-group file (a PRD renamed by an amend, an org skill
  // that wrote design first) mean a flowless turn could be touching either —
  // and a pulse on the wrong section is worse than a still rail.
  it("stays silent for flowless work when only the requirements are missing", () => {
    const sections = railSections(
      input({
        hasRequirements: false,
        hasDesign: true,
        hasValidation: false,
        agentWorking: true,
        agentFlow: "",
      }),
    );
    for (const s of sections) expect(s.state).not.toBe("active");
  });

  // The elimination argument dies with the first requirements file: a flowless
  // turn could then be touching requirements OR design, and guessing is what
  // this rail deliberately refuses to do.
  it("returns to silence for flowless work once requirements exist", () => {
    const sections = railSections(
      input({
        hasRequirements: true,
        hasDesign: false,
        hasValidation: false,
        agentWorking: true,
        agentFlow: "",
      }),
    );
    for (const s of sections) expect(s.state).not.toBe("active");
  });

  // No agent, no pulse — an empty project between turns is not-started, never
  // active, whatever the last turn's flow was.
  it("claims nothing by elimination while no agent is working", () => {
    const sections = railSections(
      input({
        hasRequirements: false,
        hasDesign: false,
        hasValidation: false,
        agentWorking: false,
      }),
    );
    expect(of(sections, "requirements").state).toBe("not-started");
  });

  // A turn is known project-wide, never per document. While every section
  // holds something there is no honest way to say which is being worked on,
  // and a pulse on the wrong section is worse than a still rail.


  describe("the requirements have moved since the design", () => {
    const sections = railSections(input({ designOutdated: true }));

    // The validation criteria are written against the same stories, and one
    // re-derivation rewrites both — so they go stale together.
    it("marks design AND validation, not requirements", () => {
      expect(of(sections, "design").state).toBe("attention");
      expect(of(sections, "validation").state).toBe("attention");
      expect(of(sections, "requirements").state).toBe("ready");
    });

    it("gives each one row pointing at the same repair", () => {
      for (const id of ["design", "validation"] as const) {
        expect(of(sections, id).reasons).toEqual([
          {
            key: "requirements-moved",
            label: "The requirements have changed since",
            count: 1,
            action: "update-design",
          },
        ]);
      }
    });

    // The agent is already resolving it; warning about the thing being fixed
    // while it is being fixed reads as a fault.
    it("yields to an agent that is working on it", () => {
      const working = railSections(
        input({ designOutdated: true, agentWorking: true, agentFlow: "design" }),
      );
      expect(of(working, "design").state).toBe("active");
    });

    // …but the reasons are RETAINED, and that is deliberate. Suppressing the
    // warning is a rendering rule and lives with the rail (`SpecFileList` gates
    // its chip on the state). The model stays a complete description because a
    // second reader depends on it: SpecView derives the Generate-design warning
    // from these counts, and its "an agent is busy" signal is room presence
    // while this one is the polled turn row — the two can disagree, so an
    // emptied list would silently drop the warning in that window and derive a
    // design against unsettled requirements without saying so.
    it("keeps the reasons on an active section for readers other than the rail", () => {
      const working = railSections(
        input({ designOutdated: true, agentWorking: true, agentFlow: "design" }),
      );
      expect(of(working, "design").reasons).toHaveLength(1);
    });

    it("keeps the requirements' reasons while they are being settled", () => {
      const working = railSections(
        input({ assumptions: 3, openQuestions: 1, agentWorking: true, agentFlow: "settle" }),
      );
      expect(of(working, "requirements").state).toBe("active");
      expect(reasonCount(of(working, "requirements").reasons)).toBe(4);
    });

    // A section with nothing in it has nothing to be stale.
    it("says nothing about a section that does not exist yet", () => {
      const none = railSections(input({ hasDesign: false, designOutdated: true }));
      expect(of(none, "design").state).toBe("not-started");
      expect(of(none, "design").reasons).toEqual([]);
    });
  });

  describe("the requirements' own reasons", () => {
    // Two different things: a judgment the agent made and you may overturn,
    // versus a hole only you can fill. Counted apart because the user does
    // different work on each.
    it("counts assumptions and open questions separately", () => {
      const sections = railSections(input({ assumptions: 3, openQuestions: 2 }));
      expect(of(sections, "requirements").state).toBe("attention");
      // Significance order: only the user can answer an open question, so
      // nothing else in the system can move it along. An assumption already
      // has an answer standing.
      expect(of(sections, "requirements").reasons.map((r) => r.label)).toEqual([
        "2 questions only you can answer",
        "3 decisions marked assumed",
      ]);
    });

    it("says one thing once", () => {
      const sections = railSections(input({ assumptions: 1, openQuestions: 1 }));
      expect(of(sections, "requirements").reasons.map((r) => r.label)).toEqual([
        "1 question only you can answer",
        "1 decision marked assumed",
      ]);
    });

    // The controls that settle them already live on the flagged lines; the rail
    // says there is something, the document is where it is done.
    it("points them at the document", () => {
      const sections = railSections(input({ assumptions: 1, openQuestions: 1 }));
      for (const r of of(sections, "requirements").reasons) {
        expect(r.action).toBe("document");
      }
    });

    // Designing against assumptions is deliberate — the requirements arrive
    // early, full of them, and are refined in place. The rail reports; it does
    // not gate.
    it("does not touch the design section", () => {
      const sections = railSections(input({ assumptions: 5, openQuestions: 5 }));
      expect(of(sections, "design").state).toBe("ready");
      expect(of(sections, "design").reasons).toEqual([]);
    });

    // Nothing to have assumptions about yet.
    it("stays quiet before the requirements exist", () => {
      const sections = railSections(input({ hasRequirements: false, assumptions: 4 }));
      expect(of(sections, "requirements").reasons).toEqual([]);
    });
  });
});

// The hover shows one thing, so which one has to be meaningful rather than
// arbitrary — it is the head of a list built in significance order.
describe("mostSignificant", () => {
  it("prefers the open question to the assumption", () => {
    const sections = railSections(input({ assumptions: 3, openQuestions: 1 }));
    expect(mostSignificant(of(sections, "requirements").reasons)?.key).toBe("open-questions");
  });

  it("falls through to the assumption when that is all there is", () => {
    const sections = railSections(input({ assumptions: 3 }));
    expect(mostSignificant(of(sections, "requirements").reasons)?.key).toBe("assumptions");
  });

  it("has nothing to say about a settled section", () => {
    expect(mostSignificant(of(railSections(input()), "requirements").reasons)).toBeUndefined();
  });
});

// The chip answers "how much is there to resolve", not "how many KINDS of
// thing" — which is a fact about our vocabulary rather than the user's work.
describe("reasonCount", () => {
  it("sums what the reasons stand for", () => {
    const sections = railSections(input({ assumptions: 3, openQuestions: 2 }));
    expect(reasonCount(of(sections, "requirements").reasons)).toBe(5);
  });

  it("counts a single-instance reason once", () => {
    const sections = railSections(input({ designOutdated: true }));
    expect(reasonCount(of(sections, "design").reasons)).toBe(1);
  });

  it("is nothing for a settled section", () => {
    expect(reasonCount(of(railSections(input()), "requirements").reasons)).toBe(0);
  });
});

// The declared plan (#576): the rail's checklist state — the count, the
// sharper active attribution, and the wreckage a dead turn leaves.
describe("the declared plan", () => {
  const entry = (
    path: string,
    status: "planned" | "writing" | "done" | "error",
    section: RailSection["id"] | null = "design",
  ) => ({ path, status, section });

  it("counts per section — done of total, only where the plan holds entries", () => {
    const sections = railSections(
      input({
        agentWorking: true,
        planEntries: [
          entry("specs/design/design.cell", "done"),
          entry("specs/design/domain-model.md", "writing"),
          entry("specs/design/components/a/design.json", "planned"),
          entry("specs/validation/validation-criteria.json", "planned", "validation"),
        ],
      }),
    );
    expect(of(sections, "design").progress).toEqual({ done: 1, total: 3 });
    expect(of(sections, "validation").progress).toEqual({ done: 0, total: 1 });
    expect(of(sections, "requirements").progress).toBeUndefined();
  });

  it("the writing entry names the active section, outranking the flow token", () => {
    const sections = railSections(
      input({
        agentWorking: true,
        agentFlow: "design",
        planEntries: [
          entry("specs/validation/validation-criteria.json", "writing", "validation"),
        ],
      }),
    );
    expect(of(sections, "validation").state).toBe("active");
    expect(of(sections, "design").state).toBe("ready");
  });

  it("wreckage is an attention reason pointing at the re-run", () => {
    const sections = railSections(
      input({
        planWreckage: true,
        planEntries: [
          entry("specs/design/design.cell", "done"),
          entry("specs/design/domain-model.md", "error"),
          entry("specs/design/components/a/design.json", "planned"),
        ],
      }),
    );
    const design = of(sections, "design");
    expect(design.state).toBe("attention");
    expect(design.reasons[0]).toMatchObject({
      key: "plan-unfinished",
      count: 2,
      action: "update-design",
    });
  });

  it("wreckage stands even on a section with nothing committed — the ghosts ARE the subject", () => {
    const sections = railSections(
      input({
        hasValidation: false,
        planWreckage: true,
        planEntries: [
          entry("specs/validation/validation-criteria.json", "planned", "validation"),
        ],
      }),
    );
    expect(of(sections, "validation").state).toBe("attention");
    expect(of(sections, "validation").reasons).toHaveLength(1);
  });

  it("a plan whose entries all landed adds a full count and no reason", () => {
    const sections = railSections(
      input({
        agentWorking: true,
        agentFlow: "design",
        planEntries: [entry("specs/design/design.cell", "done")],
      }),
    );
    expect(of(sections, "design").progress).toEqual({ done: 1, total: 1 });
    expect(of(sections, "design").reasons).toHaveLength(0);
  });
});
