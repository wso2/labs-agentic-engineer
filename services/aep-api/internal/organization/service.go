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
	"net/url"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/oidc"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
)

// ErrLLMTestRateLimited refuses a Test connection call over the per-org limit.
// Mapped to 429 llm_test_rate_limited at the HTTP edge.
var ErrLLMTestRateLimited = errors.New("orgconfig: too many connection tests")

// ErrGitHubAppNotConfigured is returned by StartGitHubConnect when the GitHub
// App OAuth client isn't wired on this deployment (the App-mode connect path is
// unavailable). Mapped to 503 at the HTTP edge.
var ErrGitHubAppNotConfigured = errors.New("orgconfig: github app oauth client not configured")

// SectionError is a per-section failure carrying the RFC-9457 location pointer
// (body.<section>) the console uses to highlight the offending form section. It
// is produced by PATCH probe/persist failures; the HTTP layer maps Status +
// Section into a problem response.
type SectionError struct {
	Section string // "llm" | "agents" | "gitProvider" | "idp" | "sreLlm" (the SRE model connection)
	Status  int    // 422 (validation) | 409 (conflict) | 502 (upstream)
	Code    string // the stable reason slug, when the refusal has one (e.g. agents_subscription_requires_claude_code)
	Message string
}

func (e *SectionError) Error() string { return "body." + e.Section + ": " + e.Message }

// sectionErrorFrom classifies a reused-service error into a SectionError with
// the right status: a section-field validation failure is a 422 pointing at the
// section, a cross-mode conflict a 409, an upstream 5xx a 502. An unclassified
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
	var ue *UpstreamError
	if errors.As(err, &ue) {
		return &SectionError{Section: section, Status: http.StatusBadGateway, Code: ue.Code, Message: ue.Error()}
	}
	return err
}

// Service is the /config orchestrator. It holds the reused services + the
// platform IDP defaults (used to synthesize a not-yet-persisted idp section on
// GET) + the GitHub App connect parameters.
type Service struct {
	credentialSvc *CredentialService
	disconnectSvc *OrgDisconnectService
	bearerSvc     *BearerService
	idpSvc        IDPService
	agentSettings *AgentSettingsService
	sreModelSvc   *SreModelConnectionService
	sreStatus     SREAgentStatusReader
	llmTests      *llmTestLimiter
	platformIDP   PlatformIDPConfig

	publicURL   string
	appClientID string
}

