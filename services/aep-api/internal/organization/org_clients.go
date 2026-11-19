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

import "fmt"

// OrgClients is the install's record of the one org's two OAuth clients on
// the platform IdP: its publisher client (aep-publisher-<org>, the coding
// agent's identity) and its AE Studio client (ae-studio-<org>, its pod's
// identity).
//
// aep-api holds no management rights on the platform IdP. Both clients are
// created before aep-api runs (by aectl on a ThunderID it administers, by the
// IdP's admin otherwise), under the client ids aep-api derives from the org's
// name, and their secrets are stored in the vault and reach aep-api as
// settings. Nothing here calls the IdP.
//
// The gitpat submit's EnsureClient asks for a client and writes its secret to
// the org's reference, on every GitHub connect. That is how a secret rotated
// in the IdP and stored again reaches the org, since aep-api cannot set one
// itself.
type OrgClients struct {
	org             string
	publisherSecret string
	studioSecret    string
}

// OrgClient is one pre-created client: its client id and its secret.
type OrgClient struct {
	ClientID string
	Secret   string
}

// NewOrgClients returns the record of org (the org's name, its OpenChoreo
// namespace) with the two client secrets (PLATFORM_IDP_ORG_PUBLISHER_SECRET,
// PLATFORM_IDP_ORG_STUDIO_SECRET).
func NewOrgClients(org, publisherSecret, studioSecret string) *OrgClients {
	return &OrgClients{org: org, publisherSecret: publisherSecret, studioSecret: studioSecret}
}

// PublisherClientID is the client id of an org's publisher client.
func PublisherClientID(org string) string { return "aep-publisher-" + org }

// StudioClientID is the client id of an org's AE Studio client.
func StudioClientID(org string) string { return "ae-studio-" + org }

// Publisher answers org's publisher client.
func (c *OrgClients) Publisher(org string) (OrgClient, error) {
	return c.client(org, PublisherClientID(org), c.publisherSecret, "PLATFORM_IDP_ORG_PUBLISHER_SECRET")
}

// Studio answers org's AE Studio client.
func (c *OrgClients) Studio(org string) (OrgClient, error) {
	return c.client(org, StudioClientID(org), c.studioSecret, "PLATFORM_IDP_ORG_STUDIO_SECRET")
}

// client is a pre-created client of org, refused for any org but the one the
// install serves and when its secret did not reach aep-api.
func (c *OrgClients) client(org, clientID, secret, setting string) (OrgClient, error) {
	if c.org == "" || org != c.org {
		return OrgClient{}, fmt.Errorf("org clients: org %q has no pre-created clients (the platform IdP serves one org)", org)
	}
	if secret == "" {
		return OrgClient{}, fmt.Errorf("org clients: %s is not set: store the secret of %s and restart", setting, clientID)
	}
	return OrgClient{ClientID: clientID, Secret: secret}, nil
}
