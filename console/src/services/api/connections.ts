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
 * Typed client for the external-connection value-save endpoint. Saving a
 * connection's per-environment values provisions the OpenChoreo Resource model
 * for it in the project and completes the gating config-collection task (the BFF
 * cascade then dispatches the dependent component tasks). See
 * asdlc-service/internal/feature/connections/connections_huma.go.
 */

import { env } from '../../config/env';
import { ApiError } from './rest';

const BASE = env.VITE_CORE_API_BASE_URL;

let _getAccessToken: (() => Promise<string>) | null = null;

export function setConnectionsTokenAccessor(fn: (() => Promise<string>) | null): void {
  _getAccessToken = fn;
}

/** Per-environment key→value map; `development` is required locally. */
export type ConnectionEnvironments = Record<string, Record<string, string>>;

export interface ConnectionConfigKey {
  key: string;
  secret: boolean;
}

export interface ConnectionConsumer {
  projectId: string;
  componentName: string;
}

/** An org-registered external connection with its consuming components. */
export interface RegisteredConnection {
  name: string;
  description?: string;
  configKeys: ConnectionConfigKey[];
  consumers: ConnectionConsumer[];
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

/** List the org's registered connections with their consuming components. */
export async function listConnections(orgHandle: string): Promise<RegisteredConnection[]> {
  const res = await fetch(
    `${BASE}/api/v1/organizations/${encodeURIComponent(orgHandle)}/connections`,
    { headers: await authHeaders() },
  );
  if (!res.ok) throw await parseError(res);
  const body = (await res.json()) as { connections?: RegisteredConnection[] };
  return body.connections ?? [];
}

/**
 * Delete a registered connection. The server returns 409 (thrown as ApiError)
 * when any component still uses it.
 */
export async function deleteConnection(orgHandle: string, name: string): Promise<void> {
  const res = await fetch(
    `${BASE}/api/v1/organizations/${encodeURIComponent(orgHandle)}/connections/${encodeURIComponent(name)}`,
    { method: 'DELETE', headers: await authHeaders() },
  );
  if (!res.ok && res.status !== 204) throw await parseError(res);
}

/**
 * Save a connection's per-environment values and provision it. Values are split
 * into plain/secret by the connection's registered schema server-side — the
 * caller supplies plain text for both.
 */
export async function saveConnectionValues(
  orgHandle: string,
  projectName: string,
  connectionName: string,
  environments: ConnectionEnvironments,
): Promise<void> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  if (_getAccessToken) {
    const token = await _getAccessToken();
    if (token) headers.Authorization = `Bearer ${token}`;
  }
  const path = `/api/v1/organizations/${encodeURIComponent(orgHandle)}/projects/${encodeURIComponent(
    projectName,
  )}/connections/${encodeURIComponent(connectionName)}/values`;
  const res = await fetch(`${BASE}${path}`, {
    method: 'POST',
    headers,
    body: JSON.stringify({ environments }),
  });
  if (!res.ok) {
    const body = await res.text();
    let message = body;
    try {
      const parsed = JSON.parse(body);
      message = parsed.detail || parsed.message || parsed.error || body;
    } catch {
      /* use raw body */
    }
    throw new ApiError(res.status, message);
  }
}
