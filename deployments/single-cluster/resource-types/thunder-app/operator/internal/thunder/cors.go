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

package thunder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
)

// corsConfigPath is ThunderID's runtime endpoint for the server-wide `cors`
// configuration. GET returns all three layers, PUT replaces the writable one.
const corsConfigPath = "/server-config/cors"

// corsConfig is ThunderID's GET /server-config/cors response.
//
// CORS on ThunderID is ONE server-wide setting with two layers:
//
//	readOnly — declared by bootstrap documents imported at install time. On a
//	           platform Thunder (T1) that is the composed allow-list in
//	           deployments/single-cluster/thunder-resources/89-platform-cors-config.yaml;
//	           on an environment Thunder (T2) it is empty, because AEP publishes
//	           no `cors` document there.
//	writable — set at runtime through PUT, and owned by this operator (see
//	           SetBrowserOrigins).
//	merged   — the union of the two, and the list the server actually enforces.
//
// The split is what makes runtime registration safe: writing the writable layer
// cannot clobber the platform-composed bootstrap singleton, which is the hazard
// deployments/scripts/verify-convergence.sh check 13 exists to catch.
type corsConfig struct {
	ReadOnly corsLayer `json:"readOnly"`
	Writable corsLayer `json:"writable"`
	Merged   corsLayer `json:"merged"`
}

type corsLayer struct {
	AllowedOrigins []string `json:"allowedOrigins"`
}

// SetBrowserOrigins makes `origins` exactly the writable CORS layer of this
// Thunder instance.
//
// Why the operator does this at all: an app provisioned through a
// ThunderApplication is a BROWSER client of the environment's IdP. Its very
// first call is a cross-origin fetch of /.well-known/openid-configuration, and
// the token exchange that follows is a cross-origin POST. With no
// Access-Control-Allow-Origin on those responses the browser refuses to hand
// them to the page, so the app hangs on its loading screen while curl against
// the identical URL returns a healthy 200. Nothing else in the platform knows
// the app's origin: the redirect URIs on the CR are the only place it appears.
//
// Replace, not merge, and deliberately so. The caller passes the union of the
// origins of every ThunderApplication on this instance, which makes the layer a
// projection of the CRs rather than an append-only list — deletes converge, and
// so does any drift (a manual edit, a restored database, a re-imported bundle).
// The consequence is that the writable layer belongs to this operator on every
// Thunder it manages, and anything else writing origins there will have them
// removed on the next reconcile. The readOnly layer is never touched, so a
// publisher that needs a permanent origin declares it in a bootstrap document,
// which is where the platform's own origins already live.
func (c *client) SetBrowserOrigins(ctx context.Context, origins []string) error {
	want := normalizeOrigins(origins)

	c.muCORS.Lock()
	defer c.muCORS.Unlock()

	token, err := c.getSystemToken(ctx)
	if err != nil {
		return err
	}
	current, err := c.getCORS(ctx, token)
	if err != nil {
		return err
	}
	if slices.Equal(normalizeOrigins(current.Writable.AllowedOrigins), want) {
		return nil
	}
	return c.putCORSWritable(ctx, token, want)
}

func (c *client) getCORS(ctx context.Context, token string) (corsConfig, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+corsConfigPath, nil)
	if err != nil {
		return corsConfig{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return corsConfig{}, fmt.Errorf("thunder get cors: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return corsConfig{}, fmt.Errorf("thunder get cors returned %d: %s", resp.StatusCode, string(body))
	}
	var out corsConfig
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return corsConfig{}, fmt.Errorf("thunder get cors decode: %w", err)
	}
	return out, nil
}

func (c *client) putCORSWritable(ctx context.Context, token string, origins []string) error {
	// A nil slice marshals to `null`, and ThunderID rejects that with
	// "cors: allowedOrigins must be a list, not null". An instance whose last
	// app was deleted must still be able to reach the empty list.
	if origins == nil {
		origins = []string{}
	}
	body, err := json.Marshal(corsLayer{AllowedOrigins: origins})
	if err != nil {
		return fmt.Errorf("thunder put cors marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+corsConfigPath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("thunder put cors: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		rb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("thunder put cors returned %d: %s", resp.StatusCode, string(rb))
	}
	return nil
}

// OriginOf reduces a redirect URI to the origin a browser sends in the Origin
// header: scheme + host + non-default port, no path. It returns "" for anything
// that cannot be one — a relative URI, a custom scheme (a native app's
// myapp://callback), or an unparseable string — so a CR carrying a redirect URI
// no browser could ever send does not widen the allow-list.
func OriginOf(redirectURI string) string {
	u, err := url.Parse(strings.TrimSpace(redirectURI))
	if err != nil || u.Host == "" {
		return ""
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// normalizeOrigins de-duplicates, drops empties and sorts, so that two calls
// describing the same set produce the same list and SetBrowserOrigins can skip
// the write. Sorted rather than insertion-ordered because the order carries no
// meaning to Thunder and a stable order is what makes the comparison cheap.
func normalizeOrigins(origins []string) []string {
	seen := make(map[string]struct{}, len(origins))
	out := make([]string, 0, len(origins))
	for _, o := range origins {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		if _, dup := seen[o]; dup {
			continue
		}
		seen[o] = struct{}{}
		out = append(out, o)
	}
	sort.Strings(out)
	return out
}
