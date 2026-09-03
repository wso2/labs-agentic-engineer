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

# I05: Platform starts a coding-agent Job, and the Job calls back to the API

**Trust Boundary:** Trust → Trust (hop 1, dispatch), then Untrust → Trust (hop 2, callback)

**Description**

This chapter is two hops on one picture. Do not split them.

**Hop 1.** The Agentic Engineer API starts a short-lived coding-agent Job on the customer dataplane. It does not talk to Kubernetes itself. It asks OpenChoreo to create a `coding-agent` Component (`workloadType: job`) in the organization’s project. OpenChoreo renders a Job in that project’s release namespace on `cloud-dp-oc-dp`.

The API writes **references** only. Secret values land in that namespace through the secret manager and ESO. The Job env names those secrets. **Intended:** `ANTHROPIC_API_KEY` or `CLAUDE_CODE_OAUTH_TOKEN`, plus `GITHUB_TOKEN`, `PUBLISHER_CLIENT_ID`, `PUBLISHER_CLIENT_SECRET`. **Today** the retained Workload used `ANTHROPIC_API_KEY`. Plain env also carries the public callback URL (`AEP_PLATFORM_URL` / `AEP_MCP_URL`), the prompt (`AEP_PROMPT`), and git identity names.

The Job image runs as a non-root user. The ComponentType does not set `privileged`. It also does not set a pod `securityContext`. Bash inside the agent is not gated. The pod is the containment boundary. The Job shares the release namespace with the customer app.

**Hop 2.** Dataplane pods cannot use control-plane DNS. The Job therefore calls the **public** Agentic Engineer API. Live `AGENT_PLATFORM_URL` is the same public API host as I01 (`*.gateway`). The Job mints a Thunder publisher token (`iss=platform-idp`, audience `aep-publisher-…`) and calls MCP (`POST /internal/v1/mcp`) and credential refresh. The public gateway jwt-auth is on. The API takes the organization from that publisher token, not from the URL or the body. Refresh is also bound to the cycle id in the path.

Stolen Agentic Engineer **user** login tokens are I01, not this chapter. Stolen **publisher** tokens are this chapter.

Image builds are I07, not this chapter.

**Intended control:** OpenChoreo entitlement answers **402** and blocks the run when the organization is over the coding-agent cap. **Today:** the Cloud billing seed has no `coding-agent` / `job/coding-agent` metric (not a kubectl object). Starting a Job with a user login token still follows I01 (no Cloud role check yet).

**Today on Cloud dev:** the API is running (one replica) on `cloud-cp`. `AGENT_PLATFORM_URL` is the public gateway API host. One organization namespace on `cloud-dp-oc-cp` has a namespaced `coding-agent` ComponentType (`workloadType: job`). On `cloud-dp-oc-dp`, a retained cycle’s ExternalSecret is still Ready with the four secret **keys** above. No live Job pod was present (Job time-to-live had ended). The ComponentType template has no `securityContext`. Public API jwt-auth is on.

**Assets Involved**

| Initiator | Intermediate | Target |
| :---- | :---- | :---- |
| Agentic Engineer API (starts the Job after a milestone run; a person starting work still uses a login token — see I01) | OpenChoreo (Component → Workload → Job). Secret manager / ESO (values in the release namespace). | Coding-agent Job in the customer release namespace on `cloud-dp-oc-dp` |
| Coding-agent Job | Public API gateway (jwt-auth). Thunder token endpoint (publisher `client_credentials`). | Agentic Engineer API (`POST /internal/v1/mcp`, credentials refresh) |

**Data Flow or Sequence Diagram**

**D-I05 — Coding-agent Job + callback**

![D-I05 Platform starts a coding-agent Job, and the Job calls back to the API](../diagrams/d-i05-coding-agent.png)

1. The API asks OpenChoreo to start a `coding-agent` Job. Secret **names** go on the Workload. Secret **values** appear in the release namespace.
2. The Job calls the public API host (`AGENT_PLATFORM_URL`) with a publisher token. MCP and credential refresh run. Organization comes from that token.

**Payload**

Hop 1 (into the Job):

- Secret refs that become `ANTHROPIC_API_KEY`, `GITHUB_TOKEN`, `PUBLISHER_CLIENT_ID`, `PUBLISHER_CLIENT_SECRET`
- Plain env: `AEP_PLATFORM_URL`, `AEP_MCP_URL`, `AEP_PROMPT`, task/org/project ids, git identity names, `PUBLISHER_TOKEN_URL`

Hop 2 (Job → public API):

- Publisher token (JWT)
- MCP JSON-RPC (can read organization git and endpoints)
- Credential refresh: a git token can come back in the body

