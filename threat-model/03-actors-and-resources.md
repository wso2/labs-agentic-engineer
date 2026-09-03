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

## Organization people (intended roles)

These people sign in with Thunder on `cloud-cp` (the shared platform IdP) and use the Agentic Engineer console. Intended roles are **Admin** and **AgenticDeveloper**.

| Actor | Description | Roles or permissions |
| :---- | :---- | :---- |
| Admin | Can configure the GitHub PAT and the Anthropic API key, and can do everything AgenticDeveloper can do. | Intended: those extra settings plus AgenticDeveloper work. Today: any valid organization login token (JWT) can call organization APIs. |
| AgenticDeveloper | Everything else inside Agentic Engineer (requirements, specs, collab, coding-agent Jobs, and other organization API work that is not Admin-only). | Intended: that work. Today: same as any organization token — the API does not check roles. |

A person with a stolen Agentic Engineer access token is in scope (see I01). A stolen Thunder/IdP session is out of scope.

## Entitlement matrix

Billing, inviting users, and similar organization-admin work are **WSO2 Cloud** features. They are not Agentic Engineer API actions and are not this matrix.

**Agentic Engineer API**

**Intended control:** **Admin** and **AgenticDeveloper**.

**Today’s gap:** there is no RBAC on the Agentic Engineer API. The tenant gate only checks that the login token (JWT) belongs to that organization. Any valid organization login token can call organization APIs.

| Action | AgenticDeveloper | Admin | Today |
| :---- | :---- | :---- | :---- |
| Sign in and use the console | Yes | Yes | Yes, with a valid organization login token |
| Call organization APIs | Intended: yes, except Admin-only settings below | Yes | **Any valid organization login token can do every organization API action** |
| Save the Anthropic API key | No | Intended: yes | Same as any organization token — gap until RBAC ships |
| Connect GitHub | No | Intended: yes | Same as any organization token — gap until RBAC ships |
| Join collab on a project they can access | Yes | Yes | Token + tenant gate only |
| Start a coding-agent Job | Intended: yes | Yes | Same as any organization token — gap until RBAC ships |

## Machines Agentic Engineer runs

| Actor | Description | Roles or permissions |
| :---- | :---- | :---- |
| Agent service | In-cluster service. Writes specs and other agent turns. The API is the caller. Uses the organization’s Anthropic key. | Machine. No human role. |
| Coding agent | Short-lived Job on the dataplane. Develops or validates the app from the spec. Calls the public API with the organization’s publisher login. Talks to GitHub and Anthropic. | Machine. Publisher client for that organization. |

## External systems

| Actor | Description | Roles or permissions |
| :---- | :---- | :---- |
| GitHub | Holds spec and code. Sends webhooks. | External. WSO2 Cloud provides a GitHub App path; Agentic Engineer has not taken it. |
| Thunder (platform IdP) | Issues Agentic Engineer console login tokens and machine tokens. Shared instance on `cloud-cp`. Not modeled as a product here. | Inherited IdP. |
| OpenChoreo | Stores projects and renders Jobs and deploys. Agentic Engineer holds no Kubernetes client. | Inherited platform. |

## Generated-app end users

These people sign in to **customer** apps, not the Agentic Engineer console. They are not Admin or AgenticDeveloper.

They sign in with **that organization’s Thunder on the dataplane**. That is not the shared platform Thunder on `cloud-cp`.

This review does not model those customer apps or that dataplane Thunder as Agentic Engineer. Stolen dataplane Thunder sessions are out of scope (see Out of scope).

## Resources

These are the resources Agentic Engineer controls in this WSO2 Cloud deployment.

| Asset | Description (usage, purpose, authentication, authorizations, and security) |
| :---- | :---- |
| Console | Browser app on `cloud-cp`. Public HTTPS. Sign-in through Thunder (platform IdP). Forwards backend calls inside the cluster, including the collab websocket. |
| Agentic Engineer API | Public user API (gateway login-token check on) and webhooks (gateway login-token check off, HMAC inside). Tenant gate binds organization from the token. Does not yet check Admin or AgenticDeveloper. Collab asks this API to check the login token before a room opens. |
| Agent service | In-cluster only. API sends the Anthropic key in a header. |
| Collab | Live spec document in memory. The browser reaches it through the console. Collab asks the Agentic Engineer API to check the login token before the room opens. |
| Postgres | Organization records, sealed secrets, conversations, runs. Cloud-managed database on the control plane (not an Agentic Engineer pod). |
| credential-encryption-key | AES-256-GCM for organization secrets in Postgres. |
| Workspaces volume | Git snapshots. The API writes. Agents read. |
| Temporal | Milestone-run workflows. The API is the worker. |
| Webhook HMAC secret | Checks GitHub webhook bodies. |
| Shared secret (API to agent service) | Shared HS256 secret between the API and the agent service. |
| API signing key | Tokens the API mints for some machine calls. Coding-agent callbacks use a Thunder publisher token, not this key. |
| Coding-agent Job | Short-lived Job in the customer release namespace. Mounts organization secrets (publisher, Anthropic, GitHub). |

## Dependencies

These are resources we do not control.

| Dependency | Description (usage, purpose, authentication, authorizations, and security) |
| :---- | :---- |
| Thunder (platform IdP) | Shared instance on `cloud-cp`. Agentic Engineer console login and machine tokens. Internals are out of scope. Stolen Agentic Engineer access tokens stay in scope (I01). |
| Thunder (dataplane) | Per-organization instance on the dataplane. Sign-in for generated-app end users. Not the shared platform Thunder. Internals are out of scope. |
| GitHub | Repos, issues, PRs, webhooks. Agentic Engineer has not taken the GitHub App path WSO2 Cloud provides, so new repos stay public. |
| Anthropic | Agent service and coding-agent models. Saving the key is I02. How we call them is I03 and I05. Their company security is not. |
| OpenChoreo | Control plane (per-org project namespaces), dataplane (apps and Jobs), workflow plane (image builds), Observer. We model only Agentic Engineer’s new calls. |
| WSO2 Cloud infrastructure | AWS, accounts, EKS, managed databases, gateways, Flux/GitOps of the platform, per-organization namespaces. Owned by Cloud platform / SRE. Billing and inviting users are Cloud features. |
| Secret manager (SM-API / Vault / ESO) | Organization secrets on the dataplane. Values should not cross planes; only a reference path should. |
| Container registry | Platform images and customer app images. |
