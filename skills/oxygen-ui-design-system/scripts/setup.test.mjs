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

// setup.mjs is the design system's Setup step: one manifest edit, one
// `npm install`. Pinned here with a stand-in `npm` on PATH that answers the
// registry lookups and records every invocation, so the tests can assert
// what was written to package.json and that npm was run exactly once.

import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, existsSync, chmodSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import path from "node:path";

const SCRIPT = path.join(import.meta.dirname, "setup.mjs");

// The registry as it stood on 2026-09-08: react-router's newest release wants
// a React newer than the exact one Oxygen pins, so `@latest` is the wrong
// answer and the script has to pick 7.18.3.
const ROUTER_RELEASES = [
  { version: "7.0.0", "peerDependencies.react": ">=18" },
  { version: "7.18.3", "peerDependencies.react": ">=18" },
  { version: "8.0.0-pre.1", "peerDependencies.react": ">=18" },
  { version: "8.3.1", "peerDependencies.react": ">=19.2.7" },
];
const OXYGEN_LATEST = { version: "0.13.1", "peerDependencies.react": "19.2.3", "peerDependencies.react-dom": "19.2.3" };

// Answers each `npm view` from a JSON file in the fixture's data dir, so a
// test can rewrite what the registry says without touching the script.
const FAKE_NPM = `#!/bin/sh
echo "$@" >> "$FAKE_NPM_LOG"
case "$1" in
  view)
    case "$2" in
      @wso2/oxygen-ui@latest) cat "$FAKE_NPM_DATA/oxygen.json";;
      @wso2/oxygen-ui-icons-react@latest) cat "$FAKE_NPM_DATA/icons.json";;
      "react-router@>=7.0.0") cat "$FAKE_NPM_DATA/router.json";;
      *) echo "unexpected view $2" >&2; exit 1;;
    esac;;
  install) test -n "$FAKE_NPM_FAIL_INSTALL" && exit 7; echo installed;;
  *) echo "unexpected npm $1" >&2; exit 1;;
esac
`;

/** A scaffold's App Path plus a bin dir holding the stand-in npm. */
function fixture({ pkg, installed = false } = {}) {
  const dir = mkdtempSync(path.join(tmpdir(), "oxygen-setup-"));
  const app = path.join(dir, "app");
  const bin = path.join(dir, "bin");
  mkdirSync(app, { recursive: true });
  mkdirSync(bin, { recursive: true });
  if (pkg !== null) {
    writeFileSync(
      path.join(app, "package.json"),
      typeof pkg === "string"
        ? pkg
        : JSON.stringify(
            pkg ?? {
              name: "todo-webapp",
              scripts: { build: "tsc --noEmit && vite build" },
              dependencies: { react: "^19.1.0", "react-dom": "^19.1.0", "openapi-fetch": "^0.14.0" },
              devDependencies: { vite: "^6.0.0", typescript: "~5.7.2" },
            },
          ),
    );
  }
  if (installed) {
    const p = path.join(app, "node_modules/@wso2/oxygen-ui/package.json");
    mkdirSync(path.dirname(p), { recursive: true });
    writeFileSync(p, JSON.stringify({ name: "@wso2/oxygen-ui", version: "0.13.1", peerDependencies: { react: "19.2.3", "react-dom": "19.2.3" } }));
  }
  writeFileSync(path.join(bin, "npm"), FAKE_NPM);
  chmodSync(path.join(bin, "npm"), 0o755);
  const data = path.join(dir, "registry");
  mkdirSync(data);
  writeFileSync(path.join(data, "oxygen.json"), JSON.stringify(OXYGEN_LATEST));
  writeFileSync(path.join(data, "icons.json"), JSON.stringify("0.13.1"));
  writeFileSync(path.join(data, "router.json"), JSON.stringify(ROUTER_RELEASES));
  return { app, bin, data, log: path.join(dir, "npm.log") };
}

function run({ app, bin, data, log }, args = [], env = {}) {
  const r = spawnSync(process.execPath, [SCRIPT, app, ...args], {
    encoding: "utf8",
    env: { ...process.env, PATH: `${bin}${path.delimiter}${process.env.PATH}`, FAKE_NPM_LOG: log, FAKE_NPM_DATA: data, ...env },
  });
  const calls = existsSync(log) ? readFileSync(log, "utf8").trim().split("\n") : [];
  let pkg = null;
  try {
    pkg = JSON.parse(readFileSync(path.join(app, "package.json"), "utf8"));
  } catch {
    // no manifest, or one the script was asked to reject — the test asserts on the output instead
  }
  return { status: r.status, out: r.stdout + r.stderr, calls, pkg };
}