The Job also calls GitHub and Anthropic from the dataplane. Those calls are not a third hop and are not STRIDE’d here. Secret **presence** on the Job is row 7. Saving the Anthropic key is I02. Design-agent calls to Anthropic are I03.

**Security Considerations**

| Area | Response | Comments |
| :---- | :---- | :---- |
| Data Confidentiality | High confidential \[C-High\] | Anthropic key, GitHub token, and publisher client secret land in the Job. Refresh can return a git token. Prompt and repo content are organization work \[C-Medium\]. |
| Communication Medium | Network interaction \[M-NT\] | Hop 1: API to OpenChoreo (control plane), then secrets into the dataplane Job. Hop 2: Job to the public API gateway, then to the API. |
| Transport Security | **TLS Encryption** | Public callback uses HTTPS on `*.gateway`. In-cluster OpenChoreo calls stay on the Cloud network. |
| Authentication | **Hop 1:** platform dispatch (no user token on the Job create). **Hop 2:** Thunder publisher token (JWT) | User login tokens that start a run are I01. The Job must not use a BFF-signed token on Cloud: the public gateway only accepts `iss=platform-idp`. |
| Accessibility | **Hop 1:** not public. **Hop 2:** public API host | The Job has no inbound HTTP. `/internal/v1` is on the same public API HTTPRoute as I01 (dataplane cannot use control-plane DNS). |
| **Access Control and Authorization** | Publisher token + cycle bind on refresh. MCP org from the token. | Intended Job cap: OpenChoreo 402. Today no coding-agent billing metric was found. Who may start a Job is I01 (no Cloud role check yet). Generic Kubernetes is out of scope except this Job’s privileges and data. |

**Threat Assessment**

| ID | [Category](https://docs.google.com/presentation/d/1m3vIE2nzS_jcW8CIhCMnCaFl3KnOXZXhSLC163shHWs/edit#slide=id.g2d28a95f41a_0_94) | Threat | Materializable | Mitigations / Comment |
| :---- | :---- | :---- | :---- | :---- |
| 1 | Spoofing | A stolen or forged Agentic Engineer **user** login token is used to start a coding-agent run. | See I01 | See I01. |
| 2 | Spoofing | A stolen **publisher** token is used to call MCP or refresh as that organization. | Yes | A valid stolen publisher token is accepted for that organization. There is no revoke list for these machine tokens. User login tokens are I01. |
| 3 | Spoofing | A forged or unsigned publisher token is accepted on the callback. | No | The gateway and the API check a real Thunder token (`iss=platform-idp`, audience `aep-publisher-…`). |
| 4 | Tampering | A caller puts another organization’s id on the callback to act as that organization. | No | MCP takes organization from the verified publisher token. Refresh also checks the cycle id in the path against that organization. |
| 5 | Tampering | Secret values are written by the API onto the Job instead of references. | No | Dispatch sends SecretReference names only. Live Workload uses `secretKeyRef` for the four secret env keys. |
| 6 | Repudiation | A person denies a coding-agent run or an MCP call and we cannot show what happened. | Yes | Cycle labels exist on the Component. Agent logs live on OpenChoreo while the Component is kept. We do not store every MCP tool call in an audit table. Cancel deletes the Component at once (logs go with it). |
| 7 | Information Disclosure | Organization Anthropic key, GitHub token, and publisher secret sit in the customer release namespace, including after the Job pod is gone. | Yes | Intended: keep the Component for a while so logs can be read (default 10). Today a retained Component still has an ExternalSecret syncing those four keys. Cancel deletes at once. |
| 8 | Information Disclosure | A stolen publisher token reads that organization’s git and endpoints through MCP, or refresh returns a git token. | Yes | MCP tools include `get_remote_git_file_contents` and `search_remote_git_code`. Organization comes from the token. Refresh returns a token in the JSON body. |
| 9 | Denial of Service | Many coding-agent Jobs, or a flood of public callbacks with a valid publisher token. | Yes | Intended: OpenChoreo 402 blocks over-cap creates. The Cloud billing seed has no coding-agent metric. The type sets `backoffLimit: 0` (no silent retry). Agentic Engineer has no application rate limit on the public API (see I01). |
| 10 | Elevation of Privilege | A breakout in the Job (ungated Bash, no pod `securityContext`) reads this organization’s secrets or the customer app in the same namespace. | Yes | The image runs as non-root `aep`. The type does not set `privileged`. It also does not pin a pod `securityContext`. The pod is the containment boundary. Secrets are per organization, not per project. Generic cluster hardening is out of scope. |
| 11 | Elevation of Privilege | A viewer (or any organization login token) starts a coding-agent Job. | Yes | Intended: Cloud roles (see actors chapter). Today the user API does not check roles. See I01. |
