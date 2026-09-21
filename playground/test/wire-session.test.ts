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
 * The parts of a wired session that are about the MACHINE rather than the
 * design: the gateway identity, who a session can be entered as, the panel's
 * keys, the agent boundary, and — the one that has actually gone wrong in
 * practice — reaping a dev server.
 *
 * The reaping test starts a real process that starts another and kills the
 * group, because that is the failure the design is defending against: `npm run
 * dev:mock` is three processes deep, and a session that killed only the child it
 * spawned left a Vite listening for the rest of the day.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { createVerify } from "node:crypto";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { decodeJwt, decodeProtectedHeader } from "jose";
import { ensureKeypair, mintAssertion, roleTokens, subjectFor, WIRE_HEADER, WIRE_ISSUER } from "../src/engine/wire/assertion.js";
import { entryUrl, findEntry, roleEntries } from "../src/engine/wire/roles.js";
import { numberedEntries, panelRows, readyLine, resolveKey } from "../src/engine/wire/panel.js";
import { boundaryDenial, deniedTools } from "../src/engine/wire/agents/guard.js";
import { needsInstall } from "../src/engine/wire/webapp.js";
import { delay, isPortBusy, startGroup, stopGroup } from "../src/engine/wire/runtime.js";
import { sessionProcessExists } from "../src/engine/wire/state.js";
import type { WireRole } from "../src/engine/wire/plan.js";

const ROLES: WireRole[] = [
  { name: "HRCoordinator", grants: ["tasks:read", "tasks:set-due-date"], username: "test-hr", usernameGuessed: false },
  { name: "ITOnboardingStaff", grants: ["tasks:read"], username: "ITOnboardingStaff", usernameGuessed: true },
];

// --- the gateway identity ---------------------------------------------------

test("the keypair is minted once and reused, and the assertion verifies against its certificate", async () => {
  const dir = mkdtempSync(join(tmpdir(), "wire-key-"));
  const first = ensureKeypair(dir);
  const again = ensureKeypair(dir);
  assert.equal(first.certificate, again.certificate, "a second session is the same gateway as the first");
  assert.match(first.certificate, /BEGIN CERTIFICATE/);

  const role = ROLES[0];
  assert.ok(role);
  const token = await mintAssertion(role, first.keyPath);

  // Verified the way the service verifies it: the certificate, and nothing else.
  const [header, payload, signature] = token.split(".");
  const verifier = createVerify("sha256");
  verifier.update(`${header ?? ""}.${payload ?? ""}`);
  assert.equal(
    verifier.verify(readFileSync(first.certPath, "utf8"), Buffer.from(signature ?? "", "base64url")),
    true,
    "the service would reject this assertion",
  );

  assert.equal(decodeProtectedHeader(token).alg, "RS256");
  const claims = decodeJwt(token);
  assert.equal(claims.iss, WIRE_ISSUER);
  // The same literal is pinned on the asset side (skills/react-webapp/assets/
  // __tests__/mock-wired.cases.mjs). Two processes mint this assertion — the dev
  // server per request, this one for the printed curl table — and a role has to
  // be the same caller in both, or rows created through one are invisible to the
  // other. If you change the derivation, both tests fail, which is the point.
  assert.equal(claims.sub, subjectFor("HRCoordinator"));
  assert.equal(claims.sub, "672ff732-07fd-0a47-5c2f-f1217acca0af");
  assert.equal(claims.username, "test-hr");
  assert.equal(claims.ouHandle, "local");
  assert.equal(claims.scope, "openid profile email group ou tasks:read tasks:set-due-date");
  assert.equal(WIRE_HEADER, "x-jwt-assertion");
});

test("a mock bearer per role, in the shape the browser mints", () => {
  const tokens = roleTokens(ROLES);
  assert.equal(tokens.HRCoordinator, "mock:HRCoordinator;tasks%3Aread,tasks%3Aset-due-date");
  // Signed in holding nothing — the NoAccess caller, which no role name expresses.
  assert.equal(tokens[""], "mock:;");
});

// --- who you can be ---------------------------------------------------------

test("the list is the design's roles plus the two callers a walk needs", () => {
  const entries = roleEntries(ROLES);
  assert.deepEqual(
    entries.map((entry) => [entry.label, entry.role]),
    [
      ["HRCoordinator", "HRCoordinator"],
      ["ITOnboardingStaff", "ITOnboardingStaff"],
      ["no role", ""],
      ["signed out", null],
    ],
  );
  assert.match(entries[1]?.hint ?? "", /no testUsers row/);
});

test("each entry is a URL the mock session already understands", () => {
  const entries = roleEntries(ROLES);
  assert.equal(entryUrl("http://localhost:5173", entries[0]!), "http://localhost:5173/?role=HRCoordinator");
  assert.equal(entryUrl("http://localhost:5173", entries[2]!), "http://localhost:5173/?role=");
  assert.equal(entryUrl("http://localhost:5173", entries[3]!), "http://localhost:5173/?auth=out");
});

