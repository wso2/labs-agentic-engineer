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

# Out-of-Scope Interactions/Risks

This review covers Agentic Engineer on WSO2 Cloud. It does not cover the items below.

## Inside Agentic Engineer

These are Agentic Engineer parts, but they are not modelled here.

- **Parts not deployed on WSO2 Cloud:** the playground, the tryit test page (a page to try API calls), the `/_dev` routes and the smee webhook relay (a tool that forwards webhooks to a developer's machine).
- **The GitHub App sign-in path.** Agentic Engineer uses a GitHub token. A GitHub App may come later.
- **Work in progress:** the incident agent loop (SRE/RCA: an agent that finds the cause of an alert), and sending the Default AI key to the Agent Manager AI gateway (a gateway that passes AI model calls on). The incident loop's API routes (`CreateIssue`, `PromoteTaskFromIssue`, `CreateRcaAgentReport`) exist; its tool server and agent are not on WSO2 Cloud.

## Outside Agentic Engineer

These are not Agentic Engineer. We cover only the calls Agentic Engineer makes into them.

- **WSO2 Cloud infrastructure:** clusters, networks, gateways, databases and operations, including operator access (standing and break-glass).
- **Platform IdP (identity provider) and Environment Thunder:** how they sign people in and issue tokens. A stolen sign-in session is out of scope.
- **OpenChoreo:** how it runs workloads, builds images and deploys apps. We cover what Agentic Engineer asks it to do.
- **The secret store and its sync:** how they store and deliver values. We cover what Agentic Engineer writes and which container reads it.
- **Kubernetes and the container runtime.**
- **The customer's running app and its users.**
- **WSO2 Cloud billing:** the console shows the org's plan by calling WSO2 Cloud's billing API with the user's login token.
- **GitHub and Anthropic as companies,** and how Anthropic handles the data it receives.
