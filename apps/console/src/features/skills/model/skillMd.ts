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

// A SKILL.md is YAML frontmatter (its name, its one-line description, and
// whatever else the skill declares) followed by a markdown body. The Skill
// card edits the description and the body apart; this module puts them back
// into the file without touching anything else in it, so a description edit
// leaves the body byte for byte, and an edit to the body leaves the
// frontmatter as it was. The frontmatter never goes through the rich-text
// editor: a markdown round-trip would corrupt it.

const FRONTMATTER = /^---[ \t]*\r?\n([\s\S]*?)\r?\n---[ \t]*(?:\r?\n|$)/;

/** The frontmatter's text (null when there is none) and the body after it, exactly as written. */
export function splitFrontmatter(skillMd: string): { frontmatter: string | null; body: string } {
  const match = FRONTMATTER.exec(skillMd);
  if (!match) return { frontmatter: null, body: skillMd };
  return { frontmatter: match[1] ?? "", body: skillMd.slice(match[0].length) };
}

// A plain YAML scalar needs none of the characters that start another kind of
// node, and no ": " or " #" inside; anything else is written double-quoted,
// which JSON's string syntax is valid YAML for.
const PLAIN_SAFE = /^[^\s\-?:,[\]{}#&*!|>'"%@`][^\n]*$/;

function yamlScalar(text: string): string {
  const plain = PLAIN_SAFE.test(text) && !/:\s|\s#|:$|\s$/.test(text);
  return plain ? text : JSON.stringify(text);
}

/** The description as one line: the field is a one-liner, and a newline would end the YAML value early. */
export function oneLine(text: string): string {
  return text.replace(/\s*\r?\n\s*/g, " ").trim();
}

function descriptionLine(description: string): string {
  return `description: ${yamlScalar(oneLine(description))}`;
}

/**
 * The frontmatter with its top-level `description` set to `description`: the
 * key's line and any indented lines that continue its value are replaced; a
 * frontmatter without one gains it after `name`, or at the end.
 */
export function withDescription(frontmatter: string, description: string): string {
  const line = descriptionLine(description);
  const lines = frontmatter.split(/\r?\n/);
  const start = lines.findIndex((l) => /^description\s*:/.test(l));
  if (start === -1) {
    const name = lines.findIndex((l) => /^name\s*:/.test(l));
    lines.splice(name === -1 ? lines.length : name + 1, 0, line);
    return lines.join("\n");
  }
  let end = start + 1;
  // A value that continues (a folded block, a long quoted line) does so on
  // indented lines; blank lines inside a block belong to it while an indented
  // line follows them.
  while (end < lines.length) {
    const next = lines[end]!;
    if (/^[ \t]+\S/.test(next)) {
      end++;
      continue;
    }
    if (next.trim() === "") {
      const after = lines.slice(end).findIndex((l) => l.trim() !== "");
      if (after !== -1 && /^[ \t]/.test(lines[end + after]!)) {
        end += after;
        continue;
      }
    }
    break;
  }
  lines.splice(start, end - start, line);
  return lines.join("\n");
}

/** The file from its frontmatter and its body, the body kept exactly as given. */
export function joinSkillMd(frontmatter: string, body: string): string {
  return `---\n${frontmatter}\n---\n${body}`;
}

/** A body the editor wrote, set off from the frontmatter by a blank line, as a SKILL.md is written. */
export function editedBody(markdown: string): string {
  const text = markdown.replace(/^\s*\n/, "");
  if (text.trim() === "") return "\n";
  return `\n${text}${text.endsWith("\n") ? "" : "\n"}`;
}

/** A new skill's file: its name and description, then the body. */
export function newSkillMd(name: string, description: string, body: string): string {
  return joinSkillMd(`name: ${name}\n${descriptionLine(description)}`, editedBody(body));
}
