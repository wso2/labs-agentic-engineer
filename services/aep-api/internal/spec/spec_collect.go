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

// spec_collect.go — OpenAPI spec fetch → validate → normalize → store pipeline.
//
// Surfaces three entry points:
//   - ValidateOpenAPI: parses and counts HTTP operations in an OpenAPI 3.x doc.
//   - FetchSpecFromURL: SSRF-guarded HTTPS GET for user-supplied spec URLs.
//   - (*ArtifactStore).StoreConsumedSpec: validate + normalize a consumed spec
//     and return the component-relative path it will live at. In the
//     committed-truth model this store has no single-file commit surface
//     (the spec save only reads + tags; all writes land via the Files API), so the
//     commit is deferred — see the spec-commit TODO in StoreConsumedSpec.

package spec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ---- ValidateOpenAPI -------------------------------------------------------

var httpMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

// ValidateOpenAPI parses an OpenAPI 3.x YAML/JSON document and returns the
// number of operations (method entries under paths). It errors if the doc is
// not OpenAPI 3.x or has no paths / operations.
func ValidateOpenAPI(raw []byte) (int, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return 0, fmt.Errorf("not valid YAML/JSON: %w", err)
	}
	ver, _ := doc["openapi"].(string)
	if !strings.HasPrefix(ver, "3.") {
		return 0, fmt.Errorf("not an OpenAPI 3.x document (openapi: %q)", ver)
	}
	paths, ok := doc["paths"].(map[string]any)
	if !ok || len(paths) == 0 {
		return 0, fmt.Errorf("OpenAPI document has no paths")
	}
	count := 0
	for _, item := range paths {
		ops, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for method := range ops {
			if httpMethods[strings.ToLower(method)] {
				count++
			}
		}
	}
	if count == 0 {
		return 0, fmt.Errorf("OpenAPI document has no operations")
	}
	return count, nil
}

// ---- FetchSpecFromURL ------------------------------------------------------

const (
	specFetchTimeout = 10 * time.Second
	specMaxBytes     = 5 << 20 // 5 MiB
)

// cgnatNet is the IANA Shared Address Space (RFC 6598, 100.64.0.0/10) used
// by carrier-grade NAT. It is not publicly routable and must be blocked to
// prevent SSRF via CGNAT-addressed hosts.
var cgnatNet = func() *net.IPNet {
	_, n, _ := net.ParseCIDR("100.64.0.0/10")
	return n
}()

// nat64Net is the IPv6 Well-Known Prefix for NAT64 (RFC 6052, 64:ff9b::/96).
// In a NAT64/DNS64 cluster a DNS64 resolver can synthesize a 64:ff9b:: AAAA for
// an attacker domain that NAT64 then routes to an embedded IPv4 — including the
// link-local cloud-metadata endpoint and the RFC1918 pod/service CIDR. None of
// Go's IsPrivate/IsLoopback/IsLinkLocalUnicast catch this prefix, so block it
// explicitly to close the metadata-SSRF-via-NAT64 vector.
var nat64Net = func() *net.IPNet {
	_, n, _ := net.ParseCIDR("64:ff9b::/96")
	return n
}()

// maxRedirects is the maximum number of redirects FetchSpecFromURL will follow.
const maxRedirects = 5

