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

package organization

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/thundersvc"
)

// PublisherBuildActor is the audit actor for
// IDPService.ProvisionPublisherForBuild — the POST /build request path,
// distinct from the "deployment" actor EnsureOrgPublisher otherwise uses.
const PublisherBuildActor = "build-provision"

// IDPService manages per-organisation IDP profiles + the matching
// Thunder publisher OAuth apps. See
// docs/design/api-platform-integration.md.
//
// Lifecycle:
//   - First protected-component deploy in an org triggers
//     EnsureOrgPublisher. Idempotent — repeating returns the existing
//     row and skips the Thunder create call.
//   - Org delete (or explicit admin action) triggers RevokeOrgPublisher.
//   - Credential compromise / scheduled rotation triggers
//     RegenerateClientSecret.
//
// Every mutation appends an audit row to idp_audit_events so the
// console "Audit" tab and incident-response have a definitive trail.
type IDPService interface {
	// GetOrCreateProfile returns the org's IDP profile, creating a
	// default platform-kind row (kind=platform, issuer/jwksURL from
	// the platform IDP config) when none exists. The publisher_*
	// columns stay empty until EnsureOrgPublisher fires.
	GetOrCreateProfile(ctx context.Context, orgID string) (*OrganizationIDPProfile, error)

	// GetProfile returns the existing profile, or nil + nil error when
	// none exists yet.
	GetProfile(ctx context.Context, orgID string) (*OrganizationIDPProfile, error)

	// EnsureOrgPublisher creates (or returns the existing) Thunder
	// publisher OAuth app for the org, persists the credentials on
	// the profile row, and writes an audit-log entry. Returns the
	// canonical client_id (always present) and the client_secret
	// (only present on creation — empty when the app already existed
	// and the secret is already in the DB; use RegenerateClientSecret
	// to rotate if it was lost).
	EnsureOrgPublisher(ctx context.Context, orgID, actor string) (clientID, clientSecret string, created bool, err error)

	// ProvisionPublisherForBuild is the POST /build provisioner: ensure
	// Thunder publisher exists and secret_ref_name is stamped. Actor is
	// PublisherBuildActor. Rotates once if the app exists without a
	// SecretReference. WritePublisher errors fail the call.
	ProvisionPublisherForBuild(ctx context.Context, orgID string) error

	// RevokeOrgPublisher deletes the Thunder publisher app + clears
	// the profile row's publisher_* columns. Idempotent.
	RevokeOrgPublisher(ctx context.Context, orgID, actor string) (bool, error)

	// RegenerateClientSecret issues a fresh client_secret + updates the
	// profile row + writes an audit entry. Returns the new secret —
	// rotate any consumer pods after this call.
	RegenerateClientSecret(ctx context.Context, orgID, actor string) (string, error)

	// UpdateProfile changes the org's IDP kind / issuer / JWKS URL.
	// Switching kind invalidates any existing publisher app (Thunder is
	// a separate keymanager from Asgardeo/custom OIDC), so the call
	// cascades a RevokeOrgPublisher against the previous kind's IDP.
	// Audit-logged.
	//
	// Operator follow-up: after this call, the platform admin must
	// ensure the new IDP's keymanager is registered in
	// deployments/manifests/api-platform/gateway-config.yaml's
	// jwtauth_v1 block, then re-run setup-prerequisites. Keymanager
	// registration is a manual ops step.
	UpdateProfile(ctx context.Context, orgID, actor string, req UpdateProfileRequest) (*OrganizationIDPProfile, error)

	// SetProfile wholesale-replaces the org's IDP configuration behind
	// PATCH /config {idp}: kind + issuer + jwksURL are written exactly as
	// given, with NONE of UpdateProfile's field-level "empty means keep"
	// carry-over (org-config-consolidation.md §4 kills that quirk — the
	// console round-trips the whole idp section from GET). kind=platform
	// restores the cluster platform defaults (a platform IDP's issuer/JWKS
	// are cluster config, not per-org data). A kind switch cascades the same
	// publisher revoke UpdateProfile does. Audit-logged.
	SetProfile(ctx context.Context, orgID, actor, kind, issuer, jwksURL string) (*OrganizationIDPProfile, error)
}

