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

// service.go — the Service orchestrator: it assembles GET /config from the
// three underlying services and runs the atomic, probe-before-persist PATCH.
// It owns no storage — every read/write delegates to the reused orgcreds/idp
// services; the only new logic here is the cross-section sequencing.

package organization

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/oidc"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
)

// ErrLLMTestRateLimited refuses a Test connection call over the per-org limit.
// Mapped to 429 llm_test_rate_limited at the HTTP edge.
var ErrLLMTestRateLimited = errors.New("orgconfig: too many connection tests")

// SectionError is a per-section failure carrying the RFC-9457 location pointer
// (body.<section>) the console uses to highlight the offending form section. It
// is produced by PATCH probe/persist failures; the HTTP layer maps Status +
// Section into a problem response.
type SectionError struct {
	Section string // "llm" | "agents" | "gitProvider" | "idp"
	Status  int    // 422 (validation) | 409 (conflict) | 502 (upstream) | 503 (no secret store)
	Code    string // the stable reason slug, when the refusal has one (e.g. agents_subscription_requires_claude_code)
	Message string
}

func (e *SectionError) Error() string { return "body." + e.Section + ": " + e.Message }

// SecretsDeliveryUnavailableCode is the section error code of a save that
// needs the secret store on an installation that has none (503).
const SecretsDeliveryUnavailableCode = "secrets_delivery_unavailable"

// sectionErrorFrom classifies a reused-service error into a SectionError with
// the right status: a section-field validation failure is a 422 pointing at the
// section, a cross-mode conflict a 409, an upstream 5xx a 502, no secret store
// a 503. An unclassified
// error is returned verbatim (the caller maps it to an opaque 500).
func sectionErrorFrom(section string, err error) error {
	var se *SectionError
	if errors.As(err, &se) {
		return se
	}
	var ve *ValidationError
	if errors.As(err, &ve) {
		return &SectionError{Section: section, Status: http.StatusUnprocessableEntity, Code: ve.Code, Message: ve.Error()}
	}
	var ce *ConflictError
	if errors.As(err, &ce) {
		return &SectionError{Section: section, Status: http.StatusConflict, Message: ce.Error()}
	}
	if errors.Is(err, ErrSecretsDeliveryUnavailable) {
		return &SectionError{Section: section, Status: http.StatusServiceUnavailable, Code: SecretsDeliveryUnavailableCode,
			Message: "This installation has no secret store configured, so the token cannot be saved. An operator must configure secrets delivery."}
	}
	var ue *UpstreamError
	if errors.As(err, &ue) {
		return &SectionError{Section: section, Status: http.StatusBadGateway, Code: ue.Code, Message: ue.Error()}
	}
	return err
}

// Service is the /config orchestrator. It holds the reused services + the
// platform IDP defaults (used to synthesize a not-yet-persisted idp section on
// GET).
type Service struct {
	credentialSvc *CredentialService
	disconnectSvc *OrgDisconnectService
	idpSvc        IDPService
	agentSettings *AgentSettingsService
	llmTests      *llmTestLimiter
	platformIDP   PlatformIDPConfig
	orgSecrets    *OrgSecretWriter
	secretRefs    OrgSecretRefReader
	converger     StudioConverger
}

// NewService wires the orchestrator. Any dependency may be nil in narrow test
// harnesses that exercise only a subset of sections; each handler nil-guards
// what it needs.
func NewService(
	credentialSvc *CredentialService,
	disconnectSvc *OrgDisconnectService,
	idpSvc IDPService,
	platformIDP PlatformIDPConfig,
) *Service {
	return &Service{
		credentialSvc: credentialSvc,
		disconnectSvc: disconnectSvc,
		idpSvc:        idpSvc,
		llmTests:      newLLMTestLimiter(time.Now),
		platformIDP:   platformIDP,
	}
}

// WithAgentSettings attaches the AI agents card (the llm and agents sections).
//
// A setter rather than another positional parameter, and the reason is the
// section's own shape: an unwired service still projects a truthful `agents` —
// the platform defaults, which is what an org that never opened the card gets
// anyway — so a harness exercising only the other sections is not made to wire
// a service it does not exercise. A PATCH that names llm or agents without one
// is a loud failure, not a silent no-op.
func (s *Service) WithAgentSettings(svc *AgentSettingsService) *Service {
	s.agentSettings = svc
	return s
}

