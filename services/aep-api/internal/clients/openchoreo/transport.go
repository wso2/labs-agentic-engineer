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

package openchoreo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/wso2/aep/aep-api/internal/clients/httpx"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo/gen"
	"github.com/wso2/aep/aep-api/internal/clients/requests"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/ocauth"
)

// Config drives the OpenChoreo client construction.
type Config struct {
	BaseURL      string
	HostHeader   string
	// PreferPlainHTTPEndpoints picks the http external URL over the https one
	// when a ReleaseBinding advertises BOTH.
	//
	// OpenChoreo advertises a binding's external URLs from the endpoint's SHAPE
	// rather than from what its gateway serves, so a plane whose gateway does not
	// terminate TLS still advertises an https URL beside the http one — and only
	// the http one answers. Preferring https unconditionally therefore hands out
	// a dead URL there.
	//
	// That was survivable while the URL only reached a console link, and stopped
	// being survivable when the runner gained a reachability gate: it probes the
	// deployed endpoints BEFORE starting the agent and refuses to run when they
	// do not answer, so an unreachable advertised URL blocks VALIDATION rather
	// than merely rendering a bad link.
	//
	// Set at the composition root from config.PlatformAPIConfig.DataPlaneGatewayTLS
	// — the stated fact, not the deployment tier, which is the wrong one: a
	// dev-tier plane WITH TLS would be told to prefer plain http, the same bug
	// mirrored.
	PreferPlainHTTPEndpoints bool
	AuthProvider ocauth.AuthProvider
	RetryConfig  requests.RequestRetryConfig

	// RequestAuthStrategy selects the credential class per OC request.
	// nil means AuthModeServiceM2M (direct-OC / all-M2M off-switch: never
	// pass through an inbound user JWT).
	RequestAuthStrategy ocauth.RequestAuthStrategy

	// ImpersonateOrgResolver, when set, maps the namespace in a request URL
	// (".../namespaces/{namespace}/...") to the org UUID sent as the
	// X-Impersonate-Org header on M2M (service-token) requests, so platform-api
	// routes and bills the target org rather than the service identity's own.
	// Only consulted on AuthModeServiceM2M. nil disables the header (e.g. local
	// k3d, which talks to OpenChoreo directly and reads the namespace from the
	// URL path).
	ImpersonateOrgResolver func(ctx context.Context, namespace string) (string, error)
}

// newGenClient builds a *gen.ClientWithResponses with the three-layer
// transport stack:
//
//  1. httpx.WrapTransport (innermost) — stamps X-Correlation-ID for tracing
//  2. RetryableHTTPClient (middle) — jittered exponential backoff on
//     transient codes; 401 invalidates the cached service token and retries
//  3. RequestEditorFn (outermost, oapi-codegen hook) — sets Authorization,
//     Host, and X-Use-OpenAPI on every request
//
// Auth lives in the editor (not a RoundTripper) so the retry middleware sees
// a fresh token after invalidation: the editor runs on every attempt and
// re-calls AuthProvider.Token(), which returns the newly-fetched token after
// the 401 callback called Invalidate().
func newGenClient(cfg Config) (*gen.ClientWithResponses, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New("openchoreo: Config.BaseURL is required")
	}

	inner := &http.Client{Transport: httpx.WrapTransport(nil)}
	outer := requests.NewRetryableHTTPClient(inner, buildRetryConfig(cfg))

	c, err := gen.NewClientWithResponses(
		cfg.BaseURL,
		gen.WithHTTPClient(outer),
		gen.WithRequestEditorFn(authRequestEditor(cfg)),
	)
	if err != nil {
		return nil, fmt.Errorf("openchoreo: build gen client: %w", err)
	}
	return c, nil
}

