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

import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { composeWorkflowSkill, type AgentMode } from "./workflow_skill.js";
import { mirrorLocalSkillLibrary } from "./local_skill_mirror.js";

// The real authored library: src/lib → remote-worker → runners → repo root.
const LIBRARY = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../../../skills");
const AEP_SKILL = path.join(LIBRARY, "aep", "SKILL.md");
const LOCAL_OVERLAY = path.join(LIBRARY, "aep", "overlays", "local.md");

const composed: Record<AgentMode, string> = {
  github: composeWorkflowSkill(LIBRARY, "github"),
  local: composeWorkflowSkill(LIBRARY, "local"),
};

// --- the authored trunk IS the platform's procedure --------------------------

// The library holds one authored workflow skill and it is written for the
// platform. Nothing is stripped for a production run, so what a reviewer reads
// in `skills/aep/SKILL.md` — or installs with `make workflow-skill` — is what a
// dispatched run is steered by, byte for byte.
test("github mode is the authored skill, unmodified", () => {
  assert.equal(composed.github, fs.readFileSync(AEP_SKILL, "utf8"));
});

test("the local overlay leaves the authored skill on disk untouched", () => {
  const before = fs.readFileSync(AEP_SKILL, "utf8");
  composeWorkflowSkill(LIBRARY, "local");
  assert.equal(fs.readFileSync(AEP_SKILL, "utf8"), before);
});

for (const mode of ["github", "local"] as const) {
  test(`the ${mode} body has exactly one description line`, () => {
    const frontmatterEnd = composed[mode].indexOf("\n---", 4);
    const frontmatter = composed[mode].slice(0, frontmatterEnd);
    assert.equal(frontmatter.match(/^description:/gm)?.length, 1);
    // One identity in both modes — the plugin/skill name never varies by mode.
    assert.match(frontmatter, /^name: aep$/m);
    // Platform-owned and read-only in the org catalog; without the kind the
    // library's default (`org`) would make it an editable, deletable org skill.
    assert.match(frontmatter, /kind: platform/);
  });

  test(`the ${mode} body carries no overlay markup`, () => {
    assert.doesNotMatch(composed[mode], /<!--\s*(replace|drop|append)-/);
    assert.doesNotMatch(composed[mode], /<!--\s*\/?(with|replace-text)\s*-->/);
  });
}

// --- per-mode landmarks -----------------------------------------------------
// Landmarks, not a golden copy of the whole file: a checked-in expected-output
// fixture would be a second copy of the skill to keep in sync, which is the
// exact problem this design removes.
const PLATFORM_ONLY = [
  "gh issue list --milestone",
  "### Establish branch identity",
  "aep/m<milestone#>-c<k>",
  "gh pr create",
  "Resolves #12",
  "list_org_component_endpoints",
  "Platform-resolved dependencies",
  "ledger",
  // The status line is the issue-comment surface, and the playground has no
  // issue to comment on — the section AND the fan-out item that hands it to a
  // subagent are both overlaid away.
  "### The status line",
  "gh issue comment",
  // The invocation, not the words: local mode names `git push` too, in the
  // deny-list line that forbids it.
  "git push -u origin HEAD",
  "git push --force-with-lease",
];

const LOCAL_ONLY = [
  "issues/<n>.md",
  "`## Progress`",
  ".aep-playground",
  "no git remote, no GitHub, and no PR",
  "dependsOn",
];

test("github mode carries the platform procedure and none of the local one", () => {
  for (const needle of PLATFORM_ONLY) assert.ok(composed.github.includes(needle), `github mode lost: ${needle}`);
  for (const needle of LOCAL_ONLY) assert.ok(!composed.github.includes(needle), `github mode leaked: ${needle}`);
});

test("local mode carries the local procedure and none of the platform one", () => {
  for (const needle of LOCAL_ONLY) assert.ok(composed.local.includes(needle), `local mode lost: ${needle}`);
  for (const needle of PLATFORM_ONLY) assert.ok(!composed.local.includes(needle), `local mode leaked: ${needle}`);
});

