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

import type { SourceDocument } from "../api/specModel";
import type { LineBlock } from "./ids";

// What each attached document gave (S5): its coverage file in the room,
// specs/requirements/sources/<document>.md (skills/prd-contract), read into
// the rows the document's page shows — place by place, what it says, and the
// line or feature it landed in, or nowhere.

const SOURCES = /^specs\/requirements\/sources\/[^/]+\.md$/;
const OFFICE = /\.(docx|xlsx|pptx)\.md$/i;
const POINT = /^(.+?):\s+(.+?)\s+→\s+(.+)$/;
const PAGES = /^pages:\s*(\d+)/i;
const LANDED = /^(F\d+(?:\.\d+)?|P\d+)\b/;

/** A document's name as the user knows it: a converted Office file by its own name. */
export function documentTitle(name: string): string {
  return OFFICE.test(name) ? name.slice(0, -3) : name;
}

interface Coverage {
  pages: number;
  rows: SourceDocument["rows"];
}

function coverageOf(lines: readonly LineBlock[]): { title: string; coverage: Coverage } | null {
  const title = lines.find((l) => l.kind === "heading" && (l.level ?? 1) === 1)?.text.trim();
  if (!title) return null;
  let pages = 0;
  const rows: SourceDocument["rows"] = [];
  for (const line of lines) {
    const text = line.text.trim();
    if (line.kind === "paragraph" && PAGES.test(text)) pages = Number(PAGES.exec(text)![1]);
    if (line.kind !== "listItem") continue;
    const point = POINT.exec(text);
    if (!point) continue;
    const landed = LANDED.exec(point[3]!.trim())?.[1] ?? null;
    rows.push({ page: point[1]!.trim(), says: point[2]!.trim(), landedIn: landed });
  }
  return { title, coverage: { pages, rows } };
}

/** A name as a slug, without its extensions: "Policy v3.docx.md" and "policy-v3.md" both read "policy-v3". */
function slug(name: string): string {
  return name
    .replace(/(\.[a-z0-9]{1,5})+$/i, "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");
}

/**
 * The documents with what their coverage files say, where there is one. A
 * coverage file is matched to its document by its title (the document's
 * name), or by its file name, which is that name as a slug — an agent may
 * title it with the document's own heading instead.
 */
export function withCoverage(documents: SourceDocument[], lines: ReadonlyMap<string, LineBlock[]>): SourceDocument[] {
  const byKey = new Map<string, Coverage>();
  for (const [path, fileLines] of lines) {
    if (!SOURCES.test(path)) continue;
    const read = coverageOf(fileLines);
    if (!read) continue;
    byKey.set(slug(path.split("/").pop() ?? ""), read.coverage);
    byKey.set(slug(read.title), read.coverage);
  }
  return documents.map((d) => {
    const title = documentTitle(d.title);
    const coverage = byKey.get(slug(title));
    return coverage ? { ...d, title, pages: coverage.pages || d.pages, rows: coverage.rows } : { ...d, title };
  });
}
