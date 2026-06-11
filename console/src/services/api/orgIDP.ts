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

// Client for /api/v1/organizations/{orgId}/idp-profile. Backs the
// Org Settings → IDP Integration page. Read-only in v1 (the BFF
// provisions everything from `api.security: required` on a component's
// design.md). Phase 7 adds a `kind` picker that flips a row into
// asgardeo / custom and re-issues keymanager entries.

import { env } from '../../config/env';
import { ApiError } from './rest';

const BASE = env.VITE_CORE_API_BASE_URL;

let _getAccessToken: (() => Promise<string>) | null = null;

export function setOrgIDPTokenAccessor(fn: (() => Promise<string>) | null): void {
  _getAccessToken = fn;
}

async function fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(init?.headers as Record<string, string>),
  };
  if (_getAccessToken) {
    const token = await _getAccessToken();
    if (token) headers.Authorization = `Bearer ${token}`;
  }
  const res = await fetch(`${BASE}${path}`, { ...init, headers });
  if (!res.ok) {
    const body = await res.text();
    let message = body;
    let code: string | undefined;
    try {
      const parsed = JSON.parse(body);
      if (parsed.message) message = parsed.message;
      if (parsed.error) message = parsed.error;
      if (parsed.code) code = parsed.code;
    } catch {
      /* use raw body */
    }
    const err = new ApiError(res.status, message);
    (err as ApiError & { code?: string }).code = code;
    throw err;
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export interface OrgIDPProfile {
  id?: string;
  orgId: string;
  kind?: 'platform' | 'asgardeo' | 'custom' | null;
  issuer?: string;
  jwksUrl?: string;
  publisherClientId?: string;
  publisherSecretRef?: string;
  adminCredsSecretRef?: string;
  createdAt?: string;
  updatedAt?: string;
  // "No profile yet" path — the BFF returns
  // {orgId, kind:null, message:"..."} and the page renders a
  // pre-provisioning explanatory state.
  message?: string;
}

export interface RotateSecretResponse {
  clientSecret: string;
}

export interface UpdateProfileRequest {
  kind?: 'platform' | 'asgardeo' | 'custom';
  issuer?: string;
  jwksUrl?: string;
}

export interface DiscoverIssuerResponse {
  issuer: string;
  jwksUrl: string;
}

export const orgIDPApi = {
  /** Read the org's IDP profile. Always succeeds — empty profile is
   * represented by kind=null. */
  async getProfile(orgHandle: string): Promise<OrgIDPProfile> {
    const raw = await fetchJSON<OrgIDPProfile | { data: OrgIDPProfile }>(
      `/api/v1/organizations/${encodeURIComponent(orgHandle)}/idp-profile`,
    );
    return (raw as { data?: OrgIDPProfile }).data ?? (raw as OrgIDPProfile);
  },

  /** Update kind / issuer / jwksUrl. Empty fields leave existing
   * values unchanged. Switching kind invalidates the publisher app —
   * next protected-component reconcile provisions a fresh one in the
   * new IDP. */
  async updateProfile(orgHandle: string, req: UpdateProfileRequest): Promise<OrgIDPProfile> {
    const raw = await fetchJSON<OrgIDPProfile | { data: OrgIDPProfile }>(
      `/api/v1/organizations/${encodeURIComponent(orgHandle)}/idp-profile`,
      { method: 'PUT', body: JSON.stringify(req) },
    );
    return (raw as { data?: OrgIDPProfile }).data ?? (raw as OrgIDPProfile);
  },

  /** OIDC discovery helper — given an issuer URL, returns the JWKS URL
   * from /.well-known/openid-configuration. Used by the BYO-IDP form
   * to auto-populate the JWKS URL field. */
  async discoverIssuer(issuer: string): Promise<DiscoverIssuerResponse> {
    const raw = await fetchJSON<DiscoverIssuerResponse | { data: DiscoverIssuerResponse }>(
      `/api/v1/idp/discover?issuer=${encodeURIComponent(issuer)}`,
    );
    return (raw as { data?: DiscoverIssuerResponse }).data ?? (raw as DiscoverIssuerResponse);
  },

  /** Rotate the publisher client secret. Returns the new secret — the
   * caller is responsible for surfacing it once and reminding the
   * operator to copy it (subsequent GETs only show has-secret state).
   */
  async rotateSecret(orgHandle: string): Promise<RotateSecretResponse> {
    const raw = await fetchJSON<RotateSecretResponse | { data: RotateSecretResponse }>(
      `/api/v1/organizations/${encodeURIComponent(orgHandle)}/idp-profile/rotate`,
      { method: 'POST' },
    );
    return (raw as { data?: RotateSecretResponse }).data ?? (raw as RotateSecretResponse);
  },
};
