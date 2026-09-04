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

// walk.sh owns the one thing the walker cannot see when it goes wrong — a dev
// server it left behind — and the one line a pull request quotes. Both are
// pinned here against a stand-in app whose `dev:mock` is a bare node listener,
// and a stand-in `gh` that records what it was asked.

import { test, after } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync, readFileSync, existsSync, rmSync, chmodSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import path from "node:path";

const SCRIPT = path.join(import.meta.dirname, "walk.sh");

const SERVER = `
const i = process.argv.indexOf("--port");
const port = Number(process.argv[i + 1]);
import("node:http").then(({ createServer }) =>
  createServer((_, res) => res.end("ok")).listen(port));
`;

/** A stand-in App Path. Its basename is the walk's state key, so each test gets its own. */
function fixtureApp({ devMock = "node server.mjs" } = {}) {
  const dir = mkdtempSync(path.join(tmpdir(), "walk-app-"));
  writeFileSync(path.join(dir, "package.json"), JSON.stringify({ scripts: { "dev:mock": devMock } }));
  writeFileSync(path.join(dir, "server.mjs"), SERVER);
  return dir;
}

// Stand-ins ahead of PATH for every run: a `gh` that logs its argv and answers
// like the real one, and an `agent-browser` that does nothing — `down` closes
// the browser, and a test must not close the one a developer is using.
const BIN = mkdtempSync(path.join(tmpdir(), "walk-bin-"));
const GH_LOG = path.join(BIN, "gh.log");
writeFileSync(
  path.join(BIN, "gh"),
  `#!/usr/bin/env bash
printf '%s\\n' "$*" >> "${GH_LOG}"
case "$1" in
  issue) echo "https://github.com/o/r/issues/$3#issuecomment-4242" ;;
  api) echo "{}" ;;
esac
`,
);
writeFileSync(path.join(BIN, "agent-browser"), "#!/usr/bin/env bash\nexit 0\n");
chmodSync(path.join(BIN, "gh"), 0o755);
chmodSync(path.join(BIN, "agent-browser"), 0o755);

function walk(cwd, args) {
  return spawnSync("bash", [SCRIPT, ...args], {
    cwd,
    encoding: "utf8",
    env: { ...process.env, PATH: `${BIN}:${process.env.PATH}` },
    timeout: 90_000,
  });
}

const stateOf = (dir) => `/tmp/walk-${path.basename(dir)}`;
const cleanups = [];
after(() => {
  for (const fn of cleanups) fn();
  rmSync(BIN, { recursive: true, force: true });
});
function track(dir) {
  cleanups.push(() => {
    walk(dir, ["down"]);
    for (const ext of ["md", "log", "pgid", "port", "comment"]) rmSync(`${stateOf(dir)}.${ext}`, { force: true });
    rmSync(dir, { recursive: true, force: true });
  });
  return dir;
}

async function isListening(port) {
  const res = await fetch(`http://localhost:${port}/`).catch(() => undefined);
  return res?.ok === true;
}

function occupy(port) {
  return new Promise((resolve, reject) => {
    const srv = createServer();
    srv.once("error", reject);
    srv.listen(port, "127.0.0.1", () => resolve(srv));
  });
}

test("up starts dev:mock on a free port and down leaves nothing listening", async () => {
  const app = track(fixtureApp());
  const up = walk(app, ["up"]);
  assert.equal(up.status, 0, up.stderr);
  const m = /^READY (http:\/\/localhost:(\d+)) · checklist (\S+)$/m.exec(up.stdout);
  assert.ok(m, `unexpected READY line: ${up.stdout}`);
  assert.equal(m[3], `${stateOf(app)}.md`, "up names the checklist path the walker writes");
  assert.ok(await isListening(Number(m[2])), "the url answers");
  assert.ok(existsSync(`${stateOf(app)}.pgid`), "the process group is recorded");

  const down = walk(app, ["down"]);
  assert.equal(down.status, 0, down.stderr);
  assert.match(down.stdout, /^STOPPED$/m);
  assert.equal(await isListening(Number(m[2])), false, "the port let go");
  assert.equal(existsSync(`${stateOf(app)}.pgid`), false);
});

test("up skips a port something else already holds", async () => {
  const app = track(fixtureApp());
  // Whatever is free first from 5173 is the port a bare `up` would take.
  let taken = 5173;
  let holder;
  for (; taken < 5199; taken++) {
    try {
      holder = await occupy(taken);
      break;
    } catch {
      /* in use by someone else — try the next */
    }
  }
  try {
    const up = walk(app, ["up"]);
    assert.equal(up.status, 0, up.stderr);
    const port = Number(/localhost:(\d+)/.exec(up.stdout)[1]);
    assert.notEqual(port, taken, "the occupied port was not chosen");
    assert.ok(port > taken);
    assert.ok(await isListening(port));
  } finally {
    holder.close();
  }
});

