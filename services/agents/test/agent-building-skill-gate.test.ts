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

// The store in `skills/agent-building/SKILL.md` is COPIED VERBATIM into every
// generated agent, so that markdown is production source that no compiler,
// linter, or type-checker ever sees. `agent-chat-contract.test.ts` does not
// cover it either: that file defines its own store, so the skill's SQL could
// lose its user scope entirely and every test would stay green.
//
// This gate is deliberately narrow. It does not check that the SQL is good;
// it checks that the specific lines the skill itself calls "the security
// boundary" are still present. A reviewer editing that section has to see
// this fail and decide, rather than delete a fence by accident.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, it } from "node:test";

// The prescribed code lives in the skill's IMPLEMENTATION reference, not its
// body: the body is the contract both the design and coding halves share, and
// pinning puts it in the coding agent's context at startup. The store is what
// that agent reads on demand — and it is the file whose SQL carries the user
// fence, so it is the file this gate has to watch.
const SKILL = readFileSync(
  fileURLToPath(
    new URL("../../../skills/agent-building/references/building.md", import.meta.url),
  ),
  "utf8",
);
const BODY = readFileSync(
  fileURLToPath(new URL("../../../skills/agent-building/SKILL.md", import.meta.url)),
  "utf8",
);

describe("agent-building SKILL.md — prescribed store invariants", () => {
  it("scopes the conversation read by user, not by id alone", () => {
    assert.match(
      SKILL,
      /SELECT messages FROM conversations WHERE id = \$1 AND user_id = \$2/,
      "the SELECT lost its `AND user_id = $2` — any caller holding an id could read another user's conversation",
    );
  });

  it("scopes the conversation write by user, so an upsert cannot cross users", () => {
    assert.match(
      SKILL,
      /WHERE conversations\.user_id = \$2/,
      "the upsert lost its user fence — a caller could overwrite another user's conversation by supplying their id",
    );
  });

  it("guards the id before it reaches Postgres' uuid cast", () => {
    assert.match(
      SKILL,
      /UUID_RE/,
      "the malformed-id guard is gone — a non-uuid id would 500 with a Postgres cast error instead of reading as not-found",
    );
  });

  it("never tells the agent to adopt a caller-chosen conversation id", () => {
    assert.doesNotMatch(
      BODY,
      /Absent or unknown on\s+the way in means "new conversation"/,
      'the prose that told the agent to treat an UNKNOWN id as a new conversation is back — it invites a caller to pick another user\'s id',
    );
  });

  it("prescribes node:http, not an Express response API", () => {
    assert.doesNotMatch(
      SKILL,
      /res\.status\(\d+\)\.end\(\)/,
      "an Express call is prescribed in a file that mandates node:http — generated agents would not compile",
    );
  });
});

// The scenarios half of the same skill: prose an agent follows at build time,
// so the same argument applies — nothing else in this repo would notice if a
// line went.
const DESIGNING = readFileSync(
  fileURLToPath(
    new URL("../../../skills/agent-building/references/designing.md", import.meta.url),
  ),
  "utf8",
);

