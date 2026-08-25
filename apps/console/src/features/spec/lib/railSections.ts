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

// The rail IS the flow (#575).
//
// The spec workspace's left column is the one surface a user reads through the
// whole journey, and it used to be a file browser: three dead headings over a
// list of filenames, saying nothing about which part of the work was happening,
// finished, or still to come. This decides what each section says.
//
// Pure — no React, no queries — so the rules are testable without a workspace.

/** What a section is doing, in the order a reader meets them. */
export type SectionState = "not-started" | "active" | "attention" | "ready";

/**
 * Why a section wants attention, and where the user goes to deal with it.
 *
 * Surfaced as an alert BUTTON on the section header, opening a dialog that
 * lists them — not as rows beneath the title, which was tried first and cost
 * the rail up to three extra lines before the user reached the documents, in a
 * column 280px wide. One button costs one slot however many problems there are,
 * and unlike a bare tooltip it is discoverable at rest: the hover is a shortcut
 * on top of an affordance, not a replacement for one.
 *
 * ORDERED BY SIGNIFICANCE, most consequential first — the hover shows the head
 * of the list, so the order is what makes that pick meaningful rather than
 * arbitrary. Ranked by how badly it hurts to ignore: a design behind its
 * requirements blocks the build and ships the wrong software if forced; an open
 * question is a hole only the user can fill, so nothing else can resolve it; an
 * assumption already HAS an answer in place, which the user may or may not
 * disagree with.
 */
export interface SectionReason {
  /** Stable across renders — the dialog keys rows on it. */
  key: string;
  label: string;
  /**
   * HOW MANY things this reason stands for — three assumptions is three, not
   * one. The chip sums these rather than counting reasons, because the user is
   * being told how much there is to resolve, not how many kinds of thing it
   * falls into.
   */
  count: number;
  /** `document` opens the requirements document, where the settle controls
   *  already live on the flagged lines; `update-design` re-derives. */
  action: "document" | "update-design";
}

export interface RailSection {
  id: "requirements" | "design" | "validation";
  title: string;
  state: SectionState;
  reasons: SectionReason[];
}

export interface RailInput {
  /** Does this section hold anything yet. */
  hasRequirements: boolean;
  hasDesign: boolean;
  hasValidation: boolean;
  /** An agent turn is running somewhere on this project. */
  agentWorking: boolean;
  /** WHICH work is running — the flow token (`start`, `design`, `settle`, …);
   *  "" for plain chat or nothing. */
  agentFlow: string;
  /** The requirements moved since the design was last derived from them. */
  designOutdated: boolean;
  /** Judgments the agent made that the user may want to challenge. */
  assumptions: number;
  /** Gaps only the user can fill. */
  openQuestions: number;
}

const REQUIREMENTS_MOVED = "The requirements have changed since";

/**
 * Which section a running turn is changing.
 *
 * `start` opens the requirements interview; `settle` and `amend` revise that
 * same document; `design` derives the design — and the acceptance criteria with
 * it, but Design is the section named because it is where the work visibly
 * lands and where the user goes to watch it.
 *
 * Guarded lookup, never indexed directly: the flow is a token a user can type,
 * so `/constructor` would otherwise reach `Object.prototype` and yield
 * something that is not a section id.
 */
const SECTION_FOR_FLOW: Record<string, RailSection["id"]> = {
  start: "requirements",
  settle: "requirements",
  amend: "requirements",
  feature: "requirements",
  actor: "requirements",
  expand: "requirements",
  design: "design",
};

function plural(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}

/**
 * The requirements' own reasons.
 *
 * Assumptions and open questions are DIFFERENT things and are counted
 * separately: an assumption is a judgment the agent made and you may want to
 * overturn, an open question is a hole only you can fill. Both point at the
 * document, because that is where the controls that settle them already live —
 * the rail says there is something, and the document is where it is done.
 *
 * Neither GATES anything. Designing against assumptions is a deliberate part of
 * this product — the requirements arrive early, full of them, and are refined
 * in place — so the rail reports them and Design stays clickable throughout.
 */
