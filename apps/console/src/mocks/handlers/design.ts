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

import { http, HttpResponse } from "msw";
import {
  PROVISIONAL_COMMENT_PATH,
  PROVISIONAL_COMMENTS_PATH,
  PROVISIONAL_DEPENDENCY_PATH,
  PROVISIONAL_DESIGN_PATH,
  type CommentAnchor,
  type DesignModel,
  type ElementRef,
} from "../../features/design/api/designModel";
import { pinComment, replyToComment, resolveComment } from "../../features/design/model/comments";
import { designView, liveDesign, saveDesign } from "../designState";

// PROVISIONAL — mock-only until E1/E4/E5; see features/design/api/designModel.ts
// and mocks/designState.ts. The design turns themselves are chat turns
// (fixtures/designTurns.ts); these are the user's own moves.

function detail(message: string, status: number): Response {
  return HttpResponse.json({ detail: message }, { status });
}

function isElementRef(value: unknown): value is ElementRef {
  const e = value as Record<string, unknown> | null;
  if (typeof e?.nth !== "number") return false;
  return (typeof e.attr === "string" && typeof e.value === "string") || (typeof e.tag === "string" && typeof e.words === "string");
}

function isAnchor(value: unknown): value is CommentAnchor {
  const a = value as Partial<CommentAnchor> | null;
  return (
    typeof a?.label === "string" &&
    (a.view === null || typeof a.view === "string") &&
    (a.element === null || isElementRef(a.element)) &&
    typeof a.x === "number" &&
    typeof a.y === "number"
  );
}

let pinned = 0;

export const designHandlers = [
  http.get(`*${PROVISIONAL_DESIGN_PATH}`, ({ params }) =>
    HttpResponse.json<DesignModel>(designView(String(params.projectName))),
  ),

  http.post(`*${PROVISIONAL_COMMENTS_PATH}`, async ({ params, request }) => {
    const projectName = String(params.projectName);
    const body = (await request.json()) as { artifactId?: unknown; anchor?: unknown; text?: unknown };
    const design = liveDesign(projectName);
    if (typeof body.artifactId !== "string" || !design.artifacts.some((a) => a.id === body.artifactId)) {
      return detail("That artifact is not in the design.", 404);
    }
    if (!isAnchor(body.anchor) || typeof body.text !== "string" || !body.text.trim()) {
      return detail("A comment needs a place and some words.", 422);
    }
    pinned += 1;
    const id = `c-${Date.now().toString(36)}-${pinned}`;
    const comments = pinComment(design.comments, { id, artifactId: body.artifactId, anchor: body.anchor, text: body.text });
    design.comments = comments.map((c) => ({ ...c, specSeen: design.comments.find((d) => d.id === c.id)?.specSeen ?? true }));
    saveDesign(projectName, design);
    return HttpResponse.json<DesignModel>(designView(projectName));
  }),

  http.post(`*${PROVISIONAL_COMMENT_PATH}`, async ({ params, request }) => {
    const projectName = String(params.projectName);
    const design = liveDesign(projectName);
    const id = String(params.commentId);
    let next;
    if (params.action === "resolve") {
      next = resolveComment(design.comments, id);
    } else if (params.action === "reply") {
      const body = (await request.json()) as { text?: unknown };
      next = typeof body.text === "string" ? replyToComment(design.comments, id, body.text) : null;
    } else {
      return detail("Unknown action.", 400);
    }
    if (!next) return detail("That comment can't take this now.", 409);
    design.comments = next.map((c) => ({ ...c, specSeen: design.comments.find((d) => d.id === c.id)?.specSeen ?? true }));
    saveDesign(projectName, design);
    return HttpResponse.json<DesignModel>(designView(projectName));
  }),

  http.post(`*${PROVISIONAL_DEPENDENCY_PATH}`, async ({ params, request }) => {
    const projectName = String(params.projectName);
    const design = liveDesign(projectName);
    const dependency = design.dependencies.find((d) => d.featureId === params.featureId && d.answer === null);
    const body = (await request.json()) as { answer?: unknown };
    if (!dependency) return detail("Nothing is waiting on that feature.", 404);
    if (typeof body.answer !== "string" || !body.answer.trim()) return detail("An answer is required.", 422);
    dependency.answer = body.answer.trim();
    saveDesign(projectName, design);
    return HttpResponse.json<DesignModel>(designView(projectName));
  }),
];
