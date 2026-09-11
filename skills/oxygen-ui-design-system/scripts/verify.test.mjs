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

// verify.mjs is the design system's one step in react-webapp's verify
// sequence. Pinned here against a stand-in App Path: each fault it exists to
// catch flips exactly one check, and a clean app passes every one.

import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import path from "node:path";

const SCRIPT = path.join(import.meta.dirname, "verify.mjs");
const REACT = "19.2.3";

/** A stand-in App Path wired the way SKILL.md's Setup leaves it. */
function fixtureApp({ deps = {}, main, src = {}, installed = true, reactInstalled = REACT, viteConfig } = {}) {
  const dir = mkdtempSync(path.join(tmpdir(), "oxygen-verify-"));
  const write = (rel, text) => {
    mkdirSync(path.dirname(path.join(dir, rel)), { recursive: true });
    writeFileSync(path.join(dir, rel), text);
  };
  write(
    "package.json",
    JSON.stringify({
      dependencies: { "@wso2/oxygen-ui": "^0.13.1", "@wso2/oxygen-ui-icons-react": "^0.13.1", react: REACT, "react-dom": REACT, ...deps },
    }),
  );
  write(
    "src/main.tsx",
    main ??
      `import { OxygenUIThemeProvider, OxygenTheme } from '@wso2/oxygen-ui';
createRoot(document.getElementById('root')!).render(<OxygenUIThemeProvider theme={OxygenTheme}><App /></OxygenUIThemeProvider>);`,
  );
  write("src/pages/Home.tsx", `import { Box } from '@wso2/oxygen-ui';\nimport { Search } from '@wso2/oxygen-ui-icons-react';\n`);
  write("vite.config.ts", viteConfig ?? `import { defineConfig } from 'vite';\nexport default defineConfig({});\n`);
  for (const [rel, text] of Object.entries(src)) write(rel, text);
  if (installed) {
    write(
      "node_modules/@wso2/oxygen-ui/package.json",
      JSON.stringify({ name: "@wso2/oxygen-ui", version: "0.13.1", peerDependencies: { react: REACT, "react-dom": REACT, "@wso2/oxygen-ui-icons-react": ">=0.1.0" } }),
    );
    write("node_modules/react/package.json", JSON.stringify({ name: "react", version: reactInstalled }));
    write("node_modules/react-dom/package.json", JSON.stringify({ name: "react-dom", version: reactInstalled }));
    // A stray import path under node_modules must never count as an offender.
    write("node_modules/@wso2/oxygen-ui/dist/index.js", `export * from "@mui/material";`);
  }
  return dir;
}

function run(appDir, { cwd = appDir, args = [] } = {}) {
  const r = spawnSync(process.execPath, [SCRIPT, ...args], { cwd, encoding: "utf8" });
  return { status: r.status, out: r.stdout + r.stderr };
}

test("a correctly wired app passes every check", () => {
  const { status, out } = run(fixtureApp());
  assert.equal(status, 0, out);
  assert.doesNotMatch(out, /^FAIL/m);
  assert.match(out, /verify: ok \(\d+ checks\)/);
});

test("the App Path may be passed as the argument instead of being the cwd", () => {
  const app = fixtureApp();
  const { status, out } = run(app, { cwd: tmpdir(), args: [app] });
  assert.equal(status, 0, out);
});

test("a direct @mui/material or @emotion dependency fails — they already arrive with Oxygen", () => {
  const { status, out } = run(fixtureApp({ deps: { "@mui/material": "^7.0.0", "@emotion/react": "^11.0.0" } }));
  assert.equal(status, 1);
  assert.match(out, /FAIL  no @mui\/\*, @emotion\/\*, lucide-react installed directly/);
  assert.match(out, /npm uninstall @mui\/material @emotion\/react/);
});

test("a source import that bypasses @wso2/oxygen-ui fails and names the file", () => {
  const { status, out } = run(
    fixtureApp({
      src: {
        "src/pages/List.tsx": `import { Table } from '@mui/material';\nimport { Trash2 } from "lucide-react";\n`,
        "src/lib/lazy.ts": `const m = await import('@mui/x-data-grid');\n`,
      },
    }),
  );
  assert.equal(status, 1);
  assert.match(out, /FAIL  src\/ and the build config import nothing from/);
  assert.match(out, /src\/pages\/List\.tsx → @mui\/material/);
  assert.match(out, /src\/pages\/List\.tsx → lucide-react/);
  assert.match(out, /src\/lib\/lazy\.ts → @mui\/x-data-grid/);
  // The one failing check is the only one: the rest of the wiring is fine.
  assert.equal(out.match(/^FAIL/gm).length, 1);
});

test("a root not wrapped in OxygenUIThemeProvider fails", () => {
  const { status, out } = run(fixtureApp({ main: `import App from './App';\ncreateRoot(document.getElementById('root')!).render(<App />);` }));
  assert.equal(status, 1);
  assert.match(out, /FAIL  src\/main\.tsx wraps the root in OxygenUIThemeProvider/);
});

test("a missing Oxygen dependency fails with the install command", () => {
  const dir = fixtureApp();
  writeFileSync(path.join(dir, "package.json"), JSON.stringify({ dependencies: { react: REACT, "react-dom": REACT } }));
  const { status, out } = run(dir);
  assert.equal(status, 1);
  assert.match(out, /FAIL  @wso2\/oxygen-ui is a dependency\n  fix: npm install @wso2\/oxygen-ui@latest/);
  assert.match(out, /FAIL  @wso2\/oxygen-ui-icons-react is a dependency/);
});

