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

// The spec's IDs and what a line of it says, read from the text itself.
//
// A feature is `F2`, a story in it `F2.5`, a product-wide item `P4`. An ID is
// permanent: when a story moves, its new line starts with its new ID and names
// the old one, "F5.1 (was F2.3)", so every reference to F2.3 still lands.
//
// Everything here reads the LIVE document's lines (the user may be typing into
// them), never a committed snapshot, so a hover shows the line as it reads now.

/** One line of a document: a heading, a paragraph, or a list entry's text. */
export interface LineBlock {
  kind: "heading" | "paragraph" | "listItem";
  /** Heading level; absent for other blocks. */
  level?: number;
  /** How deep a list entry is nested: 1 for a top-level entry, 2 for one under it. Absent for other blocks. */
  depth?: number;
  text: string;
  /** Italic runs, as offsets into `text`. The `*assumed*` tag is one. */
  emphasis: { start: number; end: number }[];
}

export interface Span {
  start: number;
  end: number;
}

export interface IdSpan extends Span {
  id: string;
}

/** What a line carries besides its words, as offsets into its text. */
export interface LineParts {
  /** The line's own ID, when it starts with a story or product-wide ID. */
  lead: IdSpan | null;
  /** "(was F2.3)" right after the lead: the ID this line replaces. */
  was: IdSpan | null;
  /** Every other ID in the line: a quiet link each. */
  refs: IdSpan[];
  /** Bracketed sources, "[T&E policy p.4]". */
  sources: Span[];
  /** The literal `*assumed*` tag closing the line (prd-contract). */
  assumed: Span | null;
  /** The literal `*blocking*` tag closing the line: an open question the interview waits on (model/questions.ts). */
  blocking: Span | null;
  /** A story's `Needs: F5.` clause: the features it waits on. Empty when it has none. */
  needs: string[];
  /** A product-wide item's `Applies to:` clause: feature IDs, or "all"; null when it has none. */
  appliesTo: string[] | "all" | null;
  /** Where the `Needs:` and `Applies to:` clauses sit, for drawing them as quiet tags. */
  clauses: Span[];
  /** The line's words alone: no ID, no "(was …)", no sources, no clause, no tag. */
  body: string;
}

// A line's own ID is a story or product-wide item. A feature ID at the start
// of a line (the product page's feature list) is a reference, not the line's
// identity: the feature's home is its own file.
const LEAD = /^(F\d+\.\d+|P\d+)(?=\s|$)/;
const WAS = /^\s*\(was (F\d+\.\d+|P\d+)\)/;
const SOURCE = /\[[^[\]]+\]/g;
const ID = /\b(?:F\d+(?:\.\d+)?|P\d+)\b/g;
const TRAILING_PUNCTUATION = /^[\s.,;:]*$/;
// The clauses a line carries after its words (skills/prd-contract, "Lines").
// Found anywhere in the line, so one written slightly out of order still reads.
const NEEDS = /\bNeeds:\s*(F\d+(?:\s*,\s*F\d+)*)\.?/i;
const APPLIES_TO = /\bApplies to:\s*(all|F\d+(?:\s*,\s*F\d+)*)\.?/i;
const FEATURE_ID = /F\d+/g;

function clause(text: string, pattern: RegExp): { span: Span; ids: string } | null {
  const m = pattern.exec(text);
  return m ? { span: { start: m.index, end: m.index + m[0].length }, ids: m[1]! } : null;
}

function overlaps(a: Span, spans: Span[]): boolean {
  return spans.some((s) => a.start < s.end && s.start < a.end);
}

/** A tag closing the line: the italic `word` with nothing after it but punctuation. */
function closingTag(text: string, emphasis: Span[], word: string): Span | null {
  const tag = emphasis
    .filter((e) => text.slice(e.start, e.end).trim() === word && TRAILING_PUNCTUATION.test(text.slice(e.end)))
    .at(-1);
  return tag ? { start: tag.start, end: tag.end } : null;
}

/** Split a line into its ID, sources, tags and words. */
export function parseLine(text: string, emphasis: Span[] = []): LineParts {
  const leadMatch = LEAD.exec(text);
  const lead = leadMatch ? { id: leadMatch[1]!, start: 0, end: leadMatch[0].length } : null;

  let was: IdSpan | null = null;
  if (lead) {
    const wasMatch = WAS.exec(text.slice(lead.end));
    if (wasMatch) {
      const end = lead.end + wasMatch[0].length;
      was = { id: wasMatch[1]!, start: end - wasMatch[0].trimStart().length, end };
    }
  }

  const sources: Span[] = [...text.matchAll(SOURCE)].map((m) => ({
    start: m.index,
    end: m.index + m[0].length,
  }));

  const assumed = closingTag(text, emphasis, "assumed");
  const blocking = closingTag(text, emphasis, "blocking");

  const needsClause = clause(text, NEEDS);
  const appliesClause = clause(text, APPLIES_TO);
  const needs = needsClause ? [...needsClause.ids.matchAll(FEATURE_ID)].map((m) => m[0]) : [];
  const appliesTo = !appliesClause
    ? null
    : appliesClause.ids.toLowerCase() === "all"
      ? "all"
      : [...appliesClause.ids.matchAll(FEATURE_ID)].map((m) => m[0]);

  const claimed: Span[] = [lead, was, assumed, blocking, ...sources].filter((s): s is Span => s !== null);
  // The IDs a clause names stay quiet links; only the words leave the body.
  const refs = [...text.matchAll(ID)]
    .map((m) => ({ id: m[0], start: m.index, end: m.index + m[0].length }))
    .filter((r) => !overlaps(r, claimed));
  const cut = [...claimed, needsClause?.span, appliesClause?.span].filter((s): s is Span => s !== undefined);

  let body = "";
  let at = 0;
  for (const s of cut.sort((a, b) => a.start - b.start)) {
    body += text.slice(at, s.start);
    at = Math.max(at, s.end);
  }
  body += text.slice(at);

  const clauses = [needsClause?.span, appliesClause?.span].filter((s): s is Span => s !== undefined);
  return { lead, was, refs, sources, assumed, blocking, needs, appliesTo, clauses, body: body.replace(/\s+/g, " ").trim() };
}

