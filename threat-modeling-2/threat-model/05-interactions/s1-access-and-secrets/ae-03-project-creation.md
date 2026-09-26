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

## AE-03: Project creation: the control plane drives the org dataplane

**Trust boundary:** Trust → Trust

**Description**

An org Admin creates a project in the console. The API creates the project in [OpenChoreo](../../01-introduction-and-architecture.md#c-openchoreo); the org's [design studio](../../01-introduction-and-architecture.md#c-design-studio) is kept up to date on each console load. The API then asks the [studio tools](../../01-introduction-and-architecture.md#c-studio-tools) container to create the GitHub repository and copy the org's skills (instruction files the org writes for its agents) into it. The control plane has no private path into the dataplane, so this call goes through the org gateway with a short token that the API signs.

**Assets Involved**

| Initiator | Intermediate | Target |
| :---- | :---- | :---- |
| Org Admin's browser | Agentic Engineer API; org gateway | OpenChoreo; studio tools; GitHub |

**Data Flow Diagram**

![D-AE-03: Project creation: the control plane drives the org dataplane](../../diagrams/d-ae-03-project.png)

**Steps**

1. The Admin creates a project in the console (see AE-01 for sign-in).
2. The API creates the project in OpenChoreo with the user's token.
3. The API keeps the org's design studio current. It checks the studio on each console load, and if its version is old, the API updates the same studio in place, passing secret names only. It never creates a second one.
4. The API signs a short token for this org and the studio tools container, and calls studio tools through the org gateway.
5. Studio tools checks the token, then creates the GitHub repository and copies the org's skills into it with the org's GitHub token. The API then starts the first design turn, where the design agent reads the first prompt and starts the spec (see AE-04).

**Payload**

- Project name and the first prompt.
- Short signed token: signed by the API, for this org and this container (audience), lives 5 minutes.
- Repository name, and the org's skill files.
- Design studio settings: image version and secret names, never values.

**Security Considerations**

| Area | Response | Comments |
| :---- | :---- | :---- |
| Data Confidentiality | High confidential [C-High] | The short token lets the control plane act in the org's dataplane. The project's spec and code are customer data. |
| Communication Medium | Network interaction [M-NT] | API to OpenChoreo, API to the org gateway, studio tools to GitHub. |
| Transport Security | TLS Encryption | The org gateway ends TLS. HTTPS to GitHub. |
| Authentication | Short token signed by the API | Studio tools checks the signature against the API's public keys, the audience, the expiry, and that the org is its own. |
| Accessibility | Publicly Accessible | The org gateway is on the internet, but every call needs a valid token. |
| Access Control and Authorization | Admin only | Creating a project needs `ae:requirement-update`. |

**Threat Assessment**

| ID | Category | Threat | Materializable | Mitigations / Comment |
| :---- | :---- | :---- | :---- | :---- |
| AE-03-1 | Spoofing | A malicious actor calls studio tools through the public org gateway, pretending to be the control plane. | No | **By design:** studio tools accepts only a short token signed by the API, and checks the signature, audience, expiry and org itself. The gateway only ends TLS. How studio tools gets the API's public keys is still to decide (O-10). |
| AE-03-2 | Tampering | A token made for one org, or for another container, is reused to act on this org's studio tools. | No | **By design:** the audience names one org and one container, studio tools checks that the org is its own, and the token lives 5 minutes. |
| AE-03-3 | Repudiation | An Admin denies creating or deleting a project. | No | **Planned:** record who creates or deletes a project (H-6). |
| AE-03-4 | Information disclosure | A project's spec and code are readable by anyone on GitHub. | No | **Planned:** project repositories become private (H-9), when Agentic Engineer moves to the GitHub App install path that OpenChoreo uses for private repositories. |
| AE-03-5 | Denial of service | A malicious actor floods the org gateway. | No | **Inherited:** the org gateway is WSO2 Cloud's. |
| AE-03-6 | Elevation of privilege | The API's signing key leaks, and a malicious actor signs tokens for any org's design studio. | No | **By design:** the key is a platform secret of the API only. Each token names one org and one container and lives 5 minutes. **Planned:** tokens come from the org's [Environment Thunder](../../01-introduction-and-architecture.md#c-environment-thunder) once WSO2 Cloud turns on token exchange, which trades a Platform IdP token for a new one there (GAP-2). |

**Product improvements flagged**

- **Planned:** project repositories become private (GitHub App install path, H-9).
- **Planned:** record who creates or deletes a project (H-6).