// WithOrgSecretRefs attaches the org secrets' reference rows, which decide
// whether a section reads as configured: a secret lives only in vault, and
// its row is the record that it was written. Unwired, no section that needs
// a row reads as configured.
func (s *Service) WithOrgSecretRefs(refs OrgSecretRefReader) *Service {
	s.secretRefs = refs
	return s
}

// --- GET /config ------------------------------------------------------------

// Get assembles the full config projection for org. A missing llm row, or a
// gitProvider without its credential and github-pat rows, maps to a null
// section (not an error); idp is always present, synthesized
// from the platform defaults when no row exists yet so GET stays side-effect
// free (no row is created on read).
func (s *Service) Get(ctx context.Context, org string) (*orgconfig.ConfigProjection, error) {
	out := &orgconfig.ConfigProjection{}

	if s.agentSettings != nil {
		llm, err := s.agentSettings.Connection(ctx, org)
		if err != nil {
			return nil, fmt.Errorf("orgconfig get llm: %w", err)
		}
		out.LLM = llm
	}

	gitProvider, err := s.gitProviderSection(ctx, org)
	if err != nil {
		return nil, err
	}
	out.GitProvider = gitProvider

	// Always present, even with no service wired: every org has an effective
	// runtime, and the default IS the answer for one that has never chosen.
	// `updatedBy` is what tells a reader which of the two it is looking at, so
	// there is nothing to fake here. The formats likewise: with no service,
	// the default runtime is the only one on offer.
	out.Agents = orgconfig.DefaultAgents()
	out.LLMFormats = llmFormatsFor(out.Agents.AvailableRuntimes)
	if s.agentSettings != nil {
		out.LLMFormats = s.agentSettings.Formats()
		proj, err := s.agentSettings.Effective(ctx, org)
		if err != nil {
			return nil, fmt.Errorf("orgconfig get agents: %w", err)
		}
		out.Agents = proj
		if out.LLM == nil {
			at, err := s.agentSettings.KeyDisconnectedAt(ctx, org)
			if err != nil {
				return nil, fmt.Errorf("orgconfig get llm: %w", err)
			}
			out.LLMDisconnectedAt = at
		}
	}

	out.IDP = s.idpProjection(ctx, org)
	return out, nil
}

// gitProviderSection is the gitProvider section, or nil when the org is not
// connected. Connected takes both a live credential row and the github-pat
// reference row: the PAT lives only in vault, so an org whose row predates
// the reference rows has no usable token and gets the onboarding wizard to
// enter it again. A disconnected row is retained by the disconnect cascade
// (audit trail) but projecting it would keep the console's onboarding gate
// (ADR-0009) and settings card treating the org as connected.
func (s *Service) gitProviderSection(ctx context.Context, org string) (*orgconfig.GitProviderProjection, error) {
	if s.credentialSvc == nil {
		return nil, nil
	}
	proj, err := s.credentialSvc.Status(ctx, org)
	if isNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("orgconfig get gitProvider: %w", err)
	}
	if proj.Status == "disconnected" || s.secretRefs == nil {
		return nil, nil
	}
	ref, err := s.secretRefs.Get(ctx, org, OrgSecretGitHubPAT)
	if err != nil {
		return nil, fmt.Errorf("orgconfig get gitProvider: read the github-pat row: %w", err)
	}
	if ref == nil {
		return nil, nil
	}
	return gitProviderProjectionFrom(proj), nil
}

// idpProjection returns the org's persisted IDP profile, or the platform
// default (kind=platform + cluster issuer/jwks) when none exists yet. Read-only
// — unlike UpdateProfile's GetOrCreateProfile, it never persists on GET.
func (s *Service) idpProjection(ctx context.Context, org string) orgconfig.IDPProjection {
	if s.idpSvc != nil {
		if profile, err := s.idpSvc.GetProfile(ctx, org); err == nil && profile != nil {
			return idpProjectionFrom(profile)
		} else if err != nil {
			slog.WarnContext(ctx, "orgconfig: idp GetProfile failed; falling back to platform default",
				"org", org, "error", err)
		}
	}
	return orgconfig.IDPProjection{
		Kind:    "platform",
		Issuer:  s.platformIDP.Issuer,
		JWKSURL: s.platformIDP.JWKSURL,
	}
}

