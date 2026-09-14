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

// The wireframe's demo data as fixtures, for the coding run's mock mode:
//
//   node scripts/seed.mjs <wireframes.dsl> [-o <fixtures.json>]
//
// A wireframe's `row` lines under a `table`, its stat `card`s, `list`s,
// `select`s and `badge`s are the data the reviewer expects to see on the
// screen. This prints them per screen as JSON, so mock handlers serve those
// rows and the numbers a stat card shows can be derived from them, instead
// of the coding agent retyping every string (and drifting on one).
//
// Only content-bearing elements are extracted; layout (`row`, `split`),
// chrome, controls without data, and `flow` blocks are not.

import { readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

/** The quoted label on an element line, with the `-> Target` and variant peeled off. */
function parseElement(rest) {
  const m = /^"((?:[^"\\]|\\.)*)"\s*(.*)$/.exec(rest);
  if (!m) return null;
  const label = m[1].replace(/\\"/g, '"');
  const tail = m[2].trim();
  const target = /->\s*(\w+)/.exec(tail)?.[1] ?? null;
  const variant = tail.replace(/->\s*\w+/, "").trim().split(/\s+/).filter(Boolean)[0] ?? null;
  return { label, target, variant };
}

const cells = (label) => label.split("|").map((s) => s.trim());

/** Strip a `//` comment that is not inside a quoted string. */
function stripComment(line) {
  let quoted = false;
  for (let i = 0; i < line.length - 1; i++) {
    if (line[i] === '"' && line[i - 1] !== "\\") quoted = !quoted;
    else if (!quoted && line[i] === "/" && line[i + 1] === "/") return line.slice(0, i);
  }
  return line;
}

/** `{ screens: { <Name>: { tables, stats, lists, selects, badges } } }` from DSL text. */
export function seedFromDsl(text) {
  const screens = {};
  let screen = null;
  let table = null; // { entry, indent } while the next lines may be its rows
  let inFlow = false;

  for (const raw of text.split("\n")) {
    const line = stripComment(raw).replace(/\s+$/, "");
    if (!line.trim()) continue;
    const indent = line.length - line.trimStart().length;
    const [keyword, ...restParts] = line.trim().split(/\s+/);
    const rest = restParts.join(" ");

    if (indent === 0) {
      table = null;
      inFlow = keyword === "flow";
      if (keyword === "screen") {
        screen = { tables: [], stats: [], lists: [], selects: [], badges: [] };
        screens[restParts[0]] = screen;
      }
      continue;
    }
    if (inFlow || !screen) continue;

    const el = parseElement(rest);

    // A quoted `row` directly under a table is that table's data; a bare `row`
    // is layout, and a quoted `row` deeper or shallower than the table's rows
    // belongs to whatever opened since.
    if (keyword === "row" && el && table && indent > table.indent) {
      const values = cells(el.label);
      const record = {};
      table.entry.columns.forEach((col, i) => {
        record[col] = values[i] ?? "";
      });
      table.entry.rows.push(record);
      continue;
    }
    if (table && indent <= table.indent) table = null;
    if (!el) continue;

    switch (keyword) {
      case "table": {
        const entry = { columns: cells(el.label), rows: [], target: el.target };
        screen.tables.push(entry);
        table = { entry, indent };
        break;
      }
      case "card": {
        const parts = cells(el.label);
        if (parts.length === 3) screen.stats.push({ label: parts[0], value: parts[1], caption: parts[2] });
        else if (parts.length === 2) screen.stats.push({ label: parts[0], value: parts[1], caption: null });
        break;
      }
      case "list":
        screen.lists.push({ items: cells(el.label) });
        break;
      case "select": {
        const m = /^([^:]+):\s*(.+)$/.exec(el.label);
        screen.selects.push(m ? { label: m[1].trim(), value: m[2].trim() } : { label: el.label, value: null });
        break;
      }
      case "badge":
        screen.badges.push({ label: el.label, variant: el.variant });
        break;
      default:
        break;
    }
  }
  return { screens };
}

function main() {
  const args = process.argv.slice(2);
  const outIdx = args.indexOf("-o");
  const outFile = outIdx === -1 ? null : args[outIdx + 1];
  const input = args.find((a, i) => !a.startsWith("-") && (outIdx === -1 || i !== outIdx + 1));
  if (!input || (outIdx !== -1 && (!outFile || outFile.startsWith("-")))) {
    console.log("usage: node seed.mjs <wireframes.dsl> [-o <fixtures.json>]");
    process.exit(2);
  }
  let text;
  try {
    text = readFileSync(input, "utf8");
  } catch (e) {
    console.log(`FAIL  cannot read ${input}: ${e.message}`);
    process.exit(1);
  }
  const json = `${JSON.stringify(seedFromDsl(text), null, 2)}\n`;
  if (outFile) {
    try {
      writeFileSync(outFile, json);
    } catch (e) {
      console.log(`FAIL  cannot write ${outFile}: ${e.message}`);
      process.exit(1);
    }
    console.log(`wrote ${outFile}`);
  } else {
    process.stdout.write(json);
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) main();
