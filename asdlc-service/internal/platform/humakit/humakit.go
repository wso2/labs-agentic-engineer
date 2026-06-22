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

// Package humakit holds the shared Huma building blocks for the BFF's
// code-first OpenAPI surface (docs/design/bff-openapi-huma-migration.md): the
// org-scoped tenant gate expressed as an embeddable input + Resolver, the
// security-requirement constants, and the sentinel→RFC 9457 error mapper.
//
// The Huma API is mounted on the apiMux that is already wrapped by the JWT +
// orgensure middleware, so operations inherit user-JWT verification and JIT org
// provisioning. This package only adds the per-route tenant check that
// tenant.BindUserOrg used to apply as leaf-wrapping middleware.
package humakit

import (
	"log/slog"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wso2/asdlc/asdlc-service/internal/platform/ids"
	"github.com/wso2/asdlc/asdlc-service/internal/platform/tenant"
	"github.com/wso2/asdlc/asdlc-service/middleware/jwt"
)

// gateMode is the process-wide tenant gate mode (ENFORCE by default — mirrors
// tenant.ParseGateMode). Set once at startup via SetGateMode.
var gateMode = tenant.GateModeEnforce

// SetGateMode configures the tenant gate mode used by OrgScopedInput.Resolve.
// Pass tenant.ParseGateMode(cfg.TenantGateMode) at composition time.
func SetGateMode(m tenant.GateMode) { gateMode = m }

// SecurityUserJWT is the OpenAPI security requirement for end-user Thunder JWT
// routes. Attach to every org-scoped + user-facing carve-out operation.
var SecurityUserJWT = []map[string][]string{{"userJWT": {}}}

// SecurityTaskJWT is the security requirement for runner-facing per-task JWT
// routes.
var SecurityTaskJWT = []map[string][]string{{"taskJWT": {}}}

// OrgScopedInput is embedded by every org-scoped operation's input struct. The
// {orgHandle} path parameter it declares makes the operation org-scoped, and
// its Resolve method IS the tenant gate (the IDOR fence): an org-scoped
// operation is gated by construction simply by embedding this struct. This is
// the Huma-native replacement for the per-route tenant.BindUserOrg wrap, and
// the arch-lock test asserts every org-scoped operation embeds it.
type OrgScopedInput struct {
	OrgHandle string `path:"orgHandle" doc:"Organization handle; must match the caller's verified JWT org"`
}

var _ huma.Resolver = (*OrgScopedInput)(nil)

// Resolve enforces the tenant gate. It reads the verified JWT claims from the
// request context (populated by the upstream jwt middleware) and compares the
// resolved token org to the {orgHandle} path value, returning a 404 on mismatch
// (enforce mode) with the same body as no-such-org so cross-org existence is
// never leaked. In log mode it emits the would-deny canary and passes through.
func (i *OrgScopedInput) Resolve(ctx huma.Context) []error {
	c := ctx.Context()
	claims := jwt.ClaimsFromContext(c)
	tokenOrg := jwt.ResolveOuHandle(claims)
	pathOrg := i.OrgHandle

	// Malformed path identifier is a 400 in both modes (a syntactic error, not
	// a cross-org existence question).
	if pathOrg != "" {
		if err := ids.Slug(pathOrg); err != nil {
			return []error{huma.Error400BadRequest("orgHandle: " + err.Error())}
		}
	}

	if tokenOrg == "" {
		slog.WarnContext(c, "[SHAKEOUT:would-deny]",
			"reason", "no-org-claim", "mode", string(gateMode), "pathOrg", pathOrg)
		if gateMode == tenant.GateModeEnforce {
			return []error{huma.Error401Unauthorized("authentication required")}
		}
		return nil
	}

	if pathOrg != "" && !strings.EqualFold(tokenOrg, pathOrg) {
		slog.WarnContext(c, "[SHAKEOUT:would-deny]",
			"reason", "org-mismatch", "mode", string(gateMode),
			"tokenOrg", tokenOrg, "pathOrg", pathOrg)
		if gateMode == tenant.GateModeEnforce {
			return []error{huma.Error404NotFound("organization not found")} // closes IDOR-1..5
		}
	}
	return nil
}

// ErrorFromStatus maps an HTTP status code to the matching Huma error, so
// handlers can translate sentinel-classified statuses (e.g. OpenChoreo
// pass-through) into RFC 9457 problem responses.
func ErrorFromStatus(status int, msg string) error {
	switch status {
	case 400:
		return huma.Error400BadRequest(msg)
	case 401:
		return huma.Error401Unauthorized(msg)
	case 403:
		return huma.Error403Forbidden(msg)
	case 404:
		return huma.Error404NotFound(msg)
	case 409:
		return huma.Error409Conflict(msg)
	default:
		return huma.Error500InternalServerError(msg)
	}
}
