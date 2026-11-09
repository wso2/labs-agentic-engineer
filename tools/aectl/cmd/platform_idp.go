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

	"github.com/wso2/aep/aectl/internal/oidc"
)

// platformIDPKindDefault is the token dialect every install has run on so
// far; idp.kind unset means it.
const platformIDPKindDefault = "thunder"

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
	// pod). Public itself when thunder.url is unset.
	InCluster oidc.Endpoints
}

// discoverPlatformIDP fetches the IdP's discovery document over
// thunder.public_url, the issuer, from the operator's machine. A failure
// stops the caller: there is no falling back to building the endpoints by
// hand, which is what this replaces.
func discoverPlatformIDP(ctx context.Context) (platformIDP, error) {
	pub, err := oidc.Discover(ctx, &http.Client{Timeout: 10 * time.Second}, viper.GetString("thunder.public_url"))
	if err != nil {
		return platformIDP{}, fmt.Errorf("platform IdP (thunder.public_url): %w", err)
	}
	idp := platformIDP{Kind: platformIDPKind(), Public: pub, InCluster: pub}
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
	return []string{
		"--set", "idp.kind=" + idp.Kind,
		"--set", "thunder.jwksURL=" + idp.InCluster.JWKSURL,
		"--set", "thunder.tokenURL=" + idp.InCluster.TokenURL,
		"--set", "aeStudio.idp.issuer=" + idp.Public.Issuer,
		"--set", "aeStudio.idp.jwksUrl=" + idp.InCluster.JWKSURL,
		"--set", "aeStudio.idp.tokenUrl=" + idp.InCluster.TokenURL,
	}
}