/** Where an ID points: a feature (its file) or a line in a file. */
export interface IdEntry {
  id: string;
  /** The file it lives in, by file key (see files.ts). */
  fileKey: string;
  /** A feature's name; absent for a line. */
  title?: string;
  /** The line's words, or a feature's purpose. */
  text: string;
}

export interface IdIndex {
  entries: ReadonlyMap<string, IdEntry>;
  /** Retired ID → the ID that replaced it, from "F5.1 (was F2.3)" and a Retired section's "F2.3 moved to F5.1". */
  retired: ReadonlyMap<string, string>;
}

/** The section a file's retired IDs are recorded in (skills/prd-contract, "IDs"). */
export const RETIRED_SECTION = "Retired";

const RETIRED_ENTRY = /^(F\d+(?:\.\d+)?|P\d+)\b(?:\s+moved to\s+(F\d+(?:\.\d+)?|P\d+)\b)?/i;

/** A Retired section's entry: the ID it retires, and the ID that replaced it when it moved. */
export function retiredEntry(text: string): { id: string; movedTo: string | null } | null {
  const m = RETIRED_ENTRY.exec(text.trim());
  return m ? { id: m[1]!, movedTo: m[2] ?? null } : null;
}

export interface IndexedFile {
  fileKey: string;
  lines: LineBlock[];
}

/** Every ID in the spec: each feature, and each line that starts with an ID. */
export function buildIdIndex(
  features: { id: string; name: string; purpose: string }[],
  files: IndexedFile[],
): IdIndex {
  const entries = new Map<string, IdEntry>();
  const retired = new Map<string, string>();
  for (const f of features) {
    entries.set(f.id, { id: f.id, fileKey: f.id, title: f.name, text: f.purpose });
  }
  for (const file of files) {
    let inRetired = false;
    for (const line of file.lines) {
      if (line.kind === "heading" && (line.level ?? 1) <= 2) {
        inRetired = line.text.trim().toLowerCase() === RETIRED_SECTION.toLowerCase();
        continue;
      }
      // A Retired entry records an ID that is gone: never a live line.
      if (inRetired) {
        const entry = line.kind === "listItem" ? retiredEntry(line.text) : null;
        if (entry?.movedTo) retired.set(entry.id, entry.movedTo);
        continue;
      }
      const parts = parseLine(line.text, line.emphasis);
      if (!parts.lead) continue;
      // First home wins: the one-home rule says an ID lives in one file, so a
      // second copy is a mistake in the document, not a second target.
      if (!entries.has(parts.lead.id)) {
        entries.set(parts.lead.id, { id: parts.lead.id, fileKey: file.fileKey, text: parts.body });
      }
      if (parts.was) retired.set(parts.was.id, parts.lead.id);
    }
  }
  return { entries, retired };
}

export interface ResolvedId {
  entry: IdEntry;
  /** The ID that was asked for, when it was retired and this is its replacement. */
  retiredFrom: string | null;
}

/** Look an ID up, following a retired ID to the line that replaced it. */
export function resolveId(index: IdIndex, id: string): ResolvedId | null {
  let current = id;
  // A replacement can itself be retired later; the chain is short, and the
  // bound only stops a malformed document from looping.
  for (let hops = 0; hops < 10; hops++) {
    const entry = index.entries.get(current);
    if (entry) return { entry, retiredFrom: current === id ? null : id };
    const next = index.retired.get(current);
    if (!next) return null;
    current = next;
  }
  return null;
}

/** Where opening an ID goes: the feature's file, or the line in its file. */
export function idTarget(entry: IdEntry): { file: string; at?: string } {
  return entry.title !== undefined ? { file: entry.fileKey } : { file: entry.fileKey, at: entry.id };
}

/** Split free text into plain runs and IDs, for text drawn outside the editor. */
export function splitIds(text: string): ({ text: string } | { id: string })[] {
  const out: ({ text: string } | { id: string })[] = [];
  let at = 0;
  for (const m of text.matchAll(ID)) {
    if (m.index > at) out.push({ text: text.slice(at, m.index) });
    out.push({ id: m[0] });
    at = m.index + m[0].length;
  }
  if (at < text.length) out.push({ text: text.slice(at) });
  return out;
}
