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
 * How sign-in and sign-out BEHAVE in the app under test — data for the
 * harness's agents, so the planner writes expectations the app can meet and
 * the walker reads what it sees correctly.
 *
 * `wire` runs the generated SPA against the mock session the react-webapp /
 * thunder-authentication skills ship (`skills/thunder-authentication/assets/
 * app/mock/authz/session.ts`): there is no identity provider, so there is no
 * sign-in page. If that asset's `signIn`/`signOut` change, this text changes
 * with them.
 */
export const WIRED_AUTH_SEMANTICS = `WIRED-MODE AUTH — how identity behaves in the app under test (a mock session stands in for the identity provider):
- \`?role=<Role>\` signs in as that role, and the choice persists for the browser tab, so the app's own links keep it. \`?role=\` (empty) is signed in holding no role: the app shows its no-access page.
- There is NO sign-in form, button or page anywhere, and none can appear. Loading with \`?auth=out\` (nobody signed in) makes the app's own sign-in guard run, and the APP ITSELF then simulates the identity provider's round trip — it drops \`auth=out\` from the address and reloads; the tester does nothing. It comes straight back SIGNED IN, as whatever role the browser tab last held (or none) — which depends on what the walk did before, so a checklist never predicts it. Observable: the address loses \`auth=out\` and the app reloads into a session, with no blank page or crash.
- The app's sign-out control forgets the tab's role and goes to "/". The app then shows its no-access state (signed in, holding no role) — never the previous role's screens or data, and never a sign-in form.
So a "signed out" item's step is only to load the app signed out, and its expectation is that the guard brings the app back through sign-in into a session without a crash; a sign-out expectation is that the previous role's screens and data are gone and the no-access state shows.`;
