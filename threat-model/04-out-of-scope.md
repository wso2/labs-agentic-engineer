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

This review does not cover the items below. Each line says why.

1. **WSO2 Cloud infrastructure** (AWS network, accounts, firewalls, WAF, EKS, Flux/GitOps of the platform, shared Cloud data stores). Owned by Cloud platform / SRE, not this product.
2. **AWS as a cloud provider** (shared-responsibility threats). Not this product.
3. **Thunder identity-provider internals** (how Thunder issues tokens, OIDC, IdP crypto and databases). Stolen Thunder/IdP sessions are out of scope. **In scope:** what a stolen Agentic Engineer access token can do (I01).
4. **OpenChoreo as the platform** (control-plane API, namespaces, ReleaseBinding render path). Agentic Engineer only models its new calls and what it puts on the platform. There is no OpenChoreo product threat model in this repo to point at.
5. **Generic Kubernetes cluster plumbing** (nodes, default cluster RBAC, registry pull), except where coding-agent Jobs add new privileges or data (I05).
6. **Installing Agentic Engineer as an OpenChoreo module** on someone else's OpenChoreo. Not this Cloud production. One short note in the introduction is enough.
7. **SRE / RCA agent and `aep-mcp-server`.** Not deployed in this Cloud production.
8. **Playground token and `/_dev` endpoints.** Local/dev only. Must not be in Cloud.
9. **Test-user password reveal / publishing test passwords to GitHub.** Being removed. Not a live Cloud control.
10. **Org-level GitHub PAT as the primary connect path.** Cloud is GitHub App and public repos only (I06 / I07).
11. **Customer-side risks:** customer-managed secrets and apps, and attacks on the user's own network (DNS/BGP MITM).
12. **Anthropic and GitHub as companies** (their breaches, their product security). **In scope:** how Agentic Engineer calls them (I02, I03, I06, I07).
