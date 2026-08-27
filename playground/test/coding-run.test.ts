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

/** Coding-run flow units: undo round trip, gates, timeline rendering. */

import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import {
  createTimelineRenderer,
  dockerInvocation,
  hostInvocation,
  isFailedSubagent,
  renderMergedTimeline,
  toolJarOverlay,
  workingTreeToolJar,
} from "../src/engine/coding-run.js";
import { REPO_ROOT } from "../src/paths.js";
import { formatLine } from "@aep/progress-view";

// The playground renders through the SAME formatter the console does, so these
// assertions pin the shared wording, not a playground-only copy of it.
const renderProgressLine = (e: Parameters<typeof formatLine>[0]): string => formatLine(e).text;
import { renderTaskContextFile } from "../src/ports/issue-store.js";
import { takeUndoSnapshot, restoreUndoSnapshot, listUndoSnapshots } from "../src/state/undo.js";
import { codeCommand } from "../src/commands.js";

function tempProject(): string {
  const dir = mkdtempSync(join(tmpdir(), "aep-play-code-"));
  mkdirSync(join(dir, "issues"), { recursive: true });
  writeFileSync(
    join(dir, "issues", "3.md"),
    renderTaskContextFile({
      issueNumber: 3,
      component: "user-service",
      title: "Implement the user service",
      dependsOn: ["auth-service"],
      body: "scope",
    }),
  );
  return dir;
}

// Which credential a coding run authenticates with is decided HERE and nowhere
// else — the runner entrypoint deliberately no longer validates the key, so a
// regression in these four would not fail anywhere downstream. It would just
// quietly bill the wrong account, or hand a shared key to a bypassPermissions
// process on the developer's own machine.
const invocationOpts = { projectDir: "/p", skillsDir: "/s" };

function withKeyInEnv(value: string, body: () => void): void {
  const restore = process.env.ANTHROPIC_API_KEY;
  process.env.ANTHROPIC_API_KEY = value;
  try {
    body();
  } finally {
    if (restore === undefined) delete process.env.ANTHROPIC_API_KEY;
    else process.env.ANTHROPIC_API_KEY = restore;
  }
}

test("host mode withholds the key, so the SDK falls back to the developer's own credentials", () => {
  withKeyInEnv("sk-ant-from-dotenv", () => {
    const { env } = hostInvocation(invocationOpts, "/r");
    assert.equal(env.ANTHROPIC_API_KEY, undefined, "the key must not reach a host session by default");
    // The rest of the environment still has to arrive, or the SDK cannot find
    // HOME — and with it the credential store it is being sent to.
    assert.equal(env.AEP_LOCAL_PROJECT_DIR, "/p");
    assert.ok(env.PATH, "PATH must survive");
  });
});

// These two are about the org with NO coding credential configured, so they
// clear it explicitly: `deployments/.env` may define one on a developer's
// machine, and `@aep/agents` merges that file into process.env at module scope.
test("host mode --api-key opts back into key auth", () => {
  withCodingKeyInEnv(undefined, () => {
    withKeyInEnv("sk-ant-explicit", () => {
      const { env } = hostInvocation({ ...invocationOpts, useApiKey: true }, "/r");
      assert.equal(env.ANTHROPIC_API_KEY, "sk-ant-explicit");
    });
  });
});

test("docker mode always passes the key through — a container reaches no credential store", () => {
  withCodingKeyInEnv(undefined, () => {
    const { args } = dockerInvocation(invocationOpts, "/r", "c1");
    const at = args.indexOf("ANTHROPIC_API_KEY");
    assert.ok(at > 0, "the key must be forwarded into the container");
    assert.equal(args[at - 1], "-e", "forwarded by name, so the value never lands in argv");
  });
});

// AEP_CODING_ANTHROPIC_KEY is the local half of the platform's per-org
// coding-agent key. It changes WHICH credential --api-key opts into; it does
// not opt in by itself. Host mode's default stays "the developer's own login",
// so a bypassPermissions process on their filesystem never picks up a shared
// credential just because a file elsewhere defined one.
function withCodingKeyInEnv(value: string | undefined, body: () => void): void {
  const restore = process.env.AEP_CODING_ANTHROPIC_KEY;
  if (value === undefined) delete process.env.AEP_CODING_ANTHROPIC_KEY;
  else process.env.AEP_CODING_ANTHROPIC_KEY = value;
  try {
    body();
  } finally {
    if (restore === undefined) delete process.env.AEP_CODING_ANTHROPIC_KEY;
    else process.env.AEP_CODING_ANTHROPIC_KEY = restore;
  }
}

