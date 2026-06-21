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

// Package httpkit holds the shared HTTP response writers (§4.0). It exposes the
// Write40x helpers the central tenant gate uses; bodies are kept byte-identical
// to the utils.WriteErrorResponse payload by delegating to it, so no client/test
// sees a changed error shape.
package httpkit

import (
	"context"
	"net/http"

	"github.com/wso2/asdlc/asdlc-service/utils"
	"github.com/wso2/asdlc/asdlc-service/utils/validate"
)

// Write400 writes a 400 Bad Request with the given client-facing message.
func Write400(w http.ResponseWriter, msg string) {
	utils.WriteErrorResponse(w, http.StatusBadRequest, msg)
}

// Write401 writes a 401 Unauthorized.
func Write401(w http.ResponseWriter) {
	utils.WriteErrorResponse(w, http.StatusUnauthorized, "authentication required")
}

// Write404 writes a 404 Not Found. The gate uses the SAME body for wrong-org
// and no-such-org so cross-org existence is never leaked (§6.1a).
func Write404(w http.ResponseWriter, msg string) {
	utils.WriteErrorResponse(w, http.StatusNotFound, msg)
}

// Write500 writes a 500 Internal Server Error with a generic message.
func Write500(w http.ResponseWriter) {
	utils.WriteErrorResponse(w, http.StatusInternalServerError, "internal error")
}

// ActorFromContext extracts the requesting user's identifier from the
// JWT-authenticated context. Falls back to "unknown" when the JWT
// middleware didn't populate claims.
func ActorFromContext(ctx context.Context) string {
	if v := ctx.Value("user.sub"); v != nil { //nolint:revive — string-keyed legacy ctx
		if s, ok := v.(string); ok {
			return s
		}
	}
	return "unknown"
}

// RequireSlug validates a DNS-label-shaped slug path param. On failure it
// writes a 400 to w (with paramName surfaced in the message) and returns
// false; otherwise returns true. This is the single home of the
// validate.Slug + WriteErrorResponse logic that the per-package require*
// helpers delegate to.
func RequireSlug(w http.ResponseWriter, paramName, v string) bool {
	if err := validate.Slug(v); err != nil {
		Write400(w, paramName+": "+err.Error())
		return false
	}
	return true
}

// RequireUUID validates a canonical UUID path param. On failure it writes a
// 400 to w (with paramName surfaced in the message) and returns false;
// otherwise returns true.
func RequireUUID(w http.ResponseWriter, paramName, v string) bool {
	if err := validate.UUID(v); err != nil {
		Write400(w, paramName+": "+err.Error())
		return false
	}
	return true
}
