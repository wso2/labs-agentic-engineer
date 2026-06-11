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

package middleware

import (
	"context"
	"net/http"
	"strings"
)

type authTokenKey struct{}

type serviceIdentityKey struct{}

// ExtractAuthToken returns a middleware that reads the Bearer token from the
// Authorization header and stores it in the request context for downstream use.
func ExtractAuthToken() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			ctx := context.WithValue(r.Context(), authTokenKey{}, token)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetAuthToken retrieves the Bearer token stored in the context.
func GetAuthToken(ctx context.Context) string {
	if token, ok := ctx.Value(authTokenKey{}).(string); ok {
		return token
	}
	return ""
}

// WithAuthToken returns a copy of ctx that carries the given Bearer token under
// the user-token key. This key means "an inbound end-user JWT to forward"; do
// NOT use it to inject a service token (that collides with the user-JWT path in
// the OpenChoreo client and suppresses per-org impersonation). For orchestration
// / async OC calls use WithServiceIdentity instead.
func WithAuthToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, authTokenKey{}, token)
}

// WithServiceIdentity marks ctx as an orchestration / async call (dispatch,
// webhook handlers, status watchers) that must authenticate with the BFF's own
// service identity (M2M) and impersonate the target org — never a forwarded
// end-user JWT. The OpenChoreo client honours this by setting X-Impersonate-Org
// (derived from the namespace in the request URL) and attaching the M2M token,
// regardless of whether a user JWT also happens to be in ctx.
func WithServiceIdentity(ctx context.Context) context.Context {
	return context.WithValue(ctx, serviceIdentityKey{}, true)
}

// IsServiceIdentity reports whether ctx was marked by WithServiceIdentity.
func IsServiceIdentity(ctx context.Context) bool {
	v, _ := ctx.Value(serviceIdentityKey{}).(bool)
	return v
}
