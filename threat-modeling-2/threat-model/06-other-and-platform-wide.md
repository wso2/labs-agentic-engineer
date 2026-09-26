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
| **O-3** | How the API and the secret sync sign in to the secret store | AE-02 |
| **O-4** | How a changed secret reaches a running pod | AE-02 |
| **O-5** | How the design studio starts before the org sets an AI key | AE-02 |
| **O-6** | How the machine login's secret is created and rotated without the control plane reading it | AE-02 |
| **O-10** | How dataplane containers get the API's public signing keys | AE-03 |

**Improvements to make**

Changes the team plans to make. Chapters mark them **Planned**.

| ID | Improvement | Chapters |
| :---- | :---- | :---- |
| **H-1** | Auto-merge merges only pull requests the coding agent opened from its own branch, and can be turned off. | AE-07, AE-08 |
| **H-2** | Test-user passwords are not posted in GitHub issue comments. | AE-07, AE-08 |
| **H-3** | The coding agent's container holds only its one AI key. Other secrets stay where the agent's shell cannot read them. | AE-06 |
| **H-4** | Guardrails on the internet calls the AI agents make or ask for, such as web search, web fetch and an OpenAPI address the agent asks for: allowed sites only, and requests checked for secrets. | AE-04, AE-06 |
| **H-6** | Changes to the GitHub token or an AI key, and creating or deleting a project, record who did it. | AE-02, AE-03 |
| **GAP-2** | Tokens from the control plane to the dataplane, and Room tokens, come from the org's [Environment Thunder](01-introduction-and-architecture.md#c-environment-thunder) once WSO2 Cloud turns on token exchange (trading the user's token for a new one there). Until then the API signs short tokens, which the dataplane checks. | AE-03, AE-04, AE-05 |
| **GAP-3** | Both agent pods run in gVisor, a stronger container sandbox, once WSO2 Cloud offers it. | AE-04, AE-06 |

## Platform-wide risks

Risks that run across all chapters.

| ID | Risk | Why it matters | Control or plan | Materializable |
| :---- | :---- | :---- | :---- | :---- |
| **PW-1** | One org reaches another org's data or workloads. | All orgs share the API, its database and its background jobs. | **Implemented:** the org always comes from the login token, and every query and platform call is limited to it. **By design:** each org has its own dataplane pods. | No |
| **PW-2** | A changed image or skill changes what the agents do. | The agents run with a shell, near the org's secrets. | **Implemented:** images are pinned to a fixed version. Only an Admin can change org skills, and git keeps their history. | No |
| **PW-3** | Runaway agents spend an org's AI budget. | Each org pays for its own AI key. | **Implemented:** time and step limits on design turns, a time limit on runs, and one turn and one build per project at a time. Admins can see usage and cost. An org-wide spend cap is flagged as a product improvement. | No |
| **PW-4** | Nobody can show who did what, or data is kept too long. | Needed for incident response and privacy. | **Implemented:** builds and deploys record who started them, and conversations are deleted after 7 days. **Planned:** record who changes secrets and who creates or deletes projects (H-6). | No |
