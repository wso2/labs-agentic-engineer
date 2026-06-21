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

// Package tenant is the pure (gorm-free) tenancy kernel. It owns the universal
// tenant key (OrgHandle), the authorized Caller produced only by the auth
// middleware, the Source provenance enum, and the central per-route gate
// (gate.go). Keeping it gorm-free lets HTTP/middleware read a Caller without
// dragging the database layer in. See docs/design/asdlc-service-modularization.md
// §6.1.
package tenant

import (
	"context"
	"errors"
	"strings"
)

// OrgHandle is the universal tenant key. It is the OC org handle and the value
// stored in every tenant table's org column (the "oc_org_id"/"org_id" columns
// all hold this). It is NOT the Thunder ouId/UUID.
type OrgHandle string

// ProjectID identifies a project within an org. It is always paired with an
// OrgHandle; it is not a tenant identifier on its own.
type ProjectID string

// Source records how a Caller's org was established, so non-user paths
// (S2S/webhook/runner) can assert their provenance.
type Source uint8

const (
	// SourceUserJWT — a verified end-user Thunder JWT; org from the ouHandle
	// claim, matched against the path (BindUserOrg).
	SourceUserJWT Source = iota
	// SourceServiceJWT — the platform-wide Service JWT (no org claim); org is
	// bound from a resolved row, providing scoping/observability not authz.
	SourceServiceJWT
	// SourceTaskJWT — a per-task RS256 bearer; org from the task row.
	SourceTaskJWT
	// SourcePublisherCC — a publisher client-credentials token.
	SourcePublisherCC
	// SourceWebhookHMAC — an HMAC-verified inbound webhook; org from the
	// routing key.
	SourceWebhookHMAC
	// SourceServiceIdentity — the BFF's own M2M identity for sweeps/dispatch.
	SourceServiceIdentity
)

func (s Source) String() string {
	switch s {
	case SourceUserJWT:
		return "user-jwt"
	case SourceServiceJWT:
		return "service-jwt"
	case SourceTaskJWT:
		return "task-jwt"
	case SourcePublisherCC:
		return "publisher-cc"
	case SourceWebhookHMAC:
		return "webhook-hmac"
	case SourceServiceIdentity:
		return "service-identity"
	default:
		return "unknown"
	}
}

// Caller is the authorized tenant context. It is produced ONLY by the auth
// middleware (the Bind* gates) and read by downstream handlers/repositories.
type Caller struct {
	// Org is the universal tenant key — never trust a path/body org without a
	// gate that set this.
	Org OrgHandle
	// ThunderUUID is the Thunder ouId, carried only for impersonation
	// (X-Impersonate-Org) and wc- namespace derivation; never a tenant column.
	ThunderUUID string
	Subject     string
	Source      Source
}

var (
	// ErrNoScope is returned when no Caller is present in context (deny, never
	// fall back to a global/all-org scope).
	ErrNoScope = errors.New("no tenant scope in context")
	// ErrOrgMismatch is returned when a path org does not match the Caller's
	// org. Surfaced to clients as a 404 (no existence leak).
	ErrOrgMismatch = errors.New("organization not found")
)

type callerCtxKey struct{}

// With returns a copy of ctx carrying the Caller.
func With(ctx context.Context, c Caller) context.Context {
	return context.WithValue(ctx, callerCtxKey{}, c)
}

// FromContext returns the Caller established by a Bind* gate, if any.
func FromContext(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(callerCtxKey{}).(Caller)
	return c, ok
}

// MustOrg returns the Caller's org or an error. It never returns an empty/
// global scope — a missing scope is a deny.
func MustOrg(ctx context.Context) (OrgHandle, error) {
	c, ok := FromContext(ctx)
	if !ok || c.Org == "" {
		return "", ErrNoScope
	}
	return c.Org, nil
}

// Scope is the seam that closes the systemic IDOR: it returns the caller's org
// only when it matches the path org (case-insensitively). Wrong-org and
// no-scope are deliberately indistinguishable to the caller (same error → same
// 404 body upstream).
func Scope(c Caller, pathOrg string) (OrgHandle, error) {
	if c.Org == "" {
		return "", ErrNoScope
	}
	if !strings.EqualFold(string(c.Org), pathOrg) {
		return "", ErrOrgMismatch
	}
	return c.Org, nil
}

// Resolver is the consumer-defined org port. The gate does not require it (a
// pure claim-vs-path check closes the IDOR class); it is defined here so the
// organization feature can satisfy it and JIT-ensure can be wired through the
// gate.
type Resolver interface {
	EnsureForOuHandle(ctx context.Context, ouHandle string, thunderOrgUUID string) error
}
