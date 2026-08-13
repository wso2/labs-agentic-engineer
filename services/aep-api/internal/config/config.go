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

package config

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

// Config holds all application configuration.
type Config struct {
	ServerHost string
	ServerPort int
	LogLevel   string

	PlatformAPI PlatformAPIConfig
	DatabaseURL string

	// Test mode — registration gate for the dev/test surface (/_dev/v1/*).
	// Defaults false, so the surface is absent in every real env.
	TestMode bool

	// LocalOpenBaoRepairEnabled gates POST /_dev/v1/secret-ref-resync — the
	// in-process SecretRefWriter resync helper (status-only response; no secret
	// material on the wire). Distinct from TestMode because TestMode is also
	// true on the shared wso2cloud dev release binding. Splitting the two means
	// the resync route only mounts where deployments/docker-compose.yml
	// explicitly opts in; cloud release bindings never set this var so the route
	// never registers in deployed environments.
	LocalOpenBaoRepairEnabled bool

	// DeploymentTier guards dev-only destructive migrations and seed paths.
	// The destructive BFF migrations refuse to run unless tier=dev.
	DeploymentTier string

	// PlaygroundTokenEnabled gates POST /internal/v1/mcp/playground-token — a
	// caller-auth-free endpoint that mints a short-lived MCP token so a human
	// can drive the @aep/playground CLI against a live aep-api
	// without a caller-auth story (an open decision this endpoint deliberately
	// does not prejudge). Defaults false, so the route is ABSENT (404, not
	// 403) everywhere except deployments/docker-compose.yml, which opts in for
	// local dev. Read from PLAYGROUND_TOKEN_ENABLED.
	PlaygroundTokenEnabled bool

	// PlatformResourcesEnabled gates discovery of cluster-scoped platform
	// resource types (ResourceTypeCatalog.List → OC ListClusterResourceTypes).
	// Defaults TRUE: platform resources are a core capability; deployments that
	// offer no platform-resource catalog must opt out explicitly
	// (PLATFORM_RESOURCES_ENABLED=false). Unlike PlaygroundTokenEnabled /
	// AutoMergeCodingPRs (opt-in extras that default false), this is an
	// opt-out. Read from PLATFORM_RESOURCES_ENABLED.
	PlatformResourcesEnabled bool

	// AutoMergeCodingPRs gates auto-merge of coding-agent pull requests: when
	// true, a coding-agent PR is squash-merged the moment it opens, removing the
	// human review gate and letting the path-based build fan-out deploy the fix
	// end-to-end without a human. Defaults FALSE (secure default): auto-merge
	// deploys UNREVIEWED agent-authored code, which is not guaranteed correct, so
	// a deployment must opt in explicitly (set it in that deployment's config,
	// e.g. docker-compose). Read from AUTO_MERGE_CODING_PRS.
	AutoMergeCodingPRs bool

	// TenantGateMode controls the central per-route tenant gate (§6.1b).
	// ENFORCE BY DEFAULT (zero-config): "enforce" 404s a path-vs-JWT org
	// mismatch (closes IDOR-1..5). Set TENANT_GATE_MODE=log to downgrade to
	// observe-only — compute the decision, emit a "would-deny" canary
	// line, and pass through. Read from TENANT_GATE_MODE; unset ⇒ enforce.
	TenantGateMode string

	// OAuthStateSigningKey is the HS256 key used to sign the connect-state
	// JWT that rides the GitHub App OAuth `state` query param (CSRF
	// protection on the connect callback). Task JWTs use RS256 via
	// TaskTokenSigningKey; this key has no other use.
	OAuthStateSigningKey string

	// BFFPublicURL is the user-visible BFF base — used as the basis for
	// the App-mode redirect after callback (302 → console settings page).
	BFFPublicURL string

	// TaskTokenSigningKey is the PEM-encoded RSA private key used to sign
	// Task JWTs. The matching public key is published at /auth/external/jwks.json.
	TaskTokenSigningKey string
	// TaskTokenIssuer is the iss claim on issued Task JWTs (e.g. "aep-bff").
	TaskTokenIssuer string
	// TaskTokenAudience is the aud claim — fixed to "git-service" today, the
	// only verifier of Task JWTs.
	TaskTokenAudience string

	// Build watcher git_clone_failed_auth retry budget. Default 3 attempts.
	// Configurable via BUILD_AUTH_RETRY_BUDGET; tests set to 0 to force
	// exhaustion on the first auth failure.
	BuildAuthRetryBudget int

	// Thunder admin client config for per-org publisher OAuth app lifecycle.
	// Loaded from env vars
	// THUNDER_ADMIN_URL / THUNDER_SYSTEM_CLIENT_ID / THUNDER_SYSTEM_CLIENT_SECRET.
	// When ClientID is empty the BFF logs a warning and the IDP service
	// returns ErrIDPThunderUnavailable (non-fatal — protected components
	// still deploy, just without per-org publishers).
	ThunderAdmin ThunderAdminConfig

	// Platform IDP defaults seeded into organization_idp_profiles rows
	// on first access. Loaded from PLATFORM_IDP_ISSUER /
	// PLATFORM_IDP_JWKS_URL — should match the cluster's Thunder
	// keymanager in gateway-config.yaml.
	PlatformIDP PlatformIDPDefaults

	Observability ObservabilityConfig
	AgentsSvc     AgentsSvcConfig
	ServiceAuth   ServiceAuthConfig
	Workspace     WorkspaceConfig

	// SkillsDir is the on-disk platform skill library the BFF seeds + reconciles
	// into each org's skills repo (SKILLS_DIR). It is COPY'd into the image from
	// the repo-root skills/ directory (the single authored source) — no longer
	// go:embed'd. Defaults to /app/skills (the image path); tests inject their
	// own fs.FS and never read this.
	SkillsDir string

	// AgentPlatformURL is the URL the coding-agent runner pod uses to call
	// back to the BFF (credentials refresh, MCP, skills). In cloud this is the
	// public/internal gateway route to app-factory-api — runner pods live in
	// the dataplane and cannot reach the control-plane ClusterIP. Locally it
	// is typically http://host.k3d.internal:9090.
	AgentPlatformURL string

	// AEPInternalBaseURL is the BFF's own base URL as reached by peer
	// cluster-internal services (agents-service) for the internal MCP
	// discovery surface (/internal/v1/mcp). The BFF hands agents-service an
	// `mcp: {url, token}` bundle in the architect request; agents-service calls
	// back to `AEPInternalBaseURL + /internal/v1/mcp` with the BFF-signed MCP
	// token. Optional — empty disables MCP propagation (the additive `mcp`
	// field is simply omitted; consumed in the E-phase). Read from
	// AEP_API_INTERNAL_BASE_URL.
	AEPInternalBaseURL string

	// JWKS settings for inbound JWT verification — Thunder publishes the
	// User JWT and Service JWT signing key at JWKSURL; verifiers refresh
	// on kid miss. Issuer and audience configure RFC 7519 claim checks.
	JWKSURL                string
	JWTAllowedIssuer       string
	JWTAllowedAudience     string
	JWTResourceMetadataURL string

	// Git-service config fields.

	// GitProvider selects the git host implementation (clients/<provider>)
	// wired behind gitrepo's provider ports. Read from GIT_PROVIDER; default
	// and only supported value today is "github". Validate() rejects anything
	// else with a boot error.
	GitProvider string

	GitHubRepoVisibility string
	GitHubCommitterName  string
	GitHubCommitterEmail string

	// WebhookDeliveryURL is the URL the platform registers on each repo.
	WebhookDeliveryURL string
	// WebhookHMACSecret is the HMAC key for inbound webhook validation.
	WebhookHMACSecret string

	// CredentialEncryptionKey is the base64-encoded 32-byte AES-256 key
	// used to encrypt per-org credentials at rest in org_secrets.
	CredentialEncryptionKey string

	// OpenBaoAddr / OpenBaoToken — local-only OpenBao connection for the
	// in-process OpenBao-direct secrets provider (NewOSSOptions). Empty
	// leaves SecretsProvider nil (delivery off). Never set in cloud.
	OpenBaoAddr  string
	OpenBaoToken string

	GitHubAppID             string
	GitHubAppClientID       string // App's OAuth client_id; used to build the OAuth authorize URL
	GitHubAppClientSecret   string
	GitHubAppSlug           string // App's URL slug, used in the install URL
	GitHubAppPrivateKeyPath string

	// CredentialValidatorInterval is the periodic credential-validator
	// sweep interval. Default 24h.
	CredentialValidatorInterval time.Duration

	// AgentRunnerImage is the docker image the runner Job uses — ONE image
	// for BOTH task kinds (implementation and validation; it bakes
	// Playwright + chromium). Pinned at deploy time, no built-in default;
	// `:latest` is OK in dev but the cloud release-binding should resolve to
	// a digest. Empty ⇒ dispatch is off and fails loudly.
	AgentRunnerImage string

	// CodingAgentComponentRetention is how many finished coding-agent
	// Components a project may keep (LRU reap before each create). Defaults
	// to codingagent.DefaultCodingAgentComponentRetention (10). Override via
	// CODING_AGENT_COMPONENT_RETENTION so local E2E can observe prune without
	// eleven cycles; cloud keeps the code default unless explicitly set.
	CodingAgentComponentRetention int

	// Temporal holds the workflow-engine connection settings for the devflow
	// feature. Enabled iff HostPort is set — unset leaves aep-api fully
	// functional with the workflow endpoints answering 503.
	Temporal TemporalConfig
}

