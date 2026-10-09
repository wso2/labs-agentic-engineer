#!/usr/bin/env node
// Bundles the two platform contracts the code-to-spec skill follows into
// its own references/, so a copy of the skill directory works outside this
// repository. The copies are verbatim and pinned by sync-references.test.mjs,
// which fails when either platform file changes without a re-run of this
// script. Run from anywhere inside the repository:
//
//   node .agents/skills/code-to-spec/scripts/sync-references.mjs

import { createHash } from "node:crypto";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
export const SKILL_DIR = resolve(here, "..");
export const REPO_ROOT = resolve(SKILL_DIR, "..", "..", "..");

const PRD_CONTRACT = "skills/prd-contract/SKILL.md";
const DESIGN = "skills/design/SKILL.md";
const DOMAIN_MODEL_START = /^3\. \*\*domain-model\.md\*\*/;
const DOMAIN_MODEL_END = /^4\. \*\*Key flows\*\*/;

function stamp(sourcePath, body) {
  const hash = createHash("sha256").update(body).digest("hex").slice(0, 12);
  return `<!-- Generated from ${sourcePath} (sha256 ${hash}) by scripts/sync-references.mjs. Do not edit. -->\n\n`;
}

/** The index of the one line matching `re`; throws unless it matches exactly once. */
function anchor(lines, re, sourcePath) {
  const hits = lines.map((l, i) => (re.test(l) ? i : -1)).filter((i) => i >= 0);
  if (hits.length !== 1) {
    throw new Error(`anchor ${re} matched ${hits.length} times in ${sourcePath}; expected exactly once`);
  }
  return hits[0];
}

/** A skill body without its YAML frontmatter. */
function stripFrontmatter(text, sourcePath) {
  const lines = text.split("\n");
  if (lines[0] !== "---") throw new Error(`${sourcePath} does not start with frontmatter`);
  const end = lines.indexOf("---", 1);
  if (end < 0) throw new Error(`${sourcePath} has an unterminated frontmatter block`);
  const body = lines.slice(end + 1);
  while (body.length && body[0].trim() === "") body.shift();
  return body.join("\n");
}

/** The two references as { path, content }, computed from the platform skills. */
export function render() {
  const prdSource = readFileSync(join(REPO_ROOT, PRD_CONTRACT), "utf8");
  const prdBody = stripFrontmatter(prdSource, PRD_CONTRACT);

  const designSource = readFileSync(join(REPO_ROOT, DESIGN), "utf8");
  const designLines = designSource.split("\n");
  const start = anchor(designLines, DOMAIN_MODEL_START, DESIGN);
  const end = anchor(designLines, DOMAIN_MODEL_END, DESIGN);
  if (end <= start) throw new Error(`${DESIGN}: the Key flows step precedes the domain-model step`);
  const step = designLines.slice(start, end).join("\n").trimEnd();
  const shape =
    "# The shape of specs/design/domain-model.md\n\n" +
    "Step 3 of the platform's design lineup, verbatim. The numbering is the lineup's; only this step applies here.\n\n" +
    step +
    "\n";

  return [
    { path: join(SKILL_DIR, "references", "prd-contract.md"), content: stamp(PRD_CONTRACT, prdBody) + prdBody },
    { path: join(SKILL_DIR, "references", "domain-model-shape.md"), content: stamp(DESIGN, step) + shape },
  ];
}

function main() {
  for (const { path, content } of render()) {
    const current = existsSync(path) ? readFileSync(path, "utf8") : null;
    if (current !== content) writeFileSync(path, content, "utf8");
    process.stdout.write(`${current === content ? "unchanged" : "wrote"} ${relative(REPO_ROOT, path)}\n`);
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main();