test("React at a version other than Oxygen's exact peer fails — two Reacts at runtime", () => {
  const { status, out } = run(fixtureApp({ reactInstalled: "19.3.0" }));
  assert.equal(status, 1);
  assert.match(out, /FAIL  react is exactly 19\.2\.3 \(Oxygen's peer dependency\)\n  fix: npm install react@19\.2\.3 — installed: 19\.3\.0/);
  assert.match(out, /FAIL  react-dom is exactly 19\.2\.3/);
});

test("before npm install the step fails on the missing package and says so", () => {
  const { status, out } = run(fixtureApp({ installed: false }));
  assert.equal(status, 1);
  assert.match(out, /FAIL  @wso2\/oxygen-ui is installed\n  fix: npm install/);
  // The peer check needs the installed package, so it is not reported as a second failure.
  assert.doesNotMatch(out, /is exactly/);
});

test("run outside an App Path, it says so instead of passing vacuously", () => {
  const empty = mkdtempSync(path.join(tmpdir(), "oxygen-verify-empty-"));
  const { status, out } = run(empty);
  assert.equal(status, 1);
  assert.match(out, /FAIL  package\.json present/);
});

test("an import pulled into the build config is caught too, not just src/", () => {
  const { status, out } = run(
    fixtureApp({ viteConfig: `import emotion from '@emotion/babel-plugin';\nexport default { plugins: [emotion] };\n` }),
  );
  assert.equal(status, 1);
  assert.match(out, /vite\.config\.ts → @emotion\/babel-plugin/);
});

test("a malformed package.json is a named failure, not a raw stack trace", () => {
  const dir = fixtureApp();
  writeFileSync(path.join(dir, "package.json"), "{ not json");
  const { status, out } = run(dir);
  assert.equal(status, 1);
  assert.match(out, /FAIL  package\.json parses/);
  assert.doesNotMatch(out, /at \w+ \(/); // no stack frames
});

// The script is the App Path's verify step, and an App Path is a directory
// name the platform does not choose. A path needing URL-escaping must not
// turn every check into a silent pass.
test("a path that needs escaping still runs the checks", () => {
  const parent = mkdtempSync(path.join(tmpdir(), "oxygen verify spaced-"));
  const app = fixtureApp();
  const script = path.join(parent, "verify.mjs");
  writeFileSync(script, readFileSync(SCRIPT, "utf8"));
  const r = spawnSync(process.execPath, [script, app], { encoding: "utf8" });
  assert.match(r.stdout + r.stderr, /verify: ok \(\d+ checks\)/);
  assert.equal(r.status, 0);
});

// --- the checks must not be fooled, in either direction ---------------------

// This skill spends paragraphs saying "never import @mui/material", so a
// comment repeating that advice inside an app is likely. It must not fail one.
test("a bundled package named in a comment is not an import", () => {
  const { status, out } = run(
    fixtureApp({
      src: {
        "src/pages/Note.tsx": `// never import { Table } from '@mui/material' — use ListingTable\n/* also not from '@emotion/react' */\nexport const n = 1;\n`,
      },
    }),
  );
  assert.equal(status, 0, out);
  assert.doesNotMatch(out, /^FAIL/m);
});

test("a real import is still caught on a line that also carries a URL", () => {
  const { status, out } = run(
    fixtureApp({ src: { "src/pages/L.tsx": `import { Table } from '@mui/material'; // see https://mui.com/table\n` } }),
  );
  assert.equal(status, 1);
  assert.match(out, /src\/pages\/L\.tsx → @mui\/material/);
});

// Importing the provider, or rendering it off the root, leaves the deployed
// page unthemed while reading as wired.
test("the provider imported but not wrapping the root fails", () => {
  const { status, out } = run(
    fixtureApp({
      main: `import { OxygenUIThemeProvider } from '@wso2/oxygen-ui';\ncreateRoot(document.getElementById('root')!).render(<App />);`,
    }),
  );
  assert.equal(status, 1);
  assert.match(out, /FAIL  src\/main\.tsx wraps the root in OxygenUIThemeProvider/);
  assert.match(out, /INSIDE createRoot/);
});

test("the provider rendered off the root fails", () => {
  const { status, out } = run(
    fixtureApp({
      main: `import { OxygenUIThemeProvider } from '@wso2/oxygen-ui';\nexport const Preview = () => <OxygenUIThemeProvider><Swatch /></OxygenUIThemeProvider>;\ncreateRoot(document.getElementById('root')!).render(<App />);`,
    }),
  );
  assert.equal(status, 1);
  assert.match(out, /FAIL  src\/main\.tsx wraps the root in OxygenUIThemeProvider/);
});

// `JSON.parse("null")` succeeds, then every field read throws — which would
// kill the run before a single row is printed.
test("a package.json that is literally null is a named failure", () => {
  const dir = fixtureApp();
  writeFileSync(path.join(dir, "package.json"), "null");
  const { status, out } = run(dir);
  assert.equal(status, 1);
  assert.match(out, /FAIL  package\.json parses/);
  assert.match(out, /expected a JSON object, got null/);
});

test("an installed Oxygen package.json that is null fails the install check, not the process", () => {
  const dir = fixtureApp();
  writeFileSync(path.join(dir, "node_modules/@wso2/oxygen-ui/package.json"), "null");
  const { status, out } = run(dir);
  assert.equal(status, 1);
  assert.match(out, /FAIL  @wso2\/oxygen-ui is installed/);
  assert.doesNotMatch(out, /at \w+ \(/); // no stack frames
});
