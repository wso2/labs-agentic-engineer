<!--
Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).

WSO2 LLC. licenses this file to you under the Apache License,
Version 2.0 (the "License"); you may not use this file except
in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing,
software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
KIND, either express or implied.  See the License for the
specific language governing permissions and limitations
under the License.
-->

# I06: Org connects GitHub (GitHub App, public repos only)

**Trust Boundary:** Untrust → Trust

**Description**

A person in an organization connects GitHub with the Agentic Engineer **GitHub App**. They start connect from the console with a login token (JWT). The API returns a GitHub OAuth URL. The browser goes to GitHub to authorize and install the App. GitHub sends the browser back to Agentic Engineer.

That callback is not checked with the console login token. It uses a short-lived connect-state token the API signed when connect started (about 15 minutes). The organization comes from that state, not from the URL. GitHub itself checks that the installer can administer the GitHub org. Agentic Engineer then binds only an install that person actually has.

GitHub as a company is out of scope. How Agentic Engineer uses the App is in scope. How WSO2 Cloud authorizes each created repo inside Cloud is out of scope (named only). Stolen Agentic Engineer login tokens are I01, not this chapter.

After connect, new project repos are created **public**. Spec, code, and issues on GitHub.com are world-readable. That is today’s Cloud gap until the workflow plane can clone private repos.

**Intended control:** only Cloud admin can start connect. **Today:** any valid organization login token can call this API. That gap is I01.

**Today on Cloud dev:** `GITHUB_REPO_VISIBILITY=public` on the API ConfigMap. The public webhook URL is set. The API Secret has `GITHUB_WEBHOOK_SECRET` and `OAUTH_STATE_SIGNING_KEY` (names only; values not recorded). The API pod does not have `GITHUB_APP_ID`, `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET`, `GITHUB_APP_PRIVATE_KEY_PATH`, or `BFF_PUBLIC_URL`. App-mode connect is not configured until those are mounted.

**Assets Involved**

| Initiator | Intermediate | Target |
| :---- | :---- | :---- |
| Browser (organization person) | Console (forwards the start-connect call inside the cluster). GitHub App OAuth / install (external). | Agentic Engineer API (`POST /config/git-provider/connect-sessions`, then `GET /api/v1/org/credentials/github/connect/callback`) |

**Data Flow or Sequence Diagram**

**D-I06 — GitHub App install (public repos)**

![D-I06 Org connects GitHub (GitHub App, public repos only)](../diagrams/d-i06-github-app.png)

1. The person starts GitHub App connect with a login token (same public path as I01).
2. The browser goes to GitHub to authorize and install the App.
3. GitHub sends the browser back to the API callback with the connect-state token. The API binds that App install to the organization from the state.
4. New project repos are created public (`GITHUB_REPO_VISIBILITY=public`).

**Payload**

- Login token (JWT) on start connect. Organization is not taken from the URL or the body. See I01.
- Connect-state token (short-lived, signed). Organization and actor come from this token on the callback.
- GitHub OAuth `code` or `installation_id`
- App installation binding (organization, installation id)
- New repo visibility: public

**Security Considerations**

| Area | Response | Comments |
| :---- | :---- | :---- |
| Data Confidentiality | High confidential \[C-High\] | Connect-state is a credential. The App install id is stored. Short-lived installation tokens are minted when needed. Spec and code on public GitHub are organization work \[C-Medium\] and world-readable today. |
| Communication Medium | Network interaction \[M-NT\] | Browser to console and to GitHub over HTTPS. GitHub redirects the browser to the API callback. |
| Transport Security | **TLS Encryption** | Public hosts use HTTPS. GitHub.com is HTTPS. |
| Authentication | **Start:** Bearer login token (JWT). **Callback:** connect-state token, not the console login token. | Thunder issues the login token (see I01). The callback is not behind the console login token. The signed connect-state is the check. |
| Accessibility | **Publicly Accessible** | Start connect uses the public user API (gateway jwtAuth on). The callback is a browser redirect from GitHub. |
| **Access Control and Authorization** | Intended: Cloud admin only. Today: tenant gate only. Bind is one install to one organization. | Who may start connect is I01 (no Cloud role check yet). GitHub checks the installer is a GitHub org admin. Agentic Engineer binds only an install that person has. |

**Threat Assessment**

| ID | [Category](https://docs.google.com/presentation/d/1m3vIE2nzS_jcW8CIhCMnCaFl3KnOXZXhSLC163shHWs/edit#slide=id.g2d28a95f41a_0_94) | Threat | Materializable | Mitigations / Comment |
| :---- | :---- | :---- | :---- | :---- |
| 1 | Spoofing | A stolen Agentic Engineer access token is used to start GitHub connect as that organization person. | See I01 | See I01. |
| 2 | Spoofing | A forged or unsigned connect-state token is accepted on the callback and binds an install. | No | The callback checks the signed connect-state. Missing or bad state is rejected. |
| 3 | Spoofing | An attacker binds a GitHub App install they do not administer to an Agentic Engineer organization. | No | GitHub requires the installer to be a GitHub org admin. The API intersects the person’s installs with this App and binds only a candidate they have. |
| 4 | Tampering | The callback puts another organization’s id in the URL to attach the install to that organization. | No | Organization comes from the connect-state token, not from the URL or the body. |
| 5 | Tampering | App-mode connect runs without the App identity mounted, so the intended bind cannot complete. | Yes | Intended: App id, OAuth client id/secret, private key, and public callback URL on the API. Today those env names are not on the API pod. Connect start is not configured (503) until they are mounted. |
| 6 | Repudiation | A person denies connecting GitHub and we cannot show who did it. | Yes | Organization config changes have section-level audit logs. We do not yet have a full log of every connect callback and every bind. |
| 7 | Information Disclosure | Spec, code, and issues in new project repos are world-readable on GitHub.com. | Yes | Live `GITHUB_REPO_VISIBILITY=public`. Intended private clone from the workflow plane is not in place. Anyone on the internet can read that GitHub repo. GitHub-the-company is out of scope; this is how Agentic Engineer creates repos. |
| 8 | Information Disclosure | Connect lists another organization’s GitHub installs. | No | There is no platform-wide “list unbound installs” API. Bind is from the signed state plus that user’s installs. |
| 9 | Denial of Service | A flood of start-connect or callback calls with a valid organization token. | Yes | Agentic Engineer has no application rate limit on this API (see I01). Volumetric DDoS at the Cloud edge is inherited Cloud infrastructure (out of scope here). |
| 10 | Elevation of Privilege | A viewer (or any organization login token) connects GitHub for the organization. | Yes | Intended: Cloud admin. Today the user API does not check roles. See I01. |
