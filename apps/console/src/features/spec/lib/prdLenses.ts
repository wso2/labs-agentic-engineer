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

// The PRD's own launchers (#579): which command each part of the document
// offers, and where.
//
// The PRD is the surface the user works through, so a command is offered AT the
// place it changes rather than from a menu that makes them supply the subject
// from memory. This module is the locator — it says what sits where; the
// ProseMirror plugin (`collab/prdLensPlugin.ts`) walks the live document into
// `PrdBlock`s and renders the result.
//
// Deliberately free of ProseMirror types: positions arrive as plain numbers, so
// the rules that decide what the document offers are testable without an editor.
//
// This replaced `countBlockingOpenQuestions`, which counted the same section to
// disable Generate design. Open questions gate nothing now (#539) — the parse
// survives to LOCATE, so the section it used to block on is now the section it
// offers `/settle` from.

/** An emphasised (`*…*`) run inside a block, at its document positions. */
export interface EmphasisRun {
  text: string;
  from: number;
  to: number;
}

/** One textblock of the live document, flattened. */
export interface PrdBlock {
  kind: "heading" | "listItem" | "paragraph";
  /** Heading depth; absent for everything else. */
  level?: number | undefined;
  /** The block's plain text. */
  text: string;
  emphasis: EmphasisRun[];
  /** Node start position. */
  from: number;
  /** Position just past the node. */
  to: number;
  /** Just inside the node's closing token — where a trailing widget anchors. */
  contentEnd: number;
}

/**
 * A **section** lens rides its heading and is always on show: it is how the
 * user finds the command at all. A **line** lens belongs to one entry and
 * appears on hover, so a twenty-story list carries one visible control, not
 * twenty.
 */
export type LensPlacement = "section" | "line";

/** A command the document offers, and the spot it is offered from. */
export interface PrdLens {
  /** Sent verbatim as the user's next message. */
  command: string;
  label: string;
  /** The control's tooltip — what firing it does. */
  title: string;
  at: number;
  placement: LensPlacement;
}

/**
 * The two kinds of unsettled, plus the deferral of one. An assumption is a
 * decision the agent already made; an open question is a hole nobody has
 * filled; a deferred question is one the user has declined for now. They read
 * differently in the document because they are different things.
 */
export type FlagKind = "assumed" | "question" | "deferred";

/** A stretch of document that reads as flagged. */
export interface PrdFlag {
  kind: FlagKind;
  from: number;
  to: number;
}

export interface PrdAffordances {
  lenses: PrdLens[];
  flags: PrdFlag[];
}

/** The PRD sections that carry an add-lens, keyed by their heading text. */
const SECTION_LENSES: Record<string, Omit<PrdLens, "at" | "placement">> = {
  actors: { command: "/actor", label: "+ Actor", title: "Add an actor to this PRD" },
  "user stories": { command: "/feature", label: "+ Feature", title: "Add a feature to this PRD" },
  stories: { command: "/feature", label: "+ Feature", title: "Add a feature to this PRD" },
};

/**
 * The lens a heading earns, or undefined.
 *
 * Guarded rather than indexed: `section` is normalised heading text the agent
 * wrote, so a bare lookup reaches `Object.prototype` for a heading reading
 * "Constructor" or "toString" and yields a function, which then spreads into a
 * lens carrying no command — a pill labelled `undefined` that sends
 * `undefined` when clicked. `COMMAND_FLOWS` in the agents service guards the
 * analogous lookup for the same reason.
 */
function sectionLens(section: string): Omit<PrdLens, "at" | "placement"> | undefined {
  return Object.hasOwn(SECTION_LENSES, section) ? SECTION_LENSES[section] : undefined;
}

/** The heading whose entries are the open questions. */
const OPEN_QUESTIONS = "open questions";

const norm = (text: string): string => text.trim().toLowerCase().replace(/\s+/g, " ");

/** A subject the agent reads back: the entry as written, on one line. */
const subjectOf = (text: string): string => text.replace(/\s+/g, " ").trim();

/** An emphasised run is the assumption flag when the run IS the word. */
const isAssumedFlag = (run: EmphasisRun): boolean => /^assumed$/i.test(run.text.trim());

/**
 * What the document offers, in document order.
 *
 * One lens per block at most, and the flags outrank the plain entries: an
 * assumption or an open question is the more urgent thing to take up, so a
 * flagged story offers `/settle` rather than `/expand`.
 */
export function prdAffordances(blocks: PrdBlock[]): PrdAffordances {
  const lenses: PrdLens[] = [];
  const flags: PrdFlag[] = [];
  // Filled in as the section's entries arrive, so an empty Open Questions
  // section never grows a "settle them" control over nothing.
  let openQuestionsHeading: PrdBlock | null = null;
  let section = "";

  for (const b of blocks) {
    if (b.kind === "heading") {
      section = norm(b.text);
      const lens = sectionLens(section);
      if (lens) lenses.push({ ...lens, at: b.contentEnd, placement: "section" });
      openQuestionsHeading = section === OPEN_QUESTIONS ? b : null;
      continue;
    }

    const assumption = b.emphasis.find(isAssumedFlag);
    const subject = subjectOf(b.text);

    if (section === OPEN_QUESTIONS && b.kind === "listItem") {
      if (openQuestionsHeading) {
        lenses.push({
          command: "/settle",
          label: "Settle",
          title: "Take up the open questions with the agent",
          at: openQuestionsHeading.contentEnd,
          placement: "section",
        });
        openQuestionsHeading = null;
      }
      flags.push({
        kind: /\bdeferred\b/i.test(b.text) ? "deferred" : "question",
        from: b.from,
        to: b.to,
      });
      lenses.push({
        command: `/settle ${subject}`,
        label: "Settle",
        title: "Answer this question with the agent",
        at: b.contentEnd,
        placement: "line",
      });
      continue;
    }

    if (assumption) {
      flags.push({ kind: "assumed", from: assumption.from, to: assumption.to });
      lenses.push({
        command: `/settle ${subject}`,
        label: "Settle",
        title: "Challenge this assumption with the agent",
        at: b.contentEnd,
        placement: "line",
      });
      continue;
    }

    if (sectionLens(section)?.command === "/feature" && b.kind === "listItem") {
      lenses.push({
        command: `/expand ${subject}`,
        label: "Go deeper",
        title: "Go deeper on this feature with the agent",
        at: b.contentEnd,
        placement: "line",
      });
    }
  }

  // The Open Questions lens is emitted at its first entry, so it lands after
  // the entries in insertion order; document order is what the caller renders.
  lenses.sort((a, b) => a.at - b.at);
  return { lenses, flags };
}
