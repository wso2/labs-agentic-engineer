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

// The PRD's own launchers (#579, re-cut in #652): what each part of the
// document offers, and where — a command, a direct edit, or a conversation.
//
// The PRD is the surface the user works through, so a command is offered AT the
// place it changes rather than from a menu that makes them supply the subject
// from memory. This module is the locator — it says what sits where; the
// ProseMirror plugin (`collab/prdLensPlugin.ts`) walks the live document into
// `DocBlock`s and renders the result.
//
// Deliberately free of ProseMirror types: positions arrive as plain numbers, so
// the rules that decide what the document offers are testable without an editor.
//
// This replaced `countBlockingOpenQuestions`, which counted the same section to
// disable Generate design. Open questions gate nothing now (#539) — the parse
// survives to LOCATE, so the section it used to block on is now the section it
// offers `/settle` from.

import type { DocBlock, EmphasisRun } from "./docBlocks.js";

/**
 * A **section** lens rides its heading and is always on show: it is how the
 * user finds the command at all. A **line** lens belongs to one entry and
 * appears on hover, so a twenty-story list carries one visible control, not
 * twenty.
 */
export type LensPlacement = "section" | "line";

interface LensBase {
  label: string;
  /** The control's tooltip — what firing it does. */
  title: string;
  at: number;
  placement: LensPlacement;
}

/** The one direct edit (#652): Agree strips the `*assumed*` marker. */
export type PrdEditKind = "agree";

/**
 * What the document offers at one spot. Three kinds, and the kind is the
 * whole difference in what a click costs:
 *
 *  - `command` sends a line as the user's next message — an agent turn.
 *  - `edit` changes the document directly, no agent, no model: **Agree**
 *    strips the `*assumed*` marker. That is itself the signal the agent reads
 *    on its next round, which is why it needs no turn — and why it stays live
 *    while an agent holds one.
 *  - `discuss` opens the aim box on the block, the same box a selection opens,
 *    with Enter sending Discuss.
 */
export type PrdLens =
  | (LensBase & {
      kind: "command";
      /** Sent verbatim as the user's next message. */
      command: string;
      /**
       * When present, the lens does not fire bare: it opens the aim box to
       * collect the command's subject first (#666), and the send is
       * `<command> <typed text>`. The add-lenses carry this — `/actor` with no
       * actor makes the agent ask what the lens could have asked on the spot.
       */
      prompt?: CommandPrompt;
    })
  | (LensBase & { kind: "edit"; edit: PrdEditKind; block: DocBlock; run: EmphasisRun })
  | (LensBase & { kind: "discuss"; block: DocBlock });

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
/** What the box asks for, and what its one button says. */
export interface CommandPrompt {
  placeholder: string;
  cta: string;
}