test("a coding key does NOT reach a host session without --api-key", () => {
  withKeyInEnv("sk-ant-from-dotenv", () => {
    withCodingKeyInEnv("sk-ant-coding", () => {
      const { env } = hostInvocation(invocationOpts, "/r");
      assert.equal(env.ANTHROPIC_API_KEY, undefined, "defining a key is not asking to use it here");
      assert.equal(env.CLAUDE_CODE_OAUTH_TOKEN, undefined);
    });
  });
});

test("--api-key opts into the CODING key when one is configured", () => {
  withKeyInEnv("sk-ant-from-dotenv", () => {
    withCodingKeyInEnv("sk-ant-coding", () => {
      const { env } = hostInvocation({ ...invocationOpts, useApiKey: true }, "/r");
      assert.equal(env.ANTHROPIC_API_KEY, "sk-ant-coding", "the coding key outranks the platform key");
    });
  });
});

test("docker mode substitutes the coding key by value, never into argv", () => {
  withKeyInEnv("sk-ant-from-dotenv", () => {
    withCodingKeyInEnv("sk-ant-coding", () => {
      const { args, env } = dockerInvocation(invocationOpts, "/r", "c1");
      assert.equal(env.ANTHROPIC_API_KEY, "sk-ant-coding", "the container must receive the coding key");
      const at = args.indexOf("ANTHROPIC_API_KEY");
      assert.equal(args[at - 1], "-e", "still forwarded BY NAME");
      assert.ok(
        !args.some((a) => a.includes("sk-ant-coding")),
        "a secret in argv is readable by any user via ps",
      );
    });
  });
});

// A `claude setup-token` token bills a Claude subscription and must arrive as
// CLAUDE_CODE_OAUTH_TOKEN. Claude Code ranks ANTHROPIC_API_KEY ABOVE it, and
// deployments/.env gives nearly every developer one, so leaving that variable
// in place would silently bill the default key and ignore the token entirely.
test("an OAuth coding token displaces ANTHROPIC_API_KEY in host mode", () => {
  withKeyInEnv("sk-ant-from-dotenv", () => {
    withCodingKeyInEnv("sk-ant-oat01-subscription-token", () => {
      const { env } = hostInvocation({ ...invocationOpts, useApiKey: true }, "/r");
      assert.equal(env.CLAUDE_CODE_OAUTH_TOKEN, "sk-ant-oat01-subscription-token");
      assert.equal(env.ANTHROPIC_API_KEY, undefined, "the API key would outrank the token and win");
    });
  });
});

test("an OAuth coding token displaces ANTHROPIC_API_KEY in docker mode too", () => {
  withKeyInEnv("sk-ant-from-dotenv", () => {
    withCodingKeyInEnv("sk-ant-oat01-subscription-token", () => {
      const { args, env } = dockerInvocation(invocationOpts, "/r", "c1");
      assert.equal(env.CLAUDE_CODE_OAUTH_TOKEN, "sk-ant-oat01-subscription-token");
      assert.equal(env.ANTHROPIC_API_KEY, undefined, "the API key would outrank the token and win");
      // Exactly one name is forwarded — the one this run authenticates with.
      const tokenAt = args.indexOf("CLAUDE_CODE_OAUTH_TOKEN");
      assert.equal(args[tokenAt - 1], "-e", "forwarded BY NAME");
      assert.ok(
        !args.some((a) => a.includes("subscription-token")),
        "a secret in argv is readable by any user via ps",
      );
    });
  });
});

