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
	"maps"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/thundersvc"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

// IDPService manages per-organisation IDP profiles + the matching
// Thunder publisher OAuth apps. See
// docs/design/api-platform-integration.md.
//
// Lifecycle:
//   - The gitpat submit runs EnsureClient for both org clients: the only
//     writer of the publisher app and of its secret, which lives only in
//     vault (the org's ae-publisher-client reference).
//   - POST /build and the deploy only read: RequirePublisherForBuild checks
//     the ae-publisher-client row, the deploy reads the profile's issuer.
//   - Org delete (or explicit admin action) triggers RevokeOrgPublisher.
//   - There is no user rotation: a lost reference is healed by the next
//     gitpat submit (reconnect GitHub).
//
// Every mutation appends an audit row to idp_audit_events so the
// console "Audit" tab and incident-response have a definitive trail.
type IDPService interface {
	// GetOrCreateProfile returns the org's IDP profile, creating a
	// default platform-kind row (kind=platform, issuer/jwksURL from
	// the platform IDP config) when none exists. The publisher_*
	// columns stay empty until the client ensure records them.
	GetOrCreateProfile(ctx context.Context, orgID string) (*OrganizationIDPProfile, error)

	// GetProfile returns the existing profile, or nil + nil error when
	// none exists yet.
	GetProfile(ctx context.Context, orgID string) (*OrganizationIDPProfile, error)

	// RequirePublisherForBuild is the POST /build gate: it reads the org's
	// ae-publisher-client row and nothing else (no Thunder call, no heal).
	// No row is delivery.ErrPublisherCredentialsMissing with the "Reconnect
	// GitHub" sentence: the gitpat submit is what writes it.
	RequirePublisherForBuild(ctx context.Context, orgID string) error

	// RevokeOrgPublisher deletes the Thunder publisher app, clears the
	// profile row's publisher ids and removes the org's ae-publisher-client
	// reference. Idempotent.
	RevokeOrgPublisher(ctx context.Context, orgID, actor string) (bool, error)

	// UpdateProfile changes the org's IDP kind / issuer / JWKS URL.
	// Switching kind invalidates any existing publisher app (Thunder is
	// a separate keymanager from Asgardeo/custom OIDC), so the call
	// cascades a RevokeOrgPublisher against the previous kind's IDP.
	// Audit-logged.
	//
	// Operator follow-up: after this call, the platform admin must
	// ensure the new IDP's keymanager is registered on the gateway of
	// EVERY environment the org deploys to — the `jwtauth_v1` keymanagers
	// of each `api-platform-<org>-<env>` release
	// (deployments/scripts/setup-environment-gateway.sh). Keymanager
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

	// EnsureClient makes sure the org's Thunder client of kind exists and
	// that its secret is in the org's vault reference (ae-publisher-client
	// or ae-studio-client): an app Thunder creates is stored with the
	// secret Thunder returns once; an app whose reference row is missing is
	// healed with a new secret, written to the vault before Thunder is given
	// it; an app with its row is left alone. See client_ensure.go.
	EnsureClient(ctx context.Context, orgID string, kind ClientKind) error
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
	// orgSecrets reads the ae-publisher-client row RequirePublisherForBuild
	// gates on. nil: the gate fails closed.
	orgSecrets OrgSecretRefReader
}

// NewIDPService builds the service. `repo` persists the idp tables; `orgRepo`
// serves the org → Thunder OU lookup the client ensure needs (reused, not
// duplicated). `thunder` may be nil in unit tests — the service rejects
// EnsureClient / RevokeOrgPublisher with ErrIDPThunderUnavailable when so.
// Read methods (GetProfile, GetOrCreateProfile) keep working. Returns the
// concrete type so the With* setters can chain at the composition root; the
// concrete value still satisfies the IDPService interface for consumers that
// store it as such.
func NewIDPService(repo IDPRepository, orgRepo OrganizationRepository, thunder thundersvc.Client, platform PlatformIDPConfig) *idpService {
	return &idpService{repo: repo, orgRepo: orgRepo, thunder: thunder, platform: platform}
}

