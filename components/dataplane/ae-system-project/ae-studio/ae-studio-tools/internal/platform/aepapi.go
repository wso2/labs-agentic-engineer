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

package platform

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/gen/aepapi"
)

const (
	// aepAPITimeout bounds one aep-api call, retry included.
	aepAPITimeout = 15 * time.Second
	// internalPrefix is the contract's server URL (packages/contracts/api/internal/v1).
	internalPrefix = "/internal/v1"
)

// NewAEPAPI returns the generated aep-api client for baseURL, aep-api's root
// (AEP_API_BASE_URL, e.g. http://aep-api.<ns>.svc.cluster.local:9090; the
// /internal/v1 prefix is added here), authenticated with tok's
// bearer token. A 401 invalidates the token, mints a fresh one and retries
// the request once; a second 401 is returned to the caller.
//
//deadcode:keep wired in Task 2.7
func NewAEPAPI(baseURL string, tok *ClientCredentials) (*aepapi.ClientWithResponses, error) {
	hc := &http.Client{Timeout: aepAPITimeout, Transport: &bearerTransport{tok: tok, next: http.DefaultTransport}}
	c, err := aepapi.NewClientWithResponses(strings.TrimRight(baseURL, "/")+internalPrefix, aepapi.WithHTTPClient(hc))
	if err != nil {
		return nil, fmt.Errorf("aep-api client: %w", err)
	}
	return c, nil
}

// bearerTransport sets the Authorization header from a ClientCredentials
// token and retries once on 401 with a freshly minted one.
type bearerTransport struct {
	tok  *ClientCredentials
	next http.RoundTripper
}

//deadcode:keep wired in Task 2.7
func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.send(req)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}
	// A body that cannot be replayed cannot be retried.
	if req.Body != nil && req.Body != http.NoBody && req.GetBody == nil {
		return resp, nil
	}
	_ = resp.Body.Close()
	t.tok.Invalidate()
	retry := req
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, fmt.Errorf("replay request body: %w", err)
		}
		retry = req.Clone(req.Context())
		retry.Body = body
	}
	return t.send(retry)
}

// send clones req (a RoundTripper must not modify its request), adds the
// bearer token and sends it.
//
//deadcode:keep wired in Task 2.7
func (t *bearerTransport) send(req *http.Request) (*http.Response, error) {
	token, err := t.tok.Token(req.Context())
	if err != nil {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, err
	}
	out := req.Clone(req.Context())
	out.Header.Set("Authorization", "Bearer "+token)
	return t.next.RoundTrip(out)
}
