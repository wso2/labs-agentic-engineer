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

import type { components } from "../../generated/aep-api";
import type { SpecFeature } from "../spec/api/specModel";
import type { ProjectCard } from "../shell/scope";

// A project has one conversation; each turn in it carries a scope, taken from
// where the user was when they sent it. A feature file open in the spec card
// scopes the turn to that feature; the design card (and the prototypes beside
// it), to the design review; anywhere else in the project, to the whole
// product. A prototype turn (Make prototype, a review's Send all) is about one
// web application, and a review's requests ride it as typed feedback.
//
// This module is the one place a scope meets the wire, as the contract's
// `scope` (TurnScope): a feature by its ID, or the design review; the whole
// product is the absence of a scope. It focuses the agent and fences nothing.
// A prototype turn sends no scope (its `/prototype` command says what it is
// about) and carries the review's batch as `prototypeFeedback`.

type TurnInputBody = components["schemas"]["TurnInputBody"];

/** A prototype review's requests, as the turn carries them. */
export type PrototypeFeedback = components["schemas"]["PrototypeFeedbackInput"];

/** What one turn is about. */
export type TurnScope =
  | { kind: "product" }
  | { kind: "feature"; featureId: string; name: string; path: string }
  | { kind: "design" }
  | { kind: "prototype"; feedback?: PrototypeFeedback };

/** A turn's request body. */
export type TurnBody = TurnInputBody;

/** The scope of a turn sent from here: the open card, and the feature open in the spec card. */
export function turnScopeFor(
  card: ProjectCard | null,
  feature: Pick<SpecFeature, "id" | "name" | "path"> | null,
): TurnScope {
  if (card === "design" || card === "prototype") return { kind: "design" };
  if (card === "spec" && feature) return featureScope(feature);
  return { kind: "product" };
}

export function featureScope(feature: Pick<SpecFeature, "id" | "name" | "path">): TurnScope {
  return { kind: "feature", featureId: feature.id, name: feature.name, path: feature.path };
}

/**
 * The request body of a turn: the user's words, scoped. `collab` makes it a
 * room turn, as the old console's spec chat is: the agent edits the shared spec.
 */
export function turnBody(instruction: string, scope: TurnScope): TurnBody {
  const body: TurnBody = { instruction, collab: true };
  if (scope.kind === "feature") body.scope = { kind: "feature", feature: scope.featureId };
  if (scope.kind === "design") body.scope = { kind: "design-review" };
  if (scope.kind === "prototype" && scope.feedback) body.prototypeFeedback = scope.feedback;
  return body;
}

/** A body's scope, read back as the server would: a feature by its ID. */
export type WireScope = { kind: "product" } | { kind: "feature"; featureId: string } | { kind: "design" };

export function scopeOfBody(body: TurnBody): WireScope {
  if (body.scope?.kind === "feature" && body.scope.feature) return { kind: "feature", featureId: body.scope.feature };
  if (body.scope?.kind === "design-review") return { kind: "design" };
  return { kind: "product" };
}
