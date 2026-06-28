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
 * Typed client for the marketplace P3.5 cross-project access-request flow. A
 * consumer whose design references an org service published only at `project`
 * visibility (dep status `unresolved`, reason `unpublished`) can request that
 * the provider publish it org-wide. The request creates a publish ComponentTask
 * + GitHub issue on the provider's repo; an `AccessRequest` row tracks it.
 * See docs/design/marketplace/access-requests.md (§2, §6) and
 * asdlc-service/internal/feature/accessrequests.
 */

import { env } from '../../config/env';
import { ApiError } from './rest';

const BASE = env.VITE_CORE_API_BASE_URL;

let _getAccessToken: (() => Promise<string>) | null = null;

export function setAccessRequestsTokenAccessor(fn: (() => Promise<string>) | null): void {
  _getAccessToken = fn;
}

/** Lifecycle of a cross-project access request. */
export type AccessRequestStatus = 'requested' | 'in_progress' | 'granted' | 'rejected';

/** A consumer's request for a provider to publish an org service cross-project. */
export interface AccessRequest {
  id: string;
  orgID: string;
  consumerProjectID: string;
  consumerComponentName: string;
  orgServiceName: string;
  providerProjectID: string;
  providerComponentName: string;
  providerTaskID: string;
  providerIssueNumber: number;
  providerIssueURL: string;
  status: AccessRequestStatus;
  createdAt: string;
  updatedAt: string;
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
 * Request cross-project access to an org service. Creates the provider publish
 * task + GitHub issue (idempotent server-side) and returns the AccessRequest
 * row tracking it.
 */
export async function requestAccess(
  orgHandle: string,
  projectName: string,
  componentName: string,
  orgServiceName: string,
): Promise<AccessRequest> {
  const path = `/api/v1/organizations/${encodeURIComponent(orgHandle)}/projects/${encodeURIComponent(
    projectName,
  )}/components/${encodeURIComponent(componentName)}/access-requests`;
  const res = await fetch(`${BASE}${path}`, {
    method: 'POST',
    headers: await authHeaders(),
    body: JSON.stringify({ orgServiceName }),
  });
  if (!res.ok) throw await parseError(res);
  return (await res.json()) as AccessRequest;
}

/** List the project's cross-project access requests (consumer side). */
export async function listAccessRequests(
  orgHandle: string,
  projectName: string,
): Promise<AccessRequest[]> {
  const path = `/api/v1/organizations/${encodeURIComponent(orgHandle)}/projects/${encodeURIComponent(
    projectName,
  )}/access-requests`;
  const res = await fetch(`${BASE}${path}`, { headers: await authHeaders() });
  if (!res.ok) throw await parseError(res);
  const body = (await res.json()) as { accessRequests?: AccessRequest[] } | AccessRequest[];
  // Tolerate either a bare array or a wrapped { accessRequests } envelope.
  return Array.isArray(body) ? body : body.accessRequests ?? [];
}