// Validate checks format/consistency invariants the per-field env readers
// can't express — e.g. an AES key must decode from base64 to exactly 32 bytes.
// Called at the end of Load; kept as a method so config_test.go can drive it
// table-style without touching the environment. Accumulates all failures.
func (c Config) Validate() error {
	var errs []string
	if key, err := base64.StdEncoding.DecodeString(c.CredentialEncryptionKey); err != nil || len(key) != 32 {
		errs = append(errs, "CREDENTIAL_ENCRYPTION_KEY must be a base64-encoded 32-byte key")
	}
	if c.GitProvider != "github" {
		errs = append(errs, fmt.Sprintf("unknown GIT_PROVIDER %q — supported: github", c.GitProvider))
	}
	// Required config — fail fast at boot instead of soft-warning and surfacing
	// the failure later at runtime. Both planes (docker-compose + Helm) always set
	// these; an empty value means a misconfigured deployment, not a valid mode.
	if c.JWKSURL == "" {
		// Without JWKS the inbound verifier rejects every /api/ request (401);
		// there is no unsigned-claim fallback.
		errs = append(errs, "JWKS_URL is required — the inbound JWT verifier cannot start without it")
	}
	if c.TaskTokenSigningKey == "" {
		// Without the RS256 signing key every task dispatch (and the runner
		// callbacks that verify against the published JWKS) fails.
		errs = append(errs, "BFF_TASK_SIGNING_KEY (or _PATH) is required — task dispatch cannot start without it")
	}
	if len(errs) > 0 {
		return fmt.Errorf("configuration errors:\n%s", strings.Join(errs, "\n"))
	}
	return nil
}

