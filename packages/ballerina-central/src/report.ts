/*
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied. See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/**
 * The report register: Markdown that DESCRIBES a package.
 *
 * A document either IS Ballerina or it describes a package, and blending the two
 * produces things like a `client class Client {` shell whose body is
 * `// WARNING: 903 resource functions` — a document that looks like a
 * declaration, is not one, and invites a reader to transcribe from it or to
 * reason about what "the file" contains. An agent that mistakes a summary for
 * source writes code against it.
 *
 * So the rules are mechanical, and `test/register.test.ts` enforces them:
 *
 *   Ballerina appears ONLY inside a fenced ```ballerina block. A fence is
 *   unambiguously a quotation, so a signature stays copyable truth while the
 *   surrounding document cannot be mistaken for source.
 *
 *   `//` annotations are illegal here. In the code register they annotate real
 *   declarations, which is what `// Special Agent Note:` does; here they were the
 *   thing doing the impersonating, and Markdown prose replaces them.
 *
 *   Structure is headings, so `grep '^## '` returns the document's sections.
 *
 *   Ballerina doc comments (`#`) stay INSIDE the fences, because they are the
 *   language's own doc syntax and belong to the quoted declaration.
 *
 * Every view builds its output through this module rather than hand-rolling a
 * heading, which is the only reason those rules hold for documents nobody has
 * written yet.
 */

/**
 * The on-disk format generation of a report document, and a skew detector.
 *
 * The binary ships baked into the runner image while the skill ships through the
 * skills git mirror, so "they land together" is not achievable. New verbs and
 * flags fail loudly under skew, but flipping the DEFAULT document is silent in
 * both directions: a new skill against an old binary gets 927,760 bytes where it
 * budgeted 6,900, and an old skill against a new binary greps `client class` in
 * Markdown, matches nothing, and then `sed -n '19050,19260p'` returns empty at
 * exit 0.
 *
 * An HTML comment on the first line makes that detectable rather than silent: it
 * survives `grep`, every Markdown reader renders it as nothing, and it cannot
 * appear in the 21,818-line Ballerina document the old default printed.
 */
export const REPORT_FORMAT = "v1";

export class Report {
  private readonly blocks: string[];

  constructor(verb: string) {
    this.blocks = [`<!-- bal-library ${verb} ${REPORT_FORMAT} -->`];
  }

  /** `# Title`, once, at the top. */
  heading(level: number, text: string): this {
    this.blocks.push(`${"#".repeat(Math.min(6, Math.max(1, level)))} ${text}`);
    return this;
  }

  paragraph(text: string): this {
    if (text.trim() !== "") this.blocks.push(text);
    return this;
  }

  /**
   * A two-column facts table. Headerless on purpose — the left column IS the
   * label, and a `| | |` header row is how GitHub-flavoured Markdown spells that.
   */
  facts(rows: readonly (readonly [string, string])[]): this {
    if (rows.length === 0) return this;
    const lines = ["| | |", "|---|---|", ...rows.map(([label, value]) => `| ${label} | ${value} |`)];
    this.blocks.push(lines.join("\n"));
    return this;
  }

  bullets(items: readonly string[]): this {
    if (items.length > 0) this.blocks.push(items.map((item) => `- ${item}`).join("\n"));
    return this;
  }

  /**
   * Quoted Ballerina. The ONLY way this module emits the language, and the reason
   * a report can carry copyable signatures without looking like source.
   */
  ballerina(declarations: readonly string[]): this {
    // A blank line between entries as soon as any of them is multi-line, which is
    // what a doc comment makes them: `# Get a repository` followed straight by the
    // next declaration's comment reads as one four-line comment on one function.
    // The API document separates declarations the same way for the same reason.
    const separator = declarations.some((entry) => entry.includes("\n")) ? "\n\n" : "\n";
    const body = declarations.join(separator);
    if (body.trim() === "") return this;
    this.blocks.push(`\`\`\`ballerina\n${body}\n\`\`\``);
    return this;
  }

  /**
   * Quoted plain text — a path tree, a segment listing. Unfenced it would be
   * prose; fenced as `ballerina` it would be a lie about what it is; so it is a
   * bare fence, which is neither.
   */
  literal(lines: readonly string[]): this {
    const body = lines.join("\n");
    if (body.trim() === "") return this;
    this.blocks.push(`\`\`\`\n${body}\n\`\`\``);
    return this;
  }

  /** Markdown from somewhere else, quoted at a fence long enough to hold it. */
  embedded(markdown: string): this {
    if (markdown.trim() === "") return this;
    this.blocks.push(markdown);
    return this;
  }

  /**
   * Blocks separated by one blank line, with exactly one trailing newline. The
   * format marker is joined directly to the title instead, so the document opens
   * on its heading for a human reader.
   */
  toString(): string {
    const [marker, ...rest] = this.blocks;
    return `${marker ?? ""}\n${rest.join("\n\n")}\n`;
  }
}

/**
 * Lay names out in aligned columns.
 *
 * A 36-entry path tree is 445 bytes in four columns and about 1.3KB one per line,
 * and the four-column form is also the one a reader can scan for the segment they
 * want. Column width comes from the longest entry so nothing is truncated —
 * truncating a name would make it unusable as the next command's argument.
 */
export function columns(entries: readonly string[], perRow = 4): string[] {
  if (entries.length === 0) return [];
  const width = entries.reduce((max, entry) => Math.max(max, entry.length), 0);
  const rows: string[] = [];
  for (let i = 0; i < entries.length; i += perRow) {
    const row = entries.slice(i, i + perRow);
    rows.push(
      row
        .map((entry, index) => (index === row.length - 1 ? entry : entry.padEnd(width + 1)))
        .join(" ")
        .trimEnd(),
    );
  }
  return rows;
}

/** `1234` → `1,234`. Byte counts and operation counts are quoted a lot here. */
export function count(value: number): string {
  return value.toLocaleString("en-US");
}

/** Inline code, for a name inside prose. */
export function code(text: string): string {
  return `\`${text}\``;
}
