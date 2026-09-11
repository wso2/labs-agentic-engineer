#!/usr/bin/env node
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

// The design system's step in `react-webapp`'s verify sequence, run from the
// App Path after `npm install` and before `npx tsc --noEmit`:
//
//   node scripts/verify.mjs [app-path]
//
// It checks the one class of fault `tsc` and `vite build` cannot see: Oxygen's
// wiring. Each of these type-checks and builds clean, then renders an unthemed
// or broken page in the cluster — a second MUI or Emotion installed beside the
// copy Oxygen bundles (two Emotion caches, the theme reaches half the tree),
// an import that bypasses `@wso2/oxygen-ui`, a root not wrapped in
// `OxygenUIThemeProvider`, or a React that is not the exact version Oxygen's
// peer dependency names (two Reacts, hooks throw at runtime). One line per
// check; a failure names its fix; a non-zero exit fails verification like any
// other step in the sequence.

import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";

const OXYGEN = "@wso2/oxygen-ui";
const ICONS = "@wso2/oxygen-ui-icons-react";

// Packages that already arrive with Oxygen — MUI, MUI X and Emotion as
// dependencies of `@wso2/oxygen-ui`, lucide as one of
// `@wso2/oxygen-ui-icons-react`. A direct copy of any of them duplicates a
// runtime that must be a singleton, so this list is the one source for all
// three ways the checks below use it: matching a dependency, matching an
// import specifier, and naming itself in a failure line. A trailing "/"
// means "this scope, any package under it".
const BUNDLED = ["@mui/", "@emotion/", "lucide-react"];
const BUNDLED_LABEL = BUNDLED.map((p) => (p.endsWith("/") ? `${p}*` : p)).join(", ");

const escapeRe = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
const SPECIFIER = BUNDLED.map((p) => (p.endsWith("/") ? `${escapeRe(p)}[^"']*` : `${escapeRe(p)}(?:\\/[^"']*)?`)).join("|");
const IMPORT_RE = new RegExp(`(?:from\\s*|import\\s*\\(?\\s*|require\\s*\\(\\s*)["'](${SPECIFIER})["']`, "g");

const SOURCE_EXT = new Set([".ts", ".tsx", ".js", ".jsx", ".mts", ".cts"]);

/** True when `dep` is one of the packages Oxygen already brings. */
const isBundled = (dep) => BUNDLED.some((p) => (p.endsWith("/") ? dep.startsWith(p) : dep === p));

/** Every source file under `dir`, depth-first; nothing under node_modules. */
function sourceFiles(dir, out = []) {
  if (!existsSync(dir)) return out;
  for (const name of readdirSync(dir)) {
    if (name === "node_modules") continue;
    const p = path.join(dir, name);
    if (statSync(p).isDirectory()) sourceFiles(p, out);
    else if (SOURCE_EXT.has(path.extname(name))) out.push(p);
  }
  return out;
}

/**
 * What an import scan covers: everything under `src/`, plus the build config
 * at the app root. A `@emotion/*` pulled into `vite.config.ts` duplicates the
 * runtime exactly as one in a page does, and it would otherwise go unseen.
 */
function scannedFiles(appDir) {
  const configs = existsSync(appDir)
    ? readdirSync(appDir)
        .filter((n) => /\.config\.[cm]?[jt]s$/.test(n))
        .map((n) => path.join(appDir, n))
    : [];
  return [...sourceFiles(path.join(appDir, "src")), ...configs];
}

/**
 * Source with comments blanked out. The import scan runs over this, because
 * this skill spends paragraphs telling people NOT to import `@mui/material` —
 * so a comment repeating that advice is likely, and matching it would fail a
 * correct app. The `[^:]` guard keeps `https://` out of the line-comment rule.
 */
