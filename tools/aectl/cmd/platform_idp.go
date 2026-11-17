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

package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/wso2/aep/aectl/internal/config"
	"github.com/wso2/aep/aectl/internal/idpclaims"
	"github.com/wso2/aep/aectl/internal/oidc"
	"github.com/wso2/aep/aectl/internal/ui"
)

// platformIDP is the platform IdP as the install writes it into the chart.
// Nothing here knows the IdP's URL layout: the endpoints come from its OIDC
// discovery document, and the rest from config.
type platformIDP struct {
	// Clients is who creates AE's clients on the IdP (idp.clients): aectl,
	// on a ThunderID it administers (config.ClientsThunder), or the IdP's
	// admin, who then imports their secrets (config.ClientsImported).
	Clients string
	// Claims is the IdP's token dialect (idp.claims): the names and values
	// of the claims that say a caller's org and caller type, told to aep-api,
	// the AE Studio pod and the console through the chart.
	Claims idpclaims.Resolved
	// Public is what the discovery document names: the issuer every token's
	// iss equals, and the endpoints at the IdP's public address.
	Public oidc.Endpoints
	// InCluster is the same endpoints at thunder.url, the address a pod
	// reaches a ThunderID aectl administers at (on k3d the public hostname is
	// loopback inside a pod). Public itself when thunder.url is unset, and
	// always with imported clients: such an IdP has no in-cluster address.
	InCluster oidc.Endpoints
	// OrgHandle and OrgNamespace name the one org the IdP serves: the handle
	// its tokens name the org by (idp.org_handle), and the org's OpenChoreo
	// namespace aep-api maps it to (oc.default_org_namespace).
	OrgHandle    string
	OrgNamespace string
}

// platformIDPClients reads idp.clients: who creates AE's clients on the
// platform IdP. Unset is thunder; config validation has already refused a
// value this build does not know.
func platformIDPClients() string {
	if c := strings.ToLower(strings.TrimSpace(viper.GetString("idp.clients"))); c != "" {
		return c
	}
	return config.ClientsThunder
}

// registersClients reports whether aectl creates AE's clients itself, on a
// ThunderID it administers. Otherwise the IdP's admin created them and aectl
// administers nothing there: it registers no client, generates no client
// secret, asks for no admin secret, and has no in-cluster IdP to reach.
func registersClients() bool {
	return platformIDPClients() == config.ClientsThunder
}

// platformIssuerKey is the config key that holds the platform IdP's issuer:
// idp.issuer, or thunder.public_url where idp.issuer is unset (a ThunderID
// aectl administers, whose public URL is its issuer).
func platformIssuerKey() string {
	if strings.TrimSpace(viper.GetString("idp.issuer")) != "" {
		return "idp.issuer"
	}
	return "thunder.public_url"
}

// discoverPlatformIDP fetches the IdP's discovery document over its issuer
// (platformIssuerKey) from the operator's
// machine. A failure stops the caller: there is no falling back to building
// the endpoints by hand, which is what this replaces.
func discoverPlatformIDP(ctx context.Context) (platformIDP, error) {
	key := platformIssuerKey()
	pub, err := oidc.Discover(ctx, &http.Client{Timeout: 10 * time.Second}, viper.GetString(key))
	if err != nil {
		return platformIDP{}, fmt.Errorf("platform IdP (%s): %w", key, err)
	}
	claims, err := platformIDPClaims()
	if err != nil {
		return platformIDP{}, err
	}
	idp := platformIDP{
		Clients: platformIDPClients(), Claims: claims, Public: pub, InCluster: pub,
		OrgHandle:    strings.TrimSpace(viper.GetString("idp.org_handle")),
		OrgNamespace: ocOrgNamespace(),
	}
	if !registersClients() {
		return idp, nil
	}
	if base := viper.GetString("thunder.url"); base != "" {
		idp.InCluster = pub.OnBase(base)
	}
	return idp, nil
}