// deployments/.env is the PLATFORM's file and `@aep/agents` merges it into
// process.env at module scope, so a CLAUDE_CODE_OAUTH_TOKEN sitting there would
// otherwise authenticate every host run — silently billing a shared credential
// on the one path whose whole purpose is to bill the developer's own login.
// Opting in is AEP_CODING_ANTHROPIC_KEY, and only that.
test("host mode withholds an inherited OAuth token too", () => {
  const restore = process.env.CLAUDE_CODE_OAUTH_TOKEN;
  process.env.CLAUDE_CODE_OAUTH_TOKEN = "sk-ant-oat01-from-deployments-env";
  try {
    withCodingKeyInEnv(undefined, () => {
      assert.equal(hostInvocation(invocationOpts, "/r").env.CLAUDE_CODE_OAUTH_TOKEN, undefined);
      // …including with --api-key, which opts into the API KEY, not a token.
      assert.equal(
        hostInvocation({ ...invocationOpts, useApiKey: true }, "/r").env.CLAUDE_CODE_OAUTH_TOKEN,
        undefined,
      );
    });
  } finally {
    if (restore === undefined) delete process.env.CLAUDE_CODE_OAUTH_TOKEN;
    else process.env.CLAUDE_CODE_OAUTH_TOKEN = restore;
  }
});

// An API-key coding credential must NOT leave a stale token behind either.
test("an API-key coding credential clears any inherited OAuth token", () => {
  const restore = process.env.CLAUDE_CODE_OAUTH_TOKEN;
  process.env.CLAUDE_CODE_OAUTH_TOKEN = "sk-ant-oat01-inherited";
  try {
    withCodingKeyInEnv("sk-ant-api03-coding", () => {
      const { env } = hostInvocation({ ...invocationOpts, useApiKey: true }, "/r");
      assert.equal(env.ANTHROPIC_API_KEY, "sk-ant-api03-coding");
      assert.equal(env.CLAUDE_CODE_OAUTH_TOKEN, undefined, "a stale token must not linger");
    });
  } finally {
    if (restore === undefined) delete process.env.CLAUDE_CODE_OAUTH_TOKEN;
    else process.env.CLAUDE_CODE_OAUTH_TOKEN = restore;
  }
});

// A blank export must not count as "configured" — otherwise --api-key would
// authenticate the run with an empty string instead of the platform key.
test("a blank coding key is not a coding key", () => {
  withKeyInEnv("sk-ant-from-dotenv", () => {
    withCodingKeyInEnv("   ", () => {
      const { env } = hostInvocation({ ...invocationOpts, useApiKey: true }, "/r");
      assert.equal(env.ANTHROPIC_API_KEY, "sk-ant-from-dotenv", "blank falls back to the platform key");
    });
  });
});

test("with no coding key set, both modes behave exactly as before", () => {
  withKeyInEnv("sk-ant-from-dotenv", () => {
    withCodingKeyInEnv(undefined, () => {
      assert.equal(hostInvocation(invocationOpts, "/r").env.ANTHROPIC_API_KEY, undefined);
      assert.equal(
        hostInvocation({ ...invocationOpts, useApiKey: true }, "/r").env.ANTHROPIC_API_KEY,
        "sk-ant-from-dotenv",
      );
      assert.equal(dockerInvocation(invocationOpts, "/r", "c1").env.ANTHROPIC_API_KEY, "sk-ant-from-dotenv");
    });
  });
});

