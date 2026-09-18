// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package envidp

import "strings"

// aepSystemClientDoc is DERIVED from
// deployments/single-cluster/thunder-env-resources/80-aep-system-client.yaml
// (comments stripped, same pattern tools/aectl/internal/addons uses for its
// embedded ClusterResourceTypes). AEP's system client on this environment's
// Thunder (T2): scope=system only, used by this package itself (to write the
// role below) and by thunder-app-operator to publish per-component OAuth
// apps into this instance. ${AEP_SYSTEM_CLIENT_SECRET}/${ORG_NAME}/${ENV_NAME}
// are substituted by renderBootstrapDocuments.
const aepSystemClientDoc = `
resource_type: application
id: aep-system-client
type: m2m
name: "AEP System Client"
description: "AEP's system client for the ${ENV_NAME} environment Thunder (org ${ORG_NAME})."
ouId: "01900000-0000-7000-8000-000000000001"
inboundAuthConfig:
  - type: oauth2
    config:
      clientId: "aep-system-client"
      clientSecret: '${AEP_SYSTEM_CLIENT_SECRET}'
      grantTypes: ["client_credentials"]
      tokenEndpointAuthMethod: "client_secret_post"
      pkceRequired: false
      publicClient: false
      scopes: ["system"]
      token:
        accessToken:
          clientConfig:
            validityPeriod: 3600
            attributes: ["ouId", "ouHandle"]
`

// aepSystemRoleDoc is DERIVED from
// deployments/single-cluster/thunder-env-resources/84-aep-system-role.yaml,
// verbatim except for stripped comments. Grants aep-system-client the
// `system` permission on ThunderID's built-in System resource server, through
// a role AEP owns (never the built-in Administrator role — see the source
// file's own comment on why an environment Thunder's publisher contract is
// "touch only what you created").
const aepSystemRoleDoc = `
resource_type: role
id: aep-system-client-thunder-admin
name: "AEP System Client Thunder Admin"
description: "Grants AEP's system client access to this environment Thunder's admin API"
ouHandle: default
permissions:
  - resourceServerId: "01900000-0000-7000-8000-000000000020"
    permissions:
      - system
assignments:
  - id: aep-system-client
    type: app
`

// systemResourceServerFixDocTemplate corrects ThunderID's built-in "System"
// resource server, whose shipped identifier
// (backend/cmd/server/bootstrap/01-default-resources.yaml, id
// 01900000-0000-7000-8000-000000000020) is hardcoded to
// https://localhost:8090/mcp — never this deployment's actual public URL.
// Without this fix, every resource-indicator mint (aep-system-client's own,
// and every generated app's) 400s with invalid_target, because the resource
// indicator names THIS environment's own issuer.
//
// Modelled on deployments/scripts/setup-env-for-aectl.sh's own
// 71-fix-system-resource-server-identifier.yaml, which fixes the identical
// problem for the platform IdP (T1) with no Agent Manager dependency at all —
// used here as the precedent for a self-contained fix, rather than
// deployments/scripts/setup-environment-thunder.sh's version, which renders
// the same document through Agent Manager's own library.
const systemResourceServerFixDocTemplate = `
resource_type: resource_server
id: "01900000-0000-7000-8000-000000000020"
name: System
description: System resource server
identifier: "${SYSTEM_RESOURCE_IDENTIFIER}"
ouHandle: default
resources:
  - name: System
    handle: system
    description: System resource
`

// bootstrapDocument is one file this package mounts into the T2 chart's
// bootstrap ConfigMap. Names are numbered so ThunderID's lexical import order
// matches thunder-env-resources/'s own numbering (the system client before
// the role that assigns it).
type bootstrapDocument struct {
	name    string
	content string
}

// renderBootstrapDocuments substitutes the three placeholders this package's
// embedded documents use and returns them ready to mount. It deliberately
// does not implement a general templating engine (envsubst, text/template) —
// an explicit replacer over a fixed, known placeholder set, like
// setup-environment-thunder.sh's own render_aep_documents, so nothing other
// than these three names is ever substituted.
func renderBootstrapDocuments(org, env, systemClientSecret, systemResourceIdentifier string) []bootstrapDocument {
	docReplacer := strings.NewReplacer(
		"${AEP_SYSTEM_CLIENT_SECRET}", systemClientSecret,
		"${ORG_NAME}", org,
		"${ENV_NAME}", env,
	)
	rsReplacer := strings.NewReplacer("${SYSTEM_RESOURCE_IDENTIFIER}", systemResourceIdentifier)

	return []bootstrapDocument{
		{name: "13-fix-thunder-system-rs-identifier.yaml", content: rsReplacer.Replace(systemResourceServerFixDocTemplate)},
		{name: "80-aep-system-client.yaml", content: docReplacer.Replace(aepSystemClientDoc)},
		{name: "84-aep-system-role.yaml", content: aepSystemRoleDoc},
	}
}
