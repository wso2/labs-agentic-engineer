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
//   - (*ArtifactStore).StoreConsumedSpec: validate + normalize + commit the spec
//     into the consumer component's dependencies/ directory.

package artifacts

import (
	"context"
	"fmt"
	"io"
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
func ValidateOpenAPI(raw string) (int, error) {
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
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

// FetchSpecFromURL GETs an OpenAPI spec from a user-supplied URL with SSRF
// guards: https only, public IPs only (no loopback/private/link-local/
// unspecified), size cap (5 MiB), and timeout (10 s).
//
// PLATFORM-TOUCHING — reviewed by platform-design-expert; do NOT weaken
// guards without a new review.
func FetchSpecFromURL(ctx context.Context, rawURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", fmt.Errorf("spec URL must be an absolute https URL")
	}
	dialer := &net.Dialer{Timeout: specFetchTimeout}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, _ := net.SplitHostPort(addr)
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
					return nil, fmt.Errorf("refusing to fetch from non-public address %s", ip)
				}
			}
			return dialer.DialContext(ctx, network, addr)
		},
	}
	client := &http.Client{Timeout: specFetchTimeout, Transport: transport}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/yaml, application/json, text/yaml, text/plain")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch spec: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("spec URL returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, specMaxBytes+1))
	if err != nil {
		return "", err
	}
	if len(body) > specMaxBytes {
		return "", fmt.Errorf("spec exceeds %d bytes", specMaxBytes)
	}
	return string(body), nil
}

// ---- StoreConsumedSpec -----------------------------------------------------

// StoreConsumedSpec validates + normalizes rawSpec and commits it to the
// consumer component's dependencies/ directory at:
//
//	specs/design/components/<component>/dependencies/<depName>.openapi.yaml
//
// Returns the component-relative specPath ("dependencies/<depName>.openapi.yaml")
// and the operation count. Commits via ArtifactService.CommitDesignFile (no
// new version tag — same untagged commit path as SetComponentOrgPublished in
// P3.5). subPath passed to CommitDesignFile is relative to specs/design/.
func (s *ArtifactStore) StoreConsumedSpec(ctx context.Context, orgID, projectID, component, depName, rawSpec string) (string, int, error) {
	opCount, err := ValidateOpenAPI(rawSpec)
	if err != nil {
		return "", 0, err
	}
	normalized, err := NormalizeOpenAPIYAML(rawSpec)
	if err != nil {
		return "", 0, fmt.Errorf("normalize spec: %w", err)
	}
	// specPath is component-relative (returned to callers / stored in dependency.specPath).
	specPath := "dependencies/" + depName + ".openapi.yaml"
	// subPath is relative to specs/design/ — CommitDesignFile prepends DesignDir internally.
	subPath := componentDirPrefix + component + "/" + specPath
	msg := fmt.Sprintf("chore(marketplace): store consumed API spec for %s/%s", component, depName)
	if _, cerr := s.artifactSvc.CommitDesignFile(ctx, orgID, projectID, subPath, normalized, msg); cerr != nil {
		return "", 0, cerr
	}
	return specPath, opCount, nil
}
