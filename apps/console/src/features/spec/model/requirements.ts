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

import type { ProductWideItem } from "../api/specModel";
import { parseLine, retiredEntry, RETIRED_SECTION, type LineBlock } from "./ids";
import { blockingQuestions, type BlockingQuestion } from "./questions";

// The requirements folder read into the model the backend decides with
// (services/aep-api/internal/platform/reqspec): features and their stories,
// product-wide items and what they apply to, needs, what is still assumed,
// blocking questions and retired IDs. The two readers follow one contract,
// skills/prd-contract, and are held to one fixture:
// packages/contracts/requirements/acme-expenses and the parse it must yield
// (expected.json there). This one reads the LIVE lines, so an edit shows at
// once.

export interface RequirementsStory {
  id: string;
  /** The ID it replaced when it moved here, "F2.3"; null when it did not move. */
  was: string | null;
  text: string;
  needs: string[];
  assumed: boolean;
}

export interface RequirementsFeature {
  id: string;
  name: string;
  /** Relative to requirements/: "features/F2-approvals.md". */
  path: string;
  needs: string[];
  stories: RequirementsStory[];
  toConfirm: number;
  blocking: BlockingQuestion[];
  retired: string[];
}

export interface RequirementsItem {
  id: string;
  text: string;
  /** Feature IDs, or the single entry "all". */
  appliesTo: string[];
  assumed: boolean;
}

export interface Requirements {
  features: RequirementsFeature[];
  productWide: RequirementsItem[];
  retiredFeatures: string[];
}

type Lines = ReadonlyMap<string, LineBlock[]>;

const ROOT = "specs/requirements/";
const PRODUCT = `${ROOT}prd.md`;
const FEATURE_FILE = /^specs\/requirements\/features\/(F\d+)-[^/]+\.md$/;
const PRODUCT_WIDE_FILE = /^specs\/requirements\/product-wide(?:\.md|\/[^/]+\.md)$/;
const NEEDS_LINE = /^needs:/i;

interface Section {
  title: string;
  lines: LineBlock[];
}

/** A file's title and its `## ` sections. */
function sectionsOf(lines: readonly LineBlock[]): { title: string; sections: Section[] } {
  let title = "";
  const sections: Section[] = [];
  for (const line of lines) {
    if (line.kind === "heading" && line.level === 1 && !title && sections.length === 0) {
      title = line.text.trim();
    } else if (line.kind === "heading" && (line.level ?? 1) <= 2) {
      sections.push({ title: line.text.trim(), lines: [] });
    } else if (sections.length > 0) {
      sections.at(-1)!.lines.push(line);
    }
  }
  return { title, sections };
}

function section(sections: Section[], title: string): LineBlock[] {
  return sections.find((s) => s.title.toLowerCase() === title.toLowerCase())?.lines ?? [];
}

/** The top-level entries of a list section. */
function items(lines: LineBlock[]): LineBlock[] {
  return lines.filter((l) => l.kind === "listItem" && (l.depth ?? 1) === 1);
}

function retiredIds(sections: Section[]): string[] {
  return items(section(sections, RETIRED_SECTION)).flatMap((l) => retiredEntry(l.text)?.id ?? []);
}

function compareIds(a: string, b: string): number {
  if (a[0] !== b[0]) return a[0]! < b[0]! ? -1 : 1;
  const pa = a.slice(1).split(".").map(Number);
  const pb = b.slice(1).split(".").map(Number);
  for (let i = 0; i < Math.min(pa.length, pb.length); i++) {
    if (pa[i] !== pb[i]) return pa[i]! - pb[i]!;
  }
  return pa.length - pb.length;
}

function readFeature(id: string, path: string, lines: readonly LineBlock[]): RequirementsFeature {
  const { title, sections } = sectionsOf(lines);
  const needsLine = section(sections, "Purpose").find((l) => l.kind === "paragraph" && NEEDS_LINE.test(l.text.trim()));
  const stories = items(section(sections, "User Stories")).flatMap((l): RequirementsStory[] => {
    const parts = parseLine(l.text, l.emphasis);
    if (!parts.lead?.id.startsWith(`${id}.`)) return [];
    return [{ id: parts.lead.id, was: parts.was?.id ?? null, text: parts.body, needs: parts.needs, assumed: parts.assumed !== null }];
  });
  const live = sections.filter((s) => s.title.toLowerCase() !== RETIRED_SECTION.toLowerCase()).flatMap((s) => s.lines);
  return {
    id,
    name: title || id,
    path: path.slice(ROOT.length),
    needs: needsLine ? parseLine(needsLine.text, needsLine.emphasis).needs : [],
    stories,
    toConfirm: live.filter((l) => parseLine(l.text, l.emphasis).assumed !== null).length,
    blocking: blockingQuestions(lines),
    retired: retiredIds(sections),
  };
}

/** Read the requirements from every markdown file's live lines (by room path). */
export function readRequirements(lines: Lines): Requirements {
  const features: RequirementsFeature[] = [];
  const productWide: RequirementsItem[] = [];
  for (const [path, fileLines] of lines) {
    const feature = FEATURE_FILE.exec(path);
    if (feature) features.push(readFeature(feature[1]!, path, fileLines));
    if (!PRODUCT_WIDE_FILE.test(path)) continue;
    for (const l of items(section(sectionsOf(fileLines).sections, "Requirements"))) {
      const parts = parseLine(l.text, l.emphasis);
      if (!parts.lead?.id.startsWith("P")) continue;
      const appliesTo = parts.appliesTo === "all" ? ["all"] : (parts.appliesTo ?? []);
      productWide.push({ id: parts.lead.id, text: parts.body, appliesTo, assumed: parts.assumed !== null });
    }
  }
  features.sort((a, b) => compareIds(a.id, b.id));
  productWide.sort((a, b) => compareIds(a.id, b.id));
  return { features, productWide, retiredFeatures: retiredIds(sectionsOf(lines.get(PRODUCT) ?? []).sections) };
}

/** The product-wide items as the design and build models take them: each with its reach. */
export function productWideItems(requirements: Requirements): ProductWideItem[] {
  return requirements.productWide.map((p) => ({ id: p.id, appliesTo: p.appliesTo.includes("all") ? "all" : p.appliesTo }));
}
