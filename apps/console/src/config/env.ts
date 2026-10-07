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

import { resolveAuthMode } from "./authMode";

interface RuntimeEnv {
  VITE_API_BASE_URL?: string;
  VITE_THUNDER_URL?: string;
  VITE_THUNDER_CLIENT_ID?: string;
  VITE_THUNDER_SCOPES?: string;
  VITE_TRY_IT_URL?: string;
  BILLING_API_BASE_URL?: string;
}

declare global {
  interface Window {
    _env_?: RuntimeEnv;
  }
}

// Runtime config (window._env_, served as /env-config.js by the hosting
// layer) takes precedence over build-time env, then a dev-proxy default.
function getEnv(key: keyof RuntimeEnv): string | undefined {
  if (typeof window !== "undefined" && window._env_) {
    const runtimeValue = window._env_[key];
    if (runtimeValue !== undefined && runtimeValue !== "") {
      return runtimeValue;
    }
  }
  return import.meta.env[key];
}

export const env = {
  apiBaseUrl: getEnv("VITE_API_BASE_URL") || "/aep-api-service",
  // VITE_AUTH_MODE is a dev-only switch, so it is read from build-time env
  // only — runtime config cannot demote production to mock auth.
  authMode: resolveAuthMode(
    import.meta.env.DEV,
    import.meta.env.VITE_API_MODE,
    import.meta.env.VITE_AUTH_MODE,
  ),
  // Mock mode (VITE_API_MODE=mock, dev only) runs the API on MSW. The few
  // things only the platform has — the collab room, above all — are stood in
  // for locally when it is set.
  apiMode: import.meta.env.DEV && import.meta.env.VITE_API_MODE === "mock" ? ("mock" as const) : ("platform" as const),
  // Thunder OIDC issuer: the REST/OIDC root, not the admin SPA. The dev
  // default is the dev-thunder-setup container, as for the old console.
  thunderUrl: getEnv("VITE_THUNDER_URL") || "http://localhost:8097",
  // The console's client. The dev server's origin (http://localhost:8090) is
  // added to its redirect URIs by hand; see README.
  thunderClientId: getEnv("VITE_THUNDER_CLIENT_ID") || "aep-console-client",
  thunderScopes: getEnv("VITE_THUNDER_SCOPES") || "openid profile email",
  // The platform's test app (apps/tryit), which Try it opens an agent in; same
  // origin as aep-api's TRY_IT_CALLBACK_URL. The default is the dev cluster's.
  tryItUrl: getEnv("VITE_TRY_IT_URL") || "http://tryit.aep.localhost:8095",
  // WSO2 Cloud's billing-user-api, which sign-in activates the org's
  // subscription with (api/billing.ts). Unset outside WSO2 Cloud: no call.
  billingApiBaseUrl: getEnv("BILLING_API_BASE_URL") || "",
} as const;
