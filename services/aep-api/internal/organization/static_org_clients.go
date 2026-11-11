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
	"fmt"

	"github.com/wso2/aep/aep-api/internal/clients/thundersvc"
)

// StaticOrgClients is the OrgClients of a platform IdP aep-api holds no
// management rights on (kind asgardeo): the one org's publisher and AE
// Studio clients are created by the IdP's admin ahead of time, under the
// client ids aep-api derives from the org's name, and their secrets are
// imported into the vault and reach aep-api as settings. Nothing here calls
// the IdP.
//
// A write on a client (SetAppSecret, DeletePublisherApp) answers
// ErrOrgClientNotManaged.
//
// Every ensure reports the client as Created, which to EnsureClient means
// "store this secret": the imported secret is written to the org's reference
// on each GitHub connect. That is how a secret rotated in the IdP and
// re-imported reaches the org, since aep-api cannot set one itself.
type StaticOrgClients struct {
	org             string
	publisherSecret string
	studioSecret    string
}

var _ OrgClients = (*StaticOrgClients)(nil)

// NewStaticOrgClients returns the registry of org (the org's name, its
// OpenChoreo namespace) with the two imported client secrets
// (PLATFORM_IDP_ORG_PUBLISHER_SECRET, PLATFORM_IDP_ORG_STUDIO_SECRET).
func NewStaticOrgClients(org, publisherSecret, studioSecret string) *StaticOrgClients {
	return &StaticOrgClients{org: org, publisherSecret: publisherSecret, studioSecret: studioSecret}
}

// EnsurePublisherApp answers the org's pre-created publisher client. The OU
// and the stored id are Thunder's lookup keys and are not read.
func (c *StaticOrgClients) EnsurePublisherApp(_ context.Context, orgHandle, _, _ string) (thundersvc.OrgApp, error) {
	if c.org == "" || orgHandle != c.org {
		return thundersvc.OrgApp{}, fmt.Errorf("static org clients: org %q has no pre-created publisher client (the platform IdP serves one org)", orgHandle)
	}
	return c.app(thundersvc.PublisherAppName(c.org), c.publisherSecret, "PLATFORM_IDP_ORG_PUBLISHER_SECRET")
}

// EnsureOrgApp answers the org's pre-created AE Studio client, the one org
// application there is.
func (c *StaticOrgClients) EnsureOrgApp(_ context.Context, spec thundersvc.OrgAppSpec) (thundersvc.OrgApp, error) {
	if c.org == "" || spec.Name != thundersvc.StudioAppName(c.org) {
		return thundersvc.OrgApp{}, fmt.Errorf("static org clients: %q is not a pre-created client of this install", spec.Name)
	}
	return c.app(spec.Name, c.studioSecret, "PLATFORM_IDP_ORG_STUDIO_SECRET")
}

// app is a pre-created client: its client id is its name (and stands in for
// the entity id, which only Thunder has), and its secret is the imported one.
func (c *StaticOrgClients) app(clientID, secret, setting string) (thundersvc.OrgApp, error) {
	if secret == "" {
		return thundersvc.OrgApp{}, fmt.Errorf("static org clients: %s is not set: import the secret of %s and restart", setting, clientID)
	}
	return thundersvc.OrgApp{EntityID: clientID, ClientID: clientID, Secret: secret, Created: true}, nil
}

// SetAppSecret is refused: the secret is the IdP admin's to rotate.
func (c *StaticOrgClients) SetAppSecret(context.Context, string, string) error {
	return fmt.Errorf("%w: rotate the client secret in the IdP and import it again", ErrOrgClientNotManaged)
}

// DeletePublisherApp is refused: the client is the IdP admin's to remove. It
// stays valid, so the caller keeps its record of it.
func (c *StaticOrgClients) DeletePublisherApp(context.Context, string, string) (bool, error) {
	return false, fmt.Errorf("%w: remove the publisher client in the IdP", ErrOrgClientNotManaged)
}
