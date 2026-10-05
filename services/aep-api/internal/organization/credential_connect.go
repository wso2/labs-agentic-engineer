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

// credential_connect.go — the Connect/replace flow: kind dispatch, the PAT
// path (validate + record the connection row + seed webhook secret), and the
// PAT's reference write the submit runs after it. The PAT itself lives only
// in vault, behind its github-pat reference.

package organization

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Connect creates or replaces the credential record for ocOrgID. The one kind
// is "user-pat": it runs the full validation chain (GET /user, membership
// probe, repo-read probe); any other kind is a kind_invalid ValidationError.
//
// 409 (ConflictError) if an existing ACTIVE row is a different kind (the
// connect-time mode is fixed; disconnect before switching kind).
//
// 400 (ValidationError) for any GitHub-side validation failure — wrapped
// with a cause code that the UI maps to a specific error message.
func (s *CredentialService) Connect(ctx context.Context, ocOrgID string, req ConnectRequest) (*Projection, error) {
	// finalize carries the post-commit work (projection re-fetch, success
	// logging) for the chosen kind. It runs AFTER repo.Tx commits and
	// releases the advisory lock.
	var finalize func() (*Projection, error)
	err := s.repo.Tx(ctx, func(tx OrgCredentialTx) error {
		// Acquire the org-scoped advisory lock for the duration of the txn so
		// two concurrent connects (or a connect and a disconnect) for the org
		// can't race the INSERT/UPDATE.
		if err := tx.AdvisoryLock("org:" + ocOrgID); err != nil {
			return fmt.Errorf("connect: org lock: %w", err)
		}

		existing, err := tx.GetByOrg(ocOrgID)
		if err != nil {
			return fmt.Errorf("connect: lookup existing: %w", err)
		}
		hadRow := existing != nil

		if hadRow && existing.Status == "active" && existing.Kind != req.Kind {
			return &ConflictError{Reason: fmt.Sprintf("active %s connection exists; disconnect before connecting %s", existing.Kind, req.Kind)}
		}

		switch req.Kind {
		case "user-pat":
			fn, err := s.connectPAT(ctx, tx, ocOrgID, hadRow, existing, req)
			if err != nil {
				return err
			}
			finalize = fn
			return nil
		default:
			return &ValidationError{Code: "kind_invalid", Message: fmt.Sprintf("unknown kind %q", req.Kind)}
		}
	})
	if err != nil {
		return nil, err
	}
	return finalize()
}

// connectPAT runs inside Connect's transaction (the org advisory lock is held).
// It does GitHub validation + the row write, then returns the finalize closure Connect calls AFTER the commit: the post-commit
// projection re-fetch (REPLACE) and the success log. The PAT is not stored
// here: the caller writes it to vault once per submit (WritePATRef).
func (s *CredentialService) connectPAT(ctx context.Context, tx OrgCredentialTx, ocOrgID string, hadRow bool, existing *OrgCredential, req ConnectRequest) (func() (*Projection, error), error) {
	identity, err := s.validatePAT(ctx, req.PAT, req.GitHubLogin)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()

	if !hadRow {
		// CREATE — seed webhook_secrets with a random value. Nothing verifies
		// against it any more (the AE Studio pod owns delivery verification);
		// the secrets_shape_per_kind CHECK still wants a non-empty list for a
		// user-pat row, until the column is dropped (phase 6).
		secret, err := generateRandomHex(32)
		if err != nil {
			return nil, fmt.Errorf("connect: gen webhook secret: %w", err)
		}
		row := OrgCredential{
			OcOrgID:         ocOrgID,
			Kind:            "user-pat",
			GitHubLogin:     req.GitHubLogin,
			IdentityName:    identity.Name,
			IdentityEmail:   identity.Email,
			IdentityLogin:   identity.Login,
			Status:          "active",
			ConnectedAt:     now,
			LastValidatedAt: &now,
			WebhookSecrets: WebhookSecrets{
				{Secret: secret, AddedAt: now},
			},
		}
		if err := tx.Create(&row); err != nil {
			return nil, fmt.Errorf("connect: insert: %w", err)
		}
		return func() (*Projection, error) {
			slog.InfoContext(ctx, "secrets.connected", "ocOrgId", ocOrgID, "kind", "user-pat", "identityLogin", identity.Login)
			return projectionFromRow(&row), nil
		}, nil
	}

	// REPLACE — preserve webhook_secrets, possibly record identity drift.
	// Cross-mode reconnect (after disconnect): also flip `kind`, clear App-only
	// columns (installation_id, selected_repos), and seed webhook_secrets if
	// the prior row was App-mode (which has webhook_secrets=NULL per the
	// CHECK constraint).
	updates := map[string]any{
		"kind":              "user-pat",
		"github_login":      req.GitHubLogin,
		"identity_name":     identity.Name,
		"identity_email":    identity.Email,
		"identity_login":    identity.Login,
		"installation_id":   nil,
		"selected_repos":    nil,
		"last_validated_at": now,
		"status":            "active",
	}
	if identity.Login != existing.IdentityLogin {
		// Identity drift — record prev_identity_login + identity_changed_at
		// per phase2.md §6.6.
		prev := existing.IdentityLogin
		updates["prev_identity_login"] = &prev
		updates["identity_changed_at"] = now
	}
	// If switching from App → PAT, the prior row had webhook_secrets=NULL
	// (the secrets_shape_per_kind CHECK requires NOT NULL with array_length>=1
	// for user-pat). Seed a random value to keep the constraint satisfied.
	if existing.Kind == "app-installation" {
		secret, sErr := generateRandomHex(32)
		if sErr != nil {
			return nil, fmt.Errorf("connect: generate webhook secret: %w", sErr)
		}
		updates["webhook_secrets"] = WebhookSecrets{{Secret: secret, AddedAt: now}}
	}
	if err := tx.UpdateColumns(ocOrgID, updates); err != nil {
		return nil, fmt.Errorf("connect: update: %w", err)
	}
	return func() (*Projection, error) {
		// Reload for accurate projection.
		row, err := s.fetchRow(ctx, ocOrgID)
		if err != nil {
			return nil, err
		}
		slog.InfoContext(ctx, "secrets.replaced", "ocOrgId", ocOrgID, "kind", "user-pat", "identityLogin", identity.Login, "drift", identity.Login != existing.IdentityLogin)
		return projectionFromRow(row), nil
	}, nil
}

