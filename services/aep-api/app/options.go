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

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
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

	// SecretsProvider is the write-only secrets delivery channel
	// (KV → SecretReference → ESO). OSS NewOSSOptions injects OpenBao-direct
	// when OPENBAO_ADDR is set; an overlay may inject its own provider.
	// Nil = secrets delivery off (degrade cleanly; no plaintext substitute).
	SecretsProvider secretsprovider.Provider

	// ResourceLabels are stamped on every OpenChoreo resource AEP writes, for
	// the platform in front of OpenChoreo rather than for AEP. wso2cloud needs
	// `cloud.wso2.com/product-name` on them: its platform API stamps the label
	// on a write made with a user's token but not on one made with the
	// impersonating service token, which is how every background write goes
	// out, and its build workflow will not render without it. Validated at boot
	// (an invalid key or value fails startup). Nil = none (local, direct OC).
	ResourceLabels map[string]string

	// openBaoAuth is the process's one OpenBao session (Kubernetes auth),
	// shared by the OpenBao-direct provider above and the environment Thunder
	// binding reader Assemble builds. Unexported: only NewOSSOptions sets it,
	// with the provider it authenticates. Nil = neither exists.
	openBaoAuth secrets.VaultAuth
}
