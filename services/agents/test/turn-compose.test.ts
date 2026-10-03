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
import { composeInstruction, eagerSkillsFor, scopeFactFor, scopeNote, toolsetFor, wantsRegisterDraftTool } from "../src/prompts/turn.js";

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
  const withText = composeInstruction({ kind: "flow", skill: "interview", text: "F2" });
  assert.ok(withText.startsWith("Load the interview skill and follow it.\n\nF2"));
  // Whitespace-only trailing text is not text.
  assert.ok(
    composeInstruction({ kind: "flow", skill: "interview", text: "   " }).startsWith(
      "Load the interview skill and follow it.\n\nSpec sources",
    ),
  );
});

test("/design names the features this run designs, and bare designs every designable one", () => {
  assert.ok(
    composeInstruction({ kind: "flow", skill: "design", text: "F1 F2" }).startsWith(
      "Load the design skill and follow it.\n\nDesign these features: F1 F2",
    ),
  );
  assert.ok(
    composeInstruction({ kind: "flow", skill: "design" }).startsWith("Load the design skill and follow it.\n\nSpec sources"),
  );
});

test("/prototype loads its skill and the brief, and trailing component names follow the brief", () => {
  const bare = composeInstruction({ kind: "flow", skill: "prototype" });
  assert.ok(bare.startsWith("Load the prototype skill and follow it.\n\nGenerate the prototype of each web-application"));
  // The brief names what to read and what to write.
  assert.match(bare, /specs\/design\/design\.cell/);
  assert.match(bare, /specs\/design\/security\.json/);
  assert.match(bare, /specs\/design\/components\/<component>\/prototype\.json/);
  assert.match(bare, /manifest\) first and then prototype\.tsx/);
  assert.match(bare, /Spec sources live under specs\//);

  const named = composeInstruction({ kind: "flow", skill: "prototype", text: "approvals-portal" });
  const brief = named.indexOf("Generate the prototype of each web-application");
  assert.ok(brief > 0 && named.indexOf("\n\napprovals-portal") > brief, "component names come after the brief");

  // A flow with no brief is unchanged.
  assert.ok(!composeInstruction({ kind: "flow", skill: "design" }).includes("Generate the prototype"));
});

const FEEDBACK = {
  prototypeHash: "b".repeat(64),
  component: "approvals-portal",
  requests: [
    {
      screenId: "screen.queue",
      flowId: "flow.approve",
      roleId: "approver",
      stateId: "state.default",
      elementIds: ["btn.approve", "tbl.expenses"],
      text: "Put the Approve button on the left.\nMake it `primary`.",
    },
    { screenId: "screen.detail", roleId: "employee", stateId: "state.empty", elementIds: [], text: "Say why it is empty" },
  ],
};

test("a /prototype turn with feedback is a revision of that one prototype, not a generation", () => {
  const out = composeInstruction({ kind: "flow", skill: "prototype", prototypeFeedback: FEEDBACK });
  assert.ok(out.startsWith('Load the prototype skill and follow it.\n\nRevise the prototype of the web-application "approvals-portal"'));
  assert.ok(!out.includes("Generate the prototype of each web-application"));
  // The files it may change, and the revision the reviewer saw.
  assert.match(out, /specs\/design\/components\/approvals-portal\/prototype\.json/);
  assert.match(out, /specs\/design\/components\/approvals-portal\/prototype\.tsx/);
  assert.match(out, /change only those two files/);
  assert.ok(out.includes("b".repeat(64)));
  // Stable ids, and an answer per request.
  assert.match(out, /Keep every manifest key and element id you do not need to change/);
  assert.match(out, /answering each request by its number, as applied .* or declined/);
});