// The command line in the skill is the ONLY thing that runs the harness during
// a build. A flag that drifts from `packages/agent-eval/src/cli.ts` does not
// fail anything here or there — it fails inside a build, as a report nobody
// asked for, so the flags are pinned where a coding agent reads them.
describe("agent-building — the evaluation step", () => {
  // The COMMAND, not the file: the flags are also described in a table beside
  // it, and a table entry is not what a build runs. Matching the whole file
  // would stay green with a flag missing from the only line that executes.
  const invocation = /```bash\n([\s\S]*?agent-eval[\s\S]*?)```/.exec(SKILL)?.[1] ?? "";

  // The loop's rules, anchored to the subsection that states them. The rest of
  // the file talks about front matter and about the allow-list as a boundary
  // for its own reasons — matched against the whole file, an assertion named
  // for the loop would be satisfied by prose that says nothing about the loop,
  // and could not fail if the rule were deleted.
  const fixLoop = /\n### The fix loop\n([\s\S]*?)\n### /.exec(SKILL)?.[1] ?? "";

  // The credential paragraph, and ONLY it. `ANTHROPIC_API_KEY` and the word
  // "key" appear all over this file — a rule matched against the whole thing
  // would be satisfied by the flag table or by the harness description and
  // could not fail when the rule itself was deleted.
  // Whitespace collapsed, because the paragraph is hard-wrapped and a sentence
  // may sit across two lines. Matching the raw text would make this gate fail on
  // a pure re-wrap and — worse — pass a deletion that happened to leave the
  // phrase split differently.
  const credential = (
    /\nThe organisation's Anthropic key is the credential,([\s\S]*?)\n\n/.exec(SKILL)?.[1] ?? ""
  )
    .replace(/\s+/g, " ")
    .trim();

  it("names the harness and every flag its CLI requires", () => {
    assert.notEqual(invocation, "", "the build reference no longer carries a runnable eval command");
    for (const flag of ["--scenarios", "--app", "--afm", "--out"]) {
      assert.match(
        invocation,
        new RegExp(`\\${flag}\\b`),
        `the command lost ${flag} — the CLI requires all four and reports a failure without them`,
      );
    }
  });

  it("invokes the workspace binary, never a registry fetch", () => {
    assert.match(invocation, /packages\/agent-eval\/dist\/bin\/agent-eval\.js/);
    assert.doesNotMatch(
      SKILL,
      /npx[^\n]*agent-eval/,
      "`@aep/agent-eval` is a private workspace package — an npx invocation fetches whatever the registry has under that name",
    );
    assert.doesNotMatch(SKILL, /@latest/, "an unpinned tool version makes a score unreproducible");
  });

  // The build pod is the case this command has to get right, and it is the one
  // nobody runs by hand. `git rev-parse --show-toplevel` answers with the
  // GENERATED PROJECT there — it is its own git repository — so a command that
  // resolved the harness that way and only that way would work everywhere it was
  // tried and nowhere it actually runs.
  it("resolves the harness in a build pod as well as in the monorepo", () => {
    assert.match(
      invocation,
      /command -v agent-eval/,
      "the command no longer detects the harness the runner image installs — in a pod there is no monorepo to build from",
    );
    assert.match(
      invocation,
      /packages\/agent-eval\/dist\/bin\/agent-eval\.js/,
      "the monorepo fallback is gone — a playground or local run has no `agent-eval` on PATH",
    );
  });

  // A key on the command line is a key in a build log.
  it("leaves the model credential to the harness", () => {
    assert.doesNotMatch(
      invocation,
      /ANTHROPIC_API_KEY=|export [A-Z_]*ANTHROPIC/,
      "the command sets a credential itself — the harness reads it from the environment the platform mounted",
    );
    assert.match(
      SKILL,
      /AEP_EVAL_ANTHROPIC_API_KEY/,
      "the reference no longer names the variable the platform mounts the org's key under",
    );
    assert.match(SKILL, /Never\s+`CLAUDE_CODE_OAUTH_TOKEN`/);
  });

  // The one instruction standing between a build agent and re-opening by hand
  // the hole the platform closes for it. In a pod `ANTHROPIC_API_KEY` holds the
  // organisation's CODING credential, deliberately withheld from evaluation; an
  // agent that reads "no key" and helpfully copies the key it can see would
  // grade agents on a budget the org ring-fenced. Ungated, that instruction is a
  // suggestion — so it is pinned to the paragraph that carries it.
  it("forbids re-using the pod's coding key as the evaluation key", () => {
    assert.notEqual(credential, "", "the reference no longer has a credential paragraph at all");
    assert.match(
      credential,
      /do NOT copy the\s+`?ANTHROPIC_API_KEY`?/,
      "nothing tells the build agent not to copy the key it can see into the evaluation variable",
    );
    assert.match(
      credential,
      /CODING credential/,
      "the paragraph no longer says WHAT the pod's ANTHROPIC_API_KEY is, so the prohibition reads as arbitrary",
    );
    assert.match(
      credential,
      /No evaluation is the correct outcome/,
      "without this the agent is told not to fix it but not that leaving it unfixed is right",
    );
  });

  it("bounds the fix loop where the agent can read it", () => {
    assert.notEqual(fixLoop, "", "the reference no longer describes a bounded fix loop at all");
    assert.match(fixLoop, /at most 3|three rounds/i);
    assert.match(fixLoop, /best-scoring prompt/, "the loop could ship the last prompt instead of the best one");
  });

  it("states the threshold and the zero tolerance that goes with it", () => {
    assert.match(fixLoop, /0\.8/, "the 0.8 threshold is gone — the loop has no bar to revise against");
    assert.match(fixLoop, /zero tolerance/i, "a mustNot is a harm, and tolerating one is not a rubric");
  });

  // The one rule whose breach is a privilege escalation, not a bug.
  it("forbids the loop touching anything but the prompt body", () => {
    assert.match(fixLoop, /only the .*(body|prompt)/i);
    assert.match(fixLoop, /never .*front matter|front matter .*never/i);
    assert.match(
      fixLoop,
      /allow[\s\S]{0,80}security\s+boundary/,
      "the allow-list is no longer named as the boundary a loop may not widen",
    );
  });

  it("keeps evaluation reporting rather than gating", () => {
    assert.match(
      SKILL,
      /never fails the build/i,
      "a build that fails on a probabilistic score gets switched off",
    );
  });

  it("spends the org's model key, never the platform's coding token", () => {
    assert.match(SKILL, /ANTHROPIC_API_KEY/);
    assert.match(
      SKILL,
      /never[\s\S]{0,40}CLAUDE_CODE_OAUTH_TOKEN/i,
      "the coding token is not the organisation's model credential",
    );
  });
});

