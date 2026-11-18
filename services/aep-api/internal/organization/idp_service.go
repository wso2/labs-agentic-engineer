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
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// IDPService manages per-organisation IDP profiles and the record of the
// org's two clients on the platform IdP. See
// docs/design/api-platform-integration.md.
//
// Lifecycle:
//   - The gitpat submit runs EnsureClient for both org clients: the only
//     writer of their ids and of their secrets, which live only in vault
//     (the org's ae-publisher-client and ae-studio-client references).
//   - POST /build and the deploy only read: RequirePublisherForBuild checks
//     the ae-publisher-client row, the deploy reads the profile's issuer.
//   - The clients themselves are created before aep-api runs (OrgClients).
//     aep-api never creates, changes or deletes one, so a profile kind
//     switch leaves the publisher's record as it is.
//   - There is no user rotation: a secret rotated in the IdP and stored
//     again reaches the org on the next gitpat submit (reconnect GitHub).
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

	// UpdateProfile changes the org's IDP kind / issuer / JWKS URL. The
	// publisher client is the platform IdP's and is not affected.
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
	// are cluster config, not per-org data). Audit-logged.
	SetProfile(ctx context.Context, orgID, actor, kind, issuer, jwksURL string) (*OrganizationIDPProfile, error)

	// EnsureClient writes the secret of the org's pre-created client of kind
	// to the org's vault reference (ae-publisher-client or ae-studio-client)
	// and records the client's ids on the profile. See client_ensure.go.
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
	clients         *OrgClients
	platform        PlatformIDPConfig
	secretRefWriter *SecretRefWriter
	// orgSecrets reads the ae-publisher-client row RequirePublisherForBuild
	// gates on. nil: the gate fails closed.
	orgSecrets OrgSecretRefReader
}

// NewIDPService builds the service. `repo` persists the idp tables; `orgRepo`
// serves the org's IdP org id the client ensure checks (reused, not
// duplicated). `clients` is the record of the org's pre-created clients; it
// may be nil in unit tests that never ensure one. Returns the concrete type
// so the With* setters can chain at the composition root; the concrete value
// still satisfies the IDPService interface for consumers that store it as
// such.
func NewIDPService(repo IDPRepository, orgRepo OrganizationRepository, clients *OrgClients, platform PlatformIDPConfig) *idpService {
	return &idpService{repo: repo, orgRepo: orgRepo, clients: clients, platform: platform}
}

// WithSecretRefWriter attaches the vault writer the client ensure stores the
// org clients' credentials through. nil, or one with Enabled()==false:
// EnsureClient fails as secrets delivery off.
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

// lookupOrgOUID returns the org's IdP org id (the JWT's org id claim, stored
// as Organization.ThunderOrgUUID), or "" when the org row or its id is
// missing.
func (s *idpService) lookupOrgOUID(ctx context.Context, orgHandle string) string {
	org, err := s.orgRepo.GetByName(ctx, orgHandle)
	if err != nil || org == nil {
		slog.DebugContext(ctx, "idp lookupOrgOUID: no org row", "orgHandle", orgHandle, "error", err)
		return ""
	}
	if org.ThunderOrgUUID == nil {
		slog.DebugContext(ctx, "idp lookupOrgOUID: org row has NULL thunder_org_uuid", "orgHandle", orgHandle)
		return ""
	}
	slog.DebugContext(ctx, "idp lookupOrgOUID: resolved the org's IdP org id",
		"orgHandle", orgHandle, "orgOU", org.ThunderOrgUUID.String())
	return org.ThunderOrgUUID.String()
}

// recordPublisherClient reads the org's pre-created publisher client, records
// its client id on the profile, and audits the ensure. The caller stores the
// returned secret in vault, the only place it is kept.
func (s *idpService) recordPublisherClient(ctx context.Context, orgID, actor string) (OrgClient, error) {
	profile, err := s.GetOrCreateProfile(ctx, orgID)
	if err != nil {
		return OrgClient{}, err
	}

	beforeJSON, _ := json.Marshal(profileSummary(profile))

	client, cerr := s.clients.Publisher(orgID)
	if cerr != nil {
		s.audit(ctx, orgID, IDPAuditEnsurePublisher, actor, beforeJSON, nil, cerr)
		return OrgClient{}, fmt.Errorf("idp_service.recordPublisherClient: %w", cerr)
	}

	// The secret is never persisted here.
	updates := map[string]interface{}{
		"publisher_client_id": client.ClientID,
		"updated_at":          time.Now().UTC(),
	}
	if err := s.repo.UpdateProfileColumns(ctx, profile, orgID, updates); err != nil {
		s.audit(ctx, orgID, IDPAuditEnsurePublisher, actor, beforeJSON, nil, err)
		return OrgClient{}, fmt.Errorf("idp_service.recordPublisherClient persist: %w", err)
	}

	// Re-read for the audit "after" snapshot.
	after, _ := s.GetProfile(ctx, orgID)
	afterJSON, _ := json.Marshal(profileSummary(after))
	s.audit(ctx, orgID, IDPAuditEnsurePublisher, actor, beforeJSON, afterJSON, nil)

	slog.InfoContext(ctx, "idp_service: publisher client recorded", "orgID", orgID, "clientID", client.ClientID)
	return client, nil
}

// RequirePublisherForBuild is the POST /build gate: the org's
// ae-publisher-client row, which the gitpat submit's client ensure writes, is
// the record that the runner's publisher credentials exist. It only reads:
// no IdP call, no write. A failed read is returned as is, so it
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

// UpdateProfile changes kind/issuer/JWKS URL for an org. The publisher
// client is the platform IdP's whatever the org's own IdP kind is, so a kind
// switch leaves its record as it is.
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

// validateIDPIssuer refuses a BYO IdP (any kind but platform) with no issuer.
// A deploy pins the org's protected APIs to the profile's issuer; without one
// they would trust every keymanager registered on the cluster, other tenants'
// IdPs included. The platform kind takes the cluster's issuer, so it needs none.
func validateIDPIssuer(kind, issuer string) error {
	if kind != "platform" && strings.TrimSpace(issuer) == "" {
		return &ValidationError{Message: "an issuer is required for an IdP of kind " + kind}
	}
	return nil
}

// SetProfile is the wholesale-replace write path behind PATCH /config {idp}.
// See the IDPService interface doc: kind/issuer/jwksURL are all written as
// given (no empty-means-keep) and kind=platform restores cluster defaults; a
// kind switch leaves the publisher's record as it is. Map-based Updates writes every
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
	if err := validateIDPIssuer(kind, issuer); err != nil {
		return nil, err
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
