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

// The design system's API lookup, run from the App Path after `npm install`:
//
//   node scripts/props.mjs [--app <app-path>] <Component> [<Component.Sub> …]
//
// For every name it prints the props of that Oxygen composite — name, type,
// required or not, the doc line — and its sub-components with their props,
// read from the `.d.ts` the installed `@wso2/oxygen-ui` ships. The types are
// generated from the code, so what this prints is the API of the version
// this app installed; the package's hand-written `.claude/components.md` is
// not (it documents a `StatCard` `title` prop that does not exist). One call
// replaces reading the docs, the declarations and the compiled source, and it
// cannot drift, because it reads nothing this skill carries.
//
// A name that is not an Oxygen composite (`Button`, `TextField`, `Tabs`) is
// plain MUI v7, re-exported unchanged except for the theme; the script says
// so and names the MUI API page.

import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";

const OXYGEN = "@wso2/oxygen-ui";
const NAMESPACES = { DataGrid: "@mui/x-data-grid", DatePickers: "@mui/x-date-pickers", TreeView: "@mui/x-tree-view" };
const TYPE_WIDTH = 72;

/** Every `.d.ts` under `dir`, depth-first. */
function declarationFiles(dir, out = []) {
  for (const name of readdirSync(dir)) {
    const p = path.join(dir, name);
    if (statSync(p).isDirectory()) declarationFiles(p, out);
    else if (name.endsWith(".d.ts")) out.push(p);
  }
  return out;
}

/** Text with comments blanked to spaces (offsets preserved), so a brace or `;` in a doc line cannot confuse the scan. */
function stripComments(text) {
  return text
    .replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, " "))
    .replace(/(^|[^:])\/\/[^\n]*/g, (m, p) => p + m.slice(p.length).replace(/[^\n]/g, " "));
}

/** Index in `plain` where the brace opened at `open` closes, or -1. */
function matchBrace(plain, open) {
  let depth = 0;
  for (let i = open; i < plain.length; i++) {
    if (plain[i] === "{") depth++;
    else if (plain[i] === "}" && --depth === 0) return i;
  }
  return -1;
}

/** The first sentence of the JSDoc block that ends right before `at` in `text`, or "". */
function docBefore(text, at) {
  const m = /\/\*\*((?:(?!\*\/)[\s\S])*)\*\/\s*$/.exec(text.slice(0, at));
  if (!m) return "";
  const line = m[1]
    .split("\n")
    .map((l) => l.replace(/^\s*\*\s?/, "").trim())
    .filter((l) => l && !l.startsWith("@"))
    .join(" ");
  return line.split(/(?<=\.)\s/)[0];
}

/**
 * The members of the object type whose `{` sits at `open` in `text`:
 * `{ name, optional, type, doc }` rows; index signatures, call signatures
 * and methods skipped. Splits on `;` at depth 0, where `=>` is not a bracket.
 */