// Two properties of the PRESCRIBED agent that only evaluation exercises: it is
// booted repeatedly on ephemeral ports, and it is booted with no database. An
// agent generated without either never becomes ready, so every scenario would
// report a boot failure rather than a score.
describe("agent-building — an agent the harness can boot", () => {
  it("takes its port from PORT, keeping 9090 as the default", () => {
    assert.match(
      SKILL,
      /process\.env\.PORT \?\? 9090/,
      "a fixed port makes the second boot of a fix loop fail on an address the first still holds",
    );
  });

  it("falls back to an in-memory store when no database is configured", () => {
    assert.match(
      SKILL,
      /config\.memoryDbHost \? postgresStore\(\) : memoryStore\(\)/,
      "the store no longer chooses a backing — an evaluated agent has no MEMORY_DB_* and would never become ready",
    );
    assert.match(
      SKILL,
      /row\.userId === userId/,
      "the in-memory store lost its user fence, so it no longer behaves like the SQL it stands in for",
    );
    assert.match(
      SKILL,
      /Never list a `MEMORY_DB_\*` variable/,
      "an absent database configuration reported as `missing` answers 503 forever",
    );
  });

  it("still keeps schema init off the path to listen()", () => {
    assert.match(SKILL, /export function initStore\(\): void/);
    assert.match(SKILL, /isStoreReady/);
  });
});

// The scenario file is the oracle the fix loop optimises against. Derived from
// the agent document, it would grade the agent on its own wording and pass
// whatever the prompt happened to say.
describe("agent-building — the scenario file", () => {
  it("is authored from the requirements alone", () => {
    assert.match(DESIGNING, /specs\/validation\/agent-scenarios\.json/);
    assert.match(DESIGNING, /specs\/requirements\/[^\n]*ONLY/);
    assert.match(DESIGNING, /never from the `agent\.afm\.md`/i);
  });

  it("explains what withholding buys", () => {
    assert.match(
      DESIGNING,
      /withholds/,
      "without withheld facts, \"asks for what it needs\" is unobservable",
    );
  });

  // The design turn learns the scenario file exists from the skill BODY, which
  // is what `FLOW_SUPPORTING_SKILLS.design` pins into its context — the
  // reference above is read on demand, so a body that stopped naming the file
  // would leave a design turn with no reason to open it.
  //
  // `validation-criteria/SKILL.md` carried this line too and was the subject
  // here; it went with the criteria+e2e path (ADR-0029), so the body is now the
  // only place the design turn is told.
  it("is named by the skill body the design turn pins, under the same input rule", () => {
    assert.match(BODY, /specs\/validation\/agent-scenarios\.json/);
    assert.match(BODY, /from the requirements alone/i);
  });
});