const stripComments = (text) => text.replace(/\/\*[\s\S]*?\*\//g, " ").replace(/(^|[^:])\/\/[^\n]*/g, "$1");

/** A JSX element for `name`, not a mention of it in an import or a string. */
const rendersElement = (src, name) => new RegExp(`<\\s*${name}[\\s>]`).test(src);

/**
 * Whether the provider actually ENCLOSES the app. `main.tsx` importing the
 * symbol, or rendering it somewhere off the root, both leave the deployed page
 * unthemed while reading as wired.
 */
function providerWrapsRoot(text) {
  const src = stripComments(text);
  if (!rendersElement(src, "OxygenUIThemeProvider")) return false;
  const root = src.indexOf(".render(");
  return root === -1 ? true : rendersElement(src.slice(root), "OxygenUIThemeProvider");
}

/**
 * `{ value }` for a JSON OBJECT, `{ error }` for anything else — never throws.
 * A bare `null` parses fine and would then throw on the first field read,
 * killing the run before a single FAIL row is printed.
 */
function tryReadJSON(p) {
  try {
    const value = JSON.parse(readFileSync(p, "utf8"));
    if (value === null || typeof value !== "object" || Array.isArray(value)) {
      return { error: `expected a JSON object, got ${Array.isArray(value) ? "an array" : String(value)}` };
    }
    return { value };
  } catch (e) {
    return { error: e.message };
  }
}

/** The checks, as `{ name, ok, fix }` rows. */
function verifyApp(appDir) {
  const rows = [];
  const pkgPath = path.join(appDir, "package.json");
  if (!existsSync(pkgPath)) {
    return [{ name: "package.json present", ok: false, fix: `no package.json at ${appDir} — run from the App Path or pass it as the argument` }];
  }
  const pkgRead = tryReadJSON(pkgPath);
  if (pkgRead.error) {
    return [{ name: "package.json parses", ok: false, fix: `${pkgPath} is not valid JSON: ${pkgRead.error}` }];
  }
  const pkg = pkgRead.value;
  const deps = { ...(pkg.dependencies ?? {}), ...(pkg.devDependencies ?? {}) };

  for (const name of [OXYGEN, ICONS]) {
    rows.push({
      name: `${name} is a dependency`,
      ok: name in (pkg.dependencies ?? {}),
      fix: `npm install ${name}@latest`,
    });
  }

  const duplicated = Object.keys(deps).filter(isBundled);
  rows.push({
    name: `no ${BUNDLED_LABEL} installed directly`,
    ok: duplicated.length === 0,
    fix: `npm uninstall ${duplicated.join(" ")} — Oxygen and its icons package already bring them; a second copy breaks theming at runtime`,
  });

  const offenders = [];
  for (const file of scannedFiles(appDir)) {
    const text = readFileSync(file, "utf8");
    for (const m of stripComments(text).matchAll(IMPORT_RE)) offenders.push(`${path.relative(appDir, file)} → ${m[1]}`);
  }
  rows.push({
    name: `src/ and the build config import nothing from ${BUNDLED_LABEL}`,
    ok: offenders.length === 0,
    fix: `import from ${OXYGEN} / ${ICONS} instead:\n    ${offenders.join("\n    ")}`,
  });

  const main = ["src/main.tsx", "src/main.jsx", "src/main.ts", "src/main.js"].map((p) => path.join(appDir, p)).find(existsSync);
  rows.push({
    name: "src/main.tsx wraps the root in OxygenUIThemeProvider",
    ok: main !== undefined && providerWrapsRoot(readFileSync(main, "utf8")),
    fix:
      main === undefined
        ? "no src/main.tsx — scaffold per react-webapp, then wire the provider per SKILL.md Setup"
        : "render <OxygenUIThemeProvider theme={…}> around the app INSIDE createRoot(...).render(...) — importing it, or rendering it off the root, leaves the page unthemed (SKILL.md, Setup)",
  });

  const installedPath = path.join(appDir, "node_modules", OXYGEN, "package.json");
  const installed = existsSync(installedPath) ? tryReadJSON(installedPath) : { error: "not installed" };
  if (installed.error) {
    rows.push({ name: `${OXYGEN} is installed`, ok: false, fix: `npm install (this step runs after it) — ${installed.error}` });
    return rows;
  }
  rows.push({ name: `${OXYGEN} is installed`, ok: true });

  const peers = installed.value.peerDependencies ?? {};
  for (const name of ["react", "react-dom"]) {
    const want = peers[name];
    if (!want || !/^\d+\.\d+\.\d+$/.test(want)) continue; // a range is npm's to enforce
    const at = path.join(appDir, "node_modules", name, "package.json");
    const read = existsSync(at) ? tryReadJSON(at) : { error: "not installed" };
    rows.push({
      name: `${name} is exactly ${want} (Oxygen's peer dependency)`,
      ok: read.value?.version === want,
      fix: `npm install ${name}@${want} — installed: ${read.value?.version ?? "none"}`,
    });
  }
  return rows;
}

function main() {
  const appDir = path.resolve(process.argv[2] ?? process.cwd());
  const rows = verifyApp(appDir);
  let failed = 0;
  for (const r of rows) {
    if (r.ok) {
      console.log(`ok    ${r.name}`);
    } else {
      failed++;
      console.log(`FAIL  ${r.name}\n  fix: ${r.fix}`);
    }
  }
  console.log(failed === 0 ? `oxygen-ui-design-system verify: ok (${rows.length} checks)` : `oxygen-ui-design-system verify: ${failed} of ${rows.length} checks failed`);
  process.exit(failed === 0 ? 0 : 1);
}

main();
