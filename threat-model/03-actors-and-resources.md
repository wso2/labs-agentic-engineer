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

Role names below are **placeholders**. Exact names will match the basic RBAC design when it ships.

| Actor (role) | Description | Roles or permissions |
| :---- | :---- | :---- |
| Org member (placeholder) | A person in an organization. Signs in with Thunder. Uses the console. Gives requirements. Reviews design. Watches builds. | Intended: organization work the RBAC design allows. Today: any valid organization login token (JWT) can call organization APIs. |
| Org admin (placeholder) | A person who should manage organization settings (keys, GitHub connect). | Intended: those extra actions when RBAC ships. Today: same as org member — the API does not check roles. |
| Generated-app end user | Signs in to **customer** apps, not the Agentic Engineer console. | Platform IdP (Thunder) for those apps. Not an Agentic Engineer console role. |
| Design agent | Writes spec and design turns. The API is the caller. Uses the organization’s Anthropic key. | Machine. No human role. |
| Coding agent | Short-lived Job on the dataplane. Implements or validates. Calls the public API with the organization’s publisher login. Talks to GitHub and Anthropic. | Machine. Publisher client for that organization. |
| GitHub | Holds spec and code. Sends webhooks. Hosts the GitHub App install. | External. |
| Thunder | Issues user login tokens and machine tokens. Not modeled as a product here. | Inherited IdP. |
| OpenChoreo | Stores projects and renders Jobs and deploys. Agentic Engineer holds no Kubernetes client. | Inherited platform. |

A person with a stolen Agentic Engineer access token is in scope (see I01). A stolen Thunder/IdP session is out of scope.

## Entitlement matrix

**Intended control:** basic RBAC. Names will match the RBAC design when it ships. The matrix uses **org member** and **org admin** as placeholders.

**Today’s gap:** there is no RBAC on the Agentic Engineer API. The tenant gate only checks that the login token (JWT) belongs to that organization. Any valid organization login token can call organization APIs.

| Action | Org member (placeholder) | Org admin (placeholder) | Today |
| :---- | :---- | :---- | :---- |
| Sign in and use the console | Yes | Yes | Yes, with a valid organization login token |
| Call organization APIs | Yes (only what RBAC will allow) | Yes | **Any valid organization login token can do every organization API action** |
| Save the Anthropic API key | Intended: no | Intended: yes | Same as any organization token — gap until RBAC ships |
| Connect GitHub (GitHub App) | Intended: no | Intended: yes | Same as any organization token — gap until RBAC ships |
| Join collab on a project they can access | Yes | Yes | Token + tenant gate only |
| Start a coding-agent Job | Yes (when RBAC allows) | Yes | Same as any organization token — gap until RBAC ships |

## Resources

These are the resources Agentic Engineer controls in this Cloud production.

| Asset | Description (usage, purpose, authentication, authorizations, and security) |
| :---- | :---- |
| Console | Browser app on `cloud-cp`. Public HTTPS. Sign-in through Thunder. Proxies API and collab inside the cluster. |
| Agentic Engineer API | Public user API (gateway login-token check on) and webhooks (gateway login-token check off, HMAC inside). Tenant gate binds organization from the token. |
| Design-agent service | In-cluster only. API sends the Anthropic key in a header. |
| Collab | Live spec document in memory. Cloud keeps one replica. Does not verify the token itself. |
| Postgres | Organization records, sealed secrets, conversations, runs. |
| credential-encryption-key | AES-256-GCM for organization secrets in Postgres. |
| Workspaces volume | Git snapshots. The API writes. Agents read. |
| Temporal | Milestone-run workflows. The API is the worker. |
| Webhook HMAC secret | Checks GitHub webhook bodies. |
| OAuth state signing key | GitHub App connect redirect. |
| Shared secret (API to design agent) | Shared HS256 secret between the API and the design-agent service. |
| API signing key | Tokens the API mints for some machine calls. Coding-agent callbacks use a Thunder publisher token, not this key. |
| Coding-agent Job | Short-lived Job in the customer release namespace. Mounts organization secrets (publisher, Anthropic, GitHub). |

`aep-mcp-server`, playground token, and `/_dev` are not Cloud production resources. See Out of scope.

## Dependencies

These are resources we do not control.

| Dependency | Description (usage, purpose, authentication, authorizations, and security) |
| :---- | :---- |
| Thunder (Platform IdP) | User login and machine tokens. Internals are out of scope. Stolen Agentic Engineer access tokens stay in scope (I01). |
| GitHub | Repos, issues, PRs, webhooks, GitHub App. Cloud uses public repos for now. |
| Anthropic | Design-agent and coding-agent models. How we call them is in scope (I02, I03, I05). Their company security is not. |
| OpenChoreo | Control plane, dataplane, workflow plane, Observer. We model only Agentic Engineer’s new calls. |
| WSO2 Cloud infrastructure | AWS, accounts, EKS, gateways, Flux/GitOps of the platform. Owned by Cloud platform / SRE. |
| Secret manager (SM-API / Vault / ESO) | Organization secrets on the dataplane. Values should not cross planes; only a reference path should. |
| Container registry | Platform images and customer app images. |
