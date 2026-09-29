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

![WSO2](diagrams/wso2-header.png)

# Agentic Engineer on WSO2 Cloud

Threat Model

**Version: 1.1**  
**Date:** `[date]`  
**Email:** `[team email]`

This product was previously called App Factory / AEP.

> **Baseline.** Written against `main` `5a6d6dcd` (2026-09-29), PR #778 (roles and permissions) treated as merged (reviewed at `ee2ba084`), and the architecture spec at `9f7e7a03`. WSO2 Cloud settings are taken from the Cloud overlay `app-factory-wso2-enterprise` `fc6bd9f` and `wso2cloud-deployement-main` `5167ecab3`.

# Revision History

| Version | Release Date | Contributors / Authors | Summary of Changes |
| ----- | ----- | ----- | ----- |
| 1.0 | `[date]` | `[contributors]` | Initial version |
| 1.1 | `[date]` | `[contributors]` | The design studio checks WSO2 Cloud sign-in (Platform IdP) tokens itself, and the browser calls it directly. Removed: tokens signed by the API, Room tokens and Environment Thunder. Added: the AE-only control-plane client, the per-org Room-join identity, the design-turn usage batch, and improvements H-10 and H-11. The AE-only machine token goes only to studio tools, which starts the API's design turns inside the pod, so it never reaches an AI container. Studio tools splits its routes into person-token reads and machine-token internal routes. Live editing accepts the Room-join token only inside the pod. The design studio applies the same permission rule as the API. H-10 notes that platform-api checks no audience. |