test("undo snapshot + restore round-trips edits and removes new files", () => {
  const dir = tempProject();
  try {
    mkdirSync(join(dir, "src"));
    writeFileSync(join(dir, "src", "main.go"), "package main\n");
    const snap = takeUndoSnapshot(dir);
    assert.ok(existsSync(snap));
    assert.equal(listUndoSnapshots(dir)[0], snap);

    // Agent damage: edit a file, add a new one, delete another.
    writeFileSync(join(dir, "src", "main.go"), "package broken\n");
    writeFileSync(join(dir, "src", "junk.go"), "junk\n");
    rmSync(join(dir, "issues", "3.md"));

    const restored = restoreUndoSnapshot(dir);
    assert.equal(restored, snap);
    assert.equal(readFileSync(join(dir, "src", "main.go"), "utf8"), "package main\n");
    assert.ok(!existsSync(join(dir, "src", "junk.go")), "post-snapshot file removed");
    assert.ok(existsSync(join(dir, "issues", "3.md")), "deleted file restored");
    // The state dir itself is never part of the snapshot scope.
    assert.ok(existsSync(join(dir, ".aep-playground", "undo")));
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("codeCommand gates: no issues fails; unconfirmed headless run fails before any snapshot", async () => {
  const empty = mkdtempSync(join(tmpdir(), "aep-play-code-empty-"));
  try {
    const noIssues = await codeCommand(empty, { silent: true });
    assert.equal(noIssues.ok, false);
    assert.match(noIssues.detail ?? "", /nothing to run/);
  } finally {
    rmSync(empty, { recursive: true, force: true });
  }

  const dir = tempProject();
  try {
    const unconfirmed = await codeCommand(dir, { silent: true });
    assert.equal(unconfirmed.ok, false);
    assert.match(unconfirmed.detail ?? "", /not confirmed/);
    assert.equal(listUndoSnapshots(dir).length, 0, "no snapshot before consent");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("renderProgressLine maps the NDJSON vocabulary to timeline lines", () => {
  // Phase ids render through the shared friendly-label map, so the playground
  // says exactly what the console says.
  assert.equal(renderProgressLine({ kind: "phase", phase: "workspace_ready" }), "▸ Workspace ready");
  assert.equal(renderProgressLine({ kind: "tool_use", tool: "Bash", summary: "go test ./..." }), "$ go test ./...");
  assert.match(renderProgressLine({ kind: "result", status: "success" }), /■ success/);
  assert.match(renderProgressLine({ kind: "result", status: "failure", error: "boom" }), /failure — boom/);
  assert.match(renderProgressLine({ kind: "gh_action", command: "pr create" }), /⚙ pr create/);
});

test("the local harness flags the operations that cannot work without a remote", () => {
  // True here and meaningless in a cluster run, so it is the ONE thing this
  // renderer adds on top of the shared wording.
  const render = createTimelineRenderer();
  assert.match(render({ kind: "gh_action", command: "pr create" }).join(""), /no GitHub in local mode/);
  assert.match(render({ kind: "git_push", branch: "main" }).join(""), /no remote in local mode/);
  // …and it annotates nothing else.
  assert.doesNotMatch(render({ kind: "tool_use", tool: "Bash", summary: "ls" }).join(""), /local mode/);
});

test("renderProgressLine reports a failed tool call, and times only the slow successes", () => {
  // A shell failure names its exit code: that is what says THIS command broke.
  assert.equal(
    renderProgressLine({ kind: "tool_result", tool: "Bash", ok: false, durationMs: 900, exitCode: 1, summary: "error: compilation contains errors" }),
    "✗ Bash exit 1 · error: compilation contains errors",
  );
  // A non-shell tool reports no code — "failed" is exactly as much as is known.
  assert.equal(
    renderProgressLine({ kind: "tool_result", tool: "Read", ok: false, summary: "File does not exist" }),
    "✗ Read failed · File does not exist",
  );
  assert.equal(renderProgressLine({ kind: "tool_result", tool: "Bash", ok: true, durationMs: 42_000 }), "↳ Bash 42.0s");
  assert.equal(renderProgressLine({ kind: "tool_result", tool: "Bash", ok: true, durationMs: 185_000 }), "↳ Bash 3m5s");
  // A fast success is deliberately silent — a tick per read would bury the failures.
  assert.equal(renderProgressLine({ kind: "tool_result", tool: "Read", ok: true, durationMs: 40 }), "");
  // A subagent's narration is header material, never a row.
  assert.equal(renderProgressLine({ kind: "activity", summary: "Writing todo-api/service.bal" }), "");
});

test("timeline renderer numbers concurrent subagents and announces each one once", () => {
  const render = createTimelineRenderer();
  const api = { emitter: "subagent", emitterId: "toolu_api", emitterLabel: "Implement todo-api (issue #3)" };
  const web = { emitter: "subagent", emitterId: "toolu_web", emitterLabel: "Implement todo-webapp (issue #4)" };

  // First sighting: the label is announced, then the line itself.
  const first = render({ kind: "tool_use", tool: "Bash", summary: "bal build", ...api });
  assert.equal(first.length, 2);
  assert.match(first.join("\n"), /⑂ \[#1\] Implement todo-api \(issue #3\)/);
  assert.match(first.join("\n"), /\[#1\] +\$ bal build/);

  // A second subagent gets its own number — the point of the whole exercise.
  const second = render({ kind: "tool_use", tool: "Write", summary: "src/App.tsx", ...web }).join("\n");
  assert.match(second, /⑂ \[#2\] Implement todo-webapp/);
  assert.match(second, /\[#2\] +\$ Write src\/App.tsx/);

  // Already announced: one line, and the SAME number as before.
  const again = render({ kind: "tool_use", tool: "Bash", summary: "bal test", ...api });
  assert.equal(again.length, 1);
  assert.match(again.join(""), /\[#1\] +\$ bal test/);
});

test("timeline renderer: main lines are unstamped, and glyphs stay in one column", () => {
  const render = createTimelineRenderer();
  const only = (lines: string[]): string => {
    assert.equal(lines.length, 1);
    return lines[0] as string;
  };

  const main = only(render({ kind: "tool_use", tool: "Bash", summary: "bal build" }));
  const sub = only(render({ kind: "tool_use", tool: "Bash", summary: "bal build", emitter: "subagent", emitterId: "x" }));

  assert.doesNotMatch(main, /#|sub/);
  assert.equal(main.indexOf("$"), sub.indexOf("$"), "same column");

  // A subagent line with no id still says it is one, rather than passing as main.
  assert.match(only(render({ kind: "tool_use", tool: "Bash", summary: "ls", emitter: "subagent" })), /\[sub\]/);

  // A silent event produces no row at all.
  assert.deepEqual(render({ kind: "tool_result", tool: "Read", ok: true, durationMs: 5 }), []);
});

test("merged pass: an outcome lands on its own action's row, console-shaped", () => {
  const out = renderMergedTimeline([
    { kind: "tool_use", tool: "Bash", summary: "bal build", toolUseId: "t1" },
    { kind: "tool_use", tool: "Read", summary: "db.bal", toolUseId: "t2" },
    // Out of order and separated from its action, as a real interleaved run is.
    { kind: "tool_result", tool: "Read", ok: true, durationMs: 20, toolUseId: "t2" },
    { kind: "tool_result", tool: "Bash", ok: false, exitCode: 1, summary: "error: compilation contains errors", durationMs: 25_100, toolUseId: "t1" },
  ]);

  assert.equal(out.length, 2, "one row per step, not one per event");
  assert.match(out[0] as string, /\$ bal build +exit 1 · error: compilation contains errors · 25\.1s/);
  // The fast read keeps its action row and gains nothing — the rule holds here too.
  assert.equal((out[1] as string).trim(), "$ Read db.bal");
});

test("merged pass: each subagent's work sits under its own report", () => {
  const api = { emitter: "subagent", emitterId: "a1", emitterLabel: "todo-api" };
  const out = renderMergedTimeline([
    { kind: "tool_use", tool: "Bash", summary: "git status", toolUseId: "m1" },
    { kind: "activity", summary: "Writing todo-api/service.bal", toolCount: 4, ...api },
    { kind: "tool_use", tool: "Write", summary: "todo-api/service.bal", toolUseId: "s1", ...api },
    { kind: "tool_result", tool: "Agent", ok: true, status: "completed", summary: "todo-api", durationMs: 209_158, toolCount: 19, linesAdded: 553, linesRemoved: 4, toolUseId: "a1", ...api },
  ]).join("\n");

  assert.match(out, /⑂ todo-api — completed · 3m29s · 19 tools · \+553\/−4 lines/);
  assert.match(out, /│ \$ Write todo-api\/service\.bal/);
  // The narration and the closing report are the header; neither is a row.
  assert.doesNotMatch(out, /Writing todo-api\/service\.bal$/m);
  // The main agent's own line keeps its place ahead of the section.
  assert.ok(out.indexOf("git status") < out.indexOf("todo-api"));
});

test("merged pass: local mode still says a push went nowhere", () => {
  const out = renderMergedTimeline([{ kind: "git_push", branch: "aep/m3", toolUseId: "p1" }]).join("\n");
  assert.match(out, /↑ push aep\/m3 — no remote in local mode/);
});

// The trigger for rescuing a stalled subagent's transcript. It is pinned against
// a VERBATIM event off a real run's progress.ndjson (todo-api99, 2026-08-02),
// because the copy it drives races the SDK's own cleanup: if a field name drifts,
// the snapshot silently never fires and the next stall is undiagnosable again.
test("failed-subagent trigger: fires on a real stalled Agent result, and nothing else", () => {
  const stalled = {
    schemaVersion: 1,
    ts: "2026-08-02T14:58:44.607Z",
    seq: 279,
    kind: "tool_result",
    ok: false,
    toolUseId: "toolu_01XTtpLCgiY6V9iiBASr8Fsz",
    tool: "Agent",
    summary: "Implement todo-api Ballerina service",
    durationMs: 1_027_107,
    emitter: "subagent",
    emitterId: "toolu_01XTtpLCgiY6V9iiBASr8Fsz",
    emitterLabel: "Implement todo-api Ballerina service",
  };
  assert.equal(isFailedSubagent(stalled), true);

  // A subagent that succeeded, a failing ordinary tool, and the subagent's own
  // in-flight lines are all normal traffic — snapshotting on any of them would
  // copy the container's state on every failed `bal build`.
  assert.equal(isFailedSubagent({ ...stalled, ok: true, status: "completed" }), false);
  assert.equal(isFailedSubagent({ ...stalled, tool: "Bash", summary: "bal build" }), false);
  assert.equal(isFailedSubagent({ ...stalled, kind: "tool_use" }), false);
});

// `bal library` is the one tool the `ballerina` skill calls by name, and the
// point of wiring it here is that an edit to it reaches the next docker run
// without an image rebuild. Asserted against the same resolver the invocation
// uses, so it holds whether or not the tool's repository happens to be checked
// out and built in the environment running the tests — with no jar to send, the
// image's own install must be left alone rather than shadowed.
test("docker mode mounts the working-tree bal library jar, or leaves the install alone", () => {
  const overlay = toolJarOverlay();
  const { args } = dockerInvocation(invocationOpts, "/r", "c1");
  const mount = args.find((a) => a.includes("/tool/libs/") && a.endsWith(":ro"));

  if (overlay === undefined) {
    assert.equal(mount, undefined, "nothing may be mounted over the image's installed tool");
    return;
  }
  assert.equal(mount, `${overlay.hostJar}:${overlay.imageJar}:ro`);
  assert.equal(overlay.hostJar, workingTreeToolJar());
});

// Host mode has no image and no PATH entry to point anywhere: `bal library` is a
// `bal` tool resolved out of the developer's own ~/.ballerina. So the environment
// must come through untouched — the failure this guards is a well-meant PATH or
// HOME edit that makes a host run read a different tool than a bare `bal library`
// in the same shell would.
test("host mode leaves the developer's own environment alone", () => {
  const { env } = hostInvocation(invocationOpts, "/r");
  assert.equal(env.PATH, process.env.PATH);
  assert.equal(env.HOME, process.env.HOME);
});

// The image installs what its first stage BUILDS from these files (ADR-0008), and
// the coordinates the container path is composed from are held to the tool's own
// metadata — a rename lands here rather than 300 layers into a docker build.
test("the bal library tool's source carries what the image install needs", () => {
  // Asserted against the SOURCE and not a built artifact: there is no checked-in
  // distribution any more, so the image's first stage runs `make-dist.sh` over
  // exactly these files. Getting this wrong silently disables the jar overlay.
  const tool = join(REPO_ROOT, "packages", "bal-library-tool");
  const version = /^version=(.+)$/m.exec(readFileSync(join(tool, "gradle.properties"), "utf8"))?.[1]?.trim();
  assert.ok(version && version.length > 0, "gradle.properties names the version install.sh registers");
  assert.ok(existsSync(join(tool, "release", "install.sh")), "the tool's own offline installer");
  assert.ok(existsSync(join(tool, "make-dist.sh")), "the one place that decides what a distribution holds");

  // install.sh derives the bala path from these, and coding-run.ts composes the
  // mount target from the same three values.
  const toml = readFileSync(join(tool, "Ballerina.toml"), "utf8");
  assert.match(toml, /^org = "ballerinax"$/m);
  assert.match(toml, /^name = "tool_library"$/m);
  const overlay = toolJarOverlay();
  if (overlay) {
    assert.ok(
      overlay.imageJar.endsWith(`/ballerinax/tool_library/${version}/any/tool/libs/native-${version}.jar`),
      `the mount target must be the path install.sh wrote: ${overlay.imageJar}`,
    );
  }
});