// buildRetryConfig returns cfg.RetryConfig with the package's default
// RetryOnStatus wired in when the caller hasn't set one: invalidate the
// cached service token and retry on 401, otherwise fall back to the
// transient-status set. Shared by newGenClient and any hand-rolled REST
// client in this package (e.g. resource_client.go) so 401 semantics stay
// identical everywhere the BFF talks to OpenChoreo. We intentionally don't
// expose this as the requests package default — keeping it here ties the
// rule to its only valid caller (AuthProvider.Invalidate).
func buildRetryConfig(cfg Config) requests.RequestRetryConfig {
	retryCfg := cfg.RetryConfig
	if retryCfg.RetryOnStatus == nil {
		retryCfg.RetryOnStatus = func(status int) bool {
			if status == http.StatusUnauthorized {
				if cfg.AuthProvider != nil {
					slog.Info("openchoreo: 401, invalidating cached token and retrying")
					cfg.AuthProvider.Invalidate()
				}
				return true
			}
			return slices.Contains(requests.TransientHTTPErrorCodes, status)
		}
	}
	return retryCfg
}

// authRequestEditor returns the per-request auth hook every OC client in this
// package applies before issuing a request — the oapi-codegen
// RequestEditorFn for the generated client, and the equivalent hook
// hand-rolled REST clients (e.g. resource_client.go) call directly from
// their `do` method. Centralizing it here means the generated and
// hand-rolled clients can never drift on auth selection.
//
// Credential class comes from RequestAuthStrategy (nil → AuthModeServiceM2M).
// Same-class 401 retry stays in buildRetryConfig via AuthProvider.Invalidate();
// this editor never falls back across credential classes.
func authRequestEditor(cfg Config) func(ctx context.Context, req *http.Request) error {
	return func(ctx context.Context, req *http.Request) error {
		if cfg.HostHeader != "" {
			req.Host = cfg.HostHeader
		}
		req.Header.Set("X-Use-OpenAPI", "true")

		mode := ocauth.AuthModeServiceM2M
		if cfg.RequestAuthStrategy != nil {
			mode = cfg.RequestAuthStrategy.Decide(ctx)
		}

		switch mode {
		case ocauth.AuthModeUserJWT:
			userJWT := auth.GetAuthToken(ctx)
			if userJWT == "" {
				return fmt.Errorf("openchoreo: AuthModeUserJWT selected but no user JWT in context")
			}
			slog.DebugContext(ctx, "openchoreo: forwarding inbound user JWT",
				"method", req.Method, "path", req.URL.Path)
			req.Header.Set("Authorization", "Bearer "+userJWT)
			return nil

		case ocauth.AuthModeServiceM2M:
			if cfg.ImpersonateOrgResolver != nil {
				if ns := namespaceFromPath(req.URL.Path); ns != "" {
					orgUUID, err := cfg.ImpersonateOrgResolver(ctx, ns)
					if err != nil {
						return fmt.Errorf("openchoreo: resolve impersonation org for namespace %q: %w", ns, err)
					}
					if orgUUID != "" {
						req.Header.Set("X-Impersonate-Org", orgUUID)
						slog.DebugContext(ctx, "openchoreo: service-identity call — impersonating org",
							"namespace", ns, "orgUUID", orgUUID, "method", req.Method, "path", req.URL.Path,
							"explicitServiceIdentity", auth.IsServiceIdentity(ctx))
					} else {
						slog.DebugContext(ctx, "openchoreo: service-identity call — resolver returned no org, sending no impersonation header",
							"namespace", ns, "method", req.Method, "path", req.URL.Path)
					}
				} else {
					slog.DebugContext(ctx, "openchoreo: service-identity call — no namespace in path, sending no impersonation header",
						"method", req.Method, "path", req.URL.Path)
				}
			}

			if cfg.AuthProvider != nil {
				tok, err := cfg.AuthProvider.Token()
				if err != nil {
					return fmt.Errorf("openchoreo: service token fetch failed: %w", err)
				}
				if tok != "" {
					req.Header.Set("Authorization", "Bearer "+tok)
				}
			}
			return nil

		default:
			// AuthModeNone: no bearer.
			return nil
		}
	}
}

// namespaceFromPath extracts the {namespace} segment from an OpenChoreo REST
// path of the form ".../namespaces/{namespace}/...". Returns "" when the path
// has no namespace segment (e.g. the namespaces collection endpoint), where no
// single org applies.
func namespaceFromPath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		if s == "namespaces" && i+1 < len(segs) && segs[i+1] != "" {
			return segs[i+1]
		}
	}
	return ""
}
