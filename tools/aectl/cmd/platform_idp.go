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
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/wso2/aep/aectl/internal/config"
	"github.com/wso2/aep/aectl/internal/oidc"
)

// platformIDPKindDefault is the platform IdP an install has unless it says
// otherwise: the in-cluster ThunderID. idp.kind unset means it.
const platformIDPKindDefault = config.IDPKindThunder

// platformIDP is the platform IdP as the install writes it into the chart.
// Nothing here knows the IdP's URL layout: the endpoints come from its OIDC
// discovery document, and the kind from config.
type platformIDP struct {
	// Kind is the IdP's token dialect (idp.kind): the claim names and rules
	// every reader of its tokens uses, told to aep-api, the AE Studio pod and
	// the console through the chart.
	Kind string
	// Public is what the discovery document names: the issuer every token's
	// iss equals, and the endpoints at the IdP's public address.
	Public oidc.Endpoints
	// InCluster is the same endpoints at thunder.url, the address a pod
	// reaches the IdP at (on k3d the public hostname is loopback inside a
	// pod). Public itself when thunder.url is unset, and always on Asgardeo,
	// which has no in-cluster address.
	InCluster oidc.Endpoints
	// OrgHandle and OrgNamespace are set on Asgardeo only: the organization's
	// handle its tokens name the one org by (idp.org_handle), and the org's
	// OpenChoreo namespace aep-api maps it to (oc.default_org_namespace).
	OrgHandle    string
	OrgNamespace string
}

// onAsgardeo reports whether the platform IdP is an Asgardeo organization,
// on which aectl administers nothing: no client is registered, no client
// secret is generated, and there is no in-cluster IdP to reach.
func onAsgardeo() bool {
	return platformIDPKind() == config.IDPKindAsgardeo
}

// platformIssuerKey is the config key that holds the platform IdP's issuer.
func platformIssuerKey() string {
	if onAsgardeo() {
		return "idp.issuer"
	}
	return "thunder.public_url"
}

// discoverPlatformIDP fetches the IdP's discovery document over its issuer
// (thunder.public_url, or idp.issuer on Asgardeo) from the operator's
// machine. A failure stops the caller: there is no falling back to building
// the endpoints by hand, which is what this replaces.
func discoverPlatformIDP(ctx context.Context) (platformIDP, error) {
	key := platformIssuerKey()
	pub, err := oidc.Discover(ctx, &http.Client{Timeout: 10 * time.Second}, viper.GetString(key))
	if err != nil {
		return platformIDP{}, fmt.Errorf("platform IdP (%s): %w", key, err)
	}
	idp := platformIDP{Kind: platformIDPKind(), Public: pub, InCluster: pub}
	if onAsgardeo() {
		idp.OrgHandle = strings.TrimSpace(viper.GetString("idp.org_handle"))
		idp.OrgNamespace = ocOrgNamespace()
		return idp, nil
	}
	if base := viper.GetString("thunder.url"); base != "" {
		idp.InCluster = pub.OnBase(base)
	}
	return idp, nil
}

// platformIDPKind reads idp.kind; config validation has already refused a
// value this build does not know.
func platformIDPKind() string {
	if k := strings.ToLower(strings.TrimSpace(viper.GetString("idp.kind"))); k != "" {
		return k
	}
	return platformIDPKindDefault
}

// platformIDPOverrides returns the helm pairs the chart derives from the
// IdP: the kind, the in-cluster JWKS and token endpoints aep-api and the
// AE Studio pod reach, and the issuer. Shared by `platform install` and
// `platform update`. Undiscovered endpoints (the zero value) set nothing,
// so a caller never writes half of them.
func platformIDPOverrides(idp platformIDP) []string {
	if !idp.Public.Configured() {
		return nil
	}
	args := []string{
		"--set", "idp.kind=" + idp.Kind,
		"--set", "thunder.jwksURL=" + idp.InCluster.JWKSURL,
		"--set", "thunder.tokenURL=" + idp.InCluster.TokenURL,
		"--set", "aeStudio.idp.issuer=" + idp.Public.Issuer,
		"--set", "aeStudio.idp.jwksUrl=" + idp.InCluster.JWKSURL,
		"--set", "aeStudio.idp.tokenUrl=" + idp.InCluster.TokenURL,
	}
	if idp.OrgHandle != "" {
		// The one org of an IdP that names it by a handle of its own.
		args = append(args,
			"--set", "idp.orgHandle="+idp.OrgHandle,
			"--set", "idp.orgNamespace="+idp.OrgNamespace)
	}
	return args
}

// envIDPPlatformTrust is the platform IdP an environment ThunderID is told
// to trust, or nothing. The trust exists for one caller, Agent Manager,
// whose token is minted by the platform IdP; Agent Manager installs on a
// Thunder platform only, so on Asgardeo nothing presents such a token and
// the environment IdP trusts only itself. A trust nothing uses would read as
// configured and tie the environment IdP to an endpoint it never needs.
func envIDPPlatformTrust(idp platformIDP) oidc.Endpoints {
	if idp.Kind == config.IDPKindAsgardeo {
		return oidc.Endpoints{}
	}
	return idp.Public
}

// platformIDPInstallArgs returns the IdP pairs only `platform install` sets:
// the issuer the console signs in at and aep-api expects in iss (the one
// string the IdP's document names), and, where the IdP is the in-cluster
// Thunder, its admin address (thunder.url). An upgrade keeps both.
func platformIDPInstallArgs(idp platformIDP) []string {
	args := []string{"--set", "console.thunderPublicURL=" + idp.Public.Issuer}
	if idp.Kind != config.IDPKindAsgardeo {
		args = append(args, "--set", "thunder.adminURL="+viper.GetString("thunder.url"))
	}
	return args
}