// validatePAT runs the full PAT validation chain (phase2.md §6.5) WITHOUT
// persisting: required-field checks, the GET /user identity fetch, the org
// membership probe, and the best-effort repo-read probe. It returns the
// resolved GitHub identity so connectPAT can persist it. Extracted so the
// probe-only public seam (ValidatePAT) and the connect path share one chain.
func (s *CredentialService) validatePAT(ctx context.Context, pat, githubLogin string) (*ghIdentity, error) {
	if pat == "" {
		return nil, &ValidationError{Code: "pat_missing", Message: "PAT is required"}
	}
	if githubLogin == "" {
		return nil, &ValidationError{Code: "github_login_missing", Message: "githubLogin is required"}
	}
	identity, err := s.fetchPATIdentity(ctx, pat)
	if err != nil {
		return nil, err
	}
	if err := s.validatePATMembership(ctx, pat, githubLogin, identity.Login); err != nil {
		return nil, err
	}
	// Repo-read probe is best-effort: if no repos exist under githubLogin
	// yet, skip the probe; first real repo create surfaces failure.
	if err := s.probePATRepoRead(ctx, pat, githubLogin); err != nil {
		return nil, err
	}
	return identity, nil
}

// ValidatePAT is the probe-only seam (mirrors AnthropicCredentialService.
// ValidateKey): it runs the full PAT validation chain without persisting, so
// the /config PATCH orchestrator can pre-flight the gitProvider section in its
// atomic pre-persist phase (org-config-consolidation.md §4). Connect reuses the
// same private validatePAT, so the two paths can't drift.
func (s *CredentialService) ValidatePAT(ctx context.Context, pat, githubLogin string) error {
	_, err := s.validatePAT(ctx, pat, githubLogin)
	return err
}

// WritePATRef stores the org's PAT as a new github-pat reference
// (SecretRefWriter.WriteGitHubPAT), the only place it is kept. The gitpat
// submit calls it once, after Connect committed and released the org lock,
// so the org-secret lock is never taken inside the org lock's transaction.
// An error fails the submit: AE Studio and the build read the token only
// from that reference. With secrets delivery off (no SecretsProvider) there
// is no reference to write and it does nothing.
func (s *CredentialService) WritePATRef(ctx context.Context, ocOrgID, pat string) error {
	if s.secretRefWriter == nil || !s.secretRefWriter.Enabled() {
		return nil
	}
	if _, err := s.secretRefWriter.WriteGitHubPAT(ctx, ocOrgID, pat); err != nil {
		return fmt.Errorf("credentials: write PAT reference: %w", err)
	}
	return nil
}