test("a scaffold gets React pinned exactly, Oxygen added, and one npm install", () => {
  const f = fixture();
  const { status, out, calls, pkg } = run(f);
  assert.equal(status, 0, out);
  assert.equal(pkg.dependencies.react, "19.2.3");
  assert.equal(pkg.dependencies["react-dom"], "19.2.3");
  assert.equal(pkg.dependencies["@wso2/oxygen-ui"], "^0.13.1");
  assert.equal(pkg.dependencies["@wso2/oxygen-ui-icons-react"], "^0.13.1");
  assert.equal(pkg.dependencies["react-router"], "^7.18.3", "not 8.3.1, whose React peer the pinned React fails");
  assert.equal(pkg.dependencies["openapi-fetch"], "^0.14.0", "the scaffold's own dependencies stay");
  assert.equal(pkg.devDependencies.vite, "^6.0.0");
  assert.equal(pkg.scripts.build, "tsc --noEmit && vite build");
  assert.deepEqual(calls.filter((c) => c.startsWith("install")), ["install"]);
  assert.match(out, /ok    react and react-dom pinned to 19\.2\.3/);
  assert.match(out, /setup: ok \(one npm install\)/);
});

test("a bundled package the scaffold slipped in is removed, and named", () => {
  const f = fixture({
    pkg: { dependencies: { react: "^19.1.0", "@mui/material": "^7.0.0", "@emotion/react": "^11.0.0" }, devDependencies: { "lucide-react": "^0.5.0" } },
  });
  const { status, out, pkg } = run(f);
  assert.equal(status, 0, out);
  assert.equal(pkg.dependencies["@mui/material"], undefined);
  assert.equal(pkg.dependencies["@emotion/react"], undefined);
  assert.equal(pkg.devDependencies["lucide-react"], undefined);
  assert.match(out, /ok    removed @mui\/material, @emotion\/react, lucide-react — Oxygen bundles them/);
});

// The peer ranges routers publish, against the React Oxygen pins.
test("the router is the newest release whose React peer range the pinned React satisfies", () => {
  const releases = (rows) => JSON.stringify(rows.map(([version, react]) => ({ version, "peerDependencies.react": react })));
  const pick = (routerJson) => {
    const f = fixture();
    writeFileSync(path.join(f.data, "router.json"), routerJson);
    const { status, out, pkg } = run(f, ["--no-install"]);
    return { status, out, router: pkg?.dependencies?.["react-router"] };
  };
  // `||` alternatives, a caret, and a bare major all read correctly.
  assert.equal(pick(releases([["7.5.0", "^18.0.0 || ^19.0.0"], ["7.9.0", "^18.0.0 || >=19.2.7"]])).router, "^7.5.0");
  assert.equal(pick(releases([["7.5.0", "18"], ["7.6.0", "19"]])).router, "^7.6.0");
  assert.equal(pick(releases([["7.5.0", "*"], ["7.6.0", ">=19.3"]])).router, "^7.5.0");
  // A release with no React peer is acceptable; the newest one wins.
  assert.equal(pick(releases([["7.5.0", undefined], ["7.4.0", ">=18"]])).router, "^7.5.0");
  // One matching release comes back from npm as an object, not an array.
  assert.equal(pick(JSON.stringify({ version: "7.18.3", "peerDependencies.react": ">=18" })).router, "^7.18.3");
  // When nothing accepts the pinned React, that is a named failure, not a guess.
  const none = pick(releases([["8.3.1", ">=19.2.7"]]));
  assert.equal(none.status, 1);
  assert.match(none.out, /FAIL  could not resolve versions\n  fix: no react-router release accepts react 19\.2\.3/);
});

// npm 12 wraps every `--json` result in an array (`[{…}]`, `["0.13.1"]`),
// where npm 10 wraps only a multi-version match. Verified against npm 12.0.2.
test("npm 12's array-wrapped results resolve the same versions as npm 10's", () => {
  const f = fixture();
  writeFileSync(path.join(f.data, "oxygen.json"), JSON.stringify([OXYGEN_LATEST]));
  writeFileSync(path.join(f.data, "icons.json"), JSON.stringify(["0.13.1"]));
  writeFileSync(path.join(f.data, "router.json"), JSON.stringify([{ version: "7.18.3", "peerDependencies.react": ">=18" }]));
  const { status, out, pkg } = run(f);
  assert.equal(status, 0, out);
  assert.equal(pkg.dependencies.react, "19.2.3");
  assert.equal(pkg.dependencies["@wso2/oxygen-ui"], "^0.13.1");
  assert.equal(pkg.dependencies["@wso2/oxygen-ui-icons-react"], "^0.13.1");
  assert.equal(pkg.dependencies["react-router"], "^7.18.3");
});