type SectionLens = { command: string; label: string; title: string; prompt?: CommandPrompt };
const FEATURE_LENS: SectionLens = {
  command: "/feature",
  label: "+ Feature",
  title: "Add a feature to this PRD",
  prompt: { placeholder: "Describe the feature in your own words…", cta: "Add feature" },
};
const SECTION_LENSES: Record<string, SectionLens> = {
  actors: {
    command: "/actor",
    label: "+ Actor",
    title: "Add an actor to this PRD",
    prompt: { placeholder: "Who are they, and what do they do?", cta: "Add actor" },
  },
  "user stories": FEATURE_LENS,
  stories: FEATURE_LENS,
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
function sectionLens(section: string): SectionLens | undefined {
  return Object.hasOwn(SECTION_LENSES, section) ? SECTION_LENSES[section] : undefined;
}

/** The heading whose entries are the open questions. */
const OPEN_QUESTIONS = "open questions";

const norm = (text: string): string => text.trim().toLowerCase().replace(/\s+/g, " ");

/** A subject the agent reads back: the entry as written, on one line. */
const subjectOf = (text: string): string => text.replace(/\s+/g, " ").trim();

/**
 * An emphasised run is the assumption flag when the run IS the word — give or
 * take the punctuation an agent wraps it in.
 *
 * `*(assumed)*` and `*[assumed]*` count, because agents write both and an exact
 * match silently dropped them: the rail stopped counting the assumption AND the
 * line lost its Settle control, so the user could neither see nor challenge a
 * judgment the agent had made. The tag is prose an LLM writes free-hand, so the
 * reader tolerates the punctuation while the contract states the plain form.
 *
 * Still the whole run, not a substring: `*assumed single approver*` is the
 * agent emphasising its reasoning, not tagging it.
 */
const isAssumedFlag = (run: EmphasisRun): boolean =>
  /^[([]?assumed[)\]]?$/i.test(run.text.trim());

/** The conversation every bullet offers (#652): the aim box, pre-aimed here. */
const discussLens = (block: DocBlock): PrdLens => ({
  kind: "discuss",
  label: "Discuss",
  title: "Talk this line through with the agent",
  at: block.contentEnd,
  placement: "line",
  block,
});

/**
 * The verdicts on an `*assumed*` run (#652): keep it, or talk about it. Two,
 * deliberately — a line with four controls on it stops reading as a line.
 * Dropping or reopening the decision is a sentence away in Discuss, and the
 * editor itself deletes a bullet as well as any control could.
 */
function assumedLenses(block: DocBlock, run: EmphasisRun): PrdLens[] {
  return [
    {
      kind: "edit",
      edit: "agree",
      label: "Agree",
      title: "Keep this decision — drop the assumed flag",
      at: block.contentEnd,
      placement: "line",
      block,
      run,
    },
    discussLens(block),
  ];
}

/**
 * What the document offers, in document order.
 *
 * The flags outrank the plain entries: an assumption or an open question is
 * the more urgent thing to take up, so a flagged story offers its verdicts
 * rather than `/expand`. Every bullet offers Discuss (#652) — a conversation
 * is something any line can start.
 */
export function prdAffordances(blocks: DocBlock[]): PrdAffordances {
  const lenses: PrdLens[] = [];
  const flags: PrdFlag[] = [];
  // Filled in as the section's entries arrive, so an empty Open Questions
  // section never grows a "settle them" control over nothing.
  let openQuestionsHeading: DocBlock | null = null;
  let section = "";

  for (const b of blocks) {
    if (b.kind === "heading") {
      section = norm(b.text);
      const lens = sectionLens(section);
      if (lens) lenses.push({ kind: "command", ...lens, at: b.contentEnd, placement: "section" });
      openQuestionsHeading = section === OPEN_QUESTIONS ? b : null;
      continue;
    }

    const assumption = b.emphasis.find(isAssumedFlag);
    const subject = subjectOf(b.text);

    if (section === OPEN_QUESTIONS && b.kind === "listItem") {
      if (openQuestionsHeading) {
        lenses.push({
          kind: "command",
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
        kind: "command",
        command: `/settle ${subject}`,
        label: "Settle",
        title: "Answer this question with the agent",
        at: b.contentEnd,
        placement: "line",
      });
      lenses.push(discussLens(b));
      continue;
    }

    if (assumption) {
      flags.push({ kind: "assumed", from: assumption.from, to: assumption.to });
      lenses.push(...assumedLenses(b, assumption));
      continue;
    }

    if (b.kind !== "listItem") continue;

    if (sectionLens(section)?.command === "/feature") {
      lenses.push({
        kind: "command",
        command: `/expand ${subject}`,
        label: "Go deeper",
        title: "Go deeper on this feature with the agent",
        at: b.contentEnd,
        placement: "line",
      });
    }
    lenses.push(discussLens(b));
  }

  // The Open Questions lens is emitted at its first entry, so it lands after
  // the entries in insertion order; document order is what the caller renders.
  // A stable sort keeps the verdicts on one line in reading order.
  lenses.sort((a, b) => a.at - b.at);
  return { lenses, flags };
}
