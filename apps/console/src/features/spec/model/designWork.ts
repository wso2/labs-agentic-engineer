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

import type { ProductWideItem, SpecFeature } from "../api/specModel";
import { PRODUCT_WIDE_PATH } from "./files";
import { parseLine, type LineBlock } from "./ids";
import { blockingQuestions } from "./questions";

// Which features the next design turn takes, worked out from the live spec.
//
// Design runs over every interviewed feature that no question in its file
// blocks (model/questions.ts). Once designed, a feature carries the spec it
// was designed from (its basis): the words of its own file and of every
// product-wide item that reaches it. When the live spec no longer reads the
// same, that feature's design is out of date, and only it goes into "Update
// design". A new product-wide item marks every feature it applies to.
// Confirming an assumed line (dropping the tag) changes no words, so it moves
// no basis.

type Lines = ReadonlyMap<string, LineBlock[]>;

/** A line's words and its own ID, without the tag or sources the design (and a build) does not read. */
export function lineWords(line: LineBlock): string {
  const parts = parseLine(line.text, line.emphasis);
  return parts.lead ? `${parts.lead.id} ${parts.body}` : parts.body;
}

/** The spec one feature's design reads, as one text: its file's lines, then the product-wide items that reach it. */
export function designBasis(
  feature: Pick<SpecFeature, "id" | "path">,
  lines: Lines,
  productWide: ProductWideItem[],
): string {
  const own = (lines.get(feature.path) ?? []).map(lineWords);
  const reach = new Set(
    productWide.filter((p) => p.appliesTo === "all" || p.appliesTo.includes(feature.id)).map((p) => p.id),
  );
  const shared = (lines.get(PRODUCT_WIDE_PATH) ?? [])
    .filter((l) => {
      const lead = parseLine(l.text, l.emphasis).lead;
      return lead !== null && reach.has(lead.id);
    })
    .map(lineWords);
  return [...own, ...shared].filter((w) => w.length > 0).join("\n");
}

/** Interviewed and not waiting on a question in its file: design can take it. */
export function isDesignable(feature: Pick<SpecFeature, "path" | "stage">, lines: Lines): boolean {
  const interviewed = feature.stage === "Interviewed" || feature.stage === "Designed";
  return interviewed && blockingQuestions(lines.get(feature.path) ?? []).length === 0;
}

export interface DesignWork {
  /** What the next design turn takes: designable features never designed, or out of date. */
  toDesign: string[];
  /** Designed features whose spec has changed since. */
  outOfDate: string[];
}

export function designWork(
  features: Pick<SpecFeature, "id" | "path" | "stage">[],
  designedFrom: Readonly<Record<string, string>>,
  lines: Lines,
  productWide: ProductWideItem[],
): DesignWork {
  const outOfDate: string[] = [];
  const toDesign: string[] = [];
  for (const f of features) {
    if (!isDesignable(f, lines)) continue;
    const basis = designedFrom[f.id];
    if (basis === undefined) {
      toDesign.push(f.id);
    } else if (basis !== designBasis(f, lines, productWide)) {
      outOfDate.push(f.id);
      toDesign.push(f.id);
    }
  }
  return { toDesign, outOfDate };
}

function plural(n: number, word: string): string {
  return `${n} ${word}${n === 1 ? "" : "s"}`;
}

/**
 * The design action's words: "Design 2 features" while any of them is new to
 * design, "Update design · 1 feature" when every one was designed before.
 * Null when there is nothing to design.
 */
export function designLabel(toDesign: string[], designedFrom: Readonly<Record<string, string>>): string | null {
  if (toDesign.length === 0) return null;
  const count = plural(toDesign.length, "feature");
  return toDesign.every((id) => designedFrom[id] !== undefined) ? `Update design · ${count}` : `Design ${count}`;
}