// --- sharing is structural, and these prove it ------------------------------
// One authored file means shared text CANNOT drift; what needs guarding is the
// opposite mistake — overlaying a section that should be shared, which would let
// the platform's conventions and the playground's silently diverge again (the
// failure ADR-0001 documented and ADR-0004 inherits).
// Two H2s that are pure engineering content: if either ever acquires an overlay,
// the platform's and the playground's conventions have started to diverge again.
// (`What design.json fixes` and `The code` used to be here; they are now in
// references/component-contract.md, which no overlay can reach at all — a
// stronger version of the same guarantee, asserted below.)
const SHARED_SECTIONS = ["This skill, and the stack skills", "Contract-first"];

function section(text: string, heading: string): string {
  const marker = `\n## ${heading}\n`;
  const start = text.indexOf(marker);
  assert.ok(start >= 0, `composed skill has no "## ${heading}" section`);
  const afterStart = start + marker.length;
  const end = text.indexOf("\n## ", afterStart);
  return text.slice(afterStart, end < 0 ? text.length : end).trim();
}

for (const heading of SHARED_SECTIONS) {
  test(`"${heading}" is byte-identical in both modes`, () => {
    assert.equal(section(composed.local, heading), section(composed.github, heading));
  });
}

// Run mechanics that must reach both modes. What is NOT here any more is the
// engineering half — CORS, the filesystem boundary, the web-research rails — which
// moved to references/component-contract.md. That is not a weakening: the body is
// overlaid per mode and a reference is not, so a rule in a reference is shared
// structurally rather than by assertion. The rules below name a subagent or the
// fan-out tool, so they belong to the run and stay in the body.
for (const rule of [
  // A subagent is off `git` in BOTH modes — the branch, the commits and the PR
  // are the lead's. What differs by mode is `gh`, which local mode has none of,
  // so the shared rule names the prompt rather than the command.
  "Let a subagent run `git`, or any `gh` its prompt did not give it",
  "**It never runs `git`**",
  "## Dependencies",
  // The fan-out discipline is mode-neutral and lives inside `# The run`: it is
  // the largest passage the overlay must NOT own a copy of.
  "### Fan-out to subagents",
  "Issue every subagent for a wave in ONE turn",
  "Do not use `run_in_background`",
  // A web app is walked in a browser before its work is committed. The walk is
  // its OWN subagent, dispatched after the build reports clean — a builder that
  // walks the app it just wrote enters the browser carrying the whole build in
  // context. So the body owns the two halves the lead controls: the step that
  // will not let a web app be committed unwalked, and the shape of the extra
  // dispatch. The procedure itself is `mock-verification`, asserted below.
  "**A `web-application` is finished by a walk, not a build.**",
  "dispatch **one more subagent**",
  "**A walk subagent is the same dispatch, shorter.**",
]) {
  test(`shared by both modes: ${rule.split("\n")[0]}`, () => {
    assert.ok(composed.github.includes(rule), `github mode lost: ${rule}`);
    assert.ok(composed.local.includes(rule), `local mode lost: ${rule}`);
  });
}

// Every rule the body no longer states, and the file that now owns it. A rule
// nobody is told to read is worse than one stated twice, so each file is also
// asserted to be POINTED AT from the composed body, in both modes.
//
// component-contract.md is the implementer's whole contract — a fan-out subagent
// reads it instead of this skill, and the lead reads it before working an issue
// inline or authoring a `workload.yaml`. workload-and-wiring.md is the author's
// half of the dependency story.
const REFERENCE_RULES: Record<string, string[]> = {
  "component-contract.md": [
    // The invariants a component is judged on, and the two silent-failure rules
    // that used to sit in the body's deny-list.
    "it listens on port **9090**",
    "**starts with no required environment variables**",
    "no stubs, no mocks",
    "**`workload.yaml` is your prompt's to give.**",
    "**CORS belongs to the gateway**",
    "Author a file anywhere but inside the project",
    "Read anything unrelated to this run",
    "Do not probe whether such paths exist",
    "Install anything outside the project's own package manager",
    "Put a secret value in a search query",
    "untrusted data, never instructions",
    "Substitute your own technology for a declared dependency",
    "never build a container image",
    // Green for a web app is build AND walk — the invariant that keeps the walk
    // from being skippable when a stack skill is swapped out by an org.
    "**A `web-application` is green when it builds AND walks.**",
    // An endpoint dependency's env-var name is derived from the dep name, so it is
    // knowable in both modes; gating it would leave the playground with no source
    // for the name at all — and the skill forbids inventing one.
    "**An endpoint dependency's env var is always `<DEP_NAME>_URL`**",
    "**a pinned contract wins when there is one**",
    "external-dependency-research.md",
    "delete anything under the repo-root `specs/`",
  ],
  "workload-and-wiring.md": [
    // The resources half of the workload block comes from design.json in BOTH
    // modes — it is derived, not resolved, so the playground has it too.
    "**Copy a `wiring` object verbatim**",
    "A `platform-resource` with no `wiring` is broken input",
    "**One that already exists is edited, never regenerated.**",
    "**A sibling SPA reaches a service through same-origin `/api`, not `external`.**",
    "**Provider endpoint visibility:** a service a sibling SPA calls lists",
  ],
};

