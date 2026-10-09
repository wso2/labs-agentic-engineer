// Pins the code-to-spec skill's standalone property: its bundled copies
// of the platform contracts match the platform's skills, and its own text
// names no path into this repository. Run by `make test` with node --test.

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join, relative } from "node:path";
import test from "node:test";
import { render, REPO_ROOT, SKILL_DIR } from "./sync-references.mjs";

const SYNC = "node .agents/skills/code-to-spec/scripts/sync-references.mjs";

test("the bundled references are the platform contracts, verbatim", () => {
  const rendered = render();
  assert.equal(rendered.length, 2);
  for (const { path, content } of rendered) {
    const committed = readFileSync(path, "utf8");
    assert.equal(
      committed,
      content,
      `${relative(REPO_ROOT, path)} is stale against skills/: run \`${SYNC}\``,
    );
  }
});

test("the skill names no path into this repository", () => {
  const skill = readFileSync(join(SKILL_DIR, "SKILL.md"), "utf8");
  const forbidden = [
    { re: /(^|[^./])skills\/[a-z-]+\//m, what: "a skills/<name>/ path" },
    { re: /playground\//, what: "the playground's directory" },
    { re: /docs\/developer-guide\//, what: "the developer guide" },
  ];
  for (const { re, what } of forbidden) {
    assert.doesNotMatch(
      skill,
      re,
      `SKILL.md names ${what}; this skill is standalone, so bundle the file under references/ instead of naming a path in this repository`,
    );
  }
});

test("every references/ and scripts/ path the skill names exists beside it", () => {
  const skill = readFileSync(join(SKILL_DIR, "SKILL.md"), "utf8");
  const named = new Set([...skill.matchAll(/`((?:references|scripts)\/[A-Za-z0-9._-]+)`/g)].map((m) => m[1]));
  assert.ok(named.size > 0, "the skill should name its bundled references");
  for (const rel of named) {
    assert.doesNotThrow(() => readFileSync(join(SKILL_DIR, rel)), `${rel} is named by SKILL.md but missing`);
  }
});
