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

// Package oidc reads the platform IdP's endpoints from its OIDC discovery
// document, so no installer code knows an IdP's URL layout. The install
// fetches the document once, over the IdP's public URL, and writes the
// endpoints it names into the chart; an IdP reached on another address
// inside the cluster (Thunder's in-cluster Service) gets the same paths on
// that base through OnBase.
package oidc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Endpoints is what the install needs of an IdP: the issuer every token's
// `iss` equals, where its signing keys are, and where a client mints a
// token.
type Endpoints struct {
	Issuer   string
	JWKSURL  string
	TokenURL string
}

// Configured reports whether the endpoints were discovered: the zero value
// means no IdP is known, and a caller leaves the IdP values out of the chart
// rather than writing half of them.
func (e Endpoints) Configured() bool {
	return e.Issuer != "" && e.JWKSURL != "" && e.TokenURL != ""
}

// OnBase returns the same endpoints reached through another scheme, host
// and port: each keeps its path and query, and takes base's scheme, host
// and port, nothing else of it. The issuer is unchanged, since it names the
// IdP rather than an address. A base that is not an absolute URL, or
// endpoints that are not, return the endpoints as they are.
func (e Endpoints) OnBase(base string) Endpoints {
	b, err := url.Parse(strings.TrimRight(strings.TrimSpace(base), "/"))
	if err != nil || b.Scheme == "" || b.Host == "" {
		return e
	}
	rebase := func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return raw
		}
		u.Scheme, u.Host = b.Scheme, b.Host
		return u.String()
	}
	return Endpoints{Issuer: e.Issuer, JWKSURL: rebase(e.JWKSURL), TokenURL: rebase(e.TokenURL)}
}

// discoveryPath is where every OIDC provider publishes its configuration
// (OpenID Connect Discovery 1.0, §4).
const discoveryPath = "/.well-known/openid-configuration"

// maxDocumentBytes bounds the document read: a discovery document is a few
// kilobytes, and an endpoint answering more is not one.
const maxDocumentBytes = 1 << 20

type document struct {
	Issuer        string `json:"issuer"`
	JWKSURI       string `json:"jwks_uri"`
	TokenEndpoint string `json:"token_endpoint"`
}

// Discover fetches the discovery document of the IdP whose issuer is
// issuer and returns the endpoints it names. The document's own issuer
// must equal the configured one (a trailing slash aside), and it must name
// a JWKS URI and a token endpoint; anything else is an error naming the URL
// fetched and what was wrong, never the body. The caller's client decides
// the timeout.
func Discover(ctx context.Context, client *http.Client, issuer string) (Endpoints, error) {
	iss := strings.TrimRight(strings.TrimSpace(issuer), "/")
	if iss == "" {
		return Endpoints{}, fmt.Errorf("oidc discovery: the issuer URL is empty")
	}
	if u, err := url.Parse(iss); err != nil || u.Scheme == "" || u.Host == "" {
		return Endpoints{}, fmt.Errorf("oidc discovery: the issuer %q is not an absolute URL", issuer)
	}
	target := iss + discoveryPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return Endpoints{}, fmt.Errorf("oidc discovery: build request for %s: %w", target, err)
	}
	req.Header.Set("Accept", "application/json")
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return Endpoints{}, fmt.Errorf("oidc discovery: fetch %s: %w", target, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Endpoints{}, fmt.Errorf("oidc discovery: %s answered HTTP %d", target, resp.StatusCode)
	}
	var doc document
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxDocumentBytes)).Decode(&doc); err != nil {
		return Endpoints{}, fmt.Errorf("oidc discovery: %s is not a discovery document: %w", target, err)
	}
	if strings.TrimRight(doc.Issuer, "/") != iss {
		return Endpoints{}, fmt.Errorf("oidc discovery: %s names issuer %q, not the configured %q", target, doc.Issuer, iss)
	}
	if doc.JWKSURI == "" || doc.TokenEndpoint == "" {
		return Endpoints{}, fmt.Errorf("oidc discovery: %s names no jwks_uri or no token_endpoint", target)
	}
	return Endpoints{Issuer: doc.Issuer, JWKSURL: doc.JWKSURI, TokenURL: doc.TokenEndpoint}, nil
}
