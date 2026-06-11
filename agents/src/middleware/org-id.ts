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

import type { Request, Response, NextFunction, RequestHandler } from "express";

export const ORG_ID_HEADER = "x-oc-org-id";

/**
 * Reads X-Oc-Org-Id from the request and stashes it on `res.locals.orgId`.
 * Required for every /v1/agents/* route — the resolver call to git-service
 * needs the org context to decide org-key vs. platform-fallback.
 *
 * Returns 400 if the header is missing or malformed (DNS-label-like;
 * mirrors git-service's `validate.Slug` guard).
 */
const ORG_ID_PATTERN = /^[a-z0-9][a-z0-9-]{0,62}$/;

export function requireOrgId(): RequestHandler {
  return function orgId(req: Request, res: Response, next: NextFunction): void {
    const raw = req.header(ORG_ID_HEADER);
    if (!raw || raw.length === 0) {
      res.status(400).json({
        error: "missing required header X-Oc-Org-Id",
        code: "org_id_required",
      });
      return;
    }
    if (!ORG_ID_PATTERN.test(raw)) {
      res.status(400).json({
        error: `X-Oc-Org-Id "${raw}" does not match a valid OC org slug`,
        code: "org_id_invalid",
      });
      return;
    }
    (res.locals as Record<string, unknown>).orgId = raw;
    next();
  };
}

/** Convenience getter — returns the org id stored by requireOrgId. */
export function getOrgId(res: Response): string {
  const v = (res.locals as Record<string, unknown>).orgId;
  if (typeof v !== "string" || v.length === 0) {
    throw new Error("orgId not present on res.locals — requireOrgId middleware not applied?");
  }
  return v;
}
