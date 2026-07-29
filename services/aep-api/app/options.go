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

package app

import (
	"context"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/ocauth"
	"github.com/wso2/aep/aep-api/secretsprovider"
)

// Options are the injectable composition-root seams for Run.
// Every field's nil value is a feature off-switch (documented per field).
// Nil never panics and never silently degrades into a different credential path.
type Options struct {
	// AuthProvider attaches a bearer on AuthModeServiceM2M OC calls.
	// Nil = no bearer attached (feature off).
	AuthProvider ocauth.AuthProvider

	// RequestAuthStrategy decides credential class per OC request.
	// Nil = all-M2M / never pass-through (direct-OC default).
	RequestAuthStrategy ocauth.RequestAuthStrategy

	// ImpersonateOrgResolver sets X-Impersonate-Org on M2M calls.
	// Nil = no impersonation header (direct-OC default).
	ImpersonateOrgResolver func(ctx context.Context, namespace string) (string, error)

	// ImpersonateOrgResolverBuilder, when non-nil, is invoked after Resolve
	// opens the DB and before Assemble — late-binding for resolvers that need
	// infra. Ignored when ImpersonateOrgResolver is already set.
	ImpersonateOrgResolverBuilder func(db *gorm.DB) func(context.Context, string) (string, error)

	// SecretsProvider, when non-nil, is used instead of constructing the
	// default SM-API provider from SECRET_MANAGER_API_URL.
	// Nil = today's default construction (SM-API when URL configured).
	SecretsProvider secretsprovider.Provider
}
