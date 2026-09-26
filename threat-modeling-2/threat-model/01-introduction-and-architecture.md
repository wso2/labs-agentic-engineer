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

# Introduction

This is the threat model for Agentic Engineer on WSO2 Cloud. Agentic Engineer lets an organization take an idea to a running app: people and an AI design agent write a spec together (the project's requirements and design, kept in its GitHub repository), coding agents build from the spec, and the platform deploys the app. It follows the WSO2 threat modeling method (What are we building? What can go wrong? What are we doing about it? Did we do a good enough job?).

The model describes the design in the Agentic Engineer architecture spec: organization secrets live only in a write-only secret store, and the [design studio](#c-design-studio) and the [coding agents](#c-coding-agent-pod) run in the organization's own dataplane (its own cluster, apart from WSO2 Cloud's shared control plane). Threats are assessed per interaction, from sign-in to deploy, in three review sessions.

**D1: Agentic Engineer on WSO2 Cloud**

![D1: Agentic Engineer on WSO2 Cloud](diagrams/d1-architecture.png)

## Components

**Agentic Engineer components**

| Component | Runs in | What it does |
| :---- | :---- | :---- |
| <a id="c-console"></a>Console | Control plane | The web app people use in the browser. |
| <a id="c-api"></a>Agentic Engineer API (`aep-api`) | Control plane | Checks every call, keeps records, writes secrets, and drives the org dataplane. |
| <a id="c-temporal"></a>Temporal | Control plane | Runs long background workflows for the API. |
| <a id="c-design-studio"></a>Design studio | Org dataplane, project `ae-system` | The org's pod where people and the design agent write the spec. It has three parts: |
| ↳ <a id="c-design-agent"></a>Design agent | Design studio | The AI that writes and updates the spec. Holds only the Default AI key. |
| ↳ <a id="c-live-editing"></a>Live editing | Design studio | Hosts Rooms, the live sessions where people and the agent edit together. |
| ↳ <a id="c-studio-tools"></a>Studio tools | Design studio | Does git and GitHub work and checks webhooks. Runs no AI. Holds the GitHub token. |
| <a id="c-coding-agent-pod"></a>Coding agent pod | Org dataplane, the app's project | Started for one run to build or test the app. It has two parts: |
| ↳ <a id="c-coding-agent"></a>Coding agent | Coding agent pod | The AI that writes or tests code. Holds only one AI key. |
| ↳ <a id="c-coding-tools"></a>Coding tools | Coding agent pod | Does git, GitHub and platform actions for that run. Runs no AI. |

**WSO2 Cloud platform (inherited)**

| Component | What it does |
| :---- | :---- |
| <a id="c-platform-idp"></a>Platform IdP | WSO2 Cloud sign-in for people and machine logins. |
| <a id="c-environment-thunder"></a>Environment Thunder | Sign-in service for one org and environment. |
| <a id="c-openchoreo"></a>OpenChoreo | Runs, builds and deploys workloads. |
| <a id="c-secret-store"></a>Secret store and secret sync | Write-only vault, and the job that copies a secret into a container. |
| <a id="c-gateways"></a>Gateways | The public API gateway and the org dataplane gateway. |
| <a id="c-postgres"></a>Postgres | The API's database. |