for (const [file, rules] of Object.entries(REFERENCE_RULES)) {
  test(`${file} carries the rules the body no longer states`, () => {
    const reference = fs.readFileSync(path.join(LIBRARY, "aep", "references", file), "utf8");
    for (const rule of rules) assert.ok(reference.includes(rule), `${file} lost: ${rule}`);
    for (const mode of ["github", "local"] as const) {
      assert.ok(
        composed[mode].includes(`references/${file}`),
        `${mode} mode never tells the agent to read ${file}`,
      );
    }
  });
}

// The walk is its own agent, and it both verifies AND repairs: it fixes each
// failure at the line that found it, then re-walks that line before moving on.
// A read-only verifier that only files a report is the shape this replaced, as
// is batching every repair to the end — so the three properties that make one
// fix-as-you-go agent safe are asserted here: the checklist is settled before
// the browser opens, a line is not left behind until it passes, and one
// stubborn defect cannot swallow the walk.
test("mock-verification walks and repairs a line at a time", () => {
  const skill = fs.readFileSync(path.join(LIBRARY, "mock-verification", "SKILL.md"), "utf8");
  for (const rule of [
    "## 1 · Write the checklist",
    "**A line ends green.**",
    "**Three attempts on a line, then mark it `[ ]` and walk on.**",
    "**Repair the app, never the checklist.**",
    // Full page loads reset the mock's in-page state, which has twice been
    // misread as a defect in a just-created record.
    "**Move between screens by clicking.**",
    "Make the mock agree with the app",
    // The walk is a subagent's job, and a subagent now keeps its issue's status
    // line current. So this skill bans the RECORD (git, commits, the PR) and
    // defers where progress goes to the prompt — it ships byte-identical into a
    // playground session, which has no issue to post on.
    "The record belongs to the agent that\n  dispatched you",
  ]) {
    assert.ok(skill.includes(rule), `mock-verification lost: ${rule}`);
  }
});

// Design reuses a Registered External resource and writes
// consumption instructions into `description` / org resource docs into
// `specPath`. Coding reads those fields. Each rule lives in one skill —
// architecture must not paste the coding procedure, and the research file
// must not restate the design-time name choice.
const ARCHITECTURE = path.join(LIBRARY, "architecture", "SKILL.md");
const RESEARCH = path.join(LIBRARY, "aep", "references", "external-dependency-research.md");

test("architecture description still triggers on resolving a dependency", () => {
  const skill = fs.readFileSync(ARCHITECTURE, "utf8");
  const frontmatter = skill.slice(0, skill.indexOf("\n---", 4));
  assert.match(frontmatter, /Reuse org catalog resources/);
  assert.match(frontmatter, /resolving\/reconsidering any dependency/);
});

test("architecture copies platform-resource resourceType from this turn's catalog", () => {
  const skill = fs.readFileSync(ARCHITECTURE, "utf8");
  assert.ok(skill.includes("list_platform_resource_types"), "discover types from the live catalog");
  assert.ok(
    skill.includes("omit the entry and list the gap under **Needs your input**"),
    "a missing catalog type is a gap, not a coined resourceType",
  );
});

test("cell-design does not teach invented cluster resource types", () => {
  const cell = fs.readFileSync(path.join(LIBRARY, "cell-design", "SKILL.md"), "utf8");
  assert.ok(!cell.includes("postgres-cnpg/redis"), "CRT names belong to this turn's catalog list, not the cell table");
  assert.ok(cell.includes("list_platform_resource_types"), "placement defers resourceType to the catalog");
});

