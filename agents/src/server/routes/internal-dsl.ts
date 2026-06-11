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

import type express from "express";
import { dslToExcalidraw, type DslKind } from "../../skills/document-generation/excalidraw-dsl.js";

const VALID_KINDS = new Set<DslKind>(["wireframes", "domain-model"]);

// POST /v1/internal/dsl/render — cluster-internal helper used by the BFF
// after a canvas-structural tool result, to re-render the .excalidraw
// sibling from the agent-edited .dsl. No auth (mirrors the existing
// `/v1/internal/cache/invalidate` convention at server/index.ts).
export function registerInternalDslRender(app: express.Express) {
  app.post("/v1/internal/dsl/render", (req, res) => {
    const body = req.body as { kind?: unknown; dsl?: unknown };
    const { kind, dsl } = body;
    if (
      typeof kind !== "string" ||
      !VALID_KINDS.has(kind as DslKind) ||
      typeof dsl !== "string"
    ) {
      res.status(400).json({
        error: "kind must be 'wireframes' or 'domain-model'; dsl must be a string",
      });
      return;
    }
    try {
      const excalidraw = dslToExcalidraw(kind as DslKind, dsl);
      res.json({ excalidraw });
    } catch (err) {
      res.status(400).json({
        error: err instanceof Error ? err.message : String(err),
      });
    }
  });
}
