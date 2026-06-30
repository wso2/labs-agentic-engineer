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
 * Typed client for the platform-resource provisioning endpoints.
 *
 * - `provisionResource` — POST …/provision: authors the OC Resource model for
 *   a platform-resource dependency and marks the matching task in-flight.
 *   Async: returns as soon as the CRs are authored; readiness is polled via
 *   `getResourceStatus`.
 * - `getResourceStatus` — GET …/status: returns task status, OC binding
 *   readiness, and MASKED output names (values are NEVER surfaced).
 *
 * See asdlc-service/internal/feature/resources/provision_huma.go (A4).
 *
 * SECURITY: secret output values must never be requested, logged, or rendered.
 * The status endpoint intentionally emits name-only outputs; this client does
 * not add any mechanism to retrieve values.
 */

import { env } from '../../config/env';
import { ApiError } from './rest';

const BASE = env.VITE_CORE_API_BASE_URL;

let _getAccessToken: (() => Promise<string>) | null = null;

export function setResourcesTokenAccessor(fn: (() => Promise<string>) | null): void {
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

// -- Request / response shapes (mirrors provision_huma.go) -------------------

export interface ProvisionResourceBody {
  /** Provisioning parameters applied to all environments. May be omitted if
   *  the resource type requires no parameters. */
  params?: Record<string, string>;
  /** Environment names to bind the resource to (e.g. ["development"]). */
  environments: string[];
}

/** Masked output from the OC resource binding. Value is never present. */
export interface ResourceOutput {
  /** Output name (e.g. "DATABASE_URL"). */
  name: string;
}

export interface ResourceStatusResponse {
  /** Mirrors the resource-provisioning task status (pending, building,
   *  deployed, failed, …). */
  status: string;
  /** Whether the OC binding's native Ready condition is True. */
  ready: boolean;
  /** Masked outputs — name only; secret values are never surfaced. */
  outputs: ResourceOutput[];
}

// -- Public API --------------------------------------------------------------

/**
 * Author the OC Resource model for `depName` on `componentName` and mark
 * the matching resource-provisioning task in-flight. Returns when the CRs
 * are authored; readiness must be polled via `getResourceStatus`.
 *
 * Route: POST /api/v1/organizations/{orgHandle}/projects/{projectName}/
 *             components/{componentName}/dependencies/{depName}/provision
 *
 * Body: { params?, environments }
 * Response: 202 { status: "provisioning" }
 */
export async function provisionResource(
  orgHandle: string,
  projectId: string,
  component: string,
  depName: string,
  body: ProvisionResourceBody,
): Promise<void> {
  const path =
    `/api/v1/organizations/${encodeURIComponent(orgHandle)}` +
    `/projects/${encodeURIComponent(projectId)}` +
    `/components/${encodeURIComponent(component)}` +
    `/dependencies/${encodeURIComponent(depName)}/provision`;
  const res = await fetch(`${BASE}${path}`, {
    method: 'POST',
    headers: await authHeaders(),
    body: JSON.stringify(body),
  });
  if (!res.ok) throw await parseError(res);
  // 202 body contains { status: "provisioning" } — caller does not need it;
  // readiness is observed via getResourceStatus.
}

/**
 * Get the provisioning status and masked outputs for `depName` on
 * `componentName`.
 *
 * Route: GET /api/v1/organizations/{orgHandle}/projects/{projectName}/
 *            components/{componentName}/dependencies/{depName}/status
 *
 * Response: { status, ready, outputs: [{ name }] }
 *
 * SECURITY: outputs are name-only (values masked at the BFF; see
 * provision_huma.go). This client must not attempt to read secret values.
 */
export async function getResourceStatus(
  orgHandle: string,
  projectId: string,
  component: string,
  depName: string,
  environment?: string,
): Promise<ResourceStatusResponse> {
  const params = environment ? `?environment=${encodeURIComponent(environment)}` : '';
  const path =
    `/api/v1/organizations/${encodeURIComponent(orgHandle)}` +
    `/projects/${encodeURIComponent(projectId)}` +
    `/components/${encodeURIComponent(component)}` +
    `/dependencies/${encodeURIComponent(depName)}/status${params}`;
  const res = await fetch(`${BASE}${path}`, {
    method: 'GET',
    headers: await authHeaders(),
  });
  if (!res.ok) throw await parseError(res);
  return (await res.json()) as ResourceStatusResponse;
}
