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

/**
 * `composeInstruction` — a TurnSpec becomes prompt text HERE and nowhere else.
 * The assertions pin the properties a caller depends on (what leads, what is
 * appended to which kind, what a blank optional field does), not every byte of
 * wording: the text is meant to be edited, the structure is not.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { SURFACES } from "@aep/agent-stream";
import { composeInstruction, eagerSkillsFor, toolsetFor } from "../src/prompts/turn.js";

/** The platform skill library this monorepo publishes to every org. */
const SKILLS_DIR = path.resolve(fileURLToPath(import.meta.url), "../../../../skills");

test("chat rides verbatim, with the spec-paths rule appended", () => {
  const out = composeInstruction({ kind: "chat", text: "add a returns policy" });
  assert.ok(out.startsWith("add a returns policy"), "the user's words lead");
  assert.match(out, /Spec sources live under specs\//);
});

test("flow points at the skill, with the user's trailing text after a blank line", () => {
  assert.ok(
    composeInstruction({ kind: "flow", skill: "design" }).startsWith("Load the design skill and follow it."),
  );
  const withText = composeInstruction({ kind: "flow", skill: "amend", text: "add an actor" });
  assert.ok(withText.startsWith("Load the amend skill and follow it.\n\nadd an actor"));
  // Whitespace-only trailing text is not text.
  assert.ok(
    composeInstruction({ kind: "flow", skill: "amend", text: "   " }).startsWith(
      "Load the amend skill and follow it.\n\nSpec sources",
    ),
  );
});

/**
 * A command names the user's intent (`/feature`), a skill names an
 * engineer-facing playbook (`amend`). The three scoped edits are branches of
 * one playbook, so the command resolves to that skill AND says which branch —
 * carrying whatever the user clicked as the branch's subject, which is what
 * makes a lens on the PRD a complete instruction rather than a menu item the
 * user has to finish from memory (#579).
 */
test("a command that names a branch resolves to the skill and says which branch", () => {
  const feature = composeInstruction({ kind: "flow", skill: "feature", text: "receipt scanning" });
  assert.ok(feature.startsWith("Load the amend skill and follow it.\n\nAdd a feature: receipt scanning"));

  const actor = composeInstruction({ kind: "flow", skill: "actor", text: "Finance reviewer" });
  assert.ok(actor.startsWith("Load the amend skill and follow it.\n\nAdd an actor: Finance reviewer"));

  const expand = composeInstruction({
    kind: "flow",
    skill: "expand",
    text: "As an Employee, I want to submit an expense.",
  });
  assert.ok(
    expand.startsWith(
      "Load the amend skill and follow it.\n\nGo deeper on this feature: As an Employee, I want to submit an expense.",
    ),
  );

  // Fired bare (the header's "+ Feature", where there is no line to carry) the
  // branch still arrives; the skill interviews for the subject.
  const bare = composeInstruction({ kind: "flow", skill: "feature" });
  assert.ok(bare.startsWith("Load the amend skill and follow it.\n\nAdd a feature."));
});

test("a branch command inlines the skill it resolves to, not its own token", () => {
  for (const token of ["feature", "actor", "expand"]) {
    assert.deepEqual(eagerSkillsFor({ kind: "flow", skill: token }), [
      "amend",
      "grilling",
      "prd-contract",
    ]);
  }
  // `/settle` is its own skill, so nothing is remapped — but it revises the
  // same document and carries the same two supporting skills.
  assert.deepEqual(eagerSkillsFor({ kind: "flow", skill: "settle" }), [
    "settle",
    "grilling",
    "prd-contract",
  ]);
});

/**
 * Both skill maps are keyed by a name the caller supplies, so a name that
 * happens to live on `Object.prototype` must MISS them rather than inherit.
 * Indexed directly, `constructor` finds a function whose `.scope` is not one
 * (a thrown turn) and `toString` finds something `...` cannot spread — where
 * the user should simply be told the skill does not exist.
 *
 * `/constructor` is typable: the grammar admits any `[a-z0-9-]+`. `toString`
 * is not, but `skill` is an ordinary wire field and nothing downstream of the
 * parsers re-checks it.
 */
test("a skill named after an Object.prototype member is an ordinary unknown skill", () => {
  for (const token of ["constructor", "toString"]) {
    const out = composeInstruction({ kind: "flow", skill: token, text: "go" });
    assert.ok(out.startsWith(`Load the ${token} skill and follow it.\n\ngo`));
    assert.deepEqual(eagerSkillsFor({ kind: "flow", skill: token }), [token]);
  }
});

test("start appends the captured idea, and appends NOTHING when there is none", () => {
  const withIdea = composeInstruction({ kind: "start", idea: "an expense tracker" });
  assert.match(withIdea, /^Load the start skill and follow it\.\n\nThe user's idea for this project:\n\nan expense tracker/);
  // A bare kickoff must be byte-identical to a skill load — the start skill
  // then asks the user for the idea instead of inventing one.
  const bare = composeInstruction({ kind: "start" });
  assert.equal(bare, composeInstruction({ kind: "start", idea: "  " }));
  assert.doesNotMatch(bare, /The user's idea/);
});

test("start lists the reference documents, and lists NOTHING when there are none", () => {
  const withRefs = composeInstruction({
    kind: "start",
    idea: "an expense tracker",
    references: ["specs/requirements/references/rfp.pdf", "specs/requirements/references/glossary.md"],
  });
  // Every path is named, and the agent is told to read them as the brief.
  assert.match(withRefs, /specs\/requirements\/references\/rfp\.pdf/);
  assert.match(withRefs, /specs\/requirements\/references\/glossary\.md/);
  assert.match(withRefs, /reference document/i);
  // The idea still rides alongside them — the two channels are independent.
  assert.match(withRefs, /The user's idea for this project:\n\nan expense tracker/);

  // Absent and empty are the same thing, and both are byte-identical to a turn
  // from before this channel existed — a docless project sees no change at all.
  const bare = composeInstruction({ kind: "start", idea: "an expense tracker" });
  assert.equal(bare, composeInstruction({ kind: "start", idea: "an expense tracker", references: [] }));
  assert.doesNotMatch(bare, /reference document/i);
});


test("flow lists the reference documents, and lists NOTHING when there are none", () => {
  const withRefs = composeInstruction({
    kind: "flow",
    skill: "design",
    references: ["specs/requirements/references/sketch.png"],
  });
  assert.match(withRefs, /^Load the design skill and follow it\./);
  assert.match(withRefs, /specs\/requirements\/references\/sketch\.png/);
  assert.match(withRefs, /reference document/i);

  // Absent/empty → byte-identical to a plain flow turn.
  const bare = composeInstruction({ kind: "flow", skill: "design" });
  assert.equal(bare, composeInstruction({ kind: "flow", skill: "design", references: [] }));
  assert.doesNotMatch(bare, /reference document/i);
});

test("target is rendered by the service, never formatted by the caller", () => {
  const out = composeInstruction({ kind: "chat", text: "tighten the spec" }, { target: "specs/requirements/prd.md" });
  assert.ok(out.endsWith("(target: specs/requirements/prd.md)"));
  // Absent or blank → no suffix at all.
  assert.doesNotMatch(composeInstruction({ kind: "chat", text: "x" }, { target: "  " }), /\(target:/);
});

test("a failed previous turn leads the instruction (D20)", () => {
  const out = composeInstruction({ kind: "chat", text: "carry on" }, { previousTurnFailed: true });
  assert.ok(out.startsWith("Note: your previous turn's changes were NOT applied;"));
  assert.match(out, /carry on/);
  assert.doesNotMatch(composeInstruction({ kind: "chat", text: "carry on" }), /NOT applied/);
});

test("headless forbids the question tools, and trails everything else", () => {
  const out = composeInstruction({ kind: "start", idea: "a shop" }, { target: "specs/requirements/prd.md", headless: true });
  assert.match(out, /do not call ask_question or ask_questions/);
  assert.ok(out.indexOf("(target:") < out.indexOf("No interview is possible"), "modifiers trail the body");
});

test("plan carries no spec-paths rule and no target — it writes no spec files", () => {
  const out = composeInstruction({ kind: "plan" }, { target: "specs/requirements/prd.md" });
  assert.ok(out.startsWith("Plan the implementation Tasks for this project."));
  assert.doesNotMatch(out, /Spec sources live under specs\//);
  assert.doesNotMatch(out, /\(target:/);
});

test("plan scope marks each story COVERED or NEEDS TASKS", () => {
  const out = composeInstruction({
    kind: "plan",
    scope: {
      tag: "spec-v3",
      stories: [
        { number: 1, title: "Sign in", covered: true },
        { number: 4, covered: false },
      ],
    },
  });
  assert.match(out, /## Milestone scope \(spec spec-v3\)/);
  assert.match(out, /- Story 1: Sign in — COVERED/);
  assert.match(out, /- Story 4 — NEEDS TASKS/, "a story with no title still gets a row");
});

test("an empty scope renders nothing", () => {
  // The base directive mentions a "Milestone scope" section, so match the
  // HEADING — the thing the block actually emits.
  assert.doesNotMatch(
    composeInstruction({ kind: "plan", scope: { tag: "t", stories: [] } }),
    /## Milestone scope/,
  );
});

test("plan context is sorted by path, so the same inputs give the same prompt", () => {
  const out = composeInstruction({
    kind: "plan",
    taskContext: [
      { path: "tasks/9.md", body: "nine" },
      { path: "tasks/2.md", body: "two" },
    ],
  });
  assert.match(out, /## Existing open Tasks in this version \(reference\)/);
  assert.ok(out.indexOf("tasks/2.md") < out.indexOf("tasks/9.md"), "deterministic order");
  assert.match(out, /\n--- tasks\/2\.md ---\ntwo\n/);
});

test("eager skills are derived from the flow, not supplied by the caller", () => {
  assert.deepEqual(eagerSkillsFor({ kind: "start" }), ["start", "grilling", "prd-contract"]);
  assert.deepEqual(eagerSkillsFor({ kind: "flow", skill: "amend" }), ["amend", "grilling", "prd-contract"]);
  // Only a chat turn names no skill — its instruction is the user's own words.
  assert.deepEqual(eagerSkillsFor({ kind: "chat", text: "x" }), []);
});

/**
 * Every non-chat instruction opens with "Load the <skill> skill and follow it", so
 * the named skill is ALWAYS inlined — otherwise the turn spends a whole model step
 * asking for a body we already hold (measured: 3.8s on `/start`, 3.6s on a plan
 * turn). A flow with no supporting skills inlines exactly its own.
 */
test("the instructed skill is always inlined, whatever the flow", () => {
  assert.deepEqual(eagerSkillsFor({ kind: "plan" }), ["task-planning"]);
  assert.deepEqual(eagerSkillsFor({ kind: "flow", skill: "wireframes" }), ["wireframes"]);
  // Resolution runs through the SkillSource, so an org-authored flow inlines too;
  // a name that resolves to nothing is skipped downstream, not here.
  assert.deepEqual(eagerSkillsFor({ kind: "flow", skill: "their-own-skill" }), ["their-own-skill"]);
});

/**
 * The PRD contract is a SIBLING skill, not a `start` reference: the model read the
 * `start` playbook, saw it cited, and spent a `loadSkillReference` step before the
 * first question on a document it would not write until the next turn. `amend`
 * writes against the same contract without wanting the cold-start playbook.
 */
test("both PRD-writing flows carry the contract as a skill, not a reference", () => {
  for (const turn of [{ kind: "start" } as const, { kind: "flow", skill: "amend" } as const]) {
    assert.ok(eagerSkillsFor(turn).includes("prd-contract"));
  }
  assert.ok(!fs.existsSync(path.join(SKILLS_DIR, "start", "references", "prd-contract.md")));
});

/**
 * The design flow inlines its whole lineup, so the list is pinned rather than
 * spot-checked: dropping a name silently reintroduces the `loadSkill` round trip
 * this exists to remove.
 */
test("the design flow inlines its whole lineup, in lineup order", () => {
  assert.deepEqual(eagerSkillsFor({ kind: "flow", skill: "design" }), [
    "design",
    // The design flow interviews at design altitude (#578), so the question
    // mechanics are inlined here exactly as they are on start and amend.
    "grilling",
    "cell-design",
    "architecture",
    "security-design",
    "openapi-conventions",
    "wireframes",
    "validation-criteria",
  ]);
});

/**
 * A name that resolves to nothing is skipped SILENTLY by `buildEagerSkillsBlock`
 * (org catalogs vary, so an absent skill must not fail a turn). That makes a typo
 * here invisible at runtime — it just quietly stops inlining. This is the drift
 * guard: every platform-flow eager name must exist in the library.
 *
 * Only PLATFORM flows are checked. `/<org-skill>` inlines a name this repo has
 * never heard of, which is the feature, not drift.
 */
test("every eager skill name exists in the platform skill library", () => {
  const turns = [
    { kind: "start" } as const,
    { kind: "plan" } as const,
    { kind: "flow", skill: "amend" } as const,
    { kind: "flow", skill: "settle" } as const,
    { kind: "flow", skill: "design" } as const,
    // The branch commands resolve to a platform skill, so they are checked too.
    { kind: "flow", skill: "feature" } as const,
    { kind: "flow", skill: "actor" } as const,
    { kind: "flow", skill: "expand" } as const,
  ];
  for (const turn of turns) {
    for (const name of eagerSkillsFor(turn)) {
      assert.ok(
        fs.existsSync(path.join(SKILLS_DIR, name, "SKILL.md")),
        `eager skill ${name} has no skills/${name}/SKILL.md — it would be skipped silently`,
      );
    }
  }
});

test("`organization` is never eager — it rides the system prompt on every turn", () => {
  for (const turn of [
    { kind: "start" } as const,
    { kind: "flow", skill: "amend" } as const,
    { kind: "flow", skill: "design" } as const,
  ]) {
    assert.ok(!eagerSkillsFor(turn).includes("organization"), `${JSON.stringify(turn)} must not inline it twice`);
  }
});

test("the tool set is derived from the kind", () => {
  assert.equal(toolsetFor({ kind: "plan" }), "task-plan");
  assert.equal(toolsetFor({ kind: "chat", text: "x" }), "files");
  assert.equal(toolsetFor({ kind: "start" }), "files");
  assert.equal(toolsetFor({ kind: "flow", skill: "design" }), "files");
});

/**
 * A surface names its own narration skill, and `buildNarrationBlock` skips a
 * name that resolves to nothing — so renaming the directory would silently
 * take the console's narration rules off every turn rather than fail anything.
 * This is that drift guard.
 */
test("every surface has a narration skill in the library, and it is design-side", () => {
  for (const surface of SURFACES) {
    const body = fs.readFileSync(path.join(SKILLS_DIR, surface, "SKILL.md"), "utf8");
    assert.match(body, new RegExp(`^name: ${surface}$`, "m"), "frontmatter name must match the directory");
    assert.match(body, /audience: \[design\]/, "narration is the design agent's — never mirrored to a coding run");
    // The composer supplies `# Narration policy`; a title in the file renders twice.
    assert.doesNotMatch(body.replace(/^---[\s\S]*?^---/m, ""), /^# /m);
  }
});
