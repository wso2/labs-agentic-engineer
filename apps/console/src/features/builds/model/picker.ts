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

import type { DesignArtifact, DesignComment, DesignDependency } from "../../design/api/designModel";
import type { ProductWideItem } from "../../spec/api/specModel";
import { PRODUCT_WIDE_PATH } from "../../spec/model/files";
import { parseLine, type LineBlock } from "../../spec/model/ids";
import type { FeatureView, NextUpItem } from "../../spec/model/workspace";
import type { ProjectBuild } from "../api/builds";
import type { BuildSelection } from "../buildSelection";
import { builtLines, changeWords, lastBuildOf, lineChanges } from "./changes";

// The build picker, "What goes into v2?", worked out from the live spec, the
// design review and the builds so far. Pure, so an edit that puts a design
// out of date takes its feature out of the picker on the next render.
//
// A build is a selection of features, and only a designed feature can be
// picked. One whose design is behind its spec, or that waits on something
// outside the product (Payroll export on Xero), shows disabled with the
// reason; one not interviewed yet, or held by a question, shows why without a
// box to tick. Each pickable row says what changed since the feature was last
// built ("new" before its first), how many lines still build as assumed, and
// how many design comments are not resolved: those warn, never block.
//
// A product-wide item added since the last build gets a row of its own:
// ticking it pulls in every built or pickable feature it applies to, and it
// cannot be picked while a built feature it applies to cannot be rebuilt.

/** offered: can be picked. disabled: designed or nearly, but held (the reason says why). fixed: nothing to pick. */
export type PickState = "offered" | "disabled" | "fixed";

export interface FeaturePickRow {
  kind: "feature";
  id: string;
  name: string;
  state: PickState;
  /** The row's second line, in parts: what changed, what warns, or why it is held. */
  detail: string[];
  /** The detail warns (amber): held, lines to confirm, or comments open. */
  warn: boolean;
}

export interface ProductWidePickRow {
  kind: "product-wide";
  id: string;
  /** The item's words, from product-wide.md. */
  text: string;
  state: Exclude<PickState, "fixed">;
  /** The features ticking it ticks too. */
  pullsIn: string[];
  detail: string[];
  warn: boolean;
}

export type PickRow = FeaturePickRow | ProductWidePickRow;

export interface PickerInput {
  features: Pick<FeatureView, "id" | "name" | "path" | "stage" | "blocking" | "toConfirm" | "designOutOfDate">[];
  designedFrom: Readonly<Record<string, string>>;
  productWide: ProductWideItem[];
  lines: ReadonlyMap<string, LineBlock[]>;
  dependencies: Pick<DesignDependency, "featureId" | "needs" | "answer">[];
  comments: Pick<DesignComment, "artifactId" | "status">[];
  artifacts: Pick<DesignArtifact, "id" | "features">[];
  /** Oldest first. */
  builds: ProjectBuild[];
}

export interface BuildOffer {
  /** The version this build becomes: "v1". */
  version: string;
  firstBuild: boolean;
  /** A build running now: the picker does not open while one runs. */
  running: ProjectBuild | null;
  /** New product-wide items first, then the features in spec order. */
  rows: PickRow[];
  /** Product-wide items for every feature, built before any feature: "P1". */
  foundation: string[];
  /** The version the foundation was built in; null before the first build. */
  foundationBuiltIn: string | null;
  /** Design comments not resolved, over the whole design. */
  unresolvedComments: number;
}

function plural(n: number, word: string): string {
  return `${n} ${word}${n === 1 ? "" : "s"}`;
}

function applies(item: ProductWideItem, featureId: string): boolean {
  return item.appliesTo === "all" || item.appliesTo.includes(featureId);
}

/** The words of a product-wide item, from the live product-wide.md. */
function productWideText(lines: ReadonlyMap<string, LineBlock[]>, id: string): string {
  for (const line of lines.get(PRODUCT_WIDE_PATH) ?? []) {
    const parts = parseLine(line.text, line.emphasis);
    if (parts.lead?.id === id) return parts.body;
  }
  return "";
}

/**
 * Why a feature cannot be picked, what is outside the spec first; empty when
 * it can. `near`: designed, or only design away from it, or held from
 * outside; the rest (not interviewed, held by a question) is not near a build.
 */
