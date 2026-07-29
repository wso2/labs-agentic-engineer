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

// credential_connect.go — the Connect/replace flow: kind dispatch,
// the PAT path (validate + seal + seed webhook secret + SM-API mirror) and
// the App-installation path.

package organization

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// Connect creates or replaces the credential record for ocOrgID. PAT mode
// runs the full validation chain (GET /user, membership probe, repo-read
// probe). App mode mints a JWT and looks up the install's account login.
//
// 409 (ConflictError) if an existing ACTIVE row is a different kind (the
// connect-time mode is fixed; disconnect before switching kind).
//
// 400 (ValidationError) for any GitHub-side validation failure — wrapped
// with a cause code that the UI maps to a specific error message.
func (s *CredentialService) Connect(ctx context.Context, ocOrgID string, req ConnectRequest) (*Projection, error) {
	// finalize carries the post-commit work (SM-API mirror, projection
	// re-fetch, success logging) for the chosen kind. It runs AFTER repo.Tx
	// commits and releases the advisory lock — exactly the commit-then-mirror
	// ordering the inline transaction used.
	var finalize func() (*Projection, error)
	err := s.repo.Tx(ctx, func(tx OrgCredentialTx) error {
		// Acquire org-scoped advisory lock for the duration of the txn so the
		// callback handler and a concurrent webhook (installation.created) can't
		// race the INSERT/UPDATE.
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
		case "app-installation":
			fn, err := s.connectApp(ctx, tx, ocOrgID, hadRow, existing, req)
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
// It does GitHub validation + the credential-store write + the row write, then
// returns the finalize closure Connect calls AFTER the commit: the SM-API
// mirror, the post-commit projection re-fetch (REPLACE), and the success log —
// preserving the original commit-then-mirror ordering.
func (s *CredentialService) connectPAT(ctx context.Context, tx OrgCredentialTx, ocOrgID string, hadRow bool, existing *OrgCredential, req ConnectRequest) (func() (*Projection, error), error) {
	identity, err := s.validatePAT(ctx, req.PAT, req.GitHubLogin)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()

	// Persist the PAT to the credential store first; if the DB row insert
	// fails below the credential entry is harmless (no referencing row yet).
	if err := s.store.Put(ctx, ocOrgID, "github/pat", []byte(req.PAT)); err != nil {
		return nil, fmt.Errorf("connect: write PAT: %w", err)
	}

	if !hadRow {
		// CREATE — use the platform's GITHUB_WEBHOOK_SECRET so per-repo
		// webhook registrations (which sign with the same env value) verify
		// against this row's secret list. Fall back to a fresh random value
		// only if env is unset (test mode).
		secret := s.envWebhookSecret
		if secret == "" {
			gen, err := generateRandomHex(32)
			if err != nil {
				return nil, fmt.Errorf("connect: gen webhook secret: %w", err)
			}
			secret = gen
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
			s.mirrorPATToSMAPI(ctx, ocOrgID, req.PAT)
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
	// for user-pat). Seed using the platform's GITHUB_WEBHOOK_SECRET so the
	// per-repo hooks (registered by the webhook feature against the
	// same env value) verify against it.
	if existing.Kind == "app-installation" {
		secret := s.envWebhookSecret
		if secret == "" {
			// No env secret available — fall back to a fresh random value.
			// PAT-mode webhooks may not verify against pre-existing repos in
			// this case, but we keep the constraint satisfied.
			gen, sErr := generateRandomHex(32)
			if sErr != nil {
				return nil, fmt.Errorf("connect: generate webhook secret: %w", sErr)
			}
			secret = gen
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
		s.mirrorPATToSMAPI(ctx, ocOrgID, req.PAT)
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

// mirrorPATToSMAPI fires the SM-API write best-effort after a Connect.
// Logged-and-swallowed on error — the org_secrets path keeps working when
// SM-API is down, so the user-facing Connect doesn't 5xx. The SM-API row
// is created/refreshed on the next successful Connect.
func (s *CredentialService) mirrorPATToSMAPI(ctx context.Context, ocOrgID, pat string) {
	if s.secretRefWriter == nil || !s.secretRefWriter.Enabled() {
		return
	}
	if _, err := s.secretRefWriter.WriteGitHubPAT(ctx, ocOrgID, pat); err != nil {
		slog.WarnContext(ctx, "credentials: SM-API mirror failed (legacy store still authoritative)",
			"ocOrgId", ocOrgID, "error", err)
	}
}

// SMAPISeedBundle packages the data the repair script needs to reseed
// OpenBao after a local cluster teardown. The BFF holds the plaintext
// (encrypted at rest in the cred store); the shell script holds vault
// access (via kubectl exec). Plaintext crosses the localhost boundary
// once, via the TestMode-gated repair endpoint.
type SMAPISeedBundle struct {
	KVPath   string `json:"kvPath"`   // remoteRef.key from the dispatcher's ExternalSecret
	Property string `json:"property"` // remoteRef.property — sub-field within the KV entry
	Value    string `json:"value"`    // plaintext secret
}

// PrepareSMAPISeed returns the OpenBao reseed bundle for the org's PAT
// credential. Returns (nil, nil) when the org has no active PAT row, the
// SM-API triplet isn't populated (Connect ran with SM-API disabled), or
// the cred-store value is missing — all idempotent no-op cases.
// App-mode rows are skipped — App installations don't carry a long-lived
// secret in OpenBao (per-request tokens are minted from the App private key).
//
// Drives the local-dev repair path. See deployments/scripts/repair-secrets.sh.
func (s *CredentialService) PrepareSMAPISeed(ctx context.Context, ocOrgID string) (*SMAPISeedBundle, error) {
	row, err := s.repo.GetByOrg(ctx, ocOrgID)
	if err != nil {
		return nil, fmt.Errorf("credentials seed: load row: %w", err)
	}
	if row == nil {
		return nil, nil
	}
	if row.Kind != "user-pat" || row.Status != "active" {
		return nil, nil
	}
	if row.SMAPIKVPath == nil || row.SMAPIProperty == nil ||
		*row.SMAPIKVPath == "" || *row.SMAPIProperty == "" {
		return nil, nil
	}
	pat, err := s.store.Get(ctx, ocOrgID, "github/pat")
	if err != nil || len(pat) == 0 {
		return nil, nil
	}
	return &SMAPISeedBundle{
		KVPath:   *row.SMAPIKVPath,
		Property: *row.SMAPIProperty,
		Value:    string(pat),
	}, nil
}

// connectApp runs inside Connect's transaction (the org advisory lock is
// held). It takes the install-scoped advisory lock, validates the installation
// against GitHub, writes the row, and returns the finalize closure Connect
// calls AFTER the commit (post-commit projection re-fetch + success log).
func (s *CredentialService) connectApp(ctx context.Context, tx OrgCredentialTx, ocOrgID string, hadRow bool, existing *OrgCredential, req ConnectRequest) (func() (*Projection, error), error) {
	if req.InstallationID == 0 {
		return nil, &ValidationError{Code: "installation_id_missing", Message: "installationId is required"}
	}
	if s.minter == nil || s.minter.AppID() == 0 {
		return nil, &ConflictError{Reason: "GitHub App not configured on this deployment"}
	}

	// Race-fix advisory lock keyed on installation_id (phase2.md §6.4).
	if err := tx.AdvisoryLock(fmt.Sprintf("install:%d", req.InstallationID)); err != nil {
		return nil, fmt.Errorf("connect: install lock: %w", err)
	}

	// Cross-org install check: if the same installation_id already maps
	// to a different ocOrgId, refuse.
	clash, err := tx.GetByInstallationID(req.InstallationID)
	if err != nil {
		return nil, fmt.Errorf("connect: install lookup: %w", err)
	}
	if clash != nil {
		if clash.OcOrgID != ocOrgID {
			return nil, &ConflictError{Reason: fmt.Sprintf("installation %d already bound to org %s", req.InstallationID, clash.OcOrgID)}
		}
		if clash.Status == "active" && hadRow && existing.OcOrgID == ocOrgID {
			// Idempotent re-connect — return current projection.
			slog.InfoContext(ctx, "secrets.connect.idempotent", "ocOrgId", ocOrgID, "kind", "app-installation", "installationId", req.InstallationID)
			return func() (*Projection, error) {
				return projectionFromRow(clash), nil
			}, nil
		}
	}

	// Fetch installation + bot identity.
	accountLogin, accountType, selectedRepos, err := s.fetchInstallation(ctx, req.InstallationID)
	if err != nil {
		return nil, err
	}
	// Refuse User-account installs. GitHub's POST /user/repos is not
	// accessible to App installation tokens (returns 403 "Resource not
	// accessible by integration"), so any first-class repo provisioning
	// fails silently after bind. Surface it at connect time instead so
	// the user knows to install on an Organization account.
	if accountType == "User" {
		return nil, &ValidationError{
			Code:    "user_account_install_unsupported",
			Message: fmt.Sprintf("GitHub App was installed on a personal user account (%s). Install on an Organization account instead — App tokens cannot create repositories on user accounts.", accountLogin),
		}
	}
	if s.minter.BotIdentity().Login == "" {
		// First connect — populate the bot identity once.
		botID, err := s.fetchAppBotIdentity(ctx)
		if err != nil {
			slog.WarnContext(ctx, "fetch bot identity failed", "error", err)
			// Use a deterministic fallback so the row passes NOT NULL constraints.
			botID = secrets.Identity{
				Name:  "AEP Platform Bot",
				Email: "bot@aep.dev",
				Login: "aep-platform[bot]",
			}
		}
		s.minter.SetBotIdentity(botID)
	}
	bot := s.minter.BotIdentity()

	now := time.Now().UTC()
	id := req.InstallationID
	if !hadRow {
		row := OrgCredential{
			OcOrgID:         ocOrgID,
			Kind:            "app-installation",
			GitHubLogin:     accountLogin,
			IdentityName:    bot.Name,
			IdentityEmail:   bot.Email,
			IdentityLogin:   bot.Login,
			InstallationID:  &id,
			SelectedRepos:   JSONStringList(selectedRepos),
			Status:          "active",
			ConnectedAt:     now,
			LastValidatedAt: &now,
		}
		if err := tx.Create(&row); err != nil {
			return nil, fmt.Errorf("connect: insert app: %w", err)
		}
		return func() (*Projection, error) {
			slog.InfoContext(ctx, "secrets.connected", "ocOrgId", ocOrgID, "kind", "app-installation", "installationId", id, "githubLogin", accountLogin)
			return projectionFromRow(&row), nil
		}, nil
	}

	// Updating existing row to App mode (post-disconnect-then-reconnect).
	updates := map[string]any{
		"kind":              "app-installation",
		"github_login":      accountLogin,
		"identity_name":     bot.Name,
		"identity_email":    bot.Email,
		"identity_login":    bot.Login,
		"installation_id":   id,
		"selected_repos":    JSONStringList(selectedRepos),
		"status":            "active",
		"connected_at":      now,
		"last_validated_at": now,
		// PAT-mode specific fields are nulled by the CHECK constraint —
		// caller side must clear webhook_secrets.
		"webhook_secrets": nil,
		"pat_secret_ref":  nil,
	}
	if err := tx.UpdateColumns(ocOrgID, updates); err != nil {
		return nil, fmt.Errorf("connect: update app: %w", err)
	}
	return func() (*Projection, error) {
		row, err := s.fetchRow(ctx, ocOrgID)
		if err != nil {
			return nil, err
		}
		slog.InfoContext(ctx, "secrets.connected", "ocOrgId", ocOrgID, "kind", "app-installation", "installationId", id, "githubLogin", accountLogin)
		return projectionFromRow(row), nil
	}, nil
}
