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

import { UserManager, WebStorageStateStore } from "oidc-client-ts";
import { env } from "../config/env";

// One UserManager for the app (thunder mode only): react-oidc-context
// renders around it, and non-React code (API client, collab provider)
// reads tokens from it via token.ts.
let instance: UserManager | null = null;

export function getUserManager(): UserManager {
  instance ??= new UserManager({
    // Thunder speaks standard OIDC discovery at
    // <thunderUrl>/.well-known/openid-configuration.
    authority: env.thunderUrl,
    client_id: env.thunderClientId,
    redirect_uri: `${window.location.origin}/callback`,
    post_logout_redirect_uri: window.location.origin,
    response_type: "code",
    scope: env.thunderScopes,
    // See env.ts's thunderResource comment: required for any ae:* scope in
    // thunderScopes to actually land on the token instead of being silently
    // dropped (and the token's aud silently redirected elsewhere).
    ...(env.thunderResource ? { resource: env.thunderResource } : {}),
    // sessionStorage per issue #91 decision (per-tab; Thunder's own session
    // cookie makes new-tab logins silent). Also required so PKCE state
    // survives the redirect.
    userStore: new WebStorageStateStore({ store: window.sessionStorage }),
    automaticSilentRenew: true,
    loadUserInfo: false,
  });
  return instance;
}