// claimsEnvKey is the environment variable that overrides one value of the
// idp.claims block for a run: AEP_IDP_CLAIMS_ORG_HANDLE for "org.handle".
func claimsEnvKey(key string) string {
	return "AEP_IDP_CLAIMS_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
}

// platformIDPClaims resolves the idp.claims block: the preset, then the
// config's idp.claims.* keys, then AEP_IDP_CLAIMS_* for this run. An
// environment value lasts for the run only: the stored config keeps what the
// config file said, so a value meant to stay belongs in that file.
func platformIDPClaims() (idpclaims.Resolved, error) {
	fromConfig := config.ClaimsOverride(viper.GetViper())
	return idpclaims.Resolve(viper.GetString("idp.claims.preset"), func(key string) (string, idpclaims.Source) {
		if v := strings.TrimSpace(os.Getenv(claimsEnvKey(key))); v != "" {
			return v, idpclaims.SourceEnvironment
		}
		return fromConfig(key)
	})
}

// printPlatformIDPClaims shows the claim values in effect and the layer each
// came from. They decide which tokens count as a machine's and whose org a
// caller is in, so a value that an environment variable changed for this run
// must not go unnoticed.
func printPlatformIDPClaims(claims idpclaims.Resolved) {
	for _, line := range claims.Describe() {
		ui.Detail(line)
	}
}

// platformIDPOverrides returns the helm pairs the chart derives from the
// IdP: who creates its clients, its claim names and values, the JWKS and
// token endpoints aep-api and the AE Studio pod reach, and the issuer. Shared by `platform install` and
// `platform update`. Undiscovered endpoints (the zero value) set nothing,
// so a caller never writes half of them.
func platformIDPOverrides(idp platformIDP) []string {
	if !idp.Public.Configured() {
		return nil
	}
	args := []string{
		"--set", "idp.clients=" + idp.Clients,
		"--set", "idp.jwksURL=" + idp.InCluster.JWKSURL,
		"--set", "idp.tokenURL=" + idp.InCluster.TokenURL,
		"--set", "aeStudio.idp.issuer=" + idp.Public.Issuer,
		"--set", "aeStudio.idp.jwksUrl=" + idp.InCluster.JWKSURL,
		"--set", "aeStudio.idp.tokenUrl=" + idp.InCluster.TokenURL,
	}
	// Every claim value is set, the empty optional ones too: the chart's own
	// defaults are one IdP's, and an unset pair would leave that default in.
	c := idp.Claims.Claims()
	args = append(args,
		"--set", "idp.claims.org.handle="+c.OrgHandle,
		"--set", "idp.claims.org.id="+c.OrgID,
		"--set", "idp.claims.org.name="+c.OrgName,
		"--set", "idp.claims.callerType.claim="+c.CallerType,
		"--set", "idp.claims.callerType.machine="+c.Machine,
		"--set", "idp.claims.callerType.user="+c.User)
	// The one org the IdP serves: the handle its tokens name the org by,
	// and the org's namespace.
	return append(args,
		"--set", "idp.orgHandle="+idp.OrgHandle,
		"--set", "idp.orgNamespace="+idp.OrgNamespace)
}

// envIDPPlatformTrust is the platform IdP an environment ThunderID is told
// to trust, or nothing. The trust exists for one caller, Agent Manager,
// whose token is minted by the platform IdP. Agent Manager needs its clients
// registered on a ThunderID, so it installs only where aectl registers
// clients; with imported clients nothing presents such a token and the
// environment IdP trusts only itself. A trust nothing uses would read as
// configured and tie the environment IdP to an endpoint it never needs.
func envIDPPlatformTrust(idp platformIDP) oidc.Endpoints {
	if idp.Clients != config.ClientsThunder {
		return oidc.Endpoints{}
	}
	return idp.Public
}

// platformIDPInstallArgs returns the IdP pair only `platform install` sets:
// the issuer the console signs in at and aep-api expects in iss (the one
// string the IdP's document names). An upgrade keeps it.
func platformIDPInstallArgs(idp platformIDP) []string {
	return []string{"--set", "idp.issuer=" + idp.Public.Issuer}
}