// NewService wires the orchestrator. The defaulting for publicURL mirrors the
// legacy NewOrgGitHubController so the connect-session redirect_uri is
// identical. Any dependency may be nil in narrow test harnesses that exercise
// only a subset of sections; each handler nil-guards what it needs.
func NewService(
	credentialSvc *CredentialService,
	disconnectSvc *OrgDisconnectService,
	bearerSvc *BearerService,
	idpSvc IDPService,
	platformIDP PlatformIDPConfig,
	publicURL, appClientID string,
) *Service {
	if publicURL == "" {
		publicURL = "http://localhost:8090"
	}
	return &Service{
		credentialSvc: credentialSvc,
		disconnectSvc: disconnectSvc,
		bearerSvc:     bearerSvc,
		idpSvc:        idpSvc,
		llmTests:      newLLMTestLimiter(time.Now),
		platformIDP:   platformIDP,
		publicURL:     publicURL,
		appClientID:   appClientID,
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

// WithSreModel attaches the org's SRE model connection (the sreLlm section).
// A setter for the same reason WithAgentSettings is one: an unwired service
// still projects a truthful sreLlm=nil ("none"), so a harness exercising only
// the other sections doesn't have to wire it.
func (s *Service) WithSreModel(svc *SreModelConnectionService) *Service {
	s.sreModelSvc = svc
	return s
}

// SREAgentStatusReader reports how the OpenChoreo SRE agent's rollout stands
// for org: status is one of unconfigured|applying|running|failed, reason says
// why when there is something to add. ok=false means the agent does not serve
// org (one observability plane serves one org), so there is nothing to show.
type SREAgentStatusReader interface {
	Status(ctx context.Context, org string) (status, reason string, ok bool, err error)
}

// sreStatusUnavailable is the reason GET /config shows when the status reader
// fails: the settings stay loadable while the cluster API is down.
const sreStatusUnavailable = "SRE agent status unavailable: cannot read the observability plane"

// WithSREAgentStatus attaches the SRE agent's status, which turns on GET
// /config's sreAgent section. Without it the server does not push the SRE
// agent's configuration, and sreAgent is null.
func (s *Service) WithSREAgentStatus(r SREAgentStatusReader) *Service {
	s.sreStatus = r
	return s
}

// --- GET /config ------------------------------------------------------------

// Get assembles the full config projection for org. A missing llm/gitProvider
// row maps to a null section (not an error); idp is always present, synthesized
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

	if s.credentialSvc != nil {
		proj, err := s.credentialSvc.Status(ctx, org)
		switch {
		// A disconnected row is retained by the disconnect cascade (audit trail,
		// app re-adoption) but the config contract says null = not connected —
		// projecting it would keep the console's onboarding gate (ADR-0009) and
		// settings card treating the org as connected.
		case err == nil && proj.Status != "disconnected":
			out.GitProvider = gitProviderProjectionFrom(proj)
		case err == nil || isNotFound(err):
			out.GitProvider = nil
		default:
			return nil, fmt.Errorf("orgconfig get gitProvider: %w", err)
		}
	}

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

	if s.sreModelSvc != nil {
		proj, err := s.sreModelSvc.Projection(ctx, org)
		if err != nil {
			return nil, fmt.Errorf("orgconfig get sreLlm: %w", err)
		}
		out.SreLLM = proj
		if s.sreStatus != nil {
			if out.SreAgent, err = s.sreAgentProjection(ctx, org); err != nil {
				return nil, fmt.Errorf("orgconfig get sreAgent: %w", err)
			}
		}
	}

	out.IDP = s.idpProjection(ctx, org)
	return out, nil
}

// sreAgentProjection is the SRE agent as the org's settings leave it: the
// connection it runs on and how its rollout stands. nil when the agent does
// not serve org. A status read that fails shows as failed rather than failing
// the whole GET.
func (s *Service) sreAgentProjection(ctx context.Context, org string) (*orgconfig.SreAgentProjection, error) {
	status, reason, ok, err := s.sreStatus.Status(ctx, org)
	switch {
	case err != nil:
		slog.WarnContext(ctx, "orgconfig.sre_agent_status_unavailable", "org", org, "err", err)
		status, reason = "failed", sreStatusUnavailable
	case !ok:
		return nil, nil
	}
	eff, err := s.sreModelSvc.EffectiveSRE(ctx, org)
	if err != nil {
		return nil, err
	}
	return &orgconfig.SreAgentProjection{
		Enabled: true,
		Source:  string(eff.Source),
		Model:   eff.Conn.Model,
		Host:    eff.Conn.Host,
		Status:  status,
		Reason:  reason,
	}, nil
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
		if err := s.credentialSvc.ValidatePAT(ctx, p.GitProvider.Value.PAT, p.GitProvider.Value.GitHubLogin); err != nil {
			return nil, sectionErrorFrom("gitProvider", err)
		}
	}
	if p.SreLLM.Sent && s.sreModelSvc == nil {
		return nil, fmt.Errorf("orgconfig patch sreLlm: service not configured")
	}
	var sreLLM sreDraft
	if p.SreLLM.Sent && !p.SreLLM.Null {
		var err error
		if sreLLM, err = s.sreModelSvc.Check(ctx, org, p.SreLLM.Value); err != nil {
			return nil, sectionErrorFrom("sreLlm", err)
		}
	}

	// 3. Persist phase — probes already passed, so these are writes over
	//    freshly-validated inputs. Ordered card → gitProvider → sreLlm → idp.
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
	if p.SreLLM.Sent {
		if p.SreLLM.Null {
			if err := s.sreModelSvc.Clear(ctx, org, actor); err != nil {
				return nil, sectionErrorFrom("sreLlm", err)
			}
		} else if err := s.sreModelSvc.Persist(ctx, org, actor, sreLLM); err != nil {
			return nil, sectionErrorFrom("sreLlm", err)
		}
		sections = append(sections, "sreLlm")
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

// StartGitHubConnect mints a connect-state JWT and returns the GitHub App OAuth
// authorize URL. Mirrors the legacy start-github-connect handler exactly (same
// state issuance, same redirect_uri built from the unchanged callback path).
func (s *Service) StartGitHubConnect(ctx context.Context, org, actor string, installationID int64) (string, error) {
	if s.appClientID == "" {
		return "", ErrGitHubAppNotConfigured
	}
	state, err := s.bearerSvc.IssueConnectState(org, actor, installationID, 15*time.Minute)
	if err != nil {
		return "", fmt.Errorf("orgconfig start connect: %w", err)
	}
	redirectURI := s.publicURL + ConnectCallbackPath
	authorizeURL := "https://github.com/login/oauth/authorize?client_id=" + url.QueryEscape(s.appClientID) +
		"&redirect_uri=" + url.QueryEscape(redirectURI) +
		"&state=" + url.QueryEscape(state)
	return authorizeURL, nil
}

// DisconnectGitProvider runs the disconnect cascade. It returns whether a
// connection existed (false → the caller reports an idempotent not_connected).
func (s *Service) DisconnectGitProvider(ctx context.Context, org string, uninstall bool) (bool, error) {
	if err := s.disconnectSvc.Disconnect(ctx, org, "manual.disconnect", uninstall); err != nil {
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
