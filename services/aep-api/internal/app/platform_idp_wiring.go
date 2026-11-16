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

package app

import (
	"github.com/wso2/aep/aep-api/internal/clients/thundersvc"
	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/auth/dialect"
)

// usesThunderAdmin reports whether the platform IdP is administered through
// Thunder's admin client: creating an org's clients and checking that an org
// unit exists. Only Thunder is. On Asgardeo aep-api holds no management
// rights on the tenant, so the client is never built there; built anyway it
// would aim Thunder's admin calls at the token endpoint's host.
func usesThunderAdmin(kind dialect.Kind) bool {
	return kind == dialect.KindThunder
}

// orgClientsFor is how an org's publisher and AE Studio clients are
// provisioned on the platform IdP of kind. Thunder: its admin client creates
// them (a nil client is a nil port, and the service answers "unavailable").
// Asgardeo: the IdP's admin pre-created them for the one org and imported
// their secrets, which the static registry hands to the same ensure.
func orgClientsFor(kind dialect.Kind, idp config.PlatformIDPDefaults, thunderAdmin thundersvc.Client) organization.OrgClients {
	if kind == dialect.KindAsgardeo {
		return organization.NewStaticOrgClients(idp.OrgNamespace, idp.OrgPublisherSecret, idp.OrgStudioSecret)
	}
	if thunderAdmin == nil {
		return nil
	}
	return thunderAdmin
}
