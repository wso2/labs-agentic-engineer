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

import type { FeatureStage, ProductWideItem, SpecFeature, SpecModel } from "../api/specModel";
import { designLabel, designWork } from "./designWork";
import { markdownFiles, PRD_PATH, PRODUCT_KEY, type MarkdownFile } from "./files";
import { buildIdIndex, parseLine, type IdIndex, type LineBlock } from "./ids";
import { blockingQuestions, type BlockingQuestion } from "./questions";
import { productWideItems, readRequirements } from "./requirements";

// The workspace as the user sees it, worked out from the spec model's state
// and the live documents: each feature's chips, the ID index, the Fog, and
// Next up. Pure, so a keystroke that removes an `*assumed*` tag, or an answer
// that takes a `*blocking*` question out of the file, changes the chip and
// Next up on the next render, with nothing to keep in sync.

export type ChipTone = "warning" | "primary" | "success";

/** Something about a feature that needs the user, beside its stage. */
export interface Chip {
  tone: ChipTone;
  label: string;
}

export interface FeatureView extends SpecFeature {
  chips: Chip[];
  /** The questions its interview waits on, from its file's Open Questions (model/questions.ts). */
  blocking: BlockingQuestion[];
  /** Lines tagged `*assumed*` in its file. */
  toConfirm: number;
  /** Designed, but its spec has changed since (model/designWork.ts). */
  designOutOfDate: boolean;
}

export type NextUpKind = "blocking" | "confirm" | "interview" | "design" | "comments" | "build" | "fog";

/** Where a Next up item goes: a file of the spec (and a line or place in it), the design card, or the build picker. */
export type NextUpTarget = { card: "spec"; file: string; at?: string } | { card: "design" } | { card: "builds" };

export interface NextUpItem {
  kind: NextUpKind;
  label: string;
  why: string;
  /** Blocks work until it is done. */
  urgent: boolean;
  target: NextUpTarget;
}

/** What the design turn would take next, and the words of its action ("Design 2 features"). */
export interface DesignQueue {
  toDesign: string[];
  outOfDate: string[];
  label: string | null;
}

export interface Workspace {
  features: FeatureView[];
  /** The product-wide items and their reach, read from product-wide.md (model/requirements.ts). */
  productWide: ProductWideItem[];
  design: DesignQueue;
  files: MarkdownFile[];
  index: IdIndex;
  fog: string[];
  nextUp: NextUpItem[];
  /** Few features: the product page shows its feature list alone. */
  small: boolean;
}

/** A product with at most this many features is small: no Next up, no Fog. */
const SMALL_PRODUCT_FEATURES = 2;

export function isSmallProduct(featureCount: number): boolean {
  return featureCount <= SMALL_PRODUCT_FEATURES;
}

/**
 * Lines still tagged `*assumed*`: each one is the user's to confirm.
 */
export function countAssumed(lines: LineBlock[]): number {
  return lines.filter((l) => parseLine(l.text, l.emphasis).assumed !== null).length;
}

/** The list entries under a document's `## <heading>`, in order. */
export function sectionItems(lines: LineBlock[], heading: string): string[] {
  const out: string[] = [];
  let inside = false;
  for (const line of lines) {
    if (line.kind === "heading" && (line.level ?? 1) <= 2) {
      inside = line.text.trim().toLowerCase() === heading.toLowerCase();
      continue;
    }
    if (inside && line.kind === "listItem" && line.text.trim()) out.push(line.text.trim());
  }
  return out;
}

export function featureChips(blocked: boolean, toConfirm: number, designOutOfDate = false): Chip[] {
  const chips: Chip[] = [];
  if (blocked) chips.push({ tone: "warning", label: "blocked" });
  if (toConfirm > 0) chips.push({ tone: "warning", label: `${toConfirm} to confirm` });
  if (designOutOfDate) chips.push({ tone: "primary", label: "design out of date" });
  return chips;
}

/** The stage's tag colour; null is the muted default. */
export function stageTone(stage: FeatureStage): ChipTone | null {
  if (stage === "Designed") return "success";
  if (stage === "Interviewing") return "primary";
  return null;
}

