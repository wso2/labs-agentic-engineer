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

# I04: Browser joins a collab document

**Trust Boundary:** Untrust → Trust

**Description**

A person in an organization opens a project spec in the console. The browser opens a websocket to the same console host (`/collab`) over public HTTPS. The console nginx forwards that connection inside the cluster to collab.

Collab is not on the public gateway. There is no HTTPRoute for it. The Service is ClusterIP on port 3400.

Collab does not check the login token itself. It only checks that the room id has the right shape (`spec-<org>-<project>`). It then sends the token and the room id to the Agentic Engineer API (`GET /api/v1/collab/validate`). The API checks the token the same way as I01. Organization comes from the verified token only. The room id must start with that organization’s prefix. The project must belong to that organization. If any of that fails, the room does not open.

The live document lives in memory on collab (one Y.Doc per project). After a quiet period (default 60 seconds), collab asks the API to save the spec (`files/apply`) using the leftover login token.

**Intended:** collab stays in-cluster. The API is the oracle. Cloud roles still apply (viewer can look, not change). Rooms can fail over (Redis / more than one replica). **Today:** the API does not check Cloud roles. Any valid organization login token that passes the tenant gate and the project check can join and edit. That gap is I01. Collab runs as one replica. Rooms are in memory. There is no Redis yet. GitOps still authors replica count 0; this Cloud **dev** cluster runs 1 replica.

Dev mode (`COLLAB_DEV`) and the mock BFF (`COLLAB_MOCK_BFF`) skip or fake the oracle. They must never run in a cluster. Today those flags are not set. The in-cluster API base is set.

Stolen Agentic Engineer access tokens are in scope here (see I01). Stolen Thunder/IdP sessions are not.

**Today on Cloud dev (`cloud-cp`):** the GitOps namespace `wso2cloud` has the collab Component (`exposed: false`, port 3400) but not the pods. Collab runs in the Agentic Engineer platform project’s OpenChoreo release namespace on the same cluster. One replica is Ready. The hop does not leave `cloud-cp`.

**Assets Involved**

| Initiator | Intermediate | Target |
| :---- | :---- | :---- |
| Browser (organization person) | Console (nginx `/collab` proxy). Collab (websocket; does not check the token). | Agentic Engineer API (`GET /api/v1/collab/validate`). Live in-memory spec document. Later: `files/apply` on flush. |

**Data Flow or Sequence Diagram**

**D-I04 — Browser → collab websocket**

![D-I04 Browser joins a collab document](../diagrams/d-i04-collab.png)

1. The person opens a websocket to the console `/collab` over public HTTPS. The login token is on that socket (same public path as I01).
2. Console nginx forwards the websocket inside the cluster to collab (ClusterIP `:3400`). That hop is not the public API host.
3. Collab calls `GET /api/v1/collab/validate` with `Authorization: Bearer <login token>` and `X-Room-Id: spec-<org>-<project>`. Collab does not check the token itself.
4. The API checks the token and the organization. It checks the room prefix and that the project belongs to that organization. The room opens only if that passes. The live spec sits in memory. After a quiet period (default 60 seconds), collab flushes with `files/apply` using the leftover token.

**Payload**

- Login token (JWT) on the websocket
- Room id (`spec-<org>-<project>`) in `X-Room-Id`
- Live spec content (Yjs updates)
- On flush: spec files to `files/apply`

Organization is not taken from the room id until the API has the verified token. Collab does not split the room id.

**Security Considerations**

| Area | Response | Comments |
| :---- | :---- | :---- |
| Data Confidentiality | Medium confidential \[C-Medium\] | Live spec content (design work). The login token on the socket is a credential \[C-High\]. Saving the Anthropic key is I02, not this chapter. |
| Communication Medium | Network interaction \[M-NT\] | Browser to console over the public Cloud gateway (websocket). Console to collab inside the cluster. Collab to API inside the cluster. |
| Transport Security | **TLS Encryption** | Public host uses HTTPS / WSS on `*.gateway`. The console-to-collab hop is in-cluster ClusterIP. |
| Authentication | **Bearer login token (JWT)** | Same as I01. Collab does not check the token. The API oracle does (Thunder public keys). |
| Accessibility | **Publicly Accessible** (console only) | Console is public. Collab is project-internal (ClusterIP, no HTTPRoute). The browser reaches collab only through the console. |
| **Access Control and Authorization** | Intended: Cloud roles (viewer looks; developer and admin edit). Today: tenant gate + project check only. | The API binds organization from the verified token. It checks the room prefix and project ownership. It does not check Cloud roles. See I01. |

**Threat Assessment**

| ID | [Category](https://docs.google.com/presentation/d/1m3vIE2nzS_jcW8CIhCMnCaFl3KnOXZXhSLC163shHWs/edit#slide=id.g2d28a95f41a_0_94) | Threat | Materializable | Mitigations / Comment |
| :---- | :---- | :---- | :---- | :---- |
| 1 | Spoofing | A stolen or forged Agentic Engineer login token is used to join a collab room. | See I01 | See I01. |
| 2 | Spoofing | A client joins a room because collab does not check the token, and the oracle is skipped (dev or mock mode). | No | Today `COLLAB_DEV` and `COLLAB_MOCK_BFF` are not set. The in-cluster API base is set, so collab calls `GET /api/v1/collab/validate`. Those flags must never be enabled in a cluster. |
| 3 | Tampering | A caller puts another organization's id into the room name to open that organization's spec. | No | Collab only checks room shape. The API takes organization from the verified token. The room must start with `spec-<org>-`. The project must belong to that organization. Mismatch is 403. |
| 4 | Tampering | After the person leaves, collab still flushes edits with a leftover login token. | Yes | Quiet-period flush defaults to 60 seconds. Last-leave flush often has no client left to refresh the token. That leftover token can still commit for up to 60 seconds. |
| 5 | Repudiation | A person denies a spec edit from collab and we cannot show who did it. | Yes | Flushes commit through the API. Participant names can go on the commit. We do not store every live Yjs change in an audit table. |
| 6 | Information Disclosure | Someone on the internet opens a collab websocket without going through the console. | No | There is no public HTTPRoute for collab. The Service is ClusterIP. The browser path is the console `/collab` proxy. The oracle still requires a login token. |
| 7 | Denial of Service | A flood of websocket joins, or the one collab pod dies and the live document is gone. | Yes | Agentic Engineer has no application rate limit on this path (see I01). Today collab is one replica. Rooms are in memory. There is no Redis. Unflushed edits (up to 60 seconds) die with the pod. |
| 8 | Elevation of Privilege | A viewer (or any organization login token) joins and edits the spec. | Yes | Intended: viewer looks; developer and admin edit. Today the API does not check roles. Token + tenant gate + project check only. See I01. |
