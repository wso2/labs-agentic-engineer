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

import { getUserManager } from "./userManager";

// Set when sign-out starts and never cleared: the page then leaves, to the
// IdP's logout or to a reload.
let signingOut = false;

/**
 * True once sign-out has started. The guard must not start a sign-in then:
 * the library drops the stored user before it navigates to the IdP's logout,
 * and a sign-in started in that gap races the logout and, on an IdP that
 * keeps its session, signs the user straight back in.
 */
export function isSigningOut(): boolean {
  return signingOut;
}

/**
 * RP-initiated logout when the IdP advertises end_session_endpoint. Where it
 * does not, revoke the tokens (best effort), drop the local session and
 * reload; the guard then starts a fresh sign-in. Uses the raw UserManager:
 * the react-oidc-context wrapper turns a signoutRedirect rejection into
 * auth.error, which would strand the user on the error screen.
 */
export async function signOut(): Promise<void> {
  signingOut = true;
  const userManager = getUserManager();
  try {
    await userManager.signoutRedirect();
  } catch {
    try {
      await userManager.revokeTokens();
    } catch {
      // Revocation failing must not block sign-out.
    }
    try {
      await userManager.removeUser();
    } catch {
      // The reload below still runs: it resets this module, so a session that
      // could not be dropped is at least shown as it is.
    }
    window.location.assign("/");
  }
}
