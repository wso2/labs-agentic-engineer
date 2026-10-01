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

// credential_lifecycle.go — read/teardown of the credential row:
// Status projection and the Disconnect cascade entry.

package organization

import (
	"context"
	"fmt"
	"log/slog"
)

// ----------------------------------------------------------------------------
// Status — GET /internal/credentials/orgs/{ocOrgId}
// ----------------------------------------------------------------------------

// Status returns the projection for ocOrgID. NotFoundError if no row exists.
func (s *CredentialService) Status(ctx context.Context, ocOrgID string) (*Projection, error) {
	row, err := s.fetchRow(ctx, ocOrgID)
	if err != nil {
		return nil, err
	}
	return projectionFromRow(row), nil
}

// ----------------------------------------------------------------------------
// Disconnect — DELETE /internal/credentials/orgs/{ocOrgId}
// ----------------------------------------------------------------------------

// Disconnect runs Phase D of the disconnect cascade (phase2.md §6.7):
// org-scoped advisory lock, status flip to 'disconnected', best-effort
// OpenBao GC of secret/aep/{ocOrgId}/{github,git}/*. Phases A/B/C live
// in the BFF (they need to enumerate ComponentTask rows that this service
// doesn't own).
//
// Idempotent: if the row is already 'disconnected' or absent, returns nil.
func (s *CredentialService) Disconnect(ctx context.Context, ocOrgID string) error {
	var row *OrgCredential
	err := s.repo.Tx(ctx, func(tx OrgCredentialTx) error {
		if err := tx.AdvisoryLock("org:" + ocOrgID); err != nil {
			return fmt.Errorf("disconnect: org lock: %w", err)
		}

		r, err := tx.GetByOrg(ocOrgID)
		if err != nil {
			return fmt.Errorf("disconnect: lookup: %w", err)
		}
		if r == nil {
			// No row — commit (releases the lock) and no-op.
			return nil
		}
		row = r

		// Status flip — already-disconnected is a no-op (200 idempotent).
		if r.Status != "disconnected" {
			if err := tx.UpdateColumns(ocOrgID, map[string]any{"status": "disconnected"}); err != nil {
				return fmt.Errorf("disconnect: status flip: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if row == nil {
		return nil
	}

	// Best-effort GC of credential-store keys. Failure is logged, not surfaced —
	// the periodic GC sweep catches anything missed.
	if row.Kind == "user-pat" {
		if err := s.store.Delete(ctx, ocOrgID, "github/pat"); err != nil {
			slog.WarnContext(ctx, "disconnect: cred-store delete failed", "ocOrgId", ocOrgID, "key", "github/pat", "error", err)
		}
	}

	// Drop every per-WorkflowRun build Secret in this org's WP namespace
	// so a disconnected org's tokens don't linger inside the cluster.
	// Best-effort.
	if s.buildSecretCleaner != nil {
		if err := s.buildSecretCleaner.DeleteBuildSecretsForOrg(ctx, ocOrgID); err != nil {
			slog.WarnContext(ctx, "disconnect: wp secret delete failed", "ocOrgId", ocOrgID, "error", err)
		}
	}

	slog.InfoContext(ctx, "credentials.disconnected", "ocOrgId", ocOrgID, "kind", row.Kind)
	return nil
}