function requirementsReasons(input: RailInput): SectionReason[] {
  const reasons: SectionReason[] = [];
  // Open questions lead: only the user can answer one, so nothing else in the
  // system can move it along. An assumption already has an answer standing.
  if (input.openQuestions > 0) {
    reasons.push({
      key: "open-questions",
      label: plural(input.openQuestions, "open question", "open questions"),
      count: input.openQuestions,
      action: "document",
    });
  }
  if (input.assumptions > 0) {
    reasons.push({
      key: "assumptions",
      label: `${plural(input.assumptions, "assumption", "assumptions")} to challenge`,
      count: input.assumptions,
      action: "document",
    });
  }
  return reasons;
}

/**
 * The one to show when there is only room for one — the hover.
 *
 * Simply the head of the list, because the list is built in significance order.
 * A separate ranking here would be a second definition of "most important", and
 * the two would disagree the first time either moved.
 */
export function mostSignificant(reasons: SectionReason[]): SectionReason | undefined {
  return reasons[0];
}

/**
 * How much there is to resolve — the number on the chip.
 *
 * The SUM of what the reasons stand for, not how many reasons there are. Two
 * open questions and three assumptions is five things to deal with; showing "2"
 * would be counting the kinds, which is a fact about our vocabulary rather than
 * about the user's work.
 */
export function reasonCount(reasons: SectionReason[]): number {
  return reasons.reduce((total, r) => total + r.count, 0);
}

/**
 * The three sections, in journey order, each carrying its state and reasons.
 *
 * ACTIVE is claimed for at most ONE section — the earliest that has nothing in
 * it — and only while an agent is working. Once every section holds something
 * nothing pulses: a turn is known project-wide, never per document, so there is
 * no honest way to say which one is being worked on, and a pulse on the wrong
 * section is worse than a still rail. The per-document work that makes this
 * precise waits on agents declaring their plan before they write.
 *
 * ATTENTION never outranks ACTIVE: an agent working on a stale design is
 * already resolving it, and a warning about the thing being fixed while it is
 * being fixed reads as a fault.
 */
export function railSections(input: RailInput): RailSection[] {
  const outdatedReason: SectionReason[] = input.designOutdated
    ? [{ key: "requirements-moved", label: REQUIREMENTS_MOVED, count: 1, action: "update-design" }]
    : [];

  const requirements = requirementsReasons(input);
  const has: Record<RailSection["id"], boolean> = {
    requirements: input.hasRequirements,
    design: input.hasDesign,
    validation: input.hasValidation,
  };

  // At most ONE section pulses, and WHICH one comes from the work in flight
  // rather than from which sections happen to be empty.
  //
  // Guessing from emptiness was wrong in both directions. Settling an
  // assumption lit Design, because Design was the first empty section though
  // the work was requirements. And the moment a design run wrote its first
  // file the pulse jumped to Validation, because Design had stopped being
  // empty — while the rest of the design was still being written.
  //
  // The flow says what the turn is for, so it maps straight onto the section
  // that will change. An unrecognised flow (a plain chat turn, an org's own
  // skill) pulses nothing: an agent IS working, but nothing here can say where,
  // and a pulse on the wrong section is worse than a still rail.
  const activeID =
    input.agentWorking && Object.hasOwn(SECTION_FOR_FLOW, input.agentFlow)
      ? SECTION_FOR_FLOW[input.agentFlow]
      : undefined;

  const section = (
    id: RailSection["id"],
    title: string,
    reasons: SectionReason[],
  ): RailSection => ({
    id,
    title,
    // ACTIVE outranks everything, and does not require the section to be empty:
    // a design run keeps pulsing Design after its first file lands, because it
    // is still writing the rest. Downstream sections stay dim through the whole
    // of the requirements interview, which is what they are — not begun, and
    // not beginnable until the thing they derive from exists.
    state:
      id === activeID
        ? "active"
        : !has[id]
          ? "not-started"
          : reasons.length > 0
            ? "attention"
            : "ready",
    reasons: has[id] ? reasons : [],
  });

  return [
    section("requirements", "Requirements", requirements),
    // "Design", not "Designs" — one design, written across several documents.
    section("design", "Design", outdatedReason),
    // The acceptance criteria are written against the same stories the design
    // is, and the same re-derivation rewrites both — so they go stale together
    // and clear together. Flagging only the design would quietly assert that
    // criteria written against a story you have since rewritten are still fine.
    section("validation", "Validation", outdatedReason),
  ];
}