function heldBecause(input: PickerInput, f: PickerInput["features"][number]): { reasons: string[]; near: boolean } {
  const reasons: string[] = [];
  const dependency = input.dependencies.find((d) => d.featureId === f.id && d.answer === null);
  if (dependency) reasons.push(`waiting on ${dependency.needs}`);
  const blocked = f.blocking.length > 0;
  const early = blocked || f.stage === "Not interviewed" || f.stage === "Interviewing";
  if (blocked) reasons.push("blocked by a question");
  else if (f.stage === "Not interviewed") reasons.push("not interviewed yet");
  else if (f.stage === "Interviewing") reasons.push("being interviewed");
  else if (input.designedFrom[f.id] === undefined) reasons.push("not designed yet");
  else if (f.designOutOfDate) reasons.push("design out of date");
  return { reasons, near: dependency !== undefined || !early };
}

export function buildOffer(input: PickerInput): BuildOffer {
  const { features, productWide } = input;
  // A version whose build failed built nothing: its features are offered again
  // (rebuilding the same spec reuses its version), and it takes no number.
  const builds = input.builds.filter((b) => b.status !== "failed");
  const firstBuild = builds.length === 0;
  // A repair build (v1.1) is a point release of the version it fixes, not a version of its own.
  const version = `v${builds.filter((b) => !b.fixes).length + 1}`;
  const builtIds = new Set(builds.flatMap((b) => b.features.map((f) => f.id)));
  const builtItems = new Set(builds.flatMap((b) => b.productWide));
  // New since the last build, and reaching something already built: what
  // reaches only unbuilt features is built with them, needing no row.
  const newItems = firstBuild
    ? []
    : productWide.filter((p) => !builtItems.has(p.id) && features.some((f) => builtIds.has(f.id) && applies(p, f.id)));

  const unresolved = input.comments.filter((c) => c.status !== "resolved");
  const commentsOn = (featureId: string) =>
    unresolved.filter((c) => input.artifacts.some((a) => a.id === c.artifactId && a.features.includes(featureId))).length;

  const featureRows: FeaturePickRow[] = features.map((f) => {
    const held = heldBecause(input, f);
    if (held.reasons.length > 0) {
      const state = held.near ? "disabled" : "fixed";
      return { kind: "feature", id: f.id, name: f.name, state, detail: held.reasons, warn: held.near };
    }
    const built = lastBuildOf(builds, f.id);
    let change = "new";
    if (built) {
      const words = changeWords(lineChanges(built.lines, builtLines(input.lines.get(f.path) ?? [])));
      const reached = newItems.filter((p) => applies(p, f.id)).map((p) => p.id);
      const parts = [words, reached.length ? `affected by ${reached.join(", ")}` : ""].filter(Boolean);
      if (parts.length === 0) {
        return { kind: "feature", id: f.id, name: f.name, state: "fixed", detail: [`built in ${built.version}, unchanged`], warn: false };
      }
      change = `changed since ${built.version}: ${parts.join("; ")}`;
    }
    const comments = commentsOn(f.id);
    const detail = [
      change,
      f.toConfirm ? `${f.toConfirm} to confirm, builds as assumed` : "",
      comments ? `${plural(comments, "comment")} not resolved` : "",
    ].filter(Boolean);
    return { kind: "feature", id: f.id, name: f.name, state: "offered", detail, warn: f.toConfirm > 0 || comments > 0 };
  });

  const itemRows: ProductWidePickRow[] = newItems.map((p) => {
    const reach = featureRows.filter((r) => applies(p, r.id));
    const pullsIn = reach.filter((r) => r.state === "offered" || builtIds.has(r.id)).map((r) => r.id);
    // A built feature it reaches that cannot be rebuilt would be left without it.
    const stuck = reach.filter((r) => builtIds.has(r.id) && r.state !== "offered");
    const names = pullsIn.map((id) => featureRows.find((r) => r.id === id)!.name);
    return {
      kind: "product-wide",
      id: p.id,
      text: productWideText(input.lines, p.id),
      state: stuck.length ? "disabled" : "offered",
      pullsIn,
      detail: [`pulls in: ${names.join(", ")}`, ...stuck.map((r) => `${r.name}: ${r.detail[0] ?? "held"}`)],
      warn: stuck.length > 0,
    };
  });

  const foundation = productWide.filter((p) => p.appliesTo === "all" && !newItems.includes(p)).map((p) => p.id);
  return {
    version,
    firstBuild,
    running: input.builds.find((b) => b.status === "building") ?? null,
    rows: [...itemRows, ...featureRows],
    foundation,
    foundationBuiltIn: builds.find((b) => foundation.some((id) => b.productWide.includes(id)))?.version ?? null,
    unresolvedComments: unresolved.length,
  };
}

