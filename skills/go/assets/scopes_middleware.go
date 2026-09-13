/*
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

// Package auth enforces the OAuth2 scope each operation declares in openapi.yaml.
//
// Copied verbatim from the `go` skill. Exactly ONE line is yours to edit: the
// import of the generated package, below.
//
// This file compiles only INSIDE a generated app. It imports that app's
// internal/gen package (the oapi-codegen output), so as it sits in the skill
// library it belongs to no module and builds nowhere; `go build ./...` in this
// repository never sees it.
//
// The generated server is the only source of truth for "which scope does this
// operation need": oapi-codegen puts the operation's declared scopes on the
// request context under gen.Oauth2Scopes. Nothing here parses the spec.
//
// WIRING (this is not optional): register RequireScope in the generated
// server's Middlewares, never with chi's r.Use / mux-level wrapping —
//
//	h := gen.HandlerWithOptions(gen.NewStrictHandler(srv, nil), gen.ChiServerOptions{
//	        BaseRouter:  chi.NewRouter(),
//	        Middlewares: []gen.MiddlewareFunc{auth.RequireScope},
//	})
//
// A router-level middleware runs BEFORE the generated per-route wrapper writes
// the scopes onto the context, so it would see nothing and let everything past.
// That failure is silent and fully fail-open: every operation answers 200.
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	// EDIT THIS ONE LINE: your module path + /internal/gen (the oapi-codegen
	// output package). The scope context key's type is unexported there, so
	// this middleware must import it; there is no way to decouple.
	"MODULE_PATH/internal/gen"
)

// Caller is the verified end-user identity the API gateway put on the request.
type Caller struct {
	UserID string
	scopes map[string]struct{}
}

type callerKey struct{}

// RequireScope enforces the operation's declared scope. Three states, taken
// from the context the generated wrapper wrote:
//
//	absent      operation has `security: []` -> public, no identity read
//	empty slice operation inherits `security: [{oauth2: []}]` -> signed-in only
//	one scope   operation declares `security: [{oauth2: [handle]}]` -> scope required
func RequireScope(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		required, guarded := r.Context().Value(gen.Oauth2Scopes).([]string)
		if !guarded {
			next.ServeHTTP(w, r) // public operation
			return
		}
		if len(required) > 1 {
			// The spec gate refuses this upstream; if one ever reaches here,
			// fail closed rather than pick a scope.
			slog.Error("operation declares more than one scope, refusing",
				"method", r.Method, "path", r.URL.Path, "scopes", required)
			deny(w, http.StatusInternalServerError, "misconfigured",
				"operation declares more than one required scope", "")
			return
		}
		userID := strings.TrimSpace(r.Header.Get("X-User-Id"))
		if userID == "" {
			deny(w, http.StatusUnauthorized, "unauthorized", "no signed-in user", "")
			return
		}
		granted := scopeSet(r.Header.Get("X-User-Scopes"))
		if len(required) == 1 {
			if _, ok := granted[required[0]]; !ok {
				deny(w, http.StatusForbidden, "insufficient_scope",
					"this action requires the "+required[0]+" permission", required[0])
				return
			}
		}
		ctx := context.WithValue(r.Context(), callerKey{}, Caller{UserID: userID, scopes: granted})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// HasScope reports whether the caller holds handle. Use it for widening
// inside a handler (own rows vs. every row), never as the only check on an
// operation - the operation's own scope is RequireScope's job.
func HasScope(ctx context.Context, handle string) bool {
	c, ok := ctx.Value(callerKey{}).(Caller)
	if !ok {
		return false
	}
	_, held := c.scopes[handle]
	return held
}

// CallerOf returns the verified identity; ok is false on a public operation.
func CallerOf(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(callerKey{}).(Caller)
	return c, ok
}

// Scopes returns the caller's granted handles, in map order.
func (c Caller) Scopes() []string {
	out := make([]string, 0, len(c.scopes))
	for s := range c.scopes {
		out = append(out, s)
	}
	return out
}

func scopeSet(header string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, s := range strings.Fields(header) {
		set[s] = struct{}{}
	}
	return set
}

func deny(w http.ResponseWriter, status int, code, message, scope string) {
	if scope != "" {
		w.Header().Set("WWW-Authenticate",
			fmt.Sprintf("Bearer error=%q, scope=%q", code, scope))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "message": message})
}