// FetchSpecFromURL GETs an OpenAPI spec from a user-supplied URL with SSRF
// guards: https only, public IPs only (no loopback/private/link-local/
// unspecified), size cap (5 MiB), timeout (10 s), redirect guard (https-only,
// max 5 hops), and TOCTOU-safe single-resolution dial (resolves the host
// exactly once and dials the validated IP directly — no second resolution).
//
// PLATFORM-TOUCHING — reviewed by platform-design-expert; do NOT weaken
// guards without a new review.
func FetchSpecFromURL(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, fmt.Errorf("spec URL must be an absolute https URL")
	}
	dialer := &net.Dialer{Timeout: specFetchTimeout}
	transport := &http.Transport{
		// DialContext resolves the host ONCE, validates every returned IP, then
		// dials the chosen IP directly — eliminating the TOCTOU DNS-rebinding
		// window that would exist if the dialer performed its own second lookup.
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("invalid address %q: %w", addr, err)
			}
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			if len(ips) == 0 {
				return nil, fmt.Errorf("no IP addresses resolved for %s", host)
			}
			// Validate every resolved IP; reject the entire set if any is non-public.
			// (Resolved IP is intentionally NOT echoed in the error — it would be a
			// blind-SSRF oracle leaking internal DNS results; log server-side instead.)
			for _, ip := range ips {
				if !ip.IsGlobalUnicast() || ip.IsPrivate() || cgnatNet.Contains(ip) || nat64Net.Contains(ip) {
					return nil, fmt.Errorf("refusing to fetch from non-public address")
				}
			}
			// Dial the first validated IP directly — no second DNS resolution.
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
		},
	}
	// CheckRedirect rejects any redirect whose target is not https and caps
	// the total number of redirects at maxRedirects. This closes the
	// https→http downgrade-via-redirect vector at the application layer.
	checkRedirect := func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("redirect to non-https URL %q is not allowed", req.URL)
		}
		if len(via) >= maxRedirects {
			return fmt.Errorf("too many redirects (max %d)", maxRedirects)
		}
		return nil
	}
	client := &http.Client{Timeout: specFetchTimeout, Transport: transport, CheckRedirect: checkRedirect}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/yaml, application/json, text/yaml, text/plain")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch spec: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("spec URL returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, specMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > specMaxBytes {
		return nil, fmt.Errorf("spec exceeds %d bytes", specMaxBytes)
	}
	return body, nil
}

// ErrInvalidSpecContent is a sentinel wrapped around validation-class errors
// from StoreConsumedSpec — depName path-traversal rejection and ValidateOpenAPI
// failures — so that callers can distinguish them from infrastructure errors
// (NormalizeOpenAPIYAML failures). A caller's HTTP handler maps this to a 400
// without importing artifacts internals.
var ErrInvalidSpecContent = errors.New("invalid spec content")

// ConsumedContractFile is the file name a collected OpenAPI document is stored
// under in the dependency's directory.
const ConsumedContractFile = "openapi.yaml"

// ConsumedSpecPath returns the REPO-relative path a collected OpenAPI spec for
// dependency depName lives at — the dependency's own directory,
// `specs/design/dependencies/<depName>/openapi.yaml`. It is what
// StoreConsumedSpec returns and what the dependency's definition records (as
// the bare file name, `contract`).
func ConsumedSpecPath(depName string) string {
	return ContractPath(depName, ConsumedContractFile)
}

// StoreConsumedSpec validates + normalizes rawSpec for the `depName` external
// dependency and returns the repo-relative contract path together with the
// normalized blob to commit.
//
// COMMITTED-TRUTH: this store has no single-file commit surface of its own —
// every file write lands via the Files API (feature/files, atomic apply →
// main). So StoreConsumedSpec does the validate+normalize half and hands the
// normalized blob back; the caller (design.CollectSpec) commits it — atomically
// with the dependency.json edit that records the contract and clears the
// needs-contract gate — through the Files commit port.
//
// Error classification:
//   - %w-wraps ErrInvalidSpecContent: depName path-traversal rejection +
//     ValidateOpenAPI failures (client/400).
//   - bare errors: NormalizeOpenAPIYAML failures (infra/500).
func (s *ArtifactStore) StoreConsumedSpec(ctx context.Context, orgID, projectID, component, depName string, rawSpec []byte) (specPath, normalized string, err error) {
	// Defense-in-depth: reject depName values that could escape the
	// dependencies/ directory via path traversal — belt-and-suspenders even
	// though depName is normally architect/catalog-controlled.
	if strings.Contains(depName, "/") || strings.Contains(depName, `\`) || strings.Contains(depName, "..") {
		return "", "", fmt.Errorf("%w: invalid dependency name %q: must not contain path separators or '..'", ErrInvalidSpecContent, depName)
	}
	if _, verr := ValidateOpenAPI(rawSpec); verr != nil {
		return "", "", fmt.Errorf("%w: %v", ErrInvalidSpecContent, verr)
	}
	normalized, err = NormalizeOpenAPIYAML(string(rawSpec))
	if err != nil {
		return "", "", fmt.Errorf("normalize spec: %w", err)
	}
	specPath = ConsumedSpecPath(depName)
	slog.DebugContext(ctx, "StoreConsumedSpec: validated + normalized consumed spec",
		"org", orgID, "project", projectID, "component", component, "dependency", depName, "specPath", specPath)
	return specPath, normalized, nil
}
