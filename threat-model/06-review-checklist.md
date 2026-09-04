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

**Live cluster check (2026-09-04):** `kubectl` against WSO2 Cloud **dev** returned Cloudflare “Access Denied / Please use VPN.” The VPN tunnel was not up. Isolation, non-root pods, network policies, replicas, and secrets in the cluster are **unknown**. That failure is not “the cluster is empty.” Re-run `kubectl` before the review meeting.

## Security Considerations

| Security Considerations [https://top10proactive.owasp.org/](https://top10proactive.owasp.org/) | State | Comments |
| :---- | :---- | :---- |
| Are all inputs and outputs validated? [Syntactic Validation and Semantic Validation](https://top10proactive.owasp.org/the-top-10/c3-validate-input-and-handle-exceptions/) | Partial | `/config` is typed. Organization comes from the login token, not the URL or body (I01). Webhooks check HMAC (I07). Spec, skill, and turn bodies have size caps. Not every field has a documented meaning check. |
| Are rate limits in place where necessary? | No | Agentic Engineer has no application rate limit on the user API (I01, I02, I05). A flood at the Cloud edge is inherited. |
| Are permissions, roles, and entitlements defined on the principle of least privilege and business needs? | Not yet | Intended: **Admin** (GitHub PAT and Anthropic key, plus everything AgenticDeveloper can do) and **AgenticDeveloper** (everything else in Agentic Engineer). Today any valid organization login token can call organization APIs (I01). |
| Are authentication and authorization properly validated at both the UI and API levels on the front end and back end, as applicable, before granting access to resources? | Partial | Console and user API use a Thunder login token. The public user API also has gateway jwt-auth on. Collab asks the API to check the token before a room opens (I04). Webhooks: jwt-auth off; HMAC inside (I07). The API does not check roles yet. |
| Are proper isolations in place between components to ensure least-privilege access and reduce the blast radius against lateral movement? | Unknown | Could not list cluster objects this session. Intended: the login-token tenant gate splits API data; one `wc-*` namespace per organization splits OpenChoreo Projects. Coding-agent Jobs share the customer app namespace (I05). |
| Have any default credentials been changed, and are the default superuser/root accounts not in use? (applicable when using 3rd party solutions) | N/A (AE) / unknown (inherited) | Agentic Engineer does not ship a default superuser. Thunder and Cloud defaults are inherited. Live pods were not checked this session. |
| Has the implementation been carried out in accordance with best-practice guidelines (OWASP/ Kubernetes/ Vendor/ Technology provider)? | Partial | Typed contracts, HTTPS on public hosts, encrypted org secrets, webhook HMAC. Gaps: no API RBAC, no API rate limit, public GitHub repos (I06), incomplete audit logs. Generic Kubernetes hardening is out of scope. |
| Is the source code kept private where applicable? | No | This product’s source is a public lab repo. New customer GitHub repos are created public (`GITHUB_REPO_VISIBILITY` defaults to `public`) until Agentic Engineer takes the Cloud GitHub App path (I06). |
| Was a code review conducted for the feature, product, software, application, or deployment with a focus on security and resiliency, and have the findings been addressed? | Not yet | Pull requests run GitHub CI (build, test, lint, license). This threat model is the Cloud security review. A person has not yet checked every control. |
| Is Static (SAST) OR IaC scanning conducted and are findings addressed? | No | CI runs `golangci-lint` and eslint. There is no SAST or IaC scanner in the GitHub workflow. |
| Is Software Composition Analysis (SCA) being conducted or integrated into the source code repository, and are findings addressed? (Examples include FOSSA, JFrog XRay, or Trivy) | No | Not found in this repo’s CI. |
| Is Dynamic (DAST) or API scanning conducted on the non-production setup, and are findings addressed? | Unknown | Not found in this repo. Unknown whether Cloud already scans the **dev** gateway. |
| Are audit logs generated in a standardized format for critical functionalities and made available to authorized users to trace critical events and aid incident response? How long are audit logs retained for? | Partial | Config patches log section names (`orgconfig.patched`), not secret values. Publisher changes write `idp_audit_events`. We do not log every API call (I01). How long logs are kept: unknown. Design-agent conversation store is swept after a time to live (I03). |
| Do audit logs for critical configuration changes include a record of the differences between the old and new versions? | Partial | `idp_audit_events` stores before and after publisher state (`hasClientSecret` is a yes/no, not the secret). Config patches log which sections changed, not a field-by-field diff. |
| Has a business impact analysis (BIA) been conducted on development to identify security and resilience requirements such as maximum tolerable downtime (MTTD), uptime, recovery point objective (RPO), and recovery time objective (RTO)? High Availability Requirements: Disaster Recovery Requirements: Backup, frequencies, and retention: Database Backups and Replication VM/System Backups Storage Backups (Storage/PV/ACR/etc.) Configuration Backups Logs Health Checks: User Banners: | Unknown | Not found for Agentic Engineer. Cloud-managed Postgres and platform HA are inherited. Collab keeps the live spec in one pod’s memory (I04). |
| Are data in transit and data at rest encrypted? | Partial | Public hosts use HTTPS. Org secrets in Postgres use AES-256-GCM. Disk encryption for Cloud Postgres is inherited / unknown. Spec and code on public GitHub are world-readable (I06). |
| Is sensitive data, such as credentials and keys, stored in secret stores like key vaults? | Partial | Intended: Vault / ESO / SecretReference; only names cross planes. Org GitHub PAT and Anthropic key also sit encrypted in Postgres. Coding-agent Jobs receive those secrets in the customer namespace (I05). Cluster inventory is unknown this session. Secret **values** were not read. |
| Have you ensured that personal, sensitive, or confidential data is not logged? | Partial | Config audit tests fail if the Anthropic key appears in the log. IDP audit stores `hasClientSecret`, not the secret. Whether every log redacts email, spec text, and tokens: unknown. |
| Have we provided users with proper instructions on secure usage? | Not yet | The console exists. The in-repo user guide is not written yet. |

## Vulnerability Management

| Question | State | Comments |
| :---- | :---- | :---- |
| How are we planning to address product vulnerabilities, and what's the patching frequency? | Unknown | Not written down for Agentic Engineer. Releases use the GitHub Release workflow. How often: unknown. |
| How are we planning to address deployment and dependency vulnerabilities, and what's the patching frequency? | Unknown | No SCA in this repo’s CI. Patching of EKS and base images is inherited Cloud / SRE work. |
| Are there any End-of-Life or End-of-Service components in use? | Unknown | Not checked this session. |

## Privacy Considerations

Agentic Engineer processes personal data: login-token claims (including email), organization membership, and organization work (specs, prompts, attached files). Specs and prompts also leave WSO2 to Anthropic (I03, I05).

| Privacy Consideration | State | Comments |
| :---- | :---- | :---- |
| Is the purpose and legal basis for the processing of personal data clearly defined? | Unknown | Not found in this repo. |
| Is the creation/collection, storage, usage (processing), sharing, archival, and disposal of personal data in accordance with the data minimization principle? | Partial | Settings secrets are write-only on GET. The design agent sends spec, prompts, skill text, attached files, and tool results to Anthropic with no documented filter (I03). Customer GitHub repos are public (I06). |
| Is personal data being stored securely? | Partial | Org secrets are encrypted at rest. Login tokens are credentials. Specs on public GitHub are not private. |
| Are privacy policies updated to reflect any new personal data processing or changes to purpose and legal basis? | Unknown | Not found. |
| Is access to personal data being granted based on the need to know? | Not yet | Intended: Admin and AgenticDeveloper. Today any valid organization login token can read that organization’s API data (I01). |
| Are data retention requirements considered? | Partial | Design-agent conversation store is swept after a time to live. Other tables: unknown. |
| Is there a process for disposing of personal data collected upon request in a timely manner while meeting retention requirements? | Unknown | Not found. Disconnecting the Anthropic key drops those bytes in Postgres (I02). |
| Have you added relevant records in “[WSO2 Data Inventory](https://docs.google.com/spreadsheets/d/1kGVhgvaAi1XYtflf5I_r6bcZQqdimRmm2VXf221FbKY/edit?gid=986734575#gid=986734575).” or [\[Cloud\] Data Storages](https://docs.google.com/spreadsheets/d/1TFajRmy3YLuYkZxNyJOkSuE9orjFcuHLmLWxvT1HWnY/edit?gid=224115104#gid=224115104) (for clouds) related to the processing of personal data? | Unknown | Not confirmed. |

## Kubernetes-based considerations (if applicable)

Generic Kubernetes is out of scope. These answers are what Agentic Engineer adds, plus intended Cloud shape. Live objects were not listed this session. **Re-check with `kubectl` before the review meeting.**

| Question | State | Comments |
| :---- | :---- | :---- |
| How is the namespace management done? | Unknown | Intended: Agentic Engineer platform on `cloud-cp` in the `wso2cloud` org. Customer OpenChoreo Projects in one `wc-*` namespace per organization on `cloud-dp-oc-cp`. GitOps still authors `PLATFORM_API_NAMESPACE_OVERRIDE` → `app-factory-user-projects`. Whether that namespace exists now: unknown. |
| Has the default namespace been utilized, and have resources been created in it? | Unknown | Could not list namespaces this session. |
| Have resource quotas and limits defined? | Unknown | Not found as an Agentic Engineer control. May be inherited Cloud / OpenChoreo. |
| Have Network Policies been configured to control traffic? | Unknown | Could not list network policies this session. |
| Have RBAC policies been implemented to grant users least-privilege access? | Not yet (AE API) / unknown (cluster) | Intended API roles: Admin and AgenticDeveloper. Today the API does not check roles. Kubernetes RBAC for Cloud operators is inherited. Cluster RoleBindings were not listed this session. |
| Are non-root users being used? | Unknown | Could not read live pods this session. Images in this repo set a non-root `USER` (API `appuser`, agents and collab `1000`, console `10001`, coding-agent `aep`). That is image source, not a live pod check. |
| Replicas | Unknown | GitOps still defaults Agentic Engineer replica vars to **0**. Researched Cloud org has only Environment `development`. Live replica counts: unknown this session. |
| Secrets in the cluster (names only) | Unknown | Intended Job secret **names** (I05): `ANTHROPIC_API_KEY`, `GITHUB_TOKEN`, `PUBLISHER_CLIENT_ID`, `PUBLISHER_CLIENT_SECRET`. Live Secret objects were not listed. Values were not read. |