test("--role finds a role however it was typed, and the empty string is a real answer", () => {
  const entries = roleEntries(ROLES);
  assert.equal(findEntry(entries, "hrcoordinator")?.role, "HRCoordinator");
  assert.equal(findEntry(entries, "signed out")?.role, null);
  assert.equal(findEntry(entries, "")?.role, "");
  assert.equal(findEntry(entries, "nobody"), null);
});

// --- the panel --------------------------------------------------------------

test("every key does what the panel says it does", () => {
  const entries = roleEntries(ROLES);
  assert.deepEqual(resolveKey("1", entries), { kind: "role", entry: entries[0] });
  assert.deepEqual(resolveKey("3", entries), { kind: "role", entry: entries[2] });
  assert.deepEqual(resolveKey("9", entries), { kind: "none" }, "a number past the list does nothing");
  assert.deepEqual(resolveKey("s", entries), { kind: "seed" });
  assert.deepEqual(resolveKey("r", entries), { kind: "rebuild" });
  assert.deepEqual(resolveKey("l", entries), { kind: "logs" });
  assert.deepEqual(resolveKey("q", entries), { kind: "quit" });
  // Raw mode means the terminal no longer turns Ctrl-C into a signal, so the
  // key table is the only thing that can still quit on it.
  assert.deepEqual(resolveKey("", entries), { kind: "quit" });
  assert.deepEqual(resolveKey("x", entries), { kind: "none" });
});

test("the panel says what is running, where, and every key it answers", () => {
  const entries = roleEntries(ROLES);
  const rows = panelRows(
    {
      url: "http://localhost:5173/?role=HRCoordinator",
      services: [
        { name: "onboarding-api", state: "running", health: "healthy" },
        { name: "onboarding-db", state: "running", health: "healthy" },
      ],
      webapp: { url: "http://localhost:5173", port: 5173 },
      note: "opened as HRCoordinator",
    },
    entries,
    100,
  );
  const text = rows.map((row) => row.text).join("\n");
  assert.match(text, /onboarding-api\s+healthy/);
  assert.match(text, /http:\/\/localhost:5173\/\?role=HRCoordinator/);
  for (const [index, entry] of numberedEntries(entries).entries()) {
    assert.ok(text.includes(`${String(index + 1)} ${entry.label}`), `${entry.label} is not on the panel`);
  }
  assert.match(text, /q quit \(tears everything down\)/);
  for (const row of rows) assert.ok(row.text.length < 100, `a row wider than the terminal wraps: ${row.text}`);
});

test("a failed service is drawn as failed, not as a dot", () => {
  const rows = panelRows(
    { url: null, services: [{ name: "orders-api", state: "exited", health: "" }] },
    roleEntries(ROLES),
    80,
  );
  const row = rows.find((candidate) => candidate.text.includes("orders-api"));
  assert.equal(row?.tone, "error");
});

test("a script waits for one line", () => {
  assert.equal(readyLine("http://localhost:5173/?role=X"), "READY http://localhost:5173/?role=X");
});

// --- the agent boundary -----------------------------------------------------

const SEED = {
  task: "The seed task",
  slug: "seed",
  mayWrite: ["/p/.aep-playground/wire/seed.sh"],
  mayCurl: "http://localhost:5173",
};
const TRIAGE = { task: "The triage task", slug: "triage", mayWrite: [] };

test("the seed task may write exactly one file", () => {
  assert.equal(boundaryDenial("Write", { file_path: "/p/.aep-playground/wire/seed.sh" }, SEED), undefined);
  assert.match(
    boundaryDenial("Write", { file_path: "/p/onboarding-api/service.bal" }, SEED) ?? "",
    /may write exactly one file/,
  );
  assert.match(boundaryDenial("Edit", { file_path: "/p/specs/design/security.json" }, SEED) ?? "", /read-only/);
});

test("the seed task may only call the running app", () => {
  assert.equal(boundaryDenial("Bash", { command: "curl -sS http://localhost:5173/api/tasks" }, SEED), undefined);
  assert.equal(
    boundaryDenial("Bash", { command: "curl -sS -X POST http://localhost:5173/api/new-hires -d '{}'" }, SEED),
    undefined,
  );
  assert.match(boundaryDenial("Bash", { command: "docker compose restart" }, SEED) ?? "", /exactly one kind/);
  assert.match(boundaryDenial("Bash", { command: "psql -c 'insert into tasks'" }, SEED) ?? "", /exactly one kind/);
});

test("a curl is not a way around the rest of the boundary", () => {
  // Another host: the boundary is this session's app, not the network.
  assert.match(boundaryDenial("Bash", { command: "curl https://example.com/x" }, SEED) ?? "", /exactly one kind/);
  // `-o` makes curl a writer, which walks straight through `mayWrite`.
  assert.match(
    boundaryDenial("Bash", { command: "curl -o /p/onboarding-api/service.bal http://localhost:5173/x" }, SEED) ?? "",
    /exactly one kind/,
  );
  // One allowed command plus a shell operator is two commands, and the second is not checked.
  for (const suffix of ["; rm -rf /p", "&& docker compose down", "| tee /p/x", "> /p/x", "`whoami`"]) {
    assert.match(
      boundaryDenial("Bash", { command: `curl http://localhost:5173/api/tasks ${suffix}` }, SEED) ?? "",
      /exactly one kind/,
      suffix,
    );
  }
});