// ThunderAdminConfig holds the aep-system-client OAuth2 credentials
// + base URL the BFF uses to manage Thunder applications (per-org
// publisher lifecycle). The same Thunder instance that fronts user
// PKCE login — see deployments/single-cluster/values-thunder.yaml's
// CONFIDENTIAL_APPS for the row that ships these credentials.
type ThunderAdminConfig struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
}

// PlatformIDPDefaults are the issuer + JWKS URL of the cluster's
// platform IDP (Thunder in v1). Seeded into every new
// organization_idp_profiles row.
type PlatformIDPDefaults struct {
	Issuer  string
	JWKSURL string
}

// ServiceAuthConfig holds OAuth2 client_credentials settings for
// service-to-service authentication (e.g. BFF → OpenChoreo API).
type ServiceAuthConfig struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
	HostHeader   string // Thunder Host header for k3d routing
}

// AgentsSvcConfig holds connection + M2M settings for the file-mutation agents
// service (services/agents) — the requirements/design/chat generation flows AND
// the tasks-github-native plan turns (toolset:"task-plan"). The legacy AI-SDK
// agents service and its config are gone. The BFF mints a per-call HS256 M2M
// bearer from JWTSecret with aud=JWTAudience (the service's AGENT_JWT_SECRET /
// AGENT_JWT_AUDIENCE).
type AgentsSvcConfig struct {
	BaseURL     string
	JWTSecret   string
	JWTAudience string
	JWTIssuer   string
}

