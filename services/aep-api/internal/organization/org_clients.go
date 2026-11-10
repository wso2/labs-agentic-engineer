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

package organization

import (
	"context"

	"github.com/wso2/aep/aep-api/internal/clients/thundersvc"
)

// OrgClients provisions an organization's two OAuth clients on the platform
// IdP: its publisher client (aep-publisher-<org>, the coding agent's identity)
// and its AE Studio client (ae-studio-<org>, its pod's identity). The gitpat
// submit's EnsureClient is the one writer; a profile kind switch and the
// publisher revoke delete.
//
// It is the four methods of the platform IdP's admin client this domain calls,
// named as that client names them so thundersvc.Client satisfies the port as
// it stands. The org-unit check is a port of its own, OUValidator.
type OrgClients interface {
	// EnsurePublisherApp finds or creates the org's publisher client under the
	// org's OU, by the entity id recorded last time ("" when none).
	EnsurePublisherApp(ctx context.Context, orgHandle, orgOUID, storedID string) (thundersvc.OrgApp, error)
	// EnsureOrgApp finds or creates the org application spec names (the AE
	// Studio client), healing one registered without its OU.
	EnsureOrgApp(ctx context.Context, spec thundersvc.OrgAppSpec) (thundersvc.OrgApp, error)
	// SetAppSecret sets the client secret of an existing application.
	SetAppSecret(ctx context.Context, entityID, secret string) error
	// DeletePublisherApp removes the org's publisher client; false when there
	// was none.
	DeletePublisherApp(ctx context.Context, orgHandle, storedID string) (bool, error)
}