/** The one short state the file rail has room for: what needs the user first, else the stage. */
export function railState(feature: Pick<FeatureView, "chips" | "stage">): { label: string; tone: ChipTone | null } {
  const chip = feature.chips[0];
  if (chip) return chip;
  return {
    label: feature.stage === "Not interviewed" ? "not yet" : feature.stage.toLowerCase(),
    tone: stageTone(feature.stage),
  };
}

function plural(n: number, word: string): string {
  return `${n} ${word}${n === 1 ? "" : "s"}`;
}

/**
 * What to do next, in a fixed order: blocking questions, lines to confirm,
 * interviews, the design turn (new or out-of-date features), comments, then
 * the Fog. The build (builds/model/picker.ts) goes in before the Fog, with
 * `withBuild`: it reads the design review and the builds, which the spec
 * workspace does not.
 */
export function nextUp(input: {
  features: FeatureView[];
  design: Pick<DesignQueue, "toDesign" | "label">;
  openComments: number;
  fog: string[];
}): NextUpItem[] {
  const { features, design, openComments, fog } = input;
  const items: NextUpItem[] = [];
  for (const f of features) {
    for (const { question } of f.blocking) {
      items.push({
        kind: "blocking",
        label: `Answer "${question}"`,
        why: `unblocks ${f.name}`,
        urgent: true,
        target: { card: "spec", file: f.id, at: "blocking" },
      });
    }
  }
  for (const f of features) {
    if (f.toConfirm === 0) continue;
    items.push({
      kind: "confirm",
      label: `Confirm ${plural(f.toConfirm, "line")} in ${f.name}`,
      why: "building as assumed until then",
      urgent: false,
      target: { card: "spec", file: f.id, at: "assumed" },
    });
  }
  for (const f of features) {
    if (f.stage !== "Not interviewed" || f.blocking.length > 0) continue;
    items.push({
      kind: "interview",
      label: `Interview ${f.name}`,
      why: "not interviewed yet",
      urgent: false,
      target: { card: "spec", file: f.id },
    });
  }
  if (design.label) {
    const names = design.toDesign.map((id) => features.find((f) => f.id === id)?.name ?? id);
    items.push({
      kind: "design",
      label: design.label,
      why: names.join(", "),
      urgent: false,
      target: { card: "design" },
    });
  }
  if (openComments > 0) {
    items.push({
      kind: "comments",
      label: `Address ${plural(openComments, "comment")}`,
      why: "design feedback",
      urgent: false,
      target: { card: "design" },
    });
  }
  for (const text of fog) {
    items.push({
      kind: "fog",
      label: `Shape "${text}"`,
      why: "Fog",
      urgent: false,
      target: { card: "spec", file: PRODUCT_KEY, at: "fog" },
    });
  }
  return items;
}

/** Next up with the build item in its place: after everything but the Fog. */
export function withBuild(items: NextUpItem[], build: NextUpItem | null): NextUpItem[] {
  if (!build) return items;
  const fog = items.findIndex((i) => i.kind === "fog");
  const at = fog < 0 ? items.length : fog;
  return [...items.slice(0, at), build, ...items.slice(at)];
}

/** The workspace from the model and every markdown file's live lines (by room path). */
export function deriveWorkspace(model: SpecModel, lines: ReadonlyMap<string, LineBlock[]>): Workspace {
  const files = markdownFiles(model.features);
  const { designedFrom } = model.design;
  const productWide = productWideItems(readRequirements(lines));
  const work = designWork(model.features, designedFrom, lines, productWide);
  const design = { ...work, label: designLabel(work.toDesign, designedFrom) };
  const features = model.features.map((f) => {
    const own = lines.get(f.path) ?? [];
    const toConfirm = countAssumed(own);
    const blocking = blockingQuestions(own);
    const designOutOfDate = work.outOfDate.includes(f.id);
    return { ...f, blocking, toConfirm, designOutOfDate, chips: featureChips(blocking.length > 0, toConfirm, designOutOfDate) };
  });
  const index = buildIdIndex(
    model.features,
    files.map((f) => ({ fileKey: f.key, lines: lines.get(f.path) ?? [] })),
  );
  const fog = sectionItems(lines.get(PRD_PATH) ?? [], "Fog");
  return {
    features,
    productWide,
    design,
    files,
    index,
    fog,
    nextUp: nextUp({
      features,
      design,
      openComments: model.design.openComments,
      fog,
    }),
    small: isSmallProduct(features.length),
  };
}
