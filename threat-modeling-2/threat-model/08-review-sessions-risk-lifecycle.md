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

# Threat Model Review Sessions

Session 1: Access and secrets (AE-01 to AE-03)

* Date:
* Participants:
* Session recording:
* Notes:
* Action items:
  - [ ]

Session 2: Design (AE-04, AE-05)

* Date:
* Participants:
* Session recording:
* Notes:
* Action items:
  - [ ]

Session 3: Build (AE-06 to AE-08, platform-wide risks)

* Date:
* Participants:
* Session recording:
* Notes:
* Action items:
  - [ ]

# Risk registry entries

No row in AE-01 to AE-08 or PW-1 to PW-4 is Materializable **Yes** or **Partially**. The items below are the planned changes those rows rely on. A tracking issue is created for each after the review.

| ID | Item | Chapters | Tracking issue |
| :---- | :---- | :---- | :---- |
| H-1 | Auto-merge merges only pull requests the coding agent opened from its own branch, and can be turned off. | AE-07, AE-08 | |
| H-2 | Test-user passwords are not posted in GitHub issue comments. | AE-07, AE-08 | |
| H-3 | The coding agent's container holds only its one AI key. | AE-06 | |
| H-4 | Guardrails on the AI agents' internet calls. | AE-04, AE-06 | |
| H-6 | Changes to the GitHub token or an AI key, and creating or deleting a project, record who did it. | AE-02, AE-03 | |
| GAP-2 | Control-plane-to-dataplane and Room tokens come from the org's Environment Thunder. | AE-03, AE-04, AE-05 | |
| GAP-3 | Both agent pods run in gVisor. | AE-04, AE-06 | |

# Document lifecycle

- [ ] The threat model moved to [Security Review Documents](https://drive.google.com/drive/folders/1xKJ0HfPaufYSouC_Rma7S2z3fKUPQega)
- [ ] Threat model reviewed by the security team and leads
- [ ] Created GitHub issues for tracking threats that need to be addressed
- [ ] Risk registry entries updated with [Asela Jayatilleke](mailto:aselaj@wso2.com) (if applicable)
