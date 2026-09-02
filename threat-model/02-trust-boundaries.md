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

# Trust Boundaries

This section lists the crossings this model cares about. Seven of them get a full STRIDE chapter later (I01–I07). The others are named here only, so a reviewer can skip them or find them folded into another chapter.

Trust types in simple words:

- **Untrust → Trust:** someone or something outside starts a call into Agentic Engineer (browser, GitHub, a dataplane Job hitting the public API).
- **Trust → Trust:** two things we run talk to each other (API to agents, API to OpenChoreo).
- **Trust → Untrust:** we call out (Anthropic, GitHub.com).

**D2 — Trust boundaries**

![D2 Trust boundaries](diagrams/d2-trust-boundaries.png)

## Seven STRIDE crossings

| ID | Interaction | Trust boundary |
| :---- | :---- | :---- |
| I01 | User signs in (Thunder) and calls the Agentic Engineer API with a login token (JWT). Tenant gate binds the organization from the token. | Untrust → Trust |
| I02 | Organization saves the Anthropic API key through the API. | Untrust → Trust |
| I03 | Design agent sends the spec (and the key) to the AI provider. | Trust → Untrust |
| I04 | Browser joins a collab document over a websocket. Collab does not check the token itself; the API does. | Untrust → Trust |
| I05a | Platform starts a coding-agent Job on the customer dataplane (secrets land in that Job). | Trust → Trust |
| I05b | The Job calls back to the public Agentic Engineer API (MCP / callbacks). | Untrust → Trust |
| I06 | Organization connects GitHub with a GitHub App. Public repos only. | Untrust → Trust |
| I07 | GitHub webhook: merge, build, and deploy. HMAC at the API. Gateway login-token check is off on this path. A customer app may go live without a login check (jwtAuth). That row lives here, not in its own chapter. | Untrust → Trust |

I05 is one later chapter with two hops. I05a is dispatch. I05b is the public callback.

## Named only (no STRIDE chapter)

| ID | Interaction | Trust boundary | Why named only |
| :---- | :---- | :---- | :---- |
| N1 | Playground token and `/_dev` endpoints | Untrust → Trust | Must stay off Cloud. Local/dev only. |
| N2 | SRE / RCA agent and `aep-mcp-server` | Untrust → Trust | Not deployed in this Cloud production. |
| N3 | Test-user password reveal / publishing test passwords to GitHub | Trust → Untrust | Being removed. Not a live Cloud control. |
| N4 | Org-level GitHub PAT as the primary connect path | Untrust → Trust | Cloud is GitHub App and public repos (I06 / I07). |
| N5 | Platform deploy / login check on first serve (jwtAuth) as its own chapter | Trust → Trust | Folded into I07. |
| N6 | Thunder product internals, generic Kubernetes, OpenChoreo as the platform | Inherited | We only model Agentic Engineer’s new calls. |
| N7 | Image build (`dockerfile-builder`) as its own story | Trust → Untrust | Folded into I07. |
