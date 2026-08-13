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

// Package orgcreds owns the per-org credential surface (GitHub, Anthropic,
// build), per docs/design/github-integration-phase2.md §5.2 and §6.4–6.7.
//
// This file holds CredentialService's core: the type, constructor, With*
// wiring, shared error/request/projection shapes, and row/crypto helpers.
// The behavior lives in sibling files, one per concern: credential_connect.go
// (connect/replace), credential_lifecycle.go (status/disconnect/uninstall),
// credential_identity.go (identity view + validator support),
// credential_webhook_secrets.go (HMAC secret rotation),
// credential_installations.go (App-installation lifecycle + webhook routing),
// credential_github_probe.go (raw GitHub REST probes for the PAT path).
package organization

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"github.com/wso2/aep/aep-api/internal/platform/text"
	"net/http"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// CredentialService is the orchestration layer behind /internal/credentials/orgs/...
//
// It owns: validation of new PATs against GitHub, App-mode connect via
// installation lookup, status projection, disconnect Phase D, webhook-secret
// rotation, lookup helpers used by the BFF's webhook routing.
//
// The Resolver (used at runtime by every git operation) doesn't change at
// connect time — it just reads whatever this service has persisted.
// BuildSecretCleaner cleans up the per-org build-credential Secret in the
// org's workflow-plane namespace. Implemented by BuildCredentialsService;
// kept as an interface here so CredentialService doesn't import a
// concrete struct from a sibling file (no real circular import today,
// but keeps the seam minimal and testable).
//
// Each WP-Secret-cleanup concern (build, anthropic, future providers)
// has its own narrowly-typed interface so cred services only depend on
// what they own.
type BuildSecretCleaner interface {
	DeleteBuildSecretsForOrg(ctx context.Context, ocOrgID string) error
}

type CredentialService struct {
	repo      OrgCredentialRepository
	store     secrets.CredentialStore
	minter    *secrets.AppTokenMinter
	githubAPI string // "https://api.github.com" by default; overridden in tests.

	// buildSecretCleaner is invoked from the Disconnect cascade so a
	// disconnected org's WP build Secret doesn't outlive its credential
	// row. nil is a graceful no-op (tests, off-cluster runs).
	buildSecretCleaner BuildSecretCleaner

	// secretRefWriter mirrors the PAT into SM-API on Connect and clears it on
	// Disconnect. nil-safe — no-op when the writer isn't configured
	// (composition-root behavior when SecretsProvider is nil).
	secretRefWriter *SecretRefWriter

	// envWebhookSecret is the platform-wide GITHUB_WEBHOOK_SECRET. The PAT
	// connect path uses this value when seeding `webhook_secrets[0]` on a
	// fresh or cross-mode-reseeded row so the per-repo webhook (which the
	// webhook feature registers with the same env value) verifies
	// against it. Rotation lands by appending a new entry via the
	// AppendWebhookSecret route. Empty in tests.
	envWebhookSecret string

	// App OAuth client_id/secret used by the discover-then-bind path
	// (BindAppInstallation). Empty values disable that path; the discover
	// endpoint surfaces 503 in that mode.
	appClientID     string
	appClientSecret string

	// githubClient is the git-host App/credential port. CredentialService
	// uses it for the discover-then-bind path (ListAppInstallations,
	// ExchangeOAuthCode, GetUserInstallations) and the uninstall cascade
	// (DeleteInstallation); the rest of CredentialService still uses raw
	// httpClient. Optional — nil disables the bind path.
	githubClient sourcecontrol.AppInstallOps

	httpClient *http.Client
}

// NewCredentialService constructs the service. db, store, minter must be
// non-nil. githubAPI may be empty (defaults to api.github.com).
// envWebhookSecret is the GITHUB_WEBHOOK_SECRET — used as the seed value
// for fresh PAT rows and cross-mode reseeds.
// appClientID / appClientSecret enable the OAuth bind path; empty values
// disable it gracefully.
// githubClient is used by the discover-then-bind path (ListAppInstallations,
// ExchangeOAuthCode, GetUserInstallations); nil disables the bind path.
func NewCredentialService(
	repo OrgCredentialRepository,
	store secrets.CredentialStore,
	minter *secrets.AppTokenMinter,
	envWebhookSecret string,
	appClientID, appClientSecret string,
	githubClient sourcecontrol.AppInstallOps,
) *CredentialService {
	return &CredentialService{
		repo:             repo,
		store:            store,
		minter:           minter,
		envWebhookSecret: envWebhookSecret,
		appClientID:      appClientID,
		appClientSecret:  appClientSecret,
		githubClient:     githubClient,
		githubAPI:        "https://api.github.com",
		httpClient:       &http.Client{Timeout: 30 * time.Second},
	}
}

// ----------------------------------------------------------------------------
// Errors with stable codes for the BFF / API layer.
// ----------------------------------------------------------------------------