test("a second up reaps the first server instead of leaking it", async () => {
  const app = track(fixtureApp());
  const first = walk(app, ["up"]);
  assert.equal(first.status, 0, first.stderr);
  const firstPort = Number(/localhost:(\d+)/.exec(first.stdout)[1]);
  const firstPgid = readFileSync(`${stateOf(app)}.pgid`, "utf8").trim();

  const second = walk(app, ["restart"]);
  assert.equal(second.status, 0, second.stderr);
  const secondPort = Number(/localhost:(\d+)/.exec(second.stdout)[1]);
  const secondPgid = readFileSync(`${stateOf(app)}.pgid`, "utf8").trim();
  assert.notEqual(secondPgid, firstPgid, "a new process group");
  assert.equal(secondPort, firstPort, "the reaped server's port is free again");
  assert.equal(spawnSync("kill", ["-0", "--", `-${firstPgid}`]).status !== 0, true, "the first group is gone");
});

test("up fails fast, with the log, when dev:mock dies at once", () => {
  const app = track(fixtureApp({ devMock: "node -e \"console.error('boom: no such mode'); process.exit(3)\"" }));
  const started = Date.now();
  const up = walk(app, ["up"]);
  assert.notEqual(up.status, 0);
  assert.ok(Date.now() - started < 20_000, "did not wait out the whole poll");
  assert.match(up.stderr, /did not come up/);
  assert.match(up.stderr, /boom: no such mode/, "the log tail is in the output");
  assert.equal(existsSync(`${stateOf(app)}.pgid`), false, "nothing is left recorded as running");
});

const CHECKLIST = `Mock verification: todo-webapp · stale counts the walker typed

flow "Approval queue" (Admin)
- [x] AdminQueue: filtered by status; "Approve" moved the row to Done
- [x] AuditDetail: FIXED back arrow went to /; now -> AdminQueue. Re-walked: lands on the queue.
- [ ] AuditExport: "Export" does nothing. Tried wiring onClick, 3 attempts. Still no request.
flow "My orders" (Customer)
- [x] Orders: table -> OrderDetail
- OrderDetail: "Cancel" button; -> Orders
screens in no flow
- [~] Settings: the 403 is the gateway's
once per app
- [x] Roles: Customer on /admin bounced to /orders
- Session
- Probes
- [x] Console: clean
`;

test("post derives the first line from the marks and keeps the title", () => {
  const app = track(fixtureApp());
  writeFileSync(`${stateOf(app)}.md`, CHECKLIST);
  const post = walk(app, ["post"]);
  assert.equal(post.status, 0, post.stderr);
  const lines = readFileSync(`${stateOf(app)}.md`, "utf8").split("\n");
  assert.equal(
    lines[0],
    "Mock verification: todo-webapp · 6 screens, 2 flows · 4 pass, 1 fixed, 1 open, 1 outside, 3 to walk",
  );
  assert.equal(lines.length, CHECKLIST.split("\n").length, "only line 1 changed");
  assert.match(post.stdout, /not published/);
});

test("post omits 'to walk' once every line is marked", () => {
  const app = track(fixtureApp());
  writeFileSync(`${stateOf(app)}.md`, CHECKLIST.replaceAll(/^- (?!\[)/gm, "- [x] "));
  walk(app, ["post"]);
  assert.equal(
    readFileSync(`${stateOf(app)}.md`, "utf8").split("\n")[0],
    "Mock verification: todo-webapp · 6 screens, 2 flows · 7 pass, 1 fixed, 1 open, 1 outside",
  );
});

test("post creates one comment and edits that same comment afterwards", () => {
  const app = track(fixtureApp());
  writeFileSync(`${stateOf(app)}.md`, CHECKLIST);

  const first = walk(app, ["post", "17"]);
  assert.equal(first.status, 0, first.stderr);
  assert.match(first.stdout, /^published to #17: Mock verification: todo-webapp · 6 screens/m);
  assert.equal(readFileSync(`${stateOf(app)}.comment`, "utf8").trim(), "4242");

  const second = walk(app, ["post", "17"]);
  assert.equal(second.status, 0, second.stderr);

  const calls = readFileSync(GH_LOG, "utf8").trim().split("\n");
  assert.deepEqual(calls, [
    `issue comment 17 --body-file ${stateOf(app)}.md`,
    `api -X PATCH repos/{owner}/{repo}/issues/comments/4242 -F body=@${stateOf(app)}.md`,
  ]);
});

test("post without a checklist is an error, not a silent no-op", () => {
  const app = track(fixtureApp());
  const post = walk(app, ["post", "17"]);
  assert.notEqual(post.status, 0);
  assert.match(post.stderr, /no checklist/);
});

test("an unknown verb prints the usage and exits 2", () => {
  const app = track(fixtureApp());
  const r = walk(app, ["sideways"]);
  assert.equal(r.status, 2);
  assert.match(r.stderr, /walk\.sh up/);
});
