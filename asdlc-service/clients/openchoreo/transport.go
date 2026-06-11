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

	"github.com/wso2/asdlc/asdlc-service/clients/httpx"
	"github.com/wso2/asdlc/asdlc-service/clients/openchoreo/gen"
	"github.com/wso2/asdlc/asdlc-service/clients/requests"
	"github.com/wso2/asdlc/asdlc-service/middleware"
)

// AuthProvider is the auth-token contract the OC client depends on. Lets us
// swap `*oauth.TokenProvider` (the only production impl) for a fake in
// tests without touching the oauth package. Method signatures intentionally
// match `*oauth.TokenProvider` so it satisfies the interface as-is.
type AuthProvider interface {
	Token() (string, error)
	Invalidate()
}

// Config drives the OpenChoreo client construction.
type Config struct {
	BaseURL      string
	HostHeader   string
	AuthProvider AuthProvider
	RetryConfig  requests.RequestRetryConfig

	// ImpersonateOrgResolver, when set, maps the namespace in a request URL
	// (".../namespaces/{namespace}/...") to the org UUID sent as the
	// X-Impersonate-Org header on M2M (service-token) requests, so platform-api
	// routes and bills the target org rather than the service identity's own.
	// Only consulted when no inbound user JWT is being forwarded. nil disables
	// the header (e.g. local k3d, which talks to OpenChoreo directly and reads
	// the namespace from the URL path).
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

	retryCfg := cfg.RetryConfig
	if retryCfg.RetryOnStatus == nil {
		// Default OC policy: invalidate token on 401, retry; otherwise fall
		// back to the transient-error set. We intentionally don't expose this
		// as the package default — passing it via Config keeps the rule next
		// to its only valid caller (AuthProvider.Invalidate).
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

	inner := &http.Client{Transport: httpx.WrapTransport(nil)}
	outer := requests.NewRetryableHTTPClient(inner, retryCfg)

	authEditor := func(ctx context.Context, req *http.Request) error {
		if cfg.HostHeader != "" {
			req.Host = cfg.HostHeader
		}
		req.Header.Set("X-Use-OpenAPI", "true")

		// Pick the credential for this call:
		//
		//   - User-facing call (an inbound user JWT is in ctx and the call is
		//     NOT marked service-identity): forward the user JWT. platform-api
		//     derives the org from the JWT's ouId; no impersonation header.
		//
		//   - Service-identity call (orchestration / async — dispatch, webhooks,
		//     watchers — marked via middleware.WithServiceIdentity, OR any call
		//     with no inbound user JWT): authenticate as the BFF's M2M service
		//     identity and impersonate the target org, taken from the namespace
		//     in the request URL, so platform-api routes and bills THAT org
		//     rather than the service identity's own (Admin) org.
		//
		// The explicit service-identity marker is what makes the orchestration
		// paths correct: they run inside the user's HTTP request (so a user JWT
		// is present in ctx), but they act on the org's behalf and must NOT
		// forward that user's JWT — otherwise the impersonation header is never
		// set and the write mis-routes to the wrong namespace.
		userJWT := middleware.GetAuthToken(ctx)
		useServiceIdentity := middleware.IsServiceIdentity(ctx) || userJWT == ""

		if !useServiceIdentity {
			slog.DebugContext(ctx, "openchoreo: forwarding inbound user JWT",
				"method", req.Method, "path", req.URL.Path)
			req.Header.Set("Authorization", "Bearer "+userJWT)
			return nil
		}

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
						"explicitServiceIdentity", middleware.IsServiceIdentity(ctx))
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
	}

	c, err := gen.NewClientWithResponses(
		cfg.BaseURL,
		gen.WithHTTPClient(outer),
		gen.WithRequestEditorFn(authEditor),
	)
	if err != nil {
		return nil, fmt.Errorf("openchoreo: build gen client: %w", err)
	}
	return c, nil
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
