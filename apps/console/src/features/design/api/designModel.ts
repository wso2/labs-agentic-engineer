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

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { ScopeRoles } from "@aep/ui-openapi-view";
import { env } from "../../../config/env";
import { mockSpecKey } from "../../spec/api/specModel";

// The design review's shape. On the platform it is worked out from the room
// (../useDesignModel.ts); the mock serves it whole, on the paths below, which
// are not in the contract. Commenting has no platform backend yet (E5,
// parked), so its mutations are the mock's alone.
//
// What follows is the mock's original note.
//
// PROVISIONAL — MOCK-ONLY until E1/E4/E5 land the design review's contract.
//
// The design card needs the design review: the artifacts the design turn
// wrote (each tagged Business or Technical, with the features it covers and
// the data its viewer draws), the dependencies that block a feature's build,
// the comments pinned on artifacts, and whether this user has been shown how
// feedback works. Nothing in aep-api serves that yet (backend items E1, E4,
// E5). Until it does, the shape lives here, behind this one module, and only
// MSW answers it (mocks/handlers/design.ts, on a path that is not in the
// contract). When they ship, this module becomes calls on the generated
// client with contract types, and the handler and these hand-written types go.
//
// An artifact carries its viewer's source inline (`source`). Once the room is
// wired, the design files live in it beside the spec and the viewers read
// them there; `source` then becomes a path.
//
// What is NOT here, on purpose: which features wait for design and which
// designs are out of date. Those are worked out in the browser from the live
// spec (spec/model/designWork.ts), so an edit shows at once.
//
// Plain `fetch`, as in spec/api/specModel.ts: the path is not in the
// contract, and no real server answers it, so there is no token to attach.

/** Who an artifact is for: the business reader, or the engineer. */
export type ArtifactDepth = "business" | "technical";

/** One lane of a flow: an actor or a system. */
export interface FlowLane {
  name: string;
  /** "employee", "payroll". */
  role?: string;
}

/** One step of a flow, from lane to lane (the same lane for an action of its own). */
export interface FlowStep {
  from: number;
  to: number;
  text: string;
  /** The story it walks, "F2.2". */
  story?: string;
  /** Where the rule comes from, "policy p.7". */
  source?: string;
}

/** What an artifact's viewer draws. */
export type ArtifactSource =
  /** A wireframes DSL, clicked through as a prototype (@aep/ui-excalidraw-view). */
  | { kind: "prototype"; path: string; dsl: string }
  | { kind: "flow"; lanes: FlowLane[]; steps: FlowStep[] }
  | { kind: "roles"; roles: string[]; rows: { action: string; grants: boolean[] }[]; note: string }
  | {
      kind: "data";
      records: { name: string; about: string; fields: { name: string; example: string }[] }[];
      relation: string;
    }
  /** The cell architecture (@aep/ui-cell-diagram-view). */
  | { kind: "architecture"; cell: string }
  /** A component's design.json and its API contract (@aep/ui-design-view, @aep/ui-openapi-view). */
  | {
      kind: "contract";
      design: string;
      openapi: string | null;
      roles?: ScopeRoles;
      /** The organization's Registered External resources the component's dependencies reuse, by name. */
      resources?: string[];
    }
  | { kind: "security"; rows: { subject: string; rule: string }[] }
  /** A feature's acceptance file, Gherkin with rules tagged `@story-F2.3` (@aep/ui-acceptance-view). */
  | { kind: "acceptance"; path: string; content: string }
  /** A design document as the design writes it: prose around one mermaid diagram (a flow, the domain model). */
  | { kind: "document"; path: string; markdown: string };

export type ArtifactKind = ArtifactSource["kind"];

export interface DesignArtifact {
  id: string;
  title: string;
  depth: ArtifactDepth;
  /** The features it covers, by ID. */
  features: string[];
  /** The design revision that added it, and the last one that changed it (DesignModel.revision). */
  addedIn: number;
  changedIn: number;
  source: ArtifactSource;
}

/** Something outside the product a feature needs before it can be built; it blocks that feature only. */
export interface DesignDependency {
  featureId: string;
  /** "Xero". */
  needs: string;
  question: string;
  /** What it blocks, in words. */
  why: string;
  options: string[];
  /** The answer, once settled. */
  answer: string | null;
}

/**
 * How a comment finds its element again in what the artifact's viewer draws
 * (commentAnchor.ts): by an id the markup gives it (`attr`), or by its words
 * and tag; `nth` is which of the elements so named it is, in document order.
 */
export type ElementRef =
  | { attr: string; value: string; nth: number }
  | { tag: string; words: string; nth: number };

/**
 * Where a comment is pinned: the element's name in the artifact, the view it
 * was on (the prototype's screen; null elsewhere), the element itself (null:
 * the artifact as a whole), and the point on it, as fractions of its box.
 */
export interface CommentAnchor {
  label: string;
  view: string | null;
  element: ElementRef | null;
  x: number;
  y: number;
}

