// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

/**
 * Typed client for the external-dependency spec-collection endpoint. When a
 * component has an external REST dependency with `needsSpec: true` and no
 * `specPath`, the user can attach an OpenAPI spec by pasting raw YAML/JSON or
 * providing a public URL. The BFF validates, stores, and commits the spec then
 * returns the relative path and operation count.
 * See asdlc-service/internal/feature/dependencies/spec_huma.go (A4).
 */

import { env } from '../../config/env';
import { ApiError } from './rest';

const BASE = env.VITE_CORE_API_BASE_URL;

let _getAccessToken: (() => Promise<string>) | null = null;

export function setSpecsTokenAccessor(fn: (() => Promise<string>) | null): void {
  _getAccessToken = fn;
}

async function authHeaders(extra?: Record<string, string>): Promise<Record<string, string>> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json', ...extra };
  if (_getAccessToken) {
    const token = await _getAccessToken();
    if (token) headers.Authorization = `Bearer ${token}`;
  }
  return headers;
}

async function parseError(res: Response): Promise<ApiError> {
  const body = await res.text();
  let message = body;
  try {
    const parsed = JSON.parse(body);
    message = parsed.detail || parsed.message || parsed.error || body;
  } catch {
    /* raw body */
  }
  return new ApiError(res.status, message);
}

/**
 * Attach an OpenAPI spec to an external dependency. Supply exactly one of
 * `rawSpec` (pasted/uploaded YAML or JSON text) or `specUrl` (a publicly
 * reachable URL the BFF will fetch). On success returns the spec file path
 * (relative to `specs/design/`) and the number of operations parsed.
 *
 * Throws `ApiError` on 400 (invalid spec / missing fields) or 502 (BFF
 * could not fetch the URL).
 */
export async function collectSpec(
  orgHandle: string,
  projectId: string,
  component: string,
  depName: string,
  body: { rawSpec: string } | { specUrl: string },
): Promise<{ specPath: string; operationCount: number }> {
  const path = `/api/v1/organizations/${encodeURIComponent(orgHandle)}/projects/${encodeURIComponent(
    projectId,
  )}/components/${encodeURIComponent(component)}/dependencies/${encodeURIComponent(depName)}/spec`;
  const res = await fetch(`${BASE}${path}`, {
    method: 'POST',
    headers: await authHeaders(),
    body: JSON.stringify(body),
  });
  if (!res.ok) throw await parseError(res);
  return (await res.json()) as { specPath: string; operationCount: number };
}