// --- PATCH /config ----------------------------------------------------------

// Patch applies a config patch atomically: it first rejects the disallowed null
// spellings, then probes every sent-with-value section, and only if ALL probes
// pass does it persist. A probe failure returns a SectionError (422/409/502
// body.<section>) with nothing written, so a bad key in one section can't leave
// another section half-applied. Returns the post-write projection (equal to an
// immediately following GET).
//
// llm and agents are the AI agents card and are judged together, on the state
// the patch leaves, then written as ONE transaction (AgentSettingsService).
func (s *Service) Patch(ctx context.Context, org, actor string, p orgconfig.ConfigPatch) (*orgconfig.ConfigProjection, error) {
	// 1. Reject the null spellings that have a dedicated action / reset instead
	//    (Decision 7). llm:null disconnects, agents:null resets.
	if p.GitProvider.Sent && p.GitProvider.Null {
		return nil, &SectionError{
			Section: "gitProvider", Status: http.StatusUnprocessableEntity,
			Message: "use POST /config/git-provider/disconnect to disconnect the git provider",
		}
	}
	if p.IDP.Sent && p.IDP.Null {
		return nil, &SectionError{
			Section: "idp", Status: http.StatusUnprocessableEntity,
			Message: `an org always has an IDP; reset it with {"kind":"platform"}`,
		}
	}
	card := p.LLM.Sent || p.Agents.Sent
	if card && s.agentSettings == nil {
		return nil, fmt.Errorf("orgconfig patch llm/agents: service not configured")
	}

	// 2. Probe phase — no writes. Any failure aborts the whole patch.
	var probed cardProbe
	if card {
		var err error
		if probed, err = s.agentSettings.probe(ctx, org, p); err != nil {
			return nil, err
		}
	}
	if p.GitProvider.Sent && !p.GitProvider.Null {
		if s.credentialSvc == nil {
			return nil, fmt.Errorf("orgconfig patch gitProvider: service not configured")
		}
		// The PAT lives only in vault: with no secret store the save is
		// refused here, before any section is written.
		if err := s.credentialSvc.RequireSecretsDelivery(); err != nil {
			return nil, sectionErrorFrom("gitProvider", err)
		}
		if err := s.credentialSvc.ValidatePAT(ctx, p.GitProvider.Value.PAT, p.GitProvider.Value.GitHubLogin); err != nil {
			return nil, sectionErrorFrom("gitProvider", err)
		}
	}

	// 3. Persist phase — probes already passed, so these are writes over
	//    freshly-validated inputs. Ordered card → gitProvider → idp.
	sections := []string{}
	if card {
		if err := s.agentSettings.apply(ctx, org, actor, p, probed); err != nil {
			return nil, err
		}
		if p.LLM.Sent {
			sections = append(sections, "llm")
		}
		if p.Agents.Sent {
			sections = append(sections, "agents")
		}
	}
	if p.GitProvider.Sent && !p.GitProvider.Null {
		if _, err := s.credentialSvc.Connect(ctx, org, ConnectRequest{
			Kind:        "user-pat",
			PAT:         p.GitProvider.Value.PAT,
			GitHubLogin: p.GitProvider.Value.GitHubLogin,
		}); err != nil {
			return nil, sectionErrorFrom("gitProvider", err)
		}
		sections = append(sections, "gitProvider")
	}
	if p.IDP.Sent && !p.IDP.Null {
		if s.idpSvc == nil {
			return nil, fmt.Errorf("orgconfig patch idp: service not configured")
		}
		if _, err := s.idpSvc.SetProfile(ctx, org, actor, p.IDP.Value.Kind, p.IDP.Value.Issuer, p.IDP.Value.JWKSURL); err != nil {
			return nil, sectionErrorFrom("idp", err)
		}
		sections = append(sections, "idp")
	}
	// The gitpat submit's setup runs after every section, so an idp kind
	// switch in the same patch (which revokes the publisher app) cannot
	// undo the clients it ensures.
	if p.GitProvider.Sent && !p.GitProvider.Null {
		if err := s.submitGitPAT(ctx, org, p.GitProvider.Value.PAT); err != nil {
			return nil, err
		}
	}

	// Audit which sections were carried — never the secret values (Decision:
	// coarser RBAC compensated by section-level audit logging).
	slog.InfoContext(ctx, "orgconfig.patched", "org", org, "sections", sections)

	out, err := s.Get(ctx, org)
	if err != nil {
		return nil, err
	}
	// What the save's probe found, so an unlisted model or a provider limit
	// is shown to the reader who just saved (Continue in onboarding saves
	// without a separate Test connection).
	out.LLMCheck = probed.check
	return out, nil
}