function members(text, open) {
  const plain = stripComments(text);
  const close = matchBrace(plain, open);
  if (close === -1) return [];
  const rows = [];
  let start = open + 1;
  let depth = 0;
  for (let i = open + 1; i <= close; i++) {
    const ch = plain[i];
    if (ch === "{" || ch === "(" || ch === "[" || (ch === "<" && plain[i - 1] !== "=")) depth++;
    else if (ch === "}" || ch === ")" || ch === "]" || (ch === ">" && plain[i - 1] !== "=")) depth--;
    if ((ch === ";" && depth === 0) || i === close) {
      const raw = plain.slice(start, i);
      const m = /^\s*(?:readonly\s+)?([\w$]+|'[^']+'|"[^"]+")\s*(\?)?\s*:\s*([\s\S]+?)\s*$/.exec(raw);
      if (m) {
        rows.push({
          name: m[1].replace(/^['"]|['"]$/g, ""),
          optional: m[2] === "?",
          type: m[3].replace(/\s+/g, " ").trim(),
          doc: docBefore(text, start + raw.search(/\S/)),
        });
      }
      start = i + 1;
    }
  }
  return rows;
}

/** Every `interface XProps` / `type XProps =` across the files, keyed by X. */
function indexProps(files) {
  const index = new Map();
  for (const file of files) {
    const text = readFileSync(file, "utf8");
    const plain = stripComments(text);
    for (const m of plain.matchAll(/\binterface\s+(\w+?)Props\b([^{]*)\{/g)) {
      const open = m.index + m[0].length - 1;
      const extendsClause = /extends\s+([^{]+)/.exec(m[2])?.[1].replace(/\s+/g, " ").trim();
      index.set(m[1], { file, extends: extendsClause, rows: members(text, open) });
    }
    for (const m of plain.matchAll(/\btype\s+(\w+?)Props\b[^=]*=([^;{]*)(\{)?/g)) {
      if (index.has(m[1])) continue;
      const base = m[2].replace(/[&\s]+$/, "").replace(/\s+/g, " ").trim();
      const open = m[3] ? m.index + m[0].length - 1 : -1;
      index.set(m[1], { file, extends: base || undefined, rows: open === -1 ? [] : members(text, open) });
    }
  }
  return index;
}

/**
 * `[{ sub, ident, type }]` for a compound component: the members of the
 * object type `declare const <Name>…` carries — inline (`React.FC<P> & { … }`,
 * or a bare `{ … }`), or through a named interface
 * (`declare const ListingTableCompound: ListingTableComponent`). `ident` is
 * the `typeof X` target when the member is one, else the member's own name.
 */
function compoundMembers(files, name) {
  const subsOf = (text, open) =>
    members(text, open).map((r) => ({ sub: r.name, ident: /^typeof\s+(\w+)$/.exec(r.type)?.[1] ?? r.name, type: r.type }));
  for (const file of files) {
    const text = readFileSync(file, "utf8");
    const plain = stripComments(text);
    const decl = new RegExp(`declare const ${name}\\w*\\s*:\\s*([^;{]*)(\\{)?`).exec(plain);
    if (!decl) continue;
    if (decl[2]) {
      const subs = subsOf(text, decl.index + decl[0].length - 1);
      if (subs.length) return subs;
    }
    const named = decl[1].trim().match(/^(\w+)$/)?.[1];
    if (named) {
      const iface = new RegExp(`\\b(?:interface|type)\\s+${named}\\b[^{]*\\{`).exec(plain);
      if (iface) {
        const subs = subsOf(text, iface.index + iface[0].length - 1);
        if (subs.length) return subs;
      }
    }
  }
  return [];
}

const truncate = (s, n) => (s.length > n ? `${s.slice(0, n - 1)}…` : s);

function printProps(label, entry, indent, fallbackType) {
  const pad = " ".repeat(indent);
  if (!entry) {
    console.log(`${pad}${label}: ${fallbackType ? truncate(fallbackType, TYPE_WIDTH) : "no props of its own"}`);
    return;
  }
  console.log(`${pad}${label}${entry.extends ? ` — plus ${entry.extends}` : ""}`);
  if (entry.rows.length === 0) console.log(`${pad}  (no own props)`);
  const width = Math.max(0, ...entry.rows.map((r) => r.name.length + 1));
  for (const r of entry.rows) {
    const name = (r.name + (r.optional ? "?" : "")).padEnd(width);
    const req = r.optional ? "          " : "required  ";
    console.log(`${pad}  ${name}  ${req}${truncate(r.type, TYPE_WIDTH)}${r.doc ? `  — ${r.doc}` : ""}`);
  }
}

/** Print one component; true when it resolved. */
function describe(componentsDir, request) {
  const [name, sub] = request.split(".");
  if (NAMESPACES[name]) {
    console.log(`${request}: a MUI X namespace re-exported from ${OXYGEN} — use ${name}.${sub ?? "<Component>"}; API at https://mui.com/x/api/ (${NAMESPACES[name]})`);
    return true;
  }
  const dir = path.join(componentsDir, name);
  if (!existsSync(dir)) {
    const kebab = name.replace(/([a-z0-9])([A-Z])/g, "$1-$2").toLowerCase();
    console.log(`${request}: not an Oxygen composite. If it is a Material UI component it keeps MUI v7's API, re-exported from ${OXYGEN} with the theme applied: https://mui.com/material-ui/api/${kebab}/`);
    return false;
  }
  const files = declarationFiles(dir);
  const index = indexProps(files);
  const subs = compoundMembers(files, name);
  const rel = (f) => path.relative(path.dirname(path.dirname(componentsDir)), f);

  if (sub) {
    const hit = subs.find((s) => s.sub === sub);
    if (!hit) {
      console.log(`${request}: ${name} has no sub-component "${sub}". It has: ${subs.map((s) => `${name}.${s.sub}`).join(", ") || "none"}`);
      return false;
    }
    const entry = index.get(hit.ident);
    printProps(`${name}.${sub}${entry ? `  (${rel(entry.file)})` : ""}`, entry, 0, hit.type);
    return true;
  }

  const main = index.get(name);
  printProps(`${name}  (${main ? rel(main.file) : rel(dir)})`, main, 0);
  if (subs.length === 0) {
    console.log("  sub-components: none");
    return true;
  }
  console.log(`  sub-components: ${subs.map((s) => `${name}.${s.sub}`).join(", ")}`);
  for (const s of subs) printProps(`${name}.${s.sub}`, index.get(s.ident), 2, s.type);
  return true;
}

function main() {
  const args = process.argv.slice(2);
  let appDir = process.cwd();
  const names = [];
  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--app") appDir = path.resolve(args[++i] ?? ".");
    else names.push(args[i]);
  }
  if (names.length === 0) {
    console.log("usage: node props.mjs [--app <app-path>] <Component> [<Component.Sub> …]");
    process.exit(2);
  }
  const componentsDir = path.join(appDir, "node_modules", OXYGEN, "dist", "components");
  if (!existsSync(componentsDir)) {
    console.log(`FAIL  ${OXYGEN} is not installed under ${appDir}\n  fix: run from the App Path after npm install, or pass it with --app`);
    process.exit(1);
  }
  const version = JSON.parse(readFileSync(path.join(appDir, "node_modules", OXYGEN, "package.json"), "utf8")).version;
  console.log(`${OXYGEN}@${version} — props from the installed .d.ts`);
  let missing = 0;
  for (const name of names) {
    console.log("");
    if (!describe(componentsDir, name)) missing++;
  }
  process.exit(missing === 0 ? 0 : 1);
}

main();
