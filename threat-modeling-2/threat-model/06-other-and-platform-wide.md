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

# Open Decisions, Improvements, Other Interactions and Platform-wide Risks

## Open decisions and improvements

Chapters point to these IDs.

**Still to decide**

| ID | Decision | Chapters |
| :---- | :---- | :---- |
| **O-3** | How the API and the secret sync sign in to the secret store | AE-02 |
| **O-4** | How a changed secret reaches a running pod | AE-02 |
| **O-5** | How the design studio starts before the org sets an AI key | AE-02 |
| **O-6** | How the machine login's secret is created and rotated without the control plane reading it | AE-02, AE-06 |
| **O-10** | How dataplane containers get the API's public signing keys | AE-03 |

**Improvements to make**

Changes the team plans to make. Chapters mark them **Planned**.

| ID | Improvement | Chapters |
| :---- | :---- | :---- |
| **H-1** | Auto-merge merges only pull requests the coding agent opened from its own branch, and can be turned off. | AE-07, AE-08 |
| **H-2** | Test-user passwords are not posted in GitHub issue comments. | AE-08 |
| **H-3** | The coding agent's container holds only its one AI key. Other secrets stay where the agent's shell cannot read them. | AE-06 |
| **H-4** | Guardrails on the internet calls the AI agents make or ask for, such as web search and web fetch: allowed sites only, and requests checked for secrets. | AE-04, AE-06 |
| **H-6** | Changes to the GitHub token or an AI key, and creating or deleting a project, record who did it. | AE-02, AE-03 |
| **GAP-2** | Tokens from the control plane to the dataplane, and Room tokens, come from the org's [Environment Thunder](01-introduction-and-architecture.md#c-environment-thunder) once WSO2 Cloud turns on token exchange (trading the user's token for a new one there). Until then the API signs short tokens, which the dataplane checks. | AE-03, AE-04, AE-05 |
| **GAP-3** | Both agent pods run in gVisor, a stronger container sandbox, once WSO2 Cloud offers it. | AE-04, AE-06 |
