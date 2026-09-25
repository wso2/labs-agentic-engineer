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
 * Everything the platform's test app needs to sign in to a project and reach
 * one of its components. All of it is public: the issuer and client id are
 * what any browser signing in would see, the resource is the token audience,
 * and the endpoint is the component's own gateway URL.
 */
export interface TryItLaunch {
  project: string;
  component: string;
  issuer: string;
  clientId: string;
  resource: string;
  scopes: readonly string[];
  endpoint: string;
}

/**
 * The launch URL the console opens in a new tab: `{base}/#/agent?…`. Hash
 * routing, so the static app serves one document and the redirect back from
 * the identity provider (to `/callback`) never has to know the route.
 */
export function tryItAppUrl(base: string, launch: TryItLaunch): string {
  const query = new URLSearchParams({
    project: launch.project,
    component: launch.component,
    issuer: launch.issuer,
    client_id: launch.clientId,
    resource: launch.resource,
    scopes: launch.scopes.join(" "),
    endpoint: launch.endpoint,
  });
  return `${base.replace(/\/+$/, "")}/#/agent?${query.toString()}`;
}