// UpdateProfileRequest is the input for IDPService.UpdateProfile.
// Empty fields leave the existing value unchanged.
type UpdateProfileRequest struct {
	Kind    string // "platform" | "asgardeo" | "custom"
	Issuer  string
	JWKSURL string
}

// PlatformIDPConfig is the cluster-level platform IDP defaults the BFF
// applies when seeding a new org profile. Loaded from env in main.go.
type PlatformIDPConfig struct {
	Issuer  string
	JWKSURL string
}

type idpService struct {
	repo            IDPRepository
	orgRepo         OrganizationRepository
	thunder         thundersvc.Client
	platform        PlatformIDPConfig
	secretRefWriter *SecretRefWriter
}

// NewIDPService builds the service. `repo` persists the idp tables; `orgRepo`
// serves the org → Thunder OU lookup the publisher-provisioning path needs
// (reused, not duplicated). `thunder` may be nil in unit tests — the service
// rejects EnsureOrgPublisher / RevokeOrgPublisher / RegenerateClientSecret
// with ErrIDPThunderUnavailable when so. Read methods (GetProfile,
// GetOrCreateProfile) keep working. Returns the concrete type so
// WithSecretRefWriter can chain at the composition root; the concrete value still
// satisfies the IDPService interface for consumers that store it as such.
func NewIDPService(repo IDPRepository, orgRepo OrganizationRepository, thunder thundersvc.Client, platform PlatformIDPConfig) *idpService {
	return &idpService{repo: repo, orgRepo: orgRepo, thunder: thunder, platform: platform}
}

// WithSecretRefWriter attaches the SM-API writer so EnsureOrgPublisher /
// RegenerateClientSecret mirror the publisher cc credentials to SM-API
// (runner pods consume them via per-run ExternalSecret). nil writer or
// one with Enabled()==false is a no-op; publisher provisioning still
// works but dispatcher's runner-auth path stays disabled.
func (s *idpService) WithSecretRefWriter(w *SecretRefWriter) *idpService {
	s.secretRefWriter = w
	return s
}

// ErrIDPThunderUnavailable means the Thunder admin client isn't wired
// (missing system credentials, etc). Callers in the dispatch / design-
// edit path treat this as
// non-fatal — protected components still deploy; per-org publisher
// provisioning is best-effort and the next dispatch tries again.
var ErrIDPThunderUnavailable = errors.New("idp_service: thunder admin client not configured")

func (s *idpService) GetProfile(ctx context.Context, orgID string) (*OrganizationIDPProfile, error) {
	if orgID == "" {
		return nil, fmt.Errorf("orgID required")
	}
	profile, err := s.repo.GetProfileByOrgID(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("idp_service.GetProfile: %w", err)
	}
	return profile, nil
}