// WithSecretRefWriter attaches the vault writer the client ensure stores the
// org clients' credentials through (and RevokeOrgPublisher removes them
// with). nil, or one with Enabled()==false: EnsureClient fails as secrets
// delivery off.
func (s *idpService) WithSecretRefWriter(w *SecretRefWriter) *idpService {
	s.secretRefWriter = w
	return s
}

// WithOrgSecretRefs attaches the org secret rows RequirePublisherForBuild
// reads; chainable.
func (s *idpService) WithOrgSecretRefs(refs OrgSecretRefReader) *idpService {
	s.orgSecrets = refs
	return s
}

// ErrIDPThunderUnavailable means the Thunder admin client isn't wired
// (missing system credentials, etc): the client ensure and the publisher
// revoke cannot run.
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

// ensurePublisherApp makes sure the org's Thunder publisher app exists
// (EnsurePublisherApp, with its OU and claim self-heal) under orgOUID,
// records its client id and entity id on the profile, and audits the ensure.
// The returned app carries the secret only when Created; the caller stores
// it in vault, the only place it is kept.
func (s *idpService) ensurePublisherApp(ctx context.Context, orgID, actor, orgOUID string) (thundersvc.OrgApp, error) {
	profile, err := s.GetOrCreateProfile(ctx, orgID)
	if err != nil {
		return thundersvc.OrgApp{}, err
	}

	beforeJSON, _ := json.Marshal(profileSummary(profile))

	// orgOUID is the org's Thunder OU id (JWT ouId), so the publisher app is
	// registered under the org's OU — its cc token then carries
	// ouHandle == orgHandle, which the publisher-token verifier requires.
	// Empty (org UUID not yet backfilled) falls back to the default OU.
	if orgOUID == "" {
		slog.WarnContext(ctx, "idp_service: org Thunder OU id unknown — publisher app will use the default OU; runner token ouHandle may not match the org. Ensure the org row has thunder_org_uuid (user must have logged in with an ouId claim).",
			"orgID", orgID)
	} else {
		slog.DebugContext(ctx, "idp_service: provisioning publisher under org OU", "orgID", orgID, "orgOU", orgOUID)
	}
	app, terr := s.thunder.EnsurePublisherApp(ctx, orgID, orgOUID, profile.PublisherThunderAppID)
	if terr != nil {
		s.audit(ctx, orgID, IDPAuditEnsurePublisher, actor, beforeJSON, nil, terr)
		return thundersvc.OrgApp{}, fmt.Errorf("idp_service.ensurePublisherApp: %w", terr)
	}

	// Persist clientId and the Thunder entity id (the next lookup reads by
	// that id). The secret is never persisted here.
	updates := map[string]interface{}{
		"publisher_client_id":      app.ClientID,
		"publisher_thunder_app_id": app.EntityID,
		"updated_at":               time.Now().UTC(),
	}
	if err := s.repo.UpdateProfileColumns(ctx, profile, orgID, updates); err != nil {
		s.audit(ctx, orgID, IDPAuditEnsurePublisher, actor, beforeJSON, nil, err)
		return thundersvc.OrgApp{}, fmt.Errorf("idp_service.ensurePublisherApp persist: %w", err)
	}

	// Re-read for the audit "after" snapshot.
	after, _ := s.GetProfile(ctx, orgID)
	afterJSON, _ := json.Marshal(profileSummary(after))
	s.audit(ctx, orgID, IDPAuditEnsurePublisher, actor, beforeJSON, afterJSON, nil)

	slog.InfoContext(ctx, "idp_service: publisher app ensured",
		"orgID", orgID,
		"clientID", app.ClientID,
		"created", app.Created,
	)
	return app, nil
}