// The registry can echo a dist-tag or a malformed entry into a version list;
// one such row must be skipped, not allowed to throw inside the sort.
test("a router release whose version is not x.y.z is skipped", () => {
  const f = fixture();
  writeFileSync(
    path.join(f.data, "router.json"),
    JSON.stringify([
      { version: "latest", "peerDependencies.react": ">=18" },
      { version: "7.18.3", "peerDependencies.react": ">=18" },
      { version: "", "peerDependencies.react": ">=18" },
    ]),
  );
  const { status, out, pkg } = run(f, ["--no-install"]);
  assert.equal(status, 0, out);
  assert.equal(pkg.dependencies["react-router"], "^7.18.3");
});

test("react-dom follows its own exact peer when Oxygen pins one, and React's version otherwise", () => {
  const f = fixture();
  writeFileSync(path.join(f.data, "oxygen.json"), JSON.stringify({ version: "0.13.1", "peerDependencies.react": "19.2.3", "peerDependencies.react-dom": "19.2.4" }));
  let r = run(f, ["--no-install"]);
  assert.equal(r.pkg.dependencies.react, "19.2.3");
  assert.equal(r.pkg.dependencies["react-dom"], "19.2.4");
  assert.match(r.out, /react pinned to 19\.2\.3, react-dom to 19\.2\.4/);

  const g = fixture();
  writeFileSync(path.join(g.data, "oxygen.json"), JSON.stringify({ version: "0.13.1", "peerDependencies.react": "19.2.3", "peerDependencies.react-dom": ">=19" }));
  r = run(g, ["--no-install"]);
  assert.equal(r.pkg.dependencies["react-dom"], "19.2.3", "a range is npm's to enforce; pin to React's exact version");
});

test("--charts adds the charts package at Oxygen's version", () => {
  const f = fixture();
  const { pkg } = run(f, ["--charts"]);
  assert.equal(pkg.dependencies["@wso2/oxygen-ui-charts-react"], "^0.13.1");
});

test("--no-install writes the manifest and runs no install", () => {
  const f = fixture();
  const { status, out, calls, pkg } = run(f, ["--no-install"]);
  assert.equal(status, 0, out);
  assert.equal(pkg.dependencies.react, "19.2.3");
  assert.ok(!calls.some((c) => c.startsWith("install")), calls.join("\n"));
  assert.match(out, /--no-install: run `npm install` from the App Path to apply/);
});

test("with Oxygen already installed the peer version is read locally, not from the registry", () => {
  const f = fixture({ installed: true });
  const { status, calls, pkg } = run(f, ["--no-install"]);
  assert.equal(status, 0);
  assert.equal(pkg.dependencies.react, "19.2.3");
  assert.ok(!calls.some((c) => c.startsWith("view @wso2/oxygen-ui@latest")), calls.join("\n"));
});

test("running it twice changes nothing the second time", () => {
  const f = fixture();
  run(f, ["--no-install"]);
  const before = readFileSync(path.join(f.app, "package.json"), "utf8");
  const { out } = run(f, ["--no-install"]);
  assert.equal(readFileSync(path.join(f.app, "package.json"), "utf8"), before);
  assert.match(out, /already carried every dependency at these versions/);
});

test("a React version in devDependencies is moved, not duplicated", () => {
  const f = fixture({ pkg: { dependencies: {}, devDependencies: { react: "^19.1.0", "react-dom": "^19.1.0" } } });
  const { pkg } = run(f, ["--no-install"]);
  assert.equal(pkg.dependencies.react, "19.2.3");
  assert.equal(pkg.devDependencies.react, undefined);
  assert.equal(pkg.devDependencies["react-dom"], undefined);
});

test("no package.json is a named failure pointing at the scaffold step", () => {
  const f = fixture({ pkg: null });
  const { status, out } = run(f);
  assert.equal(status, 1);
  assert.match(out, /FAIL  no package\.json at/);
  assert.match(out, /scaffold per react-webapp first/);
});

test("a malformed package.json is a named failure, not a stack trace", () => {
  const f = fixture({ pkg: "{ not json" });
  const { status, out } = run(f);
  assert.equal(status, 1);
  assert.match(out, /FAIL .*is not valid JSON/);
  assert.doesNotMatch(out, /at \w+ \(/);
});

test("a failing npm install fails the step with npm's exit code and no --force advice", () => {
  const f = fixture();
  const { status, out } = run(f, [], { FAKE_NPM_FAIL_INSTALL: "1" });
  assert.equal(status, 1);
  assert.match(out, /FAIL  npm install exited 7/);
  assert.match(out, /never --force or --legacy-peer-deps/);
});
