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

# Appendix

## Feature/Product Documentation

- Product overview: [README](https://github.com/wso2/labs-agentic-engineer)
- Architecture: [docs/architecture.md](https://github.com/wso2/labs-agentic-engineer/blob/main/docs/architecture.md)
- Glossary: [docs/glossary.md](https://github.com/wso2/labs-agentic-engineer/blob/main/docs/glossary.md)
- API service map: [services/aep-api/README.md](https://github.com/wso2/labs-agentic-engineer/blob/main/services/aep-api/README.md)
- Console product notes: [apps/console/PRD.md](https://github.com/wso2/labs-agentic-engineer/blob/main/apps/console/PRD.md)
- End-user guide: [docs/user-guide/README.md](https://github.com/wso2/labs-agentic-engineer/blob/main/docs/user-guide/README.md) — **not written yet**

## CNAD/Application Development Checklist

Not found in this repo. **Not yet.**

## Sample Configs

See Review Checklist for the live cluster check. Do not paste kubeconfig, Secret data, or PAT / Anthropic values here.

Authored default in this repo (`services/aep-api/internal/config/config_loader.go`): new GitHub repos are public unless the env is set:

```go
GitHubRepoVisibility: r.readOptionalString("GITHUB_REPO_VISIBILITY", "public"),
```

Cloud GitOps (authored, not live this session): user API gateway login-token check **on**; webhook route **off**; CORS locked to the console origin; replica vars still default to **0**.

## Sample Audit Logs

Real shapes from Agentic Engineer source. Placeholders only. No live rows.

Organization config patch (JSON log; section names, never the secret):

```json
{"msg":"orgconfig.patched","org":"<organization-id>","sections":["llm"]}
```

Publisher / IDP change (`idp_audit_events`). `hasClientSecret` is a boolean. The client secret is not stored in this row:

```json
{
  "orgId": "<organization-id>",
  "action": "ensure_publisher",
  "actor": "<user-or-system>",
  "occurredAt": "<time>",
  "beforeState": {},
  "afterState": {
    "kind": "<kind>",
    "issuer": "<issuer>",
    "jwksUrl": "<url>",
    "publisherClientId": "<id>",
    "hasClientSecret": true
  }
}
```

A full log of every API call does not exist yet (I01).
