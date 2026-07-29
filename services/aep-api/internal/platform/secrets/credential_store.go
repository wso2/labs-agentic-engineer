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

package secrets

import (
	"context"
	"errors"
	"regexp"
	"strings"
)

// CredentialStore is the BFF-side read/write store for per-org credential
// material (GitHub PAT, Anthropic key, etc.). The wired implementation is
// Postgres AES-256-GCM (dbStore) — see NewDBStore.
//
// ocOrgID is mandatory on every method so callers cannot omit the tenant
// scope. Delivery of secrets into the dataplane (KV → SecretReference →
// ESO) is a separate seam (secretmanagersvc.Provider).
type CredentialStore interface {
	Get(ctx context.Context, ocOrgID, key string) ([]byte, error)
	Put(ctx context.Context, ocOrgID, key string, value []byte) error
	Delete(ctx context.Context, ocOrgID, key string) error
}

// ErrOrgIDInvalid is returned when an ocOrgID doesn't match the DNS-label
// shape, contains a leading underscore, or is the reserved "_platform"
// namespace.
var ErrOrgIDInvalid = errors.New("credential store: ocOrgID is invalid")

// ErrSecretNotFound is returned by Get when no value exists for the key.
var ErrSecretNotFound = errors.New("credential store: secret not found")

var orgIDValidator = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// validateOrgID enforces the per-org isolation rule on store keys.
// Handler-boundary validation rejects empty/malformed IDs as 400 long
// before they reach this function; the checks here are defensive.
func validateOrgID(ocOrgID string) error {
	if ocOrgID == "" {
		return ErrOrgIDInvalid
	}
	if ocOrgID == "_platform" || strings.HasPrefix(ocOrgID, "_") {
		return ErrOrgIDInvalid
	}
	if !orgIDValidator.MatchString(ocOrgID) {
		return ErrOrgIDInvalid
	}
	return nil
}
