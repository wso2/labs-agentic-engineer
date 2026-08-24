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

package ocauth

import (
	"context"

	"github.com/wso2/aep/aep-api/internal/platform/auth"
)

// Claims is the public projection of the verified inbound JWT for overlay
// strategies and impersonation resolvers.
type Claims struct {
	Subject  string
	ClientID string
	OuHandle string
	OuName   string
	OuId     string
}

// IsServiceIdentity reports whether ctx was marked as an orchestration / async
// call that must authenticate with the BFF's own service identity (M2M).
// Consulted by UserJWTStrategy (app/strategy_user_jwt.go) and any overlay
// strategy.
func IsServiceIdentity(ctx context.Context) bool {
	return auth.IsServiceIdentity(ctx)
}

// GetAuthToken retrieves the inbound Bearer token stored in the context.
// Consulted by UserJWTStrategy (app/strategy_user_jwt.go) and any overlay
// strategy.
func GetAuthToken(ctx context.Context) string {
	return auth.GetAuthToken(ctx)
}

// WithAuthToken stores an inbound Bearer token in context (test / overlay helpers).
//
//deadcode:keep public overlay seam — private PAS tests/helpers import this; OSS main does not call it.
func WithAuthToken(ctx context.Context, token string) context.Context {
	return auth.WithAuthToken(ctx, token)
}

// WithServiceIdentity marks ctx as an orchestration / async call that must use
// the BFF's own service identity (M2M), even when a user JWT is also present.
//
//deadcode:keep public overlay seam — private PAS tests/helpers import this; OSS main does not call it.
func WithServiceIdentity(ctx context.Context) context.Context {
	return auth.WithServiceIdentity(ctx)
}

// ClaimsFromContext retrieves the verified JWT claims stored in context.
//
//deadcode:keep public overlay seam — private PAS impersonation resolver imports this; OSS main does not call it.
func ClaimsFromContext(ctx context.Context) *Claims {
	c := auth.ClaimsFromContext(ctx)
	if c == nil {
		return nil
	}
	return &Claims{
		Subject:  c.Subject,
		ClientID: c.ClientID,
		OuHandle: c.OuHandle,
		OuName:   c.OuName,
		OuId:     c.OuId,
	}
}

// ResolveOuHandle returns the canonical OC org handle from verified claims,
// preferring ouHandle over ouName over ouId. Returns "" when none are set.
//
//deadcode:keep public overlay seam — private PAS impersonation resolver imports this; OSS main does not call it.
func ResolveOuHandle(c *Claims) string {
	if c == nil {
		return ""
	}
	return auth.ResolveOuHandle(&auth.Claims{
		Subject:  c.Subject,
		ClientID: c.ClientID,
		OuHandle: c.OuHandle,
		OuName:   c.OuName,
		OuId:     c.OuId,
	})
}
