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

## AE-05: People and the agent edit in a Room, and edits become commits

**Trust boundary:** Untrust → Trust

**Description**

A Room is a live editing session where people and the design agent write a project's spec together, like a shared document. It is the only place where the browser talks straight to the org dataplane, so each person joins with a short Room token that the API signs for this one Room. The agent's edits show highlighted until a person accepts or removes them. The Room's files are then saved to the project's GitHub repository, and a coding run builds from them only when a person clicks Build.

**Assets Involved**

| Initiator | Intermediate | Target |
| :---- | :---- | :---- |
| Developer's browser; design agent | Agentic Engineer API; org gateway | Live editing; studio tools; GitHub |

**Data Flow Diagram**

![D-AE-05: People and the agent edit in a Room, and edits become commits](../../diagrams/d-ae-05-room.png)

**Steps**

1. The user opens a project's spec (see AE-01 for sign-in). The browser asks the [API](../../01-introduction-and-architecture.md#c-api) for a Room token. The API checks that the user may see this project's design, then signs a token for this org, the live editing container and this Room. It lives 5 minutes.
2. The browser opens the Room over a WebSocket (a live two-way connection) through the org gateway. [Live editing](../../01-introduction-and-architecture.md#c-live-editing) checks the token itself: signature, audience, expiry, and that the org is its own.
3. During a design turn (see AE-04), the [design agent](../../01-introduction-and-architecture.md#c-design-agent) joins the same Room inside the pod with its own Room token. That token names the user who started the turn and ends with the turn. The agent's edits appear highlighted for everyone in the Room.
4. Live editing saves the Room's files to [studio tools](../../01-introduction-and-architecture.md#c-studio-tools) over a file-save socket that only these two containers can see. It saves when a turn ends, when the last person leaves, and before a build.
5. Studio tools commits the files under `specs/` to the main branch and pushes them to GitHub with the GitHub token. The people in the Room are listed as co-authors of the commit.

**Payload**

- Room token: signed by the API, for this org, live editing and this Room (audience), names the user, lives 5 minutes.
- Agent Room token: the same audience, names the user and the agent, lives until the turn ends (at most 30 minutes).
- Room messages: text edits, the names and cursors of the people in the Room, and the marks that highlight the agent's text.
- Save: files under `specs/`, up to 5 MiB each. Each file carries the version it was based on, so a save never overwrites a newer change.
- Reference uploads (documents the user adds for the agent) go through the API, not the Room: up to 10 files, 5 MiB each, allowed file types only.

**Security Considerations**

| Area | Response | Comments |
| :---- | :---- | :---- |
| Data Confidentiality | High confidential [C-High] | The spec is customer data. The Room token opens the project's Room. The push uses the GitHub token. |
| Communication Medium | Network interaction [M-NT] | Browser to the API and to the org gateway, studio tools to GitHub. The agent's join and the save are sockets inside the pod. |
| Transport Security | TLS Encryption | HTTPS and secure WebSocket from the browser. The org gateway ends TLS. HTTPS to GitHub. |
| Authentication | Room token signed by the API | Live editing checks the signature against the API's public keys, the audience, the expiry and the org. The user's login token never reaches the dataplane. |
| Accessibility | Publicly Accessible | The Room address is on the org gateway on the internet, but a Room token is needed to join. |
| Access Control and Authorization | Permission check, then one Room per token | Joining a Room needs `ae:design-view`. The token opens only one Room of one org. |

**Threat Assessment**

| ID | Category | Threat | Materializable | Mitigations / Comment |
| :---- | :---- | :---- | :---- | :---- |
| AE-05-1 | Spoofing | A member of another org, or a user with a token for a different project, joins this project's Room. | No | **By design:** each org has its own live editing container. The token names one org and one Room, and live editing checks both and the expiry. The API checks the user before it signs a token. |
| AE-05-2 | Tampering | The design agent, steered by prompt injection (see AE-04-2), writes harmful spec text, and a coding run later builds from it. | No | **By design:** agent text is shown highlighted in the editor until a person accepts or removes it. Saving to `main` starts nothing. A coding run starts only when a person clicks Build, and it uses the spec as the person sees it. |
| AE-05-3 | Repudiation | A user denies making a spec change. | No | **By design:** each commit lists the people in the Room as co-authors, taken from their checked Room tokens. Git keeps the history of every change. |
| AE-05-4 | Information disclosure | A Room token leaks, and someone else reads the spec. | No | **By design:** a Room token lives 5 minutes and opens one Room. The agent's token stays in the agent's memory and ends with the turn. **Planned:** Room tokens come from the org's [Environment Thunder](../../01-introduction-and-architecture.md#c-environment-thunder) once WSO2 Cloud turns on token exchange (GAP-2). |
| AE-05-5 | Denial of service | A member sends very large edits or opens many connections, so the Room cannot save. | No | **By design:** each org has its own live editing container, so one org cannot slow another org's Rooms. **Implemented:** a save refuses a file over 5 MiB. |
| AE-05-6 | Elevation of privilege | A malicious user finds a bug in live editing through the public WebSocket, or a steered agent tries to save files itself, to reach the GitHub token or write outside the spec. | No | **By design:** live editing holds no secrets, and the GitHub token lives only in studio tools. Only live editing and studio tools can see the file-save socket; the agent cannot. **Implemented:** a save writes only files under `specs/`. |

**Product improvements flagged**

- New files, deleted files and `design.json` changes made by the agent are also highlighted, and Build warns when some agent text is not yet accepted.
- Live editing takes each person's shown name from their Room token, limits document size and connections, and credits only the people who edited.
