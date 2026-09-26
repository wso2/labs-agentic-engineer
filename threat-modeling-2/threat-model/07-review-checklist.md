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

# Review Checklist

## Security Considerations

*Reference: https://top10proactive.owasp.org/*

| Security Consideration | State | Comments |
| :---- | :---- | :---- |
| Are all inputs and outputs validated? (Syntactic and Semantic Validation) | Yes | Inputs have size limits, the org comes from the login token, and webhooks are checked by signature. |
| Are rate limits in place where necessary? | Yes | Rate limits are WSO2 Cloud's, at its gateways. Agentic Engineer also limits turns, runs and builds (PW-3). |
| Are permissions, roles, and entitlements defined on least privilege and business needs? | Yes | Two roles and 14 permissions (see Actors). Secrets and skills are Admin only. |
| Are authentication and authorization validated at both UI and API, front and back end? | Yes | The API checks the login token and the permission on every call. Dataplane containers check their own tokens. |
| Are proper isolations in place between components (least-privilege, blast-radius reduction)? | Yes | Each org has its own dataplane pods, and the AI is kept apart from the secrets (TB-5, TB-7). |
| Have default credentials been changed / default superuser accounts disabled? | Yes | WSO2 Cloud settings turn off every development path. |
| Has implementation followed best-practice guidelines (OWASP/Kubernetes/vendor)? | Yes | Pods run non-root, with a read-only file system and no Kubernetes token. |
| Is the source code kept private where applicable? | Yes | WSO2 Cloud related configs and source are kept private. |
| Was a security-focused code review conducted, and were findings addressed? | No | — |
| Is Static (SAST) or IaC scanning conducted and are findings addressed? | No | — |
| Is Software Composition Analysis (SCA) conducted (e.g., FOSSA, JFrog XRay, Trivy)? | No | — |
| Is Dynamic (DAST) or API scanning conducted on non-production setups? | No | — |
| Are audit logs generated in a standardized format, available to authorized users, with a defined retention period? | Partial | Builds and deploys record who started them. More is planned (H-6). No retention period is set. |
| Do audit logs for critical configuration changes include before/after values? | Yes | A change to the GitHub token or an AI key logs which section changed, never the value. |
| Has a Business Impact Analysis (BIA) been conducted (MTTD, uptime, RPO, RTO)? | No | — |
| Are data in transit and at rest encrypted? | Yes | TLS on every call across the internet. Encryption at rest is WSO2 Cloud's. |
| Is sensitive data (credentials, keys) stored in a secret store / key vault? | Yes | Org secrets live only in the write-only secret store. |
| Have you ensured personal, sensitive, or confidential data is not logged? | Yes | The API never logs secret values. |
| Have users been given proper instructions on secure usage? | No | — |

## Vulnerability Management

| Question | Response |
| :---- | :---- |
| How will product vulnerabilities be addressed, and at what patching frequency? | TBD |
| How will deployment and dependency vulnerabilities be addressed, and at what patching frequency? | TBD |
| Are there any End-of-Life or End-of-Service components in use? | TBD |

## Privacy Considerations

Agentic Engineer processes the name and email on the login token, and test-user accounts for each app.

| Privacy Consideration | State | Comments |
| :---- | :---- | :---- |
| Is the purpose and legal basis for processing personal data clearly defined? | — | — |
| Is creation/collection, storage, usage, sharing, archival, and disposal of personal data in line with data minimization? | Yes | Only the name and email on the login token. |
| Is personal data stored securely? | Yes | Rows are limited to the user's org. Test-user passwords are stored sealed. |
| Are privacy policies updated to reflect new personal data processing? | — | — |
| Is access to personal data granted on a need-to-know basis? | Yes | People see only their own org. Test-user passwords need `ae:build`. |
| Are data retention requirements considered? | Partial | Conversations are deleted after 7 days. Activity records have no set limit. |
| Is there a timely process for disposing of personal data on request, while meeting retention requirements? | — | — |
| Have relevant records been added to the [WSO2 Data Inventory](https://docs.google.com/spreadsheets/d/1kGVhgvaAi1XYtflf5I_r6bcZQqdimRmm2VXf221FbKY/edit?gid=986734575#gid=986734575) / [Cloud Data Storages](https://docs.google.com/spreadsheets/d/1TFajRmy3YLuYkZxNyJOkSuE9orjFcuHLmLWxvT1HWnY/edit?gid=224115104#gid=224115104) register? | — | — |

## Kubernetes-based considerations

| Consideration | Response |
| :---- | :---- |
| How is namespace management done? | OpenChoreo manages them. Each org has its own dataplane. |
| Has the default namespace been used, and have resources been created in it? | No |
| Have resource quotas and limits been defined? | Partial. Coding pods have fixed CPU and memory limits. Project quotas are WSO2 Cloud's. |
| Have Network Policies been configured to control traffic? | Yes. Agent pods reach only public addresses, and accept calls only through the org gateway (TB-8). |
| Have RBAC policies been implemented for least-privilege access? | Yes. Agent pods have no Kubernetes token, and the API has no Kubernetes access on WSO2 Cloud. |
| Are non-root users being used? | Yes. Agent pods run as non-root. |