test("the tools a task may not use are derived from its boundary, not listed beside it", () => {
  const seed = deniedTools(SEED);
  assert.ok(!seed.includes("Bash"), "the seed task curls");
  assert.ok(!seed.includes("Write"), "the seed task writes its script");
  assert.ok(seed.includes("WebFetch") && seed.includes("Task"));

  const triage = deniedTools(TRIAGE);
  assert.ok(triage.includes("Bash") && triage.includes("Write") && triage.includes("Edit"));
});

test("the triage task writes nothing and runs nothing", () => {
  assert.match(boundaryDenial("Write", { file_path: "/p/.aep-playground/wire/seed.sh" }, TRIAGE) ?? "", /writes nothing/);
  assert.match(boundaryDenial("Bash", { command: "cat log" }, TRIAGE) ?? "", /runs no commands/);
  assert.equal(boundaryDenial("Read", { file_path: "/p/onboarding-api/service.bal" }, TRIAGE), undefined);
  assert.equal(boundaryDenial("Grep", { pattern: "panic" }, TRIAGE), undefined);
});

test("neither task reaches the network or fans out", () => {
  for (const boundary of [SEED, TRIAGE]) {
    for (const tool of ["WebFetch", "WebSearch", "Task", "Agent"]) {
      assert.match(boundaryDenial(tool, {}, boundary) ?? "", new RegExp(`may not use ${tool}`));
    }
  }
});

// --- one session at a time --------------------------------------------------

test("a live session is told apart from one that died hard, by its own pid", () => {
  assert.equal(sessionProcessExists(null), false);
  assert.equal(sessionProcessExists({ composeProject: "p", startedAt: "now" }), false, "no pid: nothing to ask");
  assert.equal(sessionProcessExists({ composeProject: "p", startedAt: "now", pid: process.pid }), true);
  // A pid nothing holds. The compose project and the dev server both outlive a
  // SIGKILL, so only this can say the difference between "in use" and "left behind".
  assert.equal(sessionProcessExists({ composeProject: "p", startedAt: "now", pid: 2 ** 22 }), false);
});

// --- the host ---------------------------------------------------------------

test("node_modules built inside the runner image is not an install on this host", () => {
  const app = mkdtempSync(join(tmpdir(), "wire-app-"));
  assert.equal(needsInstall(app, "darwin", "arm64"), true, "nothing installed at all");

  mkdirSync(join(app, "node_modules", "@rollup", "rollup-linux-x64-gnu"), { recursive: true });
  assert.equal(needsInstall(app, "darwin", "arm64"), true, "installed somewhere else");
  assert.equal(needsInstall(app, "linux", "x64"), false, "installed here");

  // ARCHITECTURE COUNTS, and the platform alone cannot answer this: a Linux x64
  // install on a Linux arm64 host matches on "linux" and is still the wrong
  // binary. Vite then dies on the missing optional dependency this whole check
  // exists to pre-empt, so reading it as "installed here" is the one answer that
  // must not happen.
  assert.equal(needsInstall(app, "linux", "arm64"), true, "right platform, wrong architecture");

  const plain = mkdtempSync(join(tmpdir(), "wire-app-"));
  mkdirSync(join(plain, "node_modules"), { recursive: true });
  assert.equal(needsInstall(plain, "darwin", "arm64"), false, "no rollup: nothing platform-specific to get wrong");
});

test("stopping a dev server reaps the processes underneath it", async () => {
  const dir = mkdtempSync(join(tmpdir(), "wire-group-"));
  // Stands in for `npm run dev:mock`: a launcher that spawns the thing that
  // actually listens, which is the shape that leaks when only the child is killed.
  writeFileSync(
    join(dir, "child.mjs"),
    `import http from "node:http";
     http.createServer((_, res) => res.end("ok")).listen(Number(process.argv[2]), "127.0.0.1");`,
  );
  writeFileSync(
    join(dir, "launcher.mjs"),
    `import { spawn } from "node:child_process";
     spawn(process.execPath, ["child.mjs", process.argv[2]], { cwd: process.cwd(), stdio: "ignore" });
     setInterval(() => {}, 1000);`,
  );

  const port = 5399;
  const group = startGroup(process.execPath, ["launcher.mjs", String(port)], { cwd: dir, capture: true });
  for (let attempt = 0; attempt < 40 && !(await isPortBusy(port)); attempt += 1) await delay(100);
  assert.equal(await isPortBusy(port), true, "the stand-in never came up");

  await stopGroup(group.pid);
  for (let attempt = 0; attempt < 40 && (await isPortBusy(port)); attempt += 1) await delay(100);
  assert.equal(await isPortBusy(port), false, "the listener outlived the group it was started in");
});