test("the revision brief lists each request's place and element ids and quotes its text verbatim", () => {
  const out = composeInstruction({ kind: "flow", skill: "prototype", prototypeFeedback: FEEDBACK });
  assert.match(
    out,
    /Request 1\nWhere: screen "screen\.queue", flow "flow\.approve", role "approver", display state "state\.default"\nElements \(ids\): btn\.approve, tbl\.expenses\nThe reviewer wrote:\n> Put the Approve button on the left\.\n> Make it `primary`\./,
  );
  // No flow, no element: the request is about the whole screen.
  assert.match(out, /Request 2\nWhere: screen "screen\.detail", role "employee", display state "state\.empty"\nElements: none selected/);
  assert.match(out, /> Say why it is empty/);
  assert.match(out, /made 2 requests below/);
  assert.match(composeInstruction({ kind: "flow", skill: "prototype", prototypeFeedback: { ...FEEDBACK, requests: [FEEDBACK.requests[1]!] } }), /made one request below/);
});

test("a /prototype turn without feedback is unchanged", () => {
  const out = composeInstruction({ kind: "flow", skill: "prototype" });
  assert.match(out, /Generate the prototype of each web-application/);
  assert.doesNotMatch(out, /Revise the prototype/);
});

test("/prototype inlines the skills that read the design and say how an Oxygen screen is composed", () => {
  assert.deepEqual(eagerSkillsFor({ kind: "flow", skill: "prototype" }), [
    "prototype",
    "cell-design",
    "security-design",
    "openapi-conventions",
    "oxygen-ui-design-system",
  ]);
});

test("the resolve command carries the user's answer after the dependency's name, verbatim", () => {
  // The definition's Service card sends `/resolve-dependency <name> <answer>`;
  // the token IS its skill, so the trailing text rides through untouched for
  // the skill to read as the choice. The plural ignores trailing text.
  const answered = composeInstruction({
    kind: "flow",
    skill: "resolve-dependency",
    text: "currency-converter Open Exchange Rates",
  });
  assert.ok(
    answered.startsWith("Load the resolve-dependency skill and follow it.\n\ncurrency-converter Open Exchange Rates"),
  );
  const url = composeInstruction({ kind: "flow", skill: "resolve-dependency", text: "mail https://x/openapi.yaml" });
  assert.ok(url.startsWith("Load the resolve-dependency skill and follow it.\n\nmail https://x/openapi.yaml"));
  const all = composeInstruction({ kind: "flow", skill: "resolve-dependencies", text: "ignored" });
  assert.ok(all.startsWith("Load the resolve-dependency skill and follow it."));
  assert.ok(!all.includes("ignored"));
});

/**
 * A command names the user's intent (`/feature`), a skill names an
 * engineer-facing playbook (`refine`). The console's older commands are doors
 * into that one loop, so each resolves to the skill AND says which branch —
 * carrying whatever the user clicked as the branch's subject, which is what
 * makes a lens on the PRD a complete instruction rather than a menu item the
 * user has to finish from memory (#579).
 */
test("a command that names a branch resolves to the skill and says which branch", () => {
  const feature = composeInstruction({ kind: "flow", skill: "feature", text: "receipt scanning" });
  assert.ok(feature.startsWith("Load the refine skill and follow it.\n\nAdd a feature: receipt scanning"));

  const actor = composeInstruction({ kind: "flow", skill: "actor", text: "Finance reviewer" });
  assert.ok(actor.startsWith("Load the refine skill and follow it.\n\nAdd an actor: Finance reviewer"));

  // Fired bare (the header's "+ Feature", where there is no line to carry) the
  // branch still arrives; the skill interviews for the subject.
  const bare = composeInstruction({ kind: "flow", skill: "feature" });
  assert.ok(bare.startsWith("Load the refine skill and follow it.\n\nAdd a feature."));

  // `/settle` on a clicked line carries the line; bare, it walks the Open Questions.
  const settle = composeInstruction({ kind: "flow", skill: "settle", text: "A rejected claim goes back." });
  assert.ok(settle.startsWith("Load the refine skill and follow it.\n\nSettle this point: A rejected claim goes back."));
  assert.ok(
    composeInstruction({ kind: "flow", skill: "settle" }).startsWith(
      "Load the refine skill and follow it.\n\nSettle the Open Questions, one at a time.",
    ),
  );
});

