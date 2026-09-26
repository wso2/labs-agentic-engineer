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

# Actors and Resources

## Actors

**People**

| Actor (Role) | Description | Roles or Permissions |
| :---- | :---- | :---- |
| Org Admin (`ae-admin`) | Sets up the org: GitHub token, AI keys, skills, external resources. Can do all Developer work. | All 14 permissions (matrix below). |
| Org Developer (`ae-developer`) | Writes specs, chats with the design agent, builds and deploys. | 5 permissions (matrix below). |

**Malicious actors**

| Actor | Description | What they can reach |
| :---- | :---- | :---- |
| Malicious external actor | Anyone on the internet with no account. | Public addresses: the API, the org dataplane gateway, the webhook address. Public repositories. |
| Malicious org member | A Developer who tries to act above their role, or a member of another org. | Their own org, only with their own role's permissions. Another org: nothing, because the org comes from the login token. |
| Compromised agent | A design or coding agent whose model follows injected instructions from a spec, repository file, issue or web page. | What its container and its tools allow (TB-5, TB-7). |
| Compromised org Admin | An attacker using an Admin's account, or an Admin acting in bad faith. | Holds `ae-admin`, so it can change the GitHub token, AI keys and skills, and create or delete projects, in its own org only. It cannot reach other orgs (PW-1) or read saved secrets back (TB-9). **Planned:** record who changes secrets (H-6). |
| Malicious or compromised WSO2 operator | A WSO2 Cloud operator who misuses standing or break-glass access. | Clusters, databases and the secret store. **Inherited:** WSO2 Cloud operations controls cover this access (see Out of scope). |

**Systems Agentic Engineer runs**

| Actor | What it holds |
| :---- | :---- |
| [Agentic Engineer API](01-introduction-and-architecture.md#c-api) | The key that signs its short tokens. It cannot read secret values back. |
| [Design agent](01-introduction-and-architecture.md#c-design-agent) | Only the Default AI key. |
| [Live editing](01-introduction-and-architecture.md#c-live-editing) | No secrets. |
| [Studio tools](01-introduction-and-architecture.md#c-studio-tools) | GitHub token, webhook HMAC (the key that signs and checks webhooks), machine login. |
| [Coding agent](01-introduction-and-architecture.md#c-coding-agent) | Only the org's AI keys. |
| [Coding tools](01-introduction-and-architecture.md#c-coding-tools) | GitHub token and machine login, for this run's repository only. |
| Machine login (publisher client): one per org, used only from the dataplane to the API | Internal API routes only. No user permissions. |

## Entitlement matrix

Permissions come from the user's role. The API checks them on every call and refuses anything not listed.

| Permission | Lets you | `ae-admin` | `ae-developer` |
| :---- | :---- | :----: | :----: |
| `ae:requirement-view` | See projects and their status | Yes | Yes |
| `ae:requirement-update` | Create, change and delete projects | Yes | No |
| `ae:design-view` | Read specs, join a Room | Yes | Yes |
| `ae:design` | Edit specs, chat with the design agent | Yes | Yes |
| `ae:build-view` | See builds, deploys and test results | Yes | Yes |
| `ae:build` | Start builds and deploys, enter dependency secrets, see or rotate test-user passwords (see H-2) | Yes | Yes |
| `ae:github-config` | Connect or change the GitHub token | Yes | No |
| `ae:model-config` | Set the AI keys, coding agent runtime and model | Yes | No |
| `ae:skill-view` / `ae:skill-config` | See / manage org skills (instruction files the org writes for its agents) | Yes | No |
| `ae:resource-view` / `ae:resource-config` | See / manage external resources and the org catalog | Yes | No |
| `ae:usage-view` | See usage and cost | Yes | No |
| `ae:observability-view` | See incident reports and alerts | Yes | No |

- The machine login has none of these permissions.
- A member of another org has none of them in this org.
- Each change to the GitHub token or an AI key writes a log line naming the section changed, never the value.

## Resources

These are the resources Agentic Engineer controls, besides the systems above.

| Asset | Description (usage, purpose, authentication, authorizations, and security) |
| :---- | :---- |
| Console | Web app served over HTTPS by its own web server, which passes API calls to the API. Sign-in at the Platform IdP (identity provider). See AE-01. |
| API signing key | Signs the short tokens the dataplane trusts. [C-High]. See AE-03. |
| Org secrets | GitHub token, AI keys, webhook HMAC, machine login secret. Only in the secret store, delivered to the containers that need them. [C-High]. See AE-02. |
| Postgres | Org records, projects, conversations, runs. No org secret values. **Planned:** test-user passwords move to the secret store (H-2). |
| Temporal | Engine that runs background workflows for runs, builds and deploys. |
| Project `ae-system` | The org's OpenChoreo project that holds the design studio. See AE-03. |

## Dependencies

These are resources we do not control.

| Dependency | Description (usage, purpose, authentication, authorizations, and security) |
| :---- | :---- |
| Platform IdP | WSO2 Cloud sign-in. Signs user and machine login tokens. |
| Environment Thunder | Per org and environment sign-in. Future issuer of tokens made by token exchange, which trades a Platform IdP token for a new one there (GAP-2). |
| Secret store (secret manager API, called SM API; vault; secret sync) | Stores org secrets write-only and syncs them into the dataplane. Our boundary ends at the write. |
| OpenChoreo | Runs projects, the design studio, coding runs, builds and deploys. |
| GitHub | Repositories, issues, pull requests, webhooks. Reached with the org's GitHub token. A GitHub App may come later. |
| Anthropic | AI models. Receives specs, code and prompts. |
| WSO2 Cloud infrastructure | Clusters, networks, gateways, databases, images, operations. |
