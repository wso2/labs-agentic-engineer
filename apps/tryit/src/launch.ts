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

/**
 * Everything the console hands the app in its launch URL: where to sign in
 * (issuer, client id, resource, scopes) and what to reach (the component's
 * public gateway URL). All public; none of it is a credential.
 */
export interface Launch {
  project: string;
  component: string;
  issuer: string;
  clientId: string;
  resource: string;
  scopes: string[];
  endpoint: string;
}

const ROUTE = "#/agent?";
const REQUIRED = ["project", "component", "issuer", "client_id", "resource", "endpoint"] as const;
const STORAGE_KEY = "tryit:launch";

/** `#/agent?…` → Launch, or null for any other route or a missing coordinate. */
export function parseLaunch(hash: string): Launch | null {
  if (!hash.startsWith(ROUTE)) return null;
  const query = new URLSearchParams(hash.slice(ROUTE.length));
  const read = (key: string): string => query.get(key) ?? "";
  if (REQUIRED.some((key) => read(key) === "")) return null;
  // The issuer and the endpoint are fetched; anything but http(s) is not a
  // place to fetch from, whatever else the link says.
  if (!isHttpUrl(read("issuer")) || !isHttpUrl(read("endpoint"))) return null;
  return {
    project: read("project"),
    component: read("component"),
    issuer: read("issuer"),
    clientId: read("client_id"),
    resource: read("resource"),
    // The console always sends openid first; an empty list still signs in.
    scopes: (query.get("scopes") ?? "openid").split(/\s+/).filter(Boolean),
    endpoint: read("endpoint"),
  };
}

function isHttpUrl(value: string): boolean {
  try {
    const { protocol } = new URL(value);
    return protocol === "http:" || protocol === "https:";
  } catch {
    return false;
  }
}

/**
 * The identity provider redirects back to /callback with no hash, so the
 * launch is kept per tab across that round trip. sessionStorage, not
 * localStorage: two tabs testing two agents must not overwrite each other.
 */
export function saveLaunch(launch: Launch): void {
  sessionStorage.setItem(STORAGE_KEY, JSON.stringify(launch));
}

export function loadLaunch(): Launch | null {
  try {
    const raw = sessionStorage.getItem(STORAGE_KEY);
    return raw ? (JSON.parse(raw) as Launch) : null;
  } catch {
    return null;
  }
}

/**
 * May the app send a token to this endpoint? The launch URL names the endpoint
 * and anyone can write a launch URL, so the answer comes from the operator's
 * allowlist of gateway hosts: the endpoint must be http(s) and its hostname
 * must be one of them or sit under one. Checked before sign-in is offered, so
 * a refused link never mints a token at all.
 */
export function endpointAllowed(endpoint: string, gatewayHosts: readonly string[]): boolean {
  let url: URL;
  try {
    url = new URL(endpoint);
  } catch {
    return false;
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") return false;
  const host = url.hostname.toLowerCase();
  return gatewayHosts.some((allowed) => {
    const a = allowed.toLowerCase();
    return host === a || host.endsWith("." + a);
  });
}