/** The rows that can be picked now. */
export function offeredRows(offer: BuildOffer): PickRow[] {
  return offer.rows.filter((r) => r.state === "offered");
}

/** What the picker opens with: every pickable feature. A product-wide row is the user's to tick. */
export function defaultSelection(offer: BuildOffer): BuildSelection {
  return { features: offeredRows(offer).flatMap((r) => (r.kind === "feature" ? [r.id] : [])), productWide: [] };
}

function toggled(ids: string[], id: string, on: boolean): string[] {
  return on ? (ids.includes(id) ? ids : [...ids, id]) : ids.filter((x) => x !== id);
}

/** Tick or untick a row. Ticking a product-wide row ticks every feature it pulls in. */
export function togglePick(offer: BuildOffer, selection: BuildSelection, id: string, on: boolean): BuildSelection {
  const row = offer.rows.find((r) => r.id === id);
  if (!row || row.state !== "offered") return selection;
  if (row.kind === "feature") return { ...selection, features: toggled(selection.features, id, on) };
  const features = on ? row.pullsIn.reduce((ids, f) => toggled(ids, f, true), selection.features) : selection.features;
  return { features, productWide: toggled(selection.productWide, id, on) };
}

/** A ticked product-wide item with a feature it pulls in unticked would apply to part of the product only. */
export function pickWarnings(offer: BuildOffer, selection: BuildSelection): string[] {
  return offer.rows.flatMap((r) =>
    r.kind === "product-wide" && selection.productWide.includes(r.id) && r.pullsIn.some((f) => !selection.features.includes(f))
      ? [`${r.id} would apply to only part of the product.`]
      : [],
  );
}

/** A build needs at least one feature. */
export function canStart(selection: BuildSelection): boolean {
  return selection.features.length > 0;
}

/** The picker's closing summary: what else the build runs. */
export function pickerSummary(offer: BuildOffer): { label: string; text: string }[] {
  const when = offer.firstBuild ? `built first, in ${offer.version}` : `built in ${offer.foundationBuiltIn ?? "an earlier version"}`;
  return [
    { label: "Foundation", text: offer.foundation.length ? `${offer.foundation.join(" · ")}, ${when}` : "none" },
    {
      label: "Comments",
      text: offer.unresolvedComments
        ? `${offer.unresolvedComments} not resolved (this doesn't block the build)`
        : "all resolved",
    },
    {
      label: "Validation",
      text: offer.firstBuild
        ? "every scenario of the picked features, tagged by story"
        : "re-checks everything built so far",
    },
  ];
}

/** What a selection builds, by name, in picker order: "P5, Submit expenses, Approvals". */
export function selectionNames(offer: BuildOffer, selection: BuildSelection): string[] {
  return offer.rows.flatMap((r) =>
    r.kind === "product-wide"
      ? selection.productWide.includes(r.id) ? [r.id] : []
      : selection.features.includes(r.id) ? [r.name] : [],
  );
}

/** Next up's build item while designed features wait to build: "Build v1 · Submit expenses, Approvals". */
export function nextUpBuild(offer: BuildOffer | null): NextUpItem | null {
  if (!offer || offer.running) return null;
  const waiting = offer.rows.filter((r): r is FeaturePickRow => r.kind === "feature" && r.state === "offered");
  if (waiting.length === 0) return null;
  return {
    kind: "build",
    label: `Build ${offer.version}`,
    why: waiting.map((r) => r.name).join(", "),
    urgent: false,
    target: { card: "builds" },
  };
}