// WorkspaceConfig holds the shared git-workspaces mount settings: the mount root
// where bare repo mirrors + per-SHA snapshots live, plus the disk-lifecycle
// (reaper) knobs. aep-api is the sole writer of the mount; the agents service
// consumes read-only snapshots from the same volume.
type WorkspaceConfig struct {
	// Root is the workspace mount root (AEP_WORKSPACE_ROOT). Layout under it:
	// repos/<orgId>/<projectId>/<repoSlug>/{git,repo.lock,snapshots/<sha>},
	// trash/<ulid>, tmp/.
	Root string
	// ReapInterval is the background reaper sweep cadence (trash purge,
	// snapshot age-reap, orphan reconciliation, quota/LRU eviction).
	ReapInterval time.Duration
	// SnapshotMaxAge — snapshots/<sha> dirs older than this and not the
	// repo's current HEAD are reaped.
	SnapshotMaxAge time.Duration
	// TrashMaxAge — trash/<ulid> entries (phase 1 of the two-phase delete)
	// older than this are purged.
	TrashMaxAge time.Duration
	// OrgQuotaBytes is the per-org disk quota before LRU eviction kicks in.
	OrgQuotaBytes int64
	// DiskHighPct / DiskLowPct are the statfs water marks (%): usage above
	// high triggers eviction, which runs until usage drops below low.
	DiskHighPct int
	DiskLowPct  int
}

// ObservabilityConfig holds connection settings for the OpenChoreo Observer
// service. BaseURL is optional; if empty, the BFF returns 503
// progress_unavailable on the /progress/* endpoints. Auth fields drive the
// Thunder client_credentials flow used to read workflow-run logs.
type ObservabilityConfig struct {
	BaseURL string

	// OAuth client_credentials settings — wired to the platform-default
	// reader app `openchoreo-observer-resource-reader-client`. Promoting to
	// multi-tenant cloud should swap this for a per-app registration (see
	// task-execution-progress.md §5.4).
	TokenURL     string
	ClientID     string
	ClientSecret string
	HostHeader   string
}

// PlatformAPIConfig holds connection settings for the OpenChoreo platform API.
type PlatformAPIConfig struct {
	BaseURL    string
	HostHeader string
}

// TemporalConfig holds connection settings for the Temporal server that runs
// the milestone run supervisor (internal/delivery/run). HostPort empty ⇒ no
// worker starts, so a claimed version's run settles itself with a plan-failed
// reason rather than waiting for a supervisor that will never arrive.
type TemporalConfig struct {
	HostPort  string // TEMPORAL_HOSTPORT, e.g. host.docker.internal:7233
	Namespace string // TEMPORAL_NAMESPACE, default "default"
	TaskQueue string // TEMPORAL_TASKQUEUE, default "aep-devflow"
}

// Enabled reports whether the Temporal integration is configured.
func (t TemporalConfig) Enabled() bool { return t.HostPort != "" }