// RequirePublisherForBuild is the POST /build gate (06 §3, O-3): the org's
// ae-publisher-client row, which the gitpat submit's client ensure writes, is
// the record that the runner's publisher credentials exist. It only reads:
// no Thunder call, no heal, no write. A failed read is returned as is, so it
// is never mistaken for a missing row.
func (s *idpService) RequirePublisherForBuild(ctx context.Context, orgID string) error {
	if orgID == "" {
		return fmt.Errorf("orgID required")
	}
	if s.orgSecrets == nil {
		return fmt.Errorf("publisher gate: org secret rows not configured")
	}
	row, err := s.orgSecrets.Get(ctx, orgID, OrgSecretPublisherClient)
	if err != nil {
		return fmt.Errorf("publisher gate: read the %s row: %w", OrgSecretPublisherClient, err)
	}
	if row == nil || row.Name == "" {
		return fmt.Errorf("%w: %s", delivery.ErrPublisherCredentialsMissing, delivery.PublisherReconnectMessage)
	}
	return nil
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

	deleted, terr := s.thunder.DeletePublisherApp(ctx, orgID, profile.PublisherThunderAppID)
	if terr != nil {
		s.audit(ctx, orgID, IDPAuditRevokePublisher, actor, beforeJSON, nil, terr)
		return false, fmt.Errorf("idp_service.RevokeOrgPublisher: %w", terr)
	}

	if err := s.repo.UpdateProfileColumns(ctx, profile, orgID, clearedPublisherIDs()); err != nil {
		s.audit(ctx, orgID, IDPAuditRevokePublisher, actor, beforeJSON, nil, err)
		return deleted, fmt.Errorf("idp_service.RevokeOrgPublisher persist: %w", err)
	}
	s.forgetPublisherSecret(ctx, orgID)

	after, _ := s.GetProfile(ctx, orgID)
	afterJSON, _ := json.Marshal(profileSummary(after))
	s.audit(ctx, orgID, IDPAuditRevokePublisher, actor, beforeJSON, afterJSON, nil)

	slog.InfoContext(ctx, "idp_service: RevokeOrgPublisher",
		"orgID", orgID, "deleted", deleted)
	return deleted, nil
}

// clearedPublisherIDs is the profile update that forgets the publisher app:
// its client id and Thunder entity id.
func clearedPublisherIDs() map[string]interface{} {
	return map[string]interface{}{
		"publisher_client_id":      "",
		"publisher_thunder_app_id": "",
		"updated_at":               time.Now().UTC(),
	}
}

// forgetPublisherSecret removes the org's ae-publisher-client reference (row,
// then the vault entry) once its Thunder app is gone, so the build gate and
// dispatch refuse instead of handing out credentials Thunder no longer
// honours; the next gitpat submit writes a new one. Best-effort, logged
// value-free: a vault outage does not strand the Thunder delete.
func (s *idpService) forgetPublisherSecret(ctx context.Context, orgID string) {
	if s.secretRefWriter == nil || !s.secretRefWriter.Enabled() {
		return
	}
	if err := s.secretRefWriter.DeletePublisher(ctx, orgID); err != nil {
		slog.WarnContext(ctx, "idp_service: publisher reference delete failed (continuing)",
			"orgID", orgID, "error", err)
	}
}

// UpdateProfile changes kind/issuer/JWKS URL for an org. When kind
// switches, the existing publisher app is revoked (so the next gitpat
// submit creates a fresh one tied to the new IDP). When only issuer/JWKS URL
// change but kind stays the same, the publisher app is preserved.
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
		s.dropPublisherApp(ctx, orgID, existing)
		maps.Copy(updates, clearedPublisherIDs())
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
		s.dropPublisherApp(ctx, orgID, existing)
		maps.Copy(updates, clearedPublisherIDs())
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

// dropPublisherApp is a kind switch's publisher cleanup, before the caller
// clears the profile's publisher ids: only for the platform→other transition,
// best-effort, it deletes the Thunder app and then its ae-publisher-client
// reference, so the build gate stops passing on credentials of a deleted app.
// Skipped when thunder isn't configured (the caller just clears the ids).
func (s *idpService) dropPublisherApp(ctx context.Context, orgID string, existing *OrganizationIDPProfile) {
	if existing.Kind != "platform" || existing.PublisherClientID == "" || s.thunder == nil {
		return
	}
	if _, err := s.thunder.DeletePublisherApp(ctx, orgID, existing.PublisherThunderAppID); err != nil {
		slog.WarnContext(ctx, "idp_service: Thunder publisher cleanup on kind switch failed (ignored)",
			"orgID", orgID, "error", err)
		return
	}
	s.forgetPublisherSecret(ctx, orgID)
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
// publisher state. The profile holds no secret to summarise.
type profileSummaryFields struct {
	Kind              string `json:"kind"`
	Issuer            string `json:"issuer"`
	JWKSURL           string `json:"jwksUrl"`
	PublisherClientID string `json:"publisherClientId"`
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
	}
}