/**
 * open: waiting for Address comments. addressed: the agent changed the
 * design and replied; the user checks it. resolved: the user is done with it.
 * A reply takes an addressed comment back to open.
 */
export type CommentStatus = "open" | "addressed" | "resolved";

export interface DesignComment {
  id: string;
  /** The pin's number, in the order comments were made. */
  n: number;
  artifactId: string;
  anchor: CommentAnchor;
  /** What the user asked, latest first when they replied. */
  text: string;
  /** What the user said before, oldest first. */
  earlier: string[];
  status: CommentStatus;
  /** The agent's answer: what it changed. */
  reply: string | null;
  /** The spec line it changed, when the comment was really a requirement. */
  specLine: string | null;
}

/** The design turn running now, as the server knows it. */
export interface DesignRun {
  kind: "design" | "address";
  /** Features being designed; empty while comments are addressed. */
  features: string[];
}

export interface DesignModel {
  /** Bumped by every turn that changes the design; 0 before the first. */
  revision: number;
  running: DesignRun | null;
  artifacts: DesignArtifact[];
  dependencies: DesignDependency[];
  comments: DesignComment[];
  /** This user has done the feedback loop once: the teaching box folds away. */
  taught: boolean;
  /** Whether comments can be pinned, addressed and resolved here (E5: the mock only, for now). */
  commenting: boolean;
}

/** The provisional path MSW serves; `:projectName` is the project's slug. */
export const PROVISIONAL_DESIGN_PATH = "/api/v1/projects/:projectName/provisional/design";

/** Pin a comment: POST `{ artifactId, anchor, text }`, answered with the model. */
export const PROVISIONAL_COMMENTS_PATH = `${PROVISIONAL_DESIGN_PATH}/comments`;

/** Resolve an addressed comment, or reply to it (`{ text }`): POST, answered with the model. */
export const PROVISIONAL_COMMENT_PATH = `${PROVISIONAL_COMMENTS_PATH}/:commentId/:action`;

/** Settle a dependency: POST `{ answer }`, answered with the model. */
export const PROVISIONAL_DEPENDENCY_PATH = `${PROVISIONAL_DESIGN_PATH}/dependencies/:featureId/answer`;

/** The design model's query key: what a design turn invalidates. */
export function designKey(projectName: string) {
  return ["projects", projectName, "provisional-design"] as const;
}

function url(template: string, params: Record<string, string>): string {
  const path = template.replace(/:(\w+)/g, (_, name: string) => encodeURIComponent(params[name] ?? ""));
  return `${env.apiBaseUrl}${path}`;
}

async function readModel(response: Response, failure: string): Promise<DesignModel> {
  if (!response.ok) throw new Error(failure);
  return (await response.json()) as DesignModel;
}

function post(body?: unknown): RequestInit {
  return body === undefined
    ? { method: "POST" }
    : { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) };
}

/** The mock's design review, whole; never asked for on the platform. */
export function useMockDesignModel(projectName: string, enabled: boolean) {
  return useQuery({
    queryKey: designKey(projectName),
    queryFn: async () =>
      readModel(await fetch(url(PROVISIONAL_DESIGN_PATH, { projectName })), "Couldn't load the design"),
    enabled,
  });
}

/**
 * A change to the design review. The spec model carries the open-comment
 * count and the lines a comment changed, so it is read again too.
 */
function useDesignMutation<T>(projectName: string, send: (input: T) => Promise<DesignModel>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: send,
    onSuccess: (model) => {
      queryClient.setQueryData(designKey(projectName), model);
      void queryClient.invalidateQueries({ queryKey: mockSpecKey(projectName) });
    },
  });
}

export function usePinComment(projectName: string) {
  return useDesignMutation(projectName, async (input: { artifactId: string; anchor: CommentAnchor; text: string }) =>
    readModel(await fetch(url(PROVISIONAL_COMMENTS_PATH, { projectName }), post(input)), "Couldn't pin the comment"),
  );
}

export function useResolveComment(projectName: string) {
  return useDesignMutation(projectName, async (commentId: string) =>
    readModel(
      await fetch(url(PROVISIONAL_COMMENT_PATH, { projectName, commentId, action: "resolve" }), post(undefined)),
      "Couldn't resolve the comment",
    ),
  );
}

export function useReplyToComment(projectName: string) {
  return useDesignMutation(projectName, async (input: { commentId: string; text: string }) =>
    readModel(
      await fetch(
        url(PROVISIONAL_COMMENT_PATH, { projectName, commentId: input.commentId, action: "reply" }),
        post({ text: input.text }),
      ),
      "Couldn't send your reply",
    ),
  );
}

export function useSettleDependency(projectName: string) {
  return useDesignMutation(projectName, async (input: { featureId: string; answer: string }) =>
    readModel(
      await fetch(url(PROVISIONAL_DEPENDENCY_PATH, { projectName, featureId: input.featureId }), post({ answer: input.answer })),
      "Couldn't send your answer",
    ),
  );
}