func (s *idpService) GetOrCreateProfile(ctx context.Context, orgID string) (*OrganizationIDPProfile, error) {
	if orgID == "" {
		return nil, fmt.Errorf("orgID required")
	}
	existing, err := s.GetProfile(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		// Self-heal the cluster-level platform fields. Issuer/JWKSURL are
		// cluster config, not per-org data, but they were cached onto the
		// row at creation. If the config changed since (e.g. the cluster
		// moved from an in-cluster Thunder URL to the gateway URL), refresh
		// the row so the derived per-org publisher token URL stays correct.
		//
		// ONLY for platform-kind profiles: a BYO org (kind=custom/asgardeo)
		// owns its own issuer/JWKS URL, which never match the platform
		// defaults. Self-healing those would silently clobber the org's real
		// IDP config back to the cluster default on the next read — data loss.
		if existing.Kind == "platform" &&
			s.platform.JWKSURL != "" &&
			(existing.JWKSURL != s.platform.JWKSURL || existing.Issuer != s.platform.Issuer) {
			if err := s.repo.UpdateProfileColumns(ctx, existing, orgID,
				map[string]interface{}{
					"jwks_url":   s.platform.JWKSURL,
					"issuer":     s.platform.Issuer,
					"updated_at": time.Now().UTC(),
				}); err != nil {
				slog.WarnContext(ctx, "idp_service: platform field self-heal failed (continuing)",
					"orgID", orgID, "error", err)
			} else {
				existing.JWKSURL = s.platform.JWKSURL
				existing.Issuer = s.platform.Issuer
			}
		}
		return existing, nil
	}

	profile := OrganizationIDPProfile{
		OrgID:     orgID,
		Kind:      "platform",
		Issuer:    s.platform.Issuer,
		JWKSURL:   s.platform.JWKSURL,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := s.repo.CreateProfile(ctx, &profile); err != nil {
		// Race: another goroutine may have created the row between our
		// SELECT and INSERT. Re-read.
		if again, gerr := s.GetProfile(ctx, orgID); gerr == nil && again != nil {
			return again, nil
		}
		return nil, fmt.Errorf("idp_service.GetOrCreateProfile: %w", err)
	}
	return &profile, nil
}

// lookupOrgOUID returns the org's Thunder OU id (the JWT `ouId`, stored as
// Organization.ThunderOrgUUID) so the publisher app can be registered under
// the org's OU. Returns "" when the org row or its Thunder UUID is missing —
// the Thunder client then falls back to the default OU.
func (s *idpService) lookupOrgOUID(ctx context.Context, orgHandle string) string {
	org, err := s.orgRepo.GetByName(ctx, orgHandle)
	if err != nil || org == nil {
		slog.DebugContext(ctx, "idp lookupOrgOUID: no org row", "orgHandle", orgHandle, "error", err)
		return ""
	}
	if org.ThunderOrgUUID == nil {
		slog.DebugContext(ctx, "idp lookupOrgOUID: org row has NULL thunder_org_uuid (publisher will use default OU)", "orgHandle", orgHandle)
		return ""
	}
	slog.DebugContext(ctx, "idp lookupOrgOUID: resolved org OU for publisher provisioning",
		"orgHandle", orgHandle, "orgOU", org.ThunderOrgUUID.String())
	return org.ThunderOrgUUID.String()
}

func (s *idpService) EnsureOrgPublisher(ctx context.Context, orgID, actor string) (string, string, bool, error) {
	if orgID == "" {
		return "", "", false, fmt.Errorf("orgID required")
	}
	if s.thunder == nil {
		return "", "", false, ErrIDPThunderUnavailable
	}

	profile, err := s.GetOrCreateProfile(ctx, orgID)
	if err != nil {
		return "", "", false, err
	}

	beforeJSON, _ := json.Marshal(profileSummary(profile))

	// Resolve the org's Thunder OU id (JWT ouId) so the publisher app is
	// registered under the org's OU — its cc token then carries
	// ouHandle == orgHandle, which the publisher-token verifier requires.
	// Empty (org UUID not yet backfilled) falls back to the default OU.
	orgOUID := s.lookupOrgOUID(ctx, orgID)
	if orgOUID == "" {
		slog.WarnContext(ctx, "idp_service: org Thunder OU id unknown — publisher app will use the default OU; runner token ouHandle may not match the org. Ensure the org row has thunder_org_uuid (user must have logged in with an ouId claim).",
			"orgID", orgID)
	} else {
		slog.DebugContext(ctx, "idp_service: provisioning publisher under org OU", "orgID", orgID, "orgOU", orgOUID)
	}
	clientID, clientSecret, created, terr := s.thunder.EnsurePublisherApp(ctx, orgID, orgOUID)
	if terr != nil {
		s.audit(ctx, orgID, IDPAuditEnsurePublisher, actor, beforeJSON, nil, terr)
		return "", "", false, fmt.Errorf("idp_service.EnsureOrgPublisher: %w", terr)
	}

	// Persist clientId always; clientSecret only on creation (Thunder
	// doesn't expose it on subsequent reads).
	updates := map[string]interface{}{
		"publisher_client_id":  clientID,
		"publisher_secret_ref": secretRefPath(orgID), // logical OpenBao path persisted alongside the secret
		"updated_at":           time.Now().UTC(),
	}
	if created && clientSecret != "" {
		updates["publisher_client_secret"] = clientSecret
	}
	if err := s.repo.UpdateProfileColumns(ctx, profile, orgID, updates); err != nil {
		s.audit(ctx, orgID, IDPAuditEnsurePublisher, actor, beforeJSON, nil, err)
		return "", "", false, fmt.Errorf("idp_service.EnsureOrgPublisher persist: %w", err)
	}

	// Mirror publisher creds to SM-API so the dispatcher can mint a
	// per-run ExternalSecret for the runner's cc flow. Only on fresh
	// create (Thunder doesn't return the secret on subsequent reads); pre-
	// existing apps without SM-API mirroring recover via an explicit
	// RegenerateClientSecret. Best-effort: SM-API outage doesn't fail
	// publisher provisioning.
	if created && clientSecret != "" && s.secretRefWriter != nil && s.secretRefWriter.Enabled() {
		if _, smerr := s.secretRefWriter.WritePublisher(ctx, orgID, clientID, clientSecret); smerr != nil {
			slog.WarnContext(ctx, "idp_service: SM-API publisher write failed (continuing)",
				"orgID", orgID, "error", smerr)
		}
	}

	// Re-read for the audit "after" snapshot.
	after, _ := s.GetProfile(ctx, orgID)
	afterJSON, _ := json.Marshal(profileSummary(after))
	s.audit(ctx, orgID, IDPAuditEnsurePublisher, actor, beforeJSON, afterJSON, nil)

	slog.InfoContext(ctx, "idp_service: EnsureOrgPublisher",
		"orgID", orgID,
		"clientID", clientID,
		"created", created,
	)
	return clientID, clientSecret, created, nil
}

// ProvisionPublisherForBuild is the fail-closed counterpart to
// EnsureOrgPublisher's best-effort SM-API mirror, called from the
// POST /projects/{projectName}/build handler (user JWT in ctx, so
// WritePublisher can resolve the SM-API vault key). EnsureOrgPublisher
// itself already attempts the SM-API write on fresh creation; this method
// only steps in when that attempt didn't leave a usable secret_ref_name —
// either because Ensure's write failed/was skipped, or because the app
// already existed without ever having been mirrored (rotate once to
// recover a usable triplet). Every failure path here fails the Build.
func (s *idpService) ProvisionPublisherForBuild(ctx context.Context, orgID string) error {
	if orgID == "" {
		return fmt.Errorf("orgID required")
	}
	// Fail closed BEFORE touching Thunder: a disabled writer can never stamp
	// secret_ref_name, so letting EnsureOrgPublisher/RegenerateClientSecret
	// run anyway would rotate the Thunder client secret on every single
	// Build (each call retries the same never-stamped ref) and report
	// success in the exact state this method exists to prevent.
	if s.secretRefWriter == nil || !s.secretRefWriter.Enabled() {
		return fmt.Errorf("publisher SecretReference requires a SecretsProvider (secrets delivery is off)")
	}
	clientID, clientSecret, _, err := s.EnsureOrgPublisher(ctx, orgID, PublisherBuildActor)
	if err != nil {
		return err
	}
	row, err := s.GetProfile(ctx, orgID)
	if err != nil {
		return err
	}
	if HasPublisherSecretRef(row) {
		return nil
	}
	if strings.TrimSpace(clientSecret) != "" {
		if _, werr := s.secretRefWriter.WritePublisher(ctx, orgID, clientID, clientSecret); werr != nil {
			return fmt.Errorf("idp_service: SM-API publisher write: %w", werr)
		}
		return nil
	}
	if row != nil && strings.TrimSpace(row.PublisherClientID) != "" {
		if _, rerr := s.RegenerateClientSecret(ctx, orgID, PublisherBuildActor); rerr != nil {
			return rerr
		}
		// spec P1 step 4: rotate is a one-shot recovery, not a retry loop —
		// require the rotated secret actually landed a SecretReference
		// rather than reporting success while the ref stays null.
		after, aerr := s.GetProfile(ctx, orgID)
		if aerr != nil {
			return aerr
		}
		if !HasPublisherSecretRef(after) {
			return fmt.Errorf("publisher SecretReference still missing after rotate")
		}
		return nil
	}
	return fmt.Errorf("publisher SecretReference missing after provision")
}

func (s *idpService) RevokeOrgPublisher(ctx context.Context, orgID, actor string) (bool, error) {
	if orgID == "" {
		return false, fmt.Errorf("orgID required")
	}
	if s.thunder == nil {
		return false, ErrIDPThunderUnavailable
	}
	profile, err := s.GetProfile(ctx, orgID)
	if err != nil {
		return false, err
	}
	if profile == nil || profile.PublisherClientID == "" {
		// Nothing to revoke.
		return false, nil
	}
	beforeJSON, _ := json.Marshal(profileSummary(profile))

	deleted, terr := s.thunder.DeletePublisherApp(ctx, orgID)
	if terr != nil {
		s.audit(ctx, orgID, IDPAuditRevokePublisher, actor, beforeJSON, nil, terr)
		return false, fmt.Errorf("idp_service.RevokeOrgPublisher: %w", terr)
	}

	if err := s.repo.UpdateProfileColumns(ctx, profile, orgID,
		map[string]interface{}{
			"publisher_client_id":     "",
			"publisher_client_secret": "",
			"publisher_secret_ref":    "",
			"updated_at":              time.Now().UTC(),
		}); err != nil {
		s.audit(ctx, orgID, IDPAuditRevokePublisher, actor, beforeJSON, nil, err)
		return deleted, fmt.Errorf("idp_service.RevokeOrgPublisher persist: %w", err)
	}

	// Drop the SM-API publisher secret + clear the triplet. Best-effort
	// so a missing SM-API doesn't strand the Thunder revoke.
	if s.secretRefWriter != nil && s.secretRefWriter.Enabled() {
		if smerr := s.secretRefWriter.DeletePublisher(ctx, orgID); smerr != nil {
			slog.WarnContext(ctx, "idp_service: SM-API publisher delete failed (continuing)",
				"orgID", orgID, "error", smerr)
		}
	}

	after, _ := s.GetProfile(ctx, orgID)
	afterJSON, _ := json.Marshal(profileSummary(after))
	s.audit(ctx, orgID, IDPAuditRevokePublisher, actor, beforeJSON, afterJSON, nil)

	slog.InfoContext(ctx, "idp_service: RevokeOrgPublisher",
		"orgID", orgID, "deleted", deleted)
	return deleted, nil
}

func (s *idpService) RegenerateClientSecret(ctx context.Context, orgID, actor string) (string, error) {
	if orgID == "" {
		return "", fmt.Errorf("orgID required")
	}
	if s.thunder == nil {
		return "", ErrIDPThunderUnavailable
	}
	profile, err := s.GetProfile(ctx, orgID)
	if err != nil {
		return "", err
	}
	if profile == nil || profile.PublisherClientID == "" {
		return "", fmt.Errorf("idp_service.RegenerateClientSecret: no publisher app for org %s", orgID)
	}
	beforeJSON, _ := json.Marshal(profileSummary(profile))

	newSecret, terr := s.thunder.RegenerateClientSecret(ctx, orgID)
	if terr != nil {
		s.audit(ctx, orgID, IDPAuditRegenerateSecret, actor, beforeJSON, nil, terr)
		return "", fmt.Errorf("idp_service.RegenerateClientSecret: %w", terr)
	}

	if err := s.repo.UpdateProfileColumns(ctx, profile, orgID,
		map[string]interface{}{
			"publisher_client_secret": newSecret,
			"updated_at":              time.Now().UTC(),
		}); err != nil {
		s.audit(ctx, orgID, IDPAuditRegenerateSecret, actor, beforeJSON, nil, err)
		return "", fmt.Errorf("idp_service.RegenerateClientSecret persist: %w", err)
	}

	// Mirror the rotated secret to SM-API; runner pods picking up from
	// the next dispatch receive the new secret via ExternalSecret refresh.
	// Fail-closed: a stale secret_ref_name after rotation would hand a
	// runner pod credentials Thunder already invalidated, so a write
	// failure here fails the caller instead of swallowing it.
	if s.secretRefWriter != nil && s.secretRefWriter.Enabled() {
		if _, smerr := s.secretRefWriter.WritePublisher(ctx, orgID, profile.PublisherClientID, newSecret); smerr != nil {
			s.audit(ctx, orgID, IDPAuditRegenerateSecret, actor, beforeJSON, nil, smerr)
			// Thunder and the DB row already hold the new secret; the vault
			// still holds the old one. A non-empty secret_ref_name would look
			// healthy to ProvisionPublisherForBuild. Clear it so the next
			// Build re-provisions instead of mounting invalidated credentials.
			if cerr := s.repo.UpdateProfileColumns(ctx, profile, orgID, clearSecretRefTripletWithWrittenAt()); cerr != nil {
				slog.ErrorContext(ctx, "idp_service: clear secret_ref after failed SM-API rewrite",
					"orgID", orgID, "error", cerr)
			}
			return "", fmt.Errorf("idp_service: SM-API publisher rewrite: %w", smerr)
		}
	}

	after, _ := s.GetProfile(ctx, orgID)
	afterJSON, _ := json.Marshal(profileSummary(after))
	s.audit(ctx, orgID, IDPAuditRegenerateSecret, actor, beforeJSON, afterJSON, nil)

	slog.InfoContext(ctx, "idp_service: RegenerateClientSecret", "orgID", orgID)
	return newSecret, nil
}

// UpdateProfile changes kind/issuer/JWKS URL for an org. When kind
// switches, the existing publisher app is revoked (so the next
// EnsureOrgPublisher creates a fresh one tied to the new IDP). When
// only issuer/JWKS URL change but kind stays the same, the publisher
// app is preserved.
func (s *idpService) UpdateProfile(ctx context.Context, orgID, actor string, req UpdateProfileRequest) (*OrganizationIDPProfile, error) {
	if orgID == "" {
		return nil, fmt.Errorf("orgID required")
	}
	if req.Kind != "" {
		switch req.Kind {
		case "platform", "asgardeo", "custom":
			// ok
		default:
			return nil, fmt.Errorf("invalid kind %q (must be platform|asgardeo|custom)", req.Kind)
		}
	}

	existing, err := s.GetOrCreateProfile(ctx, orgID)
	if err != nil {
		return nil, err
	}
	beforeJSON, _ := json.Marshal(profileSummary(existing))

	kindChanged := req.Kind != "" && req.Kind != existing.Kind

	updates := map[string]interface{}{
		"updated_at": time.Now().UTC(),
	}
	if req.Kind != "" {
		updates["kind"] = req.Kind
	}
	if req.Issuer != "" {
		updates["issuer"] = req.Issuer
	}
	if req.JWKSURL != "" {
		updates["jwks_url"] = req.JWKSURL
	}

	// Clear publisher state when kind switches — the existing publisher
	// app belongs to the previous IDP. Trying to reuse it across IDPs
	// breaks the trust chain (different issuer, different signing keys).
	if kindChanged {
		// Best-effort Thunder cleanup BEFORE clearing — only for the
		// platform→other transition. We skip the call when thunder
		// isn't configured (just clear the columns).
		if existing.Kind == "platform" && existing.PublisherClientID != "" && s.thunder != nil {
			if _, derr := s.thunder.DeletePublisherApp(ctx, orgID); derr != nil {
				slog.WarnContext(ctx, "idp_service.UpdateProfile: Thunder publisher cleanup failed (ignored)",
					"orgID", orgID, "error", derr)
			}
		}
		updates["publisher_client_id"] = ""
		updates["publisher_client_secret"] = ""
		updates["publisher_secret_ref"] = ""
	}

	if err := s.repo.UpdateProfileColumns(ctx, existing, orgID, updates); err != nil {
		s.audit(ctx, orgID, IDPAuditUpdateProfile, actor, beforeJSON, nil, err)
		return nil, fmt.Errorf("idp_service.UpdateProfile persist: %w", err)
	}

	after, _ := s.GetProfile(ctx, orgID)
	afterJSON, _ := json.Marshal(profileSummary(after))
	s.audit(ctx, orgID, IDPAuditUpdateProfile, actor, beforeJSON, afterJSON, nil)
	slog.InfoContext(ctx, "idp_service: UpdateProfile",
		"orgID", orgID, "kindChanged", kindChanged,
		"newKind", req.Kind, "newIssuer", req.Issuer)
	return after, nil
}

// SetProfile is the wholesale-replace write path behind PATCH /config {idp}.
// See the IDPService interface doc: kind/issuer/jwksURL are all written as
// given (no empty-means-keep), kind=platform restores cluster defaults, and a
// kind switch cascades the publisher revoke. Map-based Updates writes every
// column including empty strings, so an omitted jwksURL genuinely clears it —
// the field-level behavior org-config-consolidation.md §4/E4 pins.
func (s *idpService) SetProfile(ctx context.Context, orgID, actor, kind, issuer, jwksURL string) (*OrganizationIDPProfile, error) {
	if orgID == "" {
		return nil, fmt.Errorf("orgID required")
	}
	switch kind {
	case "platform", "asgardeo", "custom":
		// ok
	default:
		return nil, fmt.Errorf("invalid kind %q (must be platform|asgardeo|custom)", kind)
	}
	// A platform IDP's issuer/JWKS are cluster config — reset to the
	// platform defaults regardless of anything the caller sent.
	if kind == "platform" {
		issuer = s.platform.Issuer
		jwksURL = s.platform.JWKSURL
	}

	existing, err := s.GetOrCreateProfile(ctx, orgID)
	if err != nil {
		return nil, err
	}
	beforeJSON, _ := json.Marshal(profileSummary(existing))

	kindChanged := kind != existing.Kind

	updates := map[string]interface{}{
		"kind":       kind,
		"issuer":     issuer,
		"jwks_url":   jwksURL,
		"updated_at": time.Now().UTC(),
	}
	// Clear publisher state when kind switches — the existing publisher app
	// belongs to the previous IDP (see UpdateProfile).
	if kindChanged {
		if existing.Kind == "platform" && existing.PublisherClientID != "" && s.thunder != nil {
			if _, derr := s.thunder.DeletePublisherApp(ctx, orgID); derr != nil {
				slog.WarnContext(ctx, "idp_service.SetProfile: Thunder publisher cleanup failed (ignored)",
					"orgID", orgID, "error", derr)
			}
		}
		updates["publisher_client_id"] = ""
		updates["publisher_client_secret"] = ""
		updates["publisher_secret_ref"] = ""
	}

	if err := s.repo.UpdateProfileColumns(ctx, existing, orgID, updates); err != nil {
		s.audit(ctx, orgID, IDPAuditUpdateProfile, actor, beforeJSON, nil, err)
		return nil, fmt.Errorf("idp_service.SetProfile persist: %w", err)
	}

	after, _ := s.GetProfile(ctx, orgID)
	afterJSON, _ := json.Marshal(profileSummary(after))
	s.audit(ctx, orgID, IDPAuditUpdateProfile, actor, beforeJSON, afterJSON, nil)
	slog.InfoContext(ctx, "idp_service: SetProfile",
		"orgID", orgID, "kindChanged", kindChanged, "newKind", kind)
	return after, nil
}

// audit writes one row into idp_audit_events. Best-effort — a failed
// insert logs but doesn't propagate (the principal action already
// happened on Thunder / in the DB).
func (s *idpService) audit(ctx context.Context, orgID, action, actor string, before, after []byte, opErr error) {
	row := IDPAuditEvent{
		OrgID:       orgID,
		Action:      action,
		Actor:       coalesceActor(actor),
		OccurredAt:  time.Now().UTC(),
		BeforeState: before,
		AfterState:  after,
	}
	if opErr != nil {
		row.ErrorMessage = opErr.Error()
	}
	if err := s.repo.CreateAuditEvent(ctx, &row); err != nil {
		slog.WarnContext(ctx, "idp_service: audit insert failed",
			"orgID", orgID, "action", action, "error", err)
	}
}

func coalesceActor(actor string) string {
	if actor == "" {
		return "system"
	}
	return actor
}

// profileSummary projects the row down to the audit-friendly fields —
// drops timestamps + db-internal id so the diff is purely about
// publisher state.
type profileSummaryFields struct {
	Kind              string `json:"kind"`
	Issuer            string `json:"issuer"`
	JWKSURL           string `json:"jwksUrl"`
	PublisherClientID string `json:"publisherClientId"`
	HasClientSecret   bool   `json:"hasClientSecret"`
}

func profileSummary(p *OrganizationIDPProfile) profileSummaryFields {
	if p == nil {
		return profileSummaryFields{}
	}
	return profileSummaryFields{
		Kind:              p.Kind,
		Issuer:            p.Issuer,
		JWKSURL:           p.JWKSURL,
		PublisherClientID: p.PublisherClientID,
		HasClientSecret:   p.PublisherClientSecret != "",
	}
}

// secretRefPath returns the logical OpenBao path for the publisher
// secret. The secret itself lives in PostgreSQL; this path is persisted
// on the profile so a later migration to OpenBao can rewrite the row
// without schema changes.
func secretRefPath(orgID string) string {
	return "secret/aep/" + orgID + "/idp/publisher"
}