// TestLLM probes the connection w describes, merged over the org's saved one,
// without writing anything (POST /config/llm/test). Rationed per org; a
// refusal is a SectionError on llm, like the save's.
func (s *Service) TestLLM(ctx context.Context, org string, w orgconfig.LLMPatch) (*orgconfig.LLMCheck, error) {
	if s.agentSettings == nil {
		return nil, fmt.Errorf("orgconfig test llm: service not configured")
	}
	if !s.llmTests.allow(org) {
		slog.WarnContext(ctx, "model connection test: rate limited", "org", org)
		return nil, ErrLLMTestRateLimited
	}
	check, err := s.agentSettings.testConnection(ctx, org, w)
	if err != nil {
		return nil, err
	}
	return &check, nil
}

// --- Action routes ----------------------------------------------------------

// DisconnectGitProvider runs the disconnect cascade. It returns whether a
// connection existed (false → the caller reports an idempotent not_connected).
func (s *Service) DisconnectGitProvider(ctx context.Context, org string) (bool, error) {
	if err := s.disconnectSvc.Disconnect(ctx, org, "manual.disconnect"); err != nil {
		if errors.Is(err, ErrOrgNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("orgconfig disconnect: %w", err)
	}
	return true, nil
}

// RotateIDPClientSecret mints a fresh publisher client secret (returned once).
func (s *Service) RotateIDPClientSecret(ctx context.Context, org, actor string) (string, error) {
	return s.idpSvc.RegenerateClientSecret(ctx, org, actor)
}

// DiscoverIDP resolves an OIDC issuer's discovery document.
func (s *Service) DiscoverIDP(ctx context.Context, issuer string) (issuerOut, jwksURL string, err error) {
	md, err := oidc.DiscoverFromIssuer(ctx, issuer)
	if err != nil {
		return "", "", err
	}
	return md.Issuer, md.JWKSURI, nil
}

// --- projection mappers -----------------------------------------------------

func gitProviderProjectionFrom(p *Projection) *orgconfig.GitProviderProjection {
	if p == nil {
		return nil
	}
	return &orgconfig.GitProviderProjection{
		Kind:              "github",
		Mode:              gitProviderMode(p.Kind),
		GitHubLogin:       p.GitHubLogin,
		IdentityLogin:     p.IdentityLogin,
		IdentityName:      p.IdentityName,
		IdentityEmail:     p.IdentityEmail,
		InstallationID:    p.InstallationID,
		SelectedRepos:     p.SelectedRepos,
		Status:            p.Status,
		ConnectedAt:       p.ConnectedAt,
		LastValidatedAt:   p.LastValidatedAt,
		IdentityChangedAt: p.IdentityChangedAt,
		PrevIdentityLogin: p.PrevIdentityLogin,
	}
}

// gitProviderMode re-expresses the legacy credential kind as the role-named
// mode the config surface exposes.
func gitProviderMode(kind string) string {
	switch kind {
	case "app-installation":
		return "app"
	case "user-pat":
		return "pat"
	default:
		return kind
	}
}

func idpProjectionFrom(p *OrganizationIDPProfile) orgconfig.IDPProjection {
	return orgconfig.IDPProjection{
		Kind:              p.Kind,
		Issuer:            p.Issuer,
		JWKSURL:           p.JWKSURL,
		PublisherClientID: p.PublisherClientID,
		HasClientSecret:   p.PublisherClientSecret != "",
	}
}

func isNotFound(err error) bool {
	var nfe *NotFoundError
	return errors.As(err, &nfe)
}