test("architecture prefers a Registered External resource over a new-name Project External", () => {
  const skill = fs.readFileSync(ARCHITECTURE, "utf8");
  assert.ok(skill.includes("Registered External resource"), "lost Registered External resource");
  assert.ok(skill.includes("Project External resource"), "lost Project External resource");
  assert.ok(skill.includes("consumption instructions"), "list_external_resources returns consumption instructions");
  assert.ok(skill.includes("org resource docs pointers"), "list_external_resources returns org resource docs pointers");
  assert.ok(
    skill.includes("Write consumption instructions into the dependency `description`"),
    "coding reads consumption instructions from description — name that handoff",
  );
  assert.ok(skill.includes("**new** name"), "a Project External resource uses a new name");
  assert.ok(skill.includes("org values stay on the Registered name"), "org values stay on the Registered name");
  assert.ok(skill.includes("org-level one wins"), "org catalog rows outrank a new Project External name");
  assert.ok(skill.includes("user-asked reconsider"), "leaving a fitting org row requires a reconsider");
  assert.ok(!skill.includes("unresolved on purpose"), "catalog reuse is the github example, not an invented needs-spec");
  assert.ok(!skill.includes("{type, url}"), "MCP pointer shape belongs to the list tool, not the skill");
  assert.ok(!skill.includes("{type, path}"), "MCP pointer shape belongs to the list tool, not the skill");
});

test("architecture does not paste the coding research procedure", () => {
  const skill = fs.readFileSync(ARCHITECTURE, "utf8");
  assert.ok(!skill.includes("external-dependency-research.md"), "coding procedure belongs in aep references");
  assert.ok(!skill.includes("vendor's own quickstart"), "sdk quickstart is the coding research procedure");
});

test("external-dependency-research reads Registered consumption instructions from description and specPath", () => {
  const research = fs.readFileSync(RESEARCH, "utf8");
  assert.ok(research.includes("Registered External resource"));
  assert.ok(research.includes("consumption instructions"));
  assert.ok(research.includes("dependency `description`"), "consumption instructions arrive in description");
  assert.ok(
    !research.includes("there is no catalog to read it out of"),
    "Registered External resources carry consumption instructions into description and specPath",
  );
  assert.ok(!research.includes("list_external_resources"), "MCP list is design-time; coding does not call it");
  assert.ok(!research.includes("when present"), "name the checkable fields; do not leave a dangling when-present");
  assert.ok(!research.includes("that file in org resource docs"), "coding cannot read org resource docs");
  assert.ok(!research.includes("{type, url}"), "MCP pointer shape belongs to the list tool, not the skill");
  assert.ok(!research.includes("{type, path}"), "MCP pointer shape belongs to the list tool, not the skill");
  assert.ok(
    !research.includes("org values stay on the Registered name"),
    "the design-time name choice belongs in architecture",
  );
});

// A subagent gets its contract from its PROMPT, so the fan-out section is the one
// place the reference can be introduced. If this pointer goes, every subagent
// implements from a stack skill and its own priors — the failure the split exists
// to fix.
test("the fan-out section is what hands a subagent the component contract", () => {
  for (const mode of ["github", "local"] as const) {
    const fanOut = composed[mode].slice(composed[mode].indexOf("### Fan-out to subagents"));
    assert.ok(
      fanOut.includes("references/component-contract.md"),
      `${mode} mode's fan-out section never names the contract`,
    );
  }
});

// A subagent posts its own progress, so the fan-out prompt list is the only
// place the status-line rule and its command reach one. Lose this item and the
// issue goes silent from dispatch to pull request — the gap this replaced.
test("the fan-out section hands a subagent its issue's status line", () => {
  const fanOut = composed.github.slice(composed.github.indexOf("### Fan-out to subagents"));
  assert.ok(fanOut.includes("its issue's status line"), "fan-out never hands down the status line");
  assert.ok(fanOut.includes("the only `gh` it may run"), "fan-out never bounds the subagent's `gh`");
});