// ValidationError carries a structured cause string for the connect/replace
// path so the UI can render field-level error text. The Cause field is the
// machine-readable code; the Message is the human-friendly text.
type ValidationError struct {
	Code    string
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// ConflictError signals a request that violates the credential record's
// invariants — usually a cross-mode change (existing kind=app-installation
// row, new request with kind=user-pat) or attempting App-mode webhook-secret
// management.
type ConflictError struct {
	Reason string
}

func (e *ConflictError) Error() string { return "conflict: " + e.Reason }

// NotFoundError signals "no row matches this lookup" — distinct from
// network/DB errors so the API layer can return 404 cleanly.
type NotFoundError struct {
	What string
}

func (e *NotFoundError) Error() string { return "not found: " + e.What }

// UpstreamError signals that a third-party dependency (e.g. Anthropic) failed
// on ITS side — a 5xx from the upstream API. It is distinct from ValidationError
// so the API layer maps it to 502 Bad Gateway (the client's request was fine;
// the upstream is broken) rather than a client-fault 400.
type UpstreamError struct {
	Code    string
	Message string
}

func (e *UpstreamError) Error() string { return e.Message }

// ----------------------------------------------------------------------------
// Connect / Replace — POST /internal/credentials/orgs/{ocOrgId}
// ----------------------------------------------------------------------------

// ConnectRequest is the body for POST /internal/credentials/orgs/{ocOrgId}.
// Exactly one of {AppInstallation, UserPAT} must be populated; the kind field
// must match.
type ConnectRequest struct {
	Kind           string `json:"kind"`
	InstallationID int64  `json:"installationId,omitempty"`
	PAT            string `json:"pat,omitempty"`
	GitHubLogin    string `json:"githubLogin,omitempty"`
}

// Projection is the JSON shape returned by status / connect / replace. It
// never contains the token itself.
type Projection struct {
	OcOrgID           string     `json:"ocOrgId"`
	Kind              string     `json:"kind"`
	GitHubLogin       string     `json:"githubLogin"`
	IdentityName      string     `json:"identityName,omitempty"`
	IdentityEmail     string     `json:"identityEmail,omitempty"`
	IdentityLogin     string     `json:"identityLogin"`
	InstallationID    *int64     `json:"installationId,omitempty"`
	SelectedRepos     []string   `json:"selectedRepos,omitempty"`
	Status            string     `json:"status"`
	ConnectedAt       time.Time  `json:"connectedAt"`
	LastValidatedAt   *time.Time `json:"lastValidatedAt,omitempty"`
	IdentityChangedAt *time.Time `json:"identityChangedAt,omitempty"`
	PrevIdentityLogin *string    `json:"prevIdentityLogin,omitempty"`
}

func projectionFromRow(r *OrgCredential) *Projection {
	p := &Projection{
		OcOrgID:           r.OcOrgID,
		Kind:              r.Kind,
		GitHubLogin:       r.GitHubLogin,
		IdentityName:      r.IdentityName,
		IdentityEmail:     r.IdentityEmail,
		IdentityLogin:     r.IdentityLogin,
		InstallationID:    r.InstallationID,
		Status:            r.Status,
		ConnectedAt:       r.ConnectedAt,
		LastValidatedAt:   r.LastValidatedAt,
		IdentityChangedAt: r.IdentityChangedAt,
		PrevIdentityLogin: r.PrevIdentityLogin,
	}
	if r.SelectedRepos != nil {
		p.SelectedRepos = []string(r.SelectedRepos)
	}
	return p
}

// WithBuildSecretCleaner injects the post-disconnect cleanup hook for
// the per-org build-credential Secret. Wired by main after both services
// are constructed; nil-safe so tests don't have to pass one. Returns the
// receiver to allow chained construction.
func (s *CredentialService) WithBuildSecretCleaner(cleaner BuildSecretCleaner) *CredentialService {
	s.buildSecretCleaner = cleaner
	return s
}

// WithSecretRefWriter injects the SM-API writer. When set, the PAT-mode
// Connect path uploads the PAT to SM-API after the local commit and
// stamps the triplet onto the row. nil-safe.
func (s *CredentialService) WithSecretRefWriter(w *SecretRefWriter) *CredentialService {
	s.secretRefWriter = w
	return s
}

// WithGitHubAPIBase overrides the GitHub REST base URL (default
// https://api.github.com). This is a TEST SEAM: it lets the component and
// dbtest tiers point the PAT-validation probes (fetchPATIdentity /
// validatePATMembership / probePATRepoRead) and the App-mode installation
// lookups at an httptest fake GitHub, so those paths run for real against
// controlled responses without reaching github.com. Returns the receiver for
// chained construction. Not wired in production (main leaves the default).
func (s *CredentialService) WithGitHubAPIBase(base string) *CredentialService {
	s.githubAPI = base
	return s
}

// ----------------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------------

func (s *CredentialService) fetchRow(ctx context.Context, ocOrgID string) (*OrgCredential, error) {
	row, err := s.repo.GetByOrg(ctx, ocOrgID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, &NotFoundError{What: fmt.Sprintf("org_credentials.%s", ocOrgID)}
	}
	return row, nil
}

func generateRandomHex(byteLen int) (string, error) {
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// truncateForError bounds an upstream error body for a user-visible message.
// The result lands in ValidationError.Message, which the console shows
// verbatim, so it is flattened to one line; text.Truncate owns the rune-safe
// cut (see its doc for why a byte cut is not safe here).
func truncateForError(b []byte) string {
	return strings.ReplaceAll(text.Truncate(string(b), 200), "\n", " ")
}
