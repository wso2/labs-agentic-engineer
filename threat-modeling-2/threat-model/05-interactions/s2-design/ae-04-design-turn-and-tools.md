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

## AE-04: A design agent turn and its tools

**Trust boundary:** Trust → Untrust

**Description**

The [design agent](../../01-introduction-and-architecture.md#c-design-agent) is the AI that helps people write a project's spec: they describe what they want in the console, and it writes and updates the requirements and design. To do this it reads text that nobody has checked, such as uploaded documents, the org's skills (instruction files the org writes for its agents), web search results and API descriptions from the internet. Some of that text may try to give the agent orders, which is called prompt injection. So the design keeps what the agent can reach small: it holds only one AI key, and for platform facts it must ask the [studio tools](../../01-introduction-and-architecture.md#c-studio-tools) container beside it, which keeps the secrets and answers only 11 read-only tools.

**Assets Involved**

| Initiator | Intermediate | Target |
| :---- | :---- | :---- |
| Developer's browser | Agentic Engineer API; org gateway | Design agent; studio tools; Anthropic; GitHub; public web sites |

**Data Flow Diagram**

![D-AE-04: A design agent turn and its tools](../../diagrams/d-ae-04-design-turn.png)

**Steps**

1. The Developer sends a message (see AE-01 for sign-in). The [API](../../01-introduction-and-architecture.md#c-api) checks that the user may change the design, and streams the agent's reply back to the browser as it comes.
2. The API signs a short token for this org and the design agent, and starts the turn through the org gateway.
3. The design agent reads the spec from a copy that studio tools wrote for it, and calls the AI model at Anthropic with the Default key. The model may also run a web search, up to four times per turn.
4. For platform facts, the agent calls studio tools on its own socket inside the pod. It uses MCP (Model Context Protocol, a standard way for an AI to call tools). The socket answers only 11 read-only tools.
5. Studio tools answers the call. It reads repository files on GitHub itself, with the GitHub token. For the other tools it calls the API as the org's machine login (publisher client), and the API runs the tool for this org: for example, it lists the org's services or fetches an API description (OpenAPI document) from a public web address. In a Room, the agent's edits go into the live session (see AE-05).

**Payload**

- Message: up to 64 KiB of text, plus up to 10 attachments (PDF, images, text), 5 MiB each.
- Short signed token: signed by the API, for this org and this container (audience), lives 5 minutes.
- Model call: the prompt, the spec text, tool results and the Default key. The model is one of two allowed Claude models.
- Tool call: a tool name from the list of 11 and its inputs, for example a search term, a file path or a web address.
- Reply: the agent's text and proposed file changes, streamed as events. The conversation is kept for 7 days.

**Security Considerations**

| Area | Response | Comments |
| :---- | :---- | :---- |
| Data Confidentiality | High confidential [C-High] | Specs, repository files and uploads are customer data. The model call carries the Default key. |
| Communication Medium | Network interaction [M-NT] | API to the org gateway, the agent to Anthropic, studio tools to GitHub and the API. The agent to studio tools is a socket inside the pod. |
| Transport Security | TLS Encryption | The org gateway ends TLS. HTTPS to Anthropic, GitHub, the API and web sites. |
| Authentication | Short token signed by the API; machine login | The design agent checks the API's token (signature, audience, expiry, org). Studio tools signs in to the API as the org's machine login. |
| Accessibility | Publicly Accessible | The org gateway is on the internet, but every call needs a valid token. The agent's socket is reachable only inside the pod. |
| Access Control and Authorization | Permission check, then a fixed tool list | Starting a turn needs `ae:design`. The agent can call only the 11 read-only tools. The org comes from the machine login, never from the agent. |

**Threat Assessment**

| ID | Category | Threat | Materializable | Mitigations / Comment |
| :---- | :---- | :---- | :---- | :---- |
| AE-04-1 | Spoofing | A malicious actor calls the design agent through the public org gateway, pretending to be the control plane, to run turns on the org's AI key. | No | **By design:** the agent accepts only a short token signed by the API for this org and this container, and checks it itself (see AE-03-1). |
| AE-04-2 | Tampering | Text in a spec, an upload, a skill, a search result or an OpenAPI document tells the model to write harmful spec text. | No | **By design:** there is no filter for prompt injection. Instead, the agent holds only the Default key, its file tools stay inside its copy of the spec, and its tools only read. What it writes shows up in the Room, where people see it before a build (see AE-05). |
| AE-04-3 | Repudiation | A tool call cannot be tied to the user who started the turn. | No | **Implemented:** each turn is stored with the user from the verified login token. **By design:** tool calls reach the API as the org's machine login, not the user, and the agent can call a tool between turns. The tools only read, and only this org's data. |
| AE-04-4 | Information disclosure | A steered agent sends spec or repository text out, inside a web search query or an OpenAPI web address. | No | **By design:** the agent has no web fetch tool, holds no GitHub token, and the dataplane blocks private addresses (TB-8). **Implemented:** the API fetches OpenAPI documents over HTTPS from public addresses only, with a size limit. **Planned:** guardrails on the agents' internet calls (H-4). |
| AE-04-5 | Denial of service | A user, or a steered agent in a loop, runs long turns and spends the org's AI budget. | No | **Implemented:** a turn stops after 30 minutes or 60 model steps, a project runs one turn at a time, and messages and attachments have size limits. **Inherited:** flood protection at the gateways is WSO2 Cloud's. |
| AE-04-6 | Elevation of privilege | A steered agent tries to read the GitHub token or the machine login, save files to git, or call a tool that changes something. | No | **By design:** the secrets live only in studio tools. The agent's socket answers only the 11 read-only tools, and the agent cannot see the file-save socket. The containers do not share processes, and the pod has no Kubernetes token (TB-5). |
