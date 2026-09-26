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

## AE-07: GitHub webhook, then auto-merge

**Trust boundary:** Untrust → Trust

**Description**

GitHub tells Agentic Engineer when a pull request or an issue changes, with a webhook (a call GitHub makes to a public address). Agentic Engineer uses these events to keep a run in step with GitHub: it merges the agents' ready pull requests and builds the merged code (auto-merge), and it adds an issue to the run when a person labels it. Webhooks come from the internet with no login, so [studio tools](../../01-introduction-and-architecture.md#c-studio-tools) checks each one with the org's own webhook secret before it passes it on.

**Assets Involved**

| Initiator | Intermediate | Target |
| :---- | :---- | :---- |
| GitHub | Org gateway; studio tools | Agentic Engineer API; Temporal; GitHub |

**Data Flow Diagram**

![D-AE-07: GitHub webhook, then auto-merge](../../diagrams/d-ae-07-webhook-merge.png)

**Steps**

1. GitHub posts an event through the org gateway. Studio tools checks its signature with the org's webhook secret.
2. Studio tools sends the checked event to the [API](../../01-introduction-and-architecture.md#c-api) as the org's machine login.
3. The API drops repeats, stores the event and updates the run in [Temporal](../../01-introduction-and-architecture.md#c-temporal).
4. When a ready pull request resolves one of the run's open issues, the API asks studio tools to merge it (see AE-03 for the signed token).
5. Studio tools merges it with the GitHub token. The changed parts of the app are then built (see AE-08).

**Payload**

- Webhook: the event type, a delivery id, the signature and the body (titles, text, branch names).
- To the API: the same, without the signature.
- Merge request: the repository and the pull request number.

**Security Considerations**

| Area | Response | Comments |
| :---- | :---- | :---- |
| Data Confidentiality | High confidential [C-High] | Issue and pull request text is customer data. |
| Communication Medium | Network interaction [M-NT] | |
| Transport Security | TLS Encryption | The org gateway ends TLS. |
| Authentication | Webhook signature; machine login | Each org has its own webhook secret. |
| Accessibility | Publicly Accessible | The webhook address is on the org gateway. Unsigned calls are refused. |
| Access Control and Authorization | Org from the token | The API acts only on this org's repositories. |

**Threat Assessment**

| ID | Category | Threat | Materializable | Mitigations / Comment |
| :---- | :---- | :---- | :---- | :---- |
| AE-07-1 | Spoofing | A malicious actor posts fake events, or one org sends events for another org's repository. | No | **By design:** studio tools checks every signature with the org's own secret. The API takes the org from the machine login and looks up the repository only in that org. **Inherited:** only people the repository allows can add labels. |
| AE-07-2 | Tampering | An outsider opens a pull request that names a run's issue, and it is merged. | No | **Planned:** auto-merge merges only the coding agent's own pull requests, and can be turned off (H-1). **Planned:** private repositories (H-9). |
| AE-07-3 | Repudiation | Nobody can tell why a pull request was merged. | No | **Implemented:** every event is stored. GitHub keeps the merge and the pull request. |
| AE-07-4 | Information disclosure | Test-user passwords in an issue comment are copied into the stored events. | No | **Implemented:** the API removes them before storing. **Planned:** passwords are not posted in issue comments (H-2). |
| AE-07-5 | Denial of service | A malicious actor floods the webhook address or replays events. | No | **Implemented:** a repeated event is dropped. **By design:** unsigned calls are refused. **Inherited:** the org gateway is WSO2 Cloud's. |
| AE-07-6 | Elevation of privilege | A steered coding agent's pull request (see AE-06-2) is merged and deployed. | No | **By design:** a person starts each run with Build, but no person reviews the code before auto-merge. The deploy goes only to the development environment, and the validation agent tests it (see AE-08). **Planned:** auto-merge can be turned off (H-1). |

**Product improvements flagged**

- **Planned:** auto-merge checks who opened the pull request, and can be turned off (H-1).
- A different agent could review changes before merge.
- A size limit on the webhook receiver.
