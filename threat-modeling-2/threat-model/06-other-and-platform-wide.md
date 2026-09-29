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

# Open Decisions, Improvements and Platform-wide Risks

## Open decisions and improvements

Chapters point to these IDs.

**Still to decide**

| ID | Decision | Chapters |
| :---- | :---- | :---- |
| **O-3** | How the secret sync signs in to the secret store, and how the API writes a secret when no user is on the request. A user's own write carries the user's token. | AE-02 |
| **O-4** | How a changed secret reaches a running pod | AE-02 |
| **O-5** | How the design studio starts before the org sets its Default AI key | AE-02 |
| **O-6** | How the machine login's secret is created and rotated without the control plane reading it | AE-02 |
| **O-11** | Where dependency secrets and test-user passwords live during a coding run. The intended design is that [coding tools](01-introduction-and-architecture.md#c-coding-tools) holds both, and the AI container holds neither (H-2, H-3). Once the passwords are in the write-only store, the API cannot read them back, so how a Developer sees a test-user password is not yet decided. | AE-06, AE-08 |
| **O-14** | WSO2 Cloud ask: whether the Platform IdP offers the Agent entity type (a non-human identity) for the Room-join identity `ae-studio-<org>`, with a fixed client id. If not, it is an app in the org's organization unit (its place in the Platform IdP) with an `ae-` name prefix. Either way its token carries the org. | AE-05 |
| **O-15** | WSO2 Cloud ask: provisioning the AE-only control-plane client (the API's machine sign-in for design studio work, `APP_FACTORY_BFF_TO_AE_STUDIO`, a working name) and its secret for the API. It must stay off platform-api's impersonation list (the clients platform-api lets act for any org). | AE-03 |
| **O-16** | WSO2 Cloud ask: platform-api checks the audience of a token (who it is for). It checks neither the audience nor the issuer, so a login token leaked from the dataplane also works there, even with H-10 (architecture spec O-16). | AE-01 |

**Improvements to make**

Changes the team plans to make. Chapters mark them **Planned**. GAP-n items are different: they are WSO2 Cloud controls that are missing, and they happen when WSO2 Cloud adds them, not through a team change. Product improvement bullets in the chapters without an H-n or GAP-n are suggestions, not tracked changes.

| ID | Improvement | Chapters |
| :---- | :---- | :---- |
| **H-1** | Auto-merge merges only pull requests the coding agent opened from its own branch, and can be turned off. | AE-07, AE-08 |
| **H-2** | Test-user passwords are kept in the secret store (through the secret manager API) and are not posted in GitHub issue comments. For a validation run, coding tools holds the password, and the AI container never holds it. This needs a new architecture decision record (ADR) that replaces ADR-0022, which accepted posting these passwords in issue comments. Once the passwords are in the write-only store, the API cannot read them back, so how a Developer sees a test-user password is not yet decided. Open decision O-11. | AE-02, AE-06, AE-07, AE-08 |
| **H-3** | Dependency secrets (for example the app's database password) do not land in the coding agent's container. [Coding tools](01-introduction-and-architecture.md#c-coding-tools) holds them, as it holds the GitHub token. Open decision O-11. | AE-06 |
| **H-4** | Guardrails on the internet calls the AI agents make or ask for, such as web search, web fetch and an OpenAPI address the agent asks for: allowed sites only, and requests checked for secrets. | AE-04, AE-06 |
| **H-6** | Changes to the GitHub token or an AI key, creating or deleting a project, and starting a build, record who did it. | AE-01, AE-02, AE-03, AE-06, AE-08 |
| **H-7** | WSO2 Cloud sign-in issues the `ae-admin` and `ae-developer` roles and their `ae:*` permissions, and the console asks for them. The API and the design studio then both check the permission in the token, together. Until then, both use the org in the token only. | AE-01, AE-04, AE-05 |
| **H-8** | The console calls the API through the public gateway, not only through its own web server. | AE-01 |
| **H-9** | Project repositories are private. | AE-03, AE-06, AE-07, AE-08 |
| **H-10** | Tokens sent to the design studio carry an Agentic Engineer-only audience (who the token is for). The Platform IdP gets an Agentic Engineer resource server (RFC 8707 resource indicators, a standard way to ask for a token meant for one service), the console asks for it, and the design studio stops accepting the console's audience. Then a login token leaked from the dataplane no longer works on other console APIs. This alone does not protect platform-api, which checks neither the audience nor the issuer: a leaked token still works there until platform-api checks the audience (O-16). Needs WSO2 Cloud (architecture spec O-12). | AE-01, AE-04, AE-05 |
| **H-11** | The API's calls to the design studio use machine tokens that carry their org (org-bound machine tokens, thunderid#4037). The design studio then checks the org from the token, not from the `X-Impersonate-Org` header (the header that names the org the API acts for), so a leaked copy reaches only one org. Until then a leaked copy can do almost any GitHub operation in any org's repositories through studio tools; only the API holds it. Needs WSO2 Cloud (architecture spec O-13). | AE-03, AE-04 |
| **GAP-3** | Both agent pods run in gVisor, a stronger container sandbox, once WSO2 Cloud offers it. | AE-04, AE-06 |

## Platform-wide risks

Risks that run across all chapters.

| ID | Risk | Why it matters | Control or plan | Materializable |
| :---- | :---- | :---- | :---- | :---- |
| **PW-1** | One org reaches another org's data or workloads. | All orgs share the API, its database and its background jobs, and one AE-only control-plane client serves every org's design studio. | **Implemented:** the org always comes from the login token, and every query is limited to it. **Inherited:** the platform limits each platform call to that org. **By design:** each org has its own dataplane pods, and each design studio container accepts a token only for its own org. The AE-only machine token reaches only studio tools, never an AI container. **Planned:** machine tokens that carry their org (H-11). | No |
| **PW-2** | A changed image or skill changes what the agents do. | The agents run with a shell, near the org's secrets. | **Implemented:** images are pinned by version tag. A tag can be moved, so this is weaker than pinning by digest (see Product improvements flagged). Only an Admin can change org skills, and git keeps their history. | No |
| **PW-3** | Runaway agents spend an org's AI budget. | Each org pays for its own AI key. | **Implemented:** time and step limits on design turns, a time limit on runs, and one coding run per project at a time. Admins can see usage and cost. There is no org-wide spend cap. **By design:** the design studio runs one turn per project at a time. Design-turn usage reaches the API in batches; records not yet sent are lost if the pod stops without warning (accepted, AE-04-3). | No |
| **PW-4** | Nobody can show who did what, or data is kept too long. | Needed for incident response and privacy. | **By design:** spec commits list the people in the Room as co-authors. Conversations live in the design studio pod and are deleted after 7 days. **Planned:** record who changes secrets, who creates or deletes projects and who starts a build (H-6). | No |

**Product improvements flagged**

- Pin agent images by digest, not by version tag, so a moved tag cannot change what runs.
- Name which part of the design studio runs the 7-day conversation delete.