test("a branch command inlines the skill it resolves to, not its own token", () => {
  for (const token of ["feature", "actor", "amend", "settle", "refine"]) {
    assert.deepEqual(eagerSkillsFor({ kind: "flow", skill: token }), ["refine", "grilling", "prd-contract"]);
  }
  assert.deepEqual(eagerSkillsFor({ kind: "flow", skill: "interview" }), ["interview", "grilling", "prd-contract"]);
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

const APPROVALS = "specs/requirements/features/F2-approvals.md";

test("a feature scope leads the instruction, names the file, and fences nothing (S6)", () => {
  const scope = { kind: "feature", feature: "F2", file: APPROVALS } as const;
  const out = composeInstruction({ kind: "chat", text: "deputies can approve up to 500" }, { scope });
  assert.ok(out.startsWith(`The user is looking at feature F2, whose file is ${APPROVALS}`));
  assert.match(out, /Read specs\/requirements\/prd\.md and that file before you change anything\./);
  assert.match(out, /fences nothing/);
  assert.match(out, /deputies can approve up to 500/);
  // Unscoped → byte-identical to a turn sent before scopes existed.
  assert.equal(composeInstruction({ kind: "chat", text: "x" }, {}), composeInstruction({ kind: "chat", text: "x" }));
  assert.doesNotMatch(composeInstruction({ kind: "chat", text: "x" }), /looking at/);
});

test("a feature with no file yet is still named, with where its file goes", () => {
  const out = scopeNote({ kind: "feature", feature: "F7", file: null });
  assert.match(out, /feature F7, whose file \(specs\/requirements\/features\/F7-<name>\.md\) is not in the requirements yet/);
  assert.doesNotMatch(out, /that file before/);
});

test("the design review scope reads the message as about the design", () => {
  assert.match(scopeNote({ kind: "design-review" }), /^The user is in the design review/);
});

test("scopeFactFor finds a feature's file by its ID, never a longer ID's", () => {
  const paths = [
    "specs/requirements/prd.md",
    "specs/requirements/features/F20-audit.md",
    APPROVALS,
    "specs/requirements/features/F2-approvals/notes.md",
  ];
  assert.deepEqual(scopeFactFor({ kind: "feature", feature: "F2" }, paths), { kind: "feature", feature: "F2", file: APPROVALS });
  assert.deepEqual(scopeFactFor({ kind: "feature", feature: "F3" }, paths), { kind: "feature", feature: "F3", file: null });
  assert.deepEqual(scopeFactFor({ kind: "design-review" }, paths), { kind: "design-review" });
});

test("a failed previous turn leads the instruction (D20)", () => {
  const out = composeInstruction({ kind: "chat", text: "carry on" }, { previousTurnFailed: true });
  assert.ok(out.startsWith("Note: your previous turn's changes were NOT applied;"));
  assert.match(out, /carry on/);
  assert.doesNotMatch(composeInstruction({ kind: "chat", text: "carry on" }), /NOT applied/);
});

test("headless forbids the question tools, and trails everything else", () => {
  const out = composeInstruction({ kind: "start", idea: "a shop" }, { headless: true });
  assert.match(out, /do not call ask_question or ask_questions/);
  assert.ok(out.indexOf("Spec sources live under specs/") < out.indexOf("No interview is possible"), "headless trails the body");
});

test("plan carries no spec-paths rule and no scope — it writes no spec files", () => {
  const out = composeInstruction({ kind: "plan" }, { scope: { kind: "feature", feature: "F2", file: APPROVALS } });
  assert.ok(out.startsWith("Plan the implementation Tasks for this project."));
  assert.doesNotMatch(out, /Spec sources live under specs\//);
  assert.doesNotMatch(out, /looking at feature/);
});

test("plan scope marks each story COVERED or NEEDS TASKS", () => {
  const out = composeInstruction({
    kind: "plan",
    scope: {
      tag: "spec-v3",
      stories: [
        { id: "F1.1", title: "Sign in", covered: true },
        { id: "F2.4", covered: false },
      ],
    },
  });
  assert.match(out, /## Milestone scope \(spec spec-v3\)/);
  assert.match(out, /- Story F1\.1: Sign in — COVERED/);
  assert.match(out, /- Story F2\.4 — NEEDS TASKS/, "a story with no title still gets a row");
});

test("plan scope lists the version's features with their needs, and its product-wide items", () => {
  const out = composeInstruction({
    kind: "plan",
    scope: {
      tag: "v2",
      stories: [{ id: "F2.1", title: "Approve a claim", covered: false }],
      features: [{ id: "F1", name: "Submit a claim" }, { id: "F2", name: "Approvals", needs: ["F1"] }],
      productWide: [{ id: "P1", text: "Amounts in the user's currency", appliesTo: ["all"] }],
    },
  });
  assert.match(out, /- F1 Submit a claim\n- F2 Approvals — needs F1/);
  assert.match(out, /- P1: Amounts in the user's currency \(applies to all\)/);
  assert.ok(out.indexOf("- F2 Approvals") < out.indexOf("- Story F2.1"), "features come before the stories");
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
  assert.deepEqual(eagerSkillsFor({ kind: "flow", skill: "interview" }), ["interview", "grilling", "prd-contract"]);
  // A chat turn names no skill — its instruction is the user's own words —
  // unless it was sent with a feature open: then it is the refine loop (S4).
  assert.deepEqual(eagerSkillsFor({ kind: "chat", text: "x" }), []);
  assert.deepEqual(eagerSkillsFor({ kind: "chat", text: "x" }, { kind: "design-review" }), []);
  assert.deepEqual(eagerSkillsFor({ kind: "chat", text: "x" }, { kind: "feature", feature: "F2" }), [
    "refine",
    "grilling",
    "prd-contract",
  ]);
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
 * first question on a document it would not write until the next turn.
 * `interview` and `refine` write against the same contract without wanting the
 * cold-start playbook.
 */
test("both PRD-writing flows carry the contract as a skill, not a reference", () => {
  for (const turn of [
    { kind: "start" } as const,
    { kind: "flow", skill: "interview" } as const,
    { kind: "flow", skill: "refine" } as const,
  ]) {
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
    // mechanics are inlined here exactly as they are on start and interview.
    "grilling",
    "cell-design",
    "architecture",
    "security-design",
    "openapi-conventions",
    "wireframes",
    "agent-building",
    "acceptance-criteria",
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
    { kind: "flow", skill: "interview" } as const,
    { kind: "flow", skill: "refine" } as const,
    { kind: "flow", skill: "settle" } as const,
    { kind: "flow", skill: "design" } as const,
    { kind: "flow", skill: "prototype" } as const,
    // The branch commands resolve to a platform skill, so they are checked too.
    { kind: "flow", skill: "feature" } as const,
    { kind: "flow", skill: "actor" } as const,
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
    { kind: "flow", skill: "refine" } as const,
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

test("only the register-external-resource flow gets the draft tool", () => {
  assert.equal(wantsRegisterDraftTool({ kind: "flow", skill: "register-external-resource" }), true);
  assert.equal(wantsRegisterDraftTool({ kind: "flow", skill: "design" }), false);
  assert.equal(wantsRegisterDraftTool({ kind: "chat", text: "x" }), false);
  assert.equal(wantsRegisterDraftTool({ kind: "start" }), false);
});

test("every turn on the synthetic register project gets the draft tool", () => {
  assert.equal(
    wantsRegisterDraftTool({ kind: "chat", text: "Answer to \"auth?\": PAT" }, "__marketplace_register__"),
    true,
  );
  assert.equal(wantsRegisterDraftTool({ kind: "chat", text: "x" }, "weather-api"), false);
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