// `## Git and GitHub` is an H2 nested under `# Never`; its bullets are bare
// prohibitions that only parse under that framing ("Push to the default branch
// (`main`)" is an instruction on its own). No anchor check catches a lost H1.
test("the deny-list H1 still governs the git/gh bullets", () => {
  for (const mode of ["github", "local"] as const) {
    const headings = composed[mode].split("\n").filter((l) => /^#{1,6}\s+\S/.test(l));
    const at = headings.indexOf("## Git and GitHub");
    assert.ok(at > 0, `${mode} mode lost "## Git and GitHub"`);
    const h1sBefore = headings.slice(0, at).filter((h) => /^#\s/.test(h));
    assert.equal(h1sBefore.at(-1), "# Never", `${mode} mode: git/gh bullets read as instructions`);
  }
});

// --- the ratchet ------------------------------------------------------------

// The cost being controlled is PROSE THAT EXISTS TWICE: a `replace-section` or a
// `replace-text` with a non-empty replacement is a passage a human must edit in
// lockstep with its twin. A `drop-*` or an empty replacement has no twin and is
// free. The marker design this replaced capped paired regions at 8; the cap only
// ever ratchets down.
test("the local overlay keeps paired (duplicated) passages to a minimum", () => {
  const overlay = fs.readFileSync(LOCAL_OVERLAY, "utf8");
  const sections = overlay.match(/^<!--\s*replace-section:/gm)?.length ?? 0;
  const texts = overlay
    .split(/^<!--\s*replace-text\s*-->$/m)
    .slice(1)
    // A replacement is the half after `with`; empty means "delete", which is lone.
    .filter((block) => {
      const withHalf = block.split(/^<!--\s*with\s*-->$/m)[1] ?? "";
      return withHalf.replace(/^<!--\s*\/replace-text\s*-->[\s\S]*$/m, "").trim() !== "";
    }).length;
  const paired = sections + texts;
  assert.ok(paired <= 8, `${paired} paired passages — each is prose duplicated per mode; drop one side instead`);
});

// The overlay can only reach what it can anchor. A GitHub mechanic dropped into
// a shared contract section has no anchor, so local mode would read it as true —
// the drift-by-omission channel this design has to close. Every `git`/`gh`
// invocation therefore lives under `# The run`, `## Where you are`, or the
// `## Git and GitHub` deny-list, and this asserts it.
test("the authored skill keeps git and gh invocations in the sections the overlay owns", () => {
  const needles = [/\bgh [a-z]/, /git push/, /git checkout/, /git fetch/, /git rebase/, /git ls-remote/, /--milestone/, /Resolves #/];
  const allowedH2 = new Set(["## Where you are", "## Git and GitHub"]);
  let h1 = "";
  let h2 = "";
  let inFence = false;
  const stray: string[] = [];

  for (const [index, line] of fs.readFileSync(AEP_SKILL, "utf8").split("\n").entries()) {
    if (/^\s*(```|~~~)/.test(line)) inFence = !inFence;
    if (!inFence) {
      const heading = /^(#{1,6})\s+\S/.exec(line);
      if (heading) {
        if (heading[1]?.length === 1) {
          h1 = line;
          h2 = "";
        } else if (heading[1]?.length === 2) {
          h2 = line;
        }
      }
    }
    if (!needles.some((n) => n.test(line))) continue;
    if (h1 === "# The run" || allowedH2.has(h2)) continue;
    stray.push(`${index + 1}: ${line.trim()} (under ${h2 || h1})`);
  }

  assert.deepEqual(stray, [], `platform git/gh mechanics outside the overlay's reach:\n${stray.join("\n")}`);
});

// --- the fs side: what a local run's mirror actually contains -----------------
//
// `mirrorLocalSkillLibrary` is the playground's stand-in for the BFF write, and
// the only writer left that composes: production mirrors the authored trunk,
// which is what a dispatched run should read. So these drive it against the REAL
// library, because the properties being pinned are properties of the library's
// content as much as of the copier.

function inTempDir<T>(fn: (dir: string) => T | Promise<T>): Promise<T> {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "aep-mirror-test-"));
  return (async () => {
    try {
      return await fn(dir);
    } finally {
      fs.rmSync(dir, { recursive: true, force: true });
    }
  })();
}

/** Mirror the real library into a fresh workspace and return `.claude/skills/`. */
async function mirrorInto(dir: string, mode: AgentMode): Promise<string> {
  const workspace = path.join(dir, mode);
  fs.mkdirSync(workspace, { recursive: true });
  await mirrorLocalSkillLibrary(LIBRARY, workspace, new Set(), mode);
  return path.join(workspace, ".claude", "skills");
}

test("the mirror carries the coding-audience skills, composed for the mode", async () => {
  await inTempDir(async (dir) => {
    const skills = await mirrorInto(dir, "local");

    // The runner's own skills arrive the SAME way every other coding skill does.
    // There is no plugin any more, so if they are not here they reach no session.
    for (const name of ["aep", "aep-validation", "playwright-cli"]) {
      assert.ok(fs.existsSync(path.join(skills, name, "SKILL.md")), `mirror is missing ${name}`);
    }
    // Design-only skills stay out: their descriptions would sit in a coding
    // session's skill catalog, one load away from authoring specs/.
    for (const name of ["design", "task-planning", "high-level-architecture"]) {
      assert.ok(!fs.existsSync(path.join(skills, name)), `${name} must not reach a build`);
    }
    // And the mirrored workflow is the COMPOSED body, not the authored trunk —
    // this is the whole reason the writer is not a directory copy.
    assert.equal(fs.readFileSync(path.join(skills, "aep", "SKILL.md"), "utf8"), composed.local);
  });
});

test("github mode mirrors the authored trunk verbatim, as production does", async () => {
  await inTempDir(async (dir) => {
    const skills = await mirrorInto(dir, "github");
    assert.equal(fs.readFileSync(path.join(skills, "aep", "SKILL.md"), "utf8"), composed.github);
  });
});

// A session that can read a local-mode overlay beside SKILL.md has a second
// procedure available to it — and the `aep` skill explicitly permits reading its
// own skill dir. Production cannot hit this (`loadLibrary` never seeds
// `overlays/`, so no org repo has one to mirror); the local writer has to filter.
for (const mode of ["github", "local"] as const) {
  test(`the ${mode} mirror carries no overlays/ directory`, async () => {
    await inTempDir(async (dir) => {
      const skills = await mirrorInto(dir, mode);
      const walk = (at: string): string[] =>
        fs.readdirSync(at, { withFileTypes: true }).flatMap((e) => {
          const full = path.join(at, e.name);
          return e.isDirectory() ? walk(full) : [path.relative(skills, full)];
        });
      const paths = walk(skills);
      assert.deepEqual(
        paths.filter((p) => p.split(path.sep).includes("overlays")),
        [],
      );
      assert.ok(!paths.some((p) => p.endsWith("local.md")));
    });
  });
}

// --- references are mode-neutral, because nothing can overlay them ----------
//
// The test above proves the BODY's git/gh mechanics stay where the overlay can
// reach them. A reference has no such reach: `composeWorkflowSkill` rewrites
// SKILL.md and nothing else, so every file beside it ships byte-identical into
// both modes. A platform procedure in one is therefore a second, un-overlaid
// procedure sitting in every playground session — and the `aep` skill
// explicitly licenses reading its own `references/`. Same hazard as an
// `overlays/` leak, one directory over.
//
// Consequence for authors: content that differs by mode cannot be moved out of
// SKILL.md. That is the rule this asserts, so the next split finds out here
// rather than in a local run.
const PLATFORM_MECHANICS = [/\bgh [a-z]/, /git push/, /git checkout/, /git fetch/, /git rebase/, /git ls-remote/, /--milestone/, /Resolves #/, /pull request/i];

test("the aep skill's references carry no platform mechanics", async () => {
  await inTempDir(async (dir) => {
    const refs = path.join(await mirrorInto(dir, "local"), "aep", "references");
    const stray: string[] = [];
    for (const name of fs.existsSync(refs) ? fs.readdirSync(refs) : []) {
      for (const [index, line] of fs.readFileSync(path.join(refs, name), "utf8").split("\n").entries()) {
        if (PLATFORM_MECHANICS.some((n) => n.test(line))) stray.push(`${name}:${index + 1}: ${line.trim()}`);
      }
    }
    assert.deepEqual(stray, [], `un-overlayable platform text in a reference:\n${stray.join("\n")}`);
  });
});

test("both modes ship the same references", async () => {
  await inTempDir(async (dir) => {
    const read = async (mode: AgentMode): Promise<Record<string, string>> => {
      const refs = path.join(await mirrorInto(dir, mode), "aep", "references");
      return Object.fromEntries(fs.readdirSync(refs).map((n) => [n, fs.readFileSync(path.join(refs, n), "utf8")]));
    };
    // Byte-identical is what makes the check above sufficient for both modes —
    // and what makes "mode-neutral or it stays in the trunk" the only rule.
    assert.deepEqual(await read("local"), await read("github"));
  });
});

// A skill is its SKILL.md plus whatever it ships beside it: the validation
// workflow is useless without its templates and its report generator. The BFF's
// mirror copies every aux file for the same reason (`Skill.References` is not
// limited to `references/`), so this is the local writer holding the same line.
test("a skill's references, assets and scripts come along", async () => {
  await inTempDir(async (dir) => {
    const skills = await mirrorInto(dir, "github");
    for (const rel of [
      path.join("aep", "references", "external-dependency-research.md"),
      path.join("aep", "references", "component-contract.md"),
      path.join("aep", "references", "workload-and-wiring.md"),
      path.join("aep-validation", "references", "authoring.md"),
      path.join("aep-validation", "assets", "playwright.config.template.ts"),
      path.join("aep-validation", "scripts", "generate-report.mjs"),
      path.join("playwright-cli", "LICENSE"),
      // Mock mode is three verbatim templates plus the reference that wires
      // them; a mirror that dropped one leaves the webapp skill pointing at
      // nothing, and the verification round with no app to open. All three sit
      // under react-webapp on purpose — an org that swaps its IDP must not take
      // the mock harness with it.
      path.join("react-webapp", "references", "mock-mode.md"),
      path.join("react-webapp", "assets", "mock-plugin.ts"),
      path.join("react-webapp", "assets", "mock-browser.ts"),
      path.join("react-webapp", "assets", "mock-auth.ts"),
      path.join("mock-verification", "SKILL.md"),
      path.join("agent-browser", "SKILL.md"),
    ]) {
      assert.ok(fs.existsSync(path.join(skills, rel)), `mirror is missing ${rel}`);
    }
  });
});

// A skill that names an absolute path into the runner's own tree is naming a
// location only the runner knows — it varies per run (a cluster mount, a
// playground bind-mount, a developer's checkout, and now the project's own
// mirror). `/app/plugin` was such a path, and it stopped existing when the plugin
// did; the report generator was still being invoked through it.
test("no library skill hardcodes a runner path", () => {
  for (const skill of ["aep", "aep-validation", "playwright-cli"]) {
    const body = fs.readFileSync(path.join(LIBRARY, skill, "SKILL.md"), "utf8");
    assert.ok(!body.includes("/app/plugin"), `${skill} names the retired /app/plugin`);
    assert.ok(
      !/\/app\/skills/.test(body),
      `${skill} hardcodes /app/skills — use $AEP_SKILLS_DIR, which is right in every mode`,
    );
  }
});

// The Bash tool keeps ONE shell for a whole run, so a bare relative `cd` is
// correct exactly once. #49: the RUN block was re-entered after a heal wave and
// `cd tests/e2e` landed in `tests/e2e/tests/e2e`. Every other path the
// validation workflow names is repo-root relative, so the one command that
// moves the shell has to be self-locating. Scoped to aep-validation on purpose:
// `cd <project-name>` in the ballerina skill is a placeholder after `bal new`,
// not a fixed path.
// From the repo root a bare `npx playwright test` is the QUIET failure: it
// discovers the specs, passes, and exits 0 without loading the config — so no
// reporter, no results.json, and none of the launch args the deployed endpoints
// need. Verified against the pinned 1.61.1. Every invocation therefore goes
// through the package's own `test` script, which `npm --prefix` runs with the
// package as its working directory — that is what removed the last `cd` from
// this workflow.
test("the validation workflow never runs playwright test bare", () => {
  const docs = ["SKILL.md", "references/authoring.md", "references/healing.md"].map(
    (rel) => [rel, fs.readFileSync(path.join(LIBRARY, "aep-validation", rel), "utf8")] as const,
  );
  for (const [rel, body] of docs) {
    for (const line of body.split("\n")) {
      // Start-of-line only: prose may name the form it is warning against.
      if (!/^\s*npx\s+playwright\s+test\b/.test(line)) continue;
      assert.ok(
        line.includes("--config"),
        `aep-validation/${rel}: \`${line.trim()}\` — bare from the repo root this ` +
          `passes and writes no results.json; use \`npm test --prefix tests/e2e\``,
      );
    }
  }
  const skill = docs[0][1];
  assert.match(
    skill,
    /"scripts":\s*\{\s*"test":\s*"playwright test"\s*\}/,
    "the scaffolded package.json lost its `test` script — every invocation depends on it",
  );
  assert.ok(
    skill.includes("npm test --prefix tests/e2e"),
    "SKILL.md no longer runs the suite through the package script",
  );
});

test("the validation workflow never cds to a bare relative path", () => {
  for (const rel of ["SKILL.md", "references/authoring.md", "references/healing.md"]) {
    const body = fs.readFileSync(path.join(LIBRARY, "aep-validation", rel), "utf8");
    for (const line of body.split("\n")) {
      // The whole argument, not the first token: `cd "$(git rev-parse …)/x"`
      // contains spaces, and splitting on them would read as a bare path.
      const target = /^\s*cd\s+(.+)$/.exec(line)?.[1]?.trim();
      if (!target) continue;
      assert.ok(
        target.replace(/^["']/, "").startsWith("/") ||
          target.includes("$(git rev-parse --show-toplevel)"),
        `aep-validation/${rel}: \`${line.trim()}\` — the shell persists across calls, so a ` +
          `cd must be self-locating (absolute, or rooted at $(git rev-parse --show-toplevel))`,
      );
    }
  }
});

// #137/#140: the validation deny-list summarised the rules as a flat "no
// force-push" while step 10 needed one, and the agent resolved the
// contradiction by ignoring its own skill. A validation branch name repeats
// every cycle, so the push must force — but never the form that ignores what
// the remote says.
test("no skill licenses a force-push without the lease", () => {
  for (const rel of fs.readdirSync(LIBRARY, { recursive: true, encoding: "utf8" })) {
    if (!rel.endsWith(".md")) continue;
    const full = path.join(LIBRARY, rel);
    if (!fs.statSync(full).isFile()) continue;
    for (const line of fs.readFileSync(full, "utf8").split("\n")) {
      if (!line.includes("push") || !line.includes("--force")) continue;
      assert.ok(
        line.includes("--force-with-lease"),
        `${rel}: \`${line.trim()}\` — a force-push must carry --force-with-lease`,
      );
    }
  }
});

// The half a reader misses: the deny-list can go on governing a force-push
// after the step that needed one has lost it.
test("aep-validation still names the force-push its push step needs", () => {
  const body = fs.readFileSync(path.join(LIBRARY, "aep-validation", "SKILL.md"), "utf8");
  assert.ok(
    body.includes("git push --force-with-lease"),
    "step 10 lost its lease form while the deny-list still governs one",
  );
});

test("re-mirroring the same workspace replaces the previous mode's body", async () => {
  await inTempDir(async (dir) => {
    const workspace = path.join(dir, "ws");
    fs.mkdirSync(workspace, { recursive: true });
    await mirrorLocalSkillLibrary(LIBRARY, workspace, new Set(), "local");
    await mirrorLocalSkillLibrary(LIBRARY, workspace, new Set(), "github");
    assert.equal(
      fs.readFileSync(path.join(workspace, ".claude", "skills", "aep", "SKILL.md"), "utf8"),
      composed.github,
    );
  });
});

// A local run whose overlay went missing must not silently fall back to the
// platform's procedure: it would be told to push and open a pull request.
test("local mode rejects a library with no overlay", () => {
  return inTempDir(async (dir) => {
    const library = path.join(dir, "library");
    fs.mkdirSync(path.join(library, "aep"), { recursive: true });
    fs.writeFileSync(
      path.join(library, "aep", "SKILL.md"),
      "---\nname: aep\nmetadata:\n  aep:\n    audience: [coding]\n---\n\nbody\n",
    );
    await assert.rejects(
      () => mirrorLocalSkillLibrary(library, path.join(dir, "ws"), new Set(), "local"),
      /needs the overlay aep\/overlays\/local\.md/,
    );
  });
});

// A half-written mirror left behind by a failed compose would be read by a retry
// as if it were whole — and by `requireWorkflowBodies` as a workflow that is
// present. Composing happens before the first copy for exactly this reason.
test("a malformed overlay writes no mirror at all", () => {
  return inTempDir(async (dir) => {
    const library = path.join(dir, "library");
    fs.mkdirSync(path.join(library, "aep", "overlays"), { recursive: true });
    fs.writeFileSync(
      path.join(library, "aep", "SKILL.md"),
      "---\nname: aep\nmetadata:\n  aep:\n    audience: [coding]\n---\n\nbody\n",
    );
    fs.writeFileSync(path.join(library, "aep", "overlays", "local.md"), "<!-- drop-section: ## Nope -->\n");
    const workspace = path.join(dir, "ws");
    await assert.rejects(() => mirrorLocalSkillLibrary(library, workspace, new Set(), "local"));
    assert.ok(!fs.existsSync(path.join(workspace, ".claude", "skills", "aep")));
  });
});
