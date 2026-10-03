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

// Package kubeauth is how the hand-rolled Kubernetes API clients
// (clients/thunderapp) authenticate: the Authorization header
// from a static bearer or a service-account token file, and a TLS transport
// that trusts the cluster CA. Plain net/http, no client-go.
package kubeauth

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// Authorizer yields the Authorization header for a Kubernetes API request.
type Authorizer struct {
	bearer    string
	tokenFile string
}

// NewAuthorizer authenticates with bearer when set (the static KUBE_API_BEARER
// override), else with the token in tokenFile, re-read on every request so a
// projected service-account token rotation is picked up. Both empty sends no
// Authorization header.
func NewAuthorizer(bearer, tokenFile string) Authorizer {
	return Authorizer{bearer: bearer, tokenFile: tokenFile}
}

// Header returns the Authorization header value, or "" when there is none to
// send.
func (a Authorizer) Header() (string, error) {
	if a.bearer != "" {
		return "Bearer " + a.bearer, nil
	}
	if a.tokenFile == "" {
		return "", nil
	}
	b, err := os.ReadFile(a.tokenFile)
	if err != nil {
		return "", fmt.Errorf("read token file: %w", err)
	}
	tok := strings.TrimSpace(string(b))
	if tok == "" {
		return "", fmt.Errorf("token file is empty")
	}
	return "Bearer " + tok, nil
}

// Transport returns a transport that trusts the PEM bundle in caFile, or nil
// (the default transport, system roots) when caFile is empty. A set caFile
// that is unreadable, empty or not PEM is an error.
func Transport(caFile string) (http.RoundTripper, error) {
	if caFile == "" {
		return nil, nil
	}
	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read CA file %s: %w", caFile, err)
	}
	if len(pem) == 0 {
		return nil, fmt.Errorf("CA file %s is empty", caFile)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("CA file %s is not valid PEM", caFile)
	}
	return &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
	}, nil
}
