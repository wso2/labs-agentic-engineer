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
	"encoding/json"
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

	// PlatformResourcesEnabled gates discovery of cluster-scoped platform
	// resource types (ResourceTypeCatalog.List → OC ListClusterResourceTypes).
	// Defaults TRUE: platform resources are a core capability; deployments that
	// offer no platform-resource catalog must opt out explicitly
	// (PLATFORM_RESOURCES_ENABLED=false). Unlike AutoMergeCodingPRs (an opt-in extra that
	// defaults false), this is an opt-out. Read from PLATFORM_RESOURCES_ENABLED.
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

	// SREHandoffToken and SREHandoffOrg configure the long-lived credential
	// aep-mcp-server forwards on behalf of the OpenChoreo SRE agent for the
	// /internal/v1/sre/… ops only (internal/edge/internal.go's sre/ gate).
	// Both must be set together — either empty leaves the verifier disabled
	// (secure default) and every sre/ op answers 401. /api/v1 never accepts
	// this credential. Read from SRE_HANDOFF_TOKEN /
	// SRE_HANDOFF_ORG. Never a ConfigMap value — Secret only, same posture
	// as every other credential in this file.
	SREHandoffToken string
	SREHandoffOrg   string

	// TryItCallbackURL is the platform tester's OAuth callback, registered as a
	// redirect URI on every project's sign-in resource so a client that is not
	// one of the project's own components — the platform's test app — can
	// complete a sign-in there. One fixed URL for the whole platform. Empty
	// disables the registration.
	TryItCallbackURL string

	// Build watcher git_clone_failed_auth retry budget. Default 3 attempts.
	// Configurable via BUILD_AUTH_RETRY_BUDGET; tests set to 0 to force
	// exhaustion on the first auth failure.
	BuildAuthRetryBudget int

	// Thunder admin client config for per-org publisher OAuth app lifecycle.
	// Loaded from env vars THUNDER_ADMIN_URL / THUNDER_SYSTEM_CLIENT_ID /
	// THUNDER_SYSTEM_CLIENT_SECRET / THUNDER_SYSTEM_RESOURCE_IDENTIFIER.
	// When ClientID is empty the BFF logs a warning and the IDP service
	// returns ErrIDPThunderUnavailable (non-fatal — protected components
	// still deploy, just without per-org publishers).
	ThunderAdmin ThunderAdminConfig

	// ThunderEnvAdminRoute picks which address admin calls to an ENVIRONMENT's
	// Thunder go to — the second identity tier, one instance per (org,
	// environment), whose binding the OpenChoreo Environment records.
	//
	//	issuer   the instance's public issuer. The only address that
	//	         works from the local docker-compose stack, where aep-api runs
	//	         outside the cluster and the binding's `*.svc.cluster.local`
	//	         admin URL resolves to nothing.
	//	binding  the in-cluster Service address the binding records. Right for an
	//	         aep-api running INSIDE the cluster, where the public hostnames do
	//	         not resolve from a pod.
	//
	// From THUNDER_ENV_ADMIN_ROUTE. Unset, it follows where this process is
	// running — `binding` in a pod (KUBERNETES_SERVICE_HOST is set), `issuer`
	// otherwise — because the wrong one of the two reaches nothing at all.
	// Anything but `binding` reads as `issuer`.
	ThunderEnvAdminRoute string

	// KubeAPI is the Kubernetes API the Thunder Application CR LIST uses.
	// Set KUBE_API_BASE_URL to the dataplane apiserver on a split-plane
	// install; otherwise the in-cluster host is used. BaseURL empty ⇒
	// thunder wait stays unwired (local compose has no kube API).
	KubeAPI KubeAPIConfig

	// APIGatewayHost is an OVERRIDE for host:port of the API Platform gateway
	// runtime — the hop that terminates authentication for a managed API.
	// Published to a consumer of a protected sibling as `<DEP>_GATEWAY_URL` so a
	// SPA's nginx can proxy the browser's /api through it instead of straight at
	// the project Service.
	//
	// Loaded from API_GATEWAY_HOST. Empty — the normal case — derives the
	// address per (org, environment): there is one gateway per environment, and
	// the derivation lives in the projects domain beside the context-path
	// builder it must agree with (projects.APIGatewayHost). Set, it WINS for
	// every environment, which only a data plane that names its gateway
	// differently wants.
	APIGatewayHost string

	// Platform IDP defaults seeded into organization_idp_profiles rows
	// on first access. Loaded from PLATFORM_IDP_ISSUER /
	// PLATFORM_IDP_JWKS_URL.
	PlatformIDP PlatformIDPDefaults

	// AEStudio configures the per-org AE Studio data-plane Resource. Every
	// field is optional at boot (AE_STUDIO_*); Missing() names what Ensure
	// needs and lacks, so an unconfigured install fails Ensure loudly instead
	// of failing boot.
	AEStudio AEStudioConfig

	Observability ObservabilityConfig
	ServiceAuth   ServiceAuthConfig
	AgentManager  AgentManagerConfig
	Workspace     WorkspaceConfig

	// SkillsDir is the on-disk platform skill library the BFF seeds + reconciles
	// into each org's skills repo (SKILLS_DIR). It is COPY'd into the image from
	// the repo-root skills/ directory (the single authored source) — no longer
	// go:embed'd. Defaults to /app/skills (the image path); tests inject their
	// own fs.FS and never read this.
	SkillsDir string

	// AgentPlatformURL is the URL the coding-agent runner pod uses to call
	// back to the BFF (MCP, validation context). In cloud this is the
	// public/internal gateway route to app-factory-api — runner pods live in
	// the dataplane and cannot reach the control-plane ClusterIP. Locally it
	// is typically http://host.k3d.internal:9090.
	AgentPlatformURL string

	// JWKS settings for /api/v1's user-JWT verification — Thunder publishes
	// the signing key at JWKSURL; verifiers refresh on kid miss. Issuer and
	// audience configure RFC 7519 claim checks; the audience is the console
	// client's, the one client that issues user tokens for this edge.
	JWKSURL                string
	JWTAllowedIssuer       string
	JWTAllowedAudience     string
	JWTResourceMetadataURL string

	// Git-service config fields.

	GitHubRepoVisibility string

	// CredentialEncryptionKey is the base64-encoded 32-byte AES-256 key
	// used to encrypt per-org credentials at rest in org_secrets.
	CredentialEncryptionKey string

	// OpenBaoAddr / OpenBaoToken — local-only OpenBao connection for the
	// in-process OpenBao-direct secrets provider (NewOSSOptions). Empty
	// leaves SecretsProvider nil (delivery off). Never set in cloud.
	OpenBaoAddr  string
	OpenBaoToken string

	GitHubAppID             string
	GitHubAppSlug           string // App's URL slug; names the platform bot sender (githubBotLogin)
	GitHubAppPrivateKeyPath string

	// CredentialValidatorInterval is the periodic credential-validator
	// sweep interval. Default 24h.
	CredentialValidatorInterval time.Duration

	// AgentRunnerImage is the docker image the runner Job uses — ONE image
	// for BOTH task kinds (implementation and validation). Pinned at deploy
	// time, no built-in default;
	// `:latest` is OK in dev but the cloud release-binding should resolve to
	// a digest. Empty ⇒ dispatch is off and fails loudly.
	AgentRunnerImage string

	// AgentRunnerImageOpenCode is the runner image for an organization whose
	// coding-agent runtime is OpenCode: the same Dockerfile's runner-opencode
	// stage, which adds the pinned OpenCode binary on top of AgentRunnerImage's
	// layers. Pinned at deploy time like AgentRunnerImage and for the same
	// reason (no built-in default; compose defaults it to aep-runner-opencode:dev,
	// Helm reads codingAgentRunner.opencodeImage). Empty ⇒ an OpenCode cycle's
	// dispatch fails naming this setting; Claude Code orgs are unaffected.
	AgentRunnerImageOpenCode string

	// CodingAgentComponentRetention is how many finished coding-agent
	// Components a project may keep (LRU reap before each create). Defaults
	// to codingagent.DefaultCodingAgentComponentRetention (10). Override via
	// CODING_AGENT_COMPONENT_RETENTION so local E2E can observe prune without
	// eleven cycles; cloud keeps the code default unless explicitly set.
	CodingAgentComponentRetention int

	// CodingAgentJobTTL is how long a finished coding-agent Job (and its pod)
	// is kept before Kubernetes deletes it, rendered per Component as the
	// ComponentType's ttlSecondsAfterFinished. Default 600s (CODING_AGENT_JOB_TTL).
	// Once a cycle is over its binding is suspended, so the Job OpenChoreo
	// re-creates after the TTL never runs the runner again.
	CodingAgentJobTTL time.Duration

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
	// Required config — fail fast at boot instead of soft-warning and surfacing
	// the failure later at runtime. Both planes (docker-compose + Helm) always set
	// these; an empty value means a misconfigured deployment, not a valid mode.
	if c.JWKSURL == "" {
		// Without JWKS the inbound verifier rejects every /api/ request (401);
		// there is no unsigned-claim fallback.
		errs = append(errs, "JWKS_URL is required — the inbound JWT verifier cannot start without it")
	}
	if len(errs) > 0 {
		return fmt.Errorf("configuration errors:\n%s", strings.Join(errs, "\n"))
	}
	return nil
}

// ThunderAdminConfig holds the aep-system-client OAuth2 credentials
// + base URL the BFF uses to manage Thunder applications (per-org
// publisher lifecycle). The same Thunder instance that fronts user
// PKCE login — see deployments/single-cluster/thunder-resources/
// 81-aep-system-client.yaml for the document that ships these credentials.
type ThunderAdminConfig struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
	// SystemResourceIdentifier is the `resource` indicator every system-token
	// request carries: the identifier of the identity provider's System
	// resource server, the one that owns the `system` scope. Empty means
	// "derive it from the platform IdP's public issuer" (<issuer>/mcp, the
	// ThunderID convention) — the composition root does that, so a deployment
	// only sets this when its IdP uses a non-conventional identifier.
	SystemResourceIdentifier string
}

// KubeAPIConfig is the Kubernetes API endpoint used to LIST ThunderApplication
// CRs (plain net/http — not controller-runtime). Resolved at Load:
//
//	BaseURL — KUBE_API_BASE_URL when set (dataplane apiserver on a split
//	install); else https://$KUBERNETES_SERVICE_HOST:$KUBERNETES_SERVICE_PORT
//	when both are set (Helm pods on the same cluster as the CRD).
//	BearerToken — KUBE_API_BEARER static override (not a rotating SA token).
//	TokenFile — KUBE_API_TOKEN_FILE when set; else the in-cluster SA token
//	when BaseURL is not an override. The client reads the file per request
//	so a projected rotation is picked up.
//	CAFile — KUBE_API_CA_FILE when set; else the in-cluster SA ca.crt when
//	BaseURL is not an override. An override without this file uses system
//	roots, which will not verify an EKS cluster CA.
type KubeAPIConfig struct {
	BaseURL     string
	BearerToken string
	TokenFile   string
	CAFile      string
}

// PlatformIDPDefaults are the issuer + JWKS URL of the cluster's
// platform IDP (Thunder in v1). Seeded into every new
// organization_idp_profiles row.
type PlatformIDPDefaults struct {
	Issuer  string
	JWKSURL string
}

// AEStudioConfig is the deployment-supplied input of the AE Studio Resource.
// One AE_STUDIO_* env per field; only ExtraEgress is JSON.
type AEStudioConfig struct {
	Images struct{ DesignAgent, Collab, StudioTools string }

	GatewayHost      string
	PublicScheme     string
	PublicPortSuffix string
	ListenerName     string
	ConsoleOrigins   []string

	IDP struct {
		Issuer, JWKSURL, TokenURL string
		UserAudiences             []string
	}

	AEPAPIBaseURL string
	// InternalClientID is aep-api's own AE-only client. The secret is not
	// needed by Ensure (used from phase 2), so Missing() ignores it and boot
	// never requires it.
	InternalClientID     string
	InternalClientSecret string

	RuntimeClassName string
	Cilium           bool
	ExtraEgress      json.RawMessage // default "[]"

	Storage struct {
		SizeLimit, EphemeralRequest string
		BudgetBytes                 int64
	}
	PullSecret struct{ Key, Property string }

	// WebhookRelaySeed (AE_STUDIO_WEBHOOK_RELAY_SEED) keys each org's smee.io
	// relay channel (aestudio.WebhookRelayURL); its text is the HMAC key as
	// stored. A secret: never logged. Unset (Cloud) = no relay.
	WebhookRelaySeed string
	// WebhookRelayImage (AE_STUDIO_WEBHOOK_RELAY_IMAGE) is the relay
	// container's image, needed only with a seed.
	WebhookRelayImage string
}

// Missing returns the env names Ensure needs and lacks, in a stable order.
func (c AEStudioConfig) Missing() []string {
	var missing []string
	for _, f := range []struct{ env, val string }{
		{"AE_STUDIO_IMAGE_DESIGN_AGENT", c.Images.DesignAgent},
		{"AE_STUDIO_IMAGE_COLLAB", c.Images.Collab},
		{"AE_STUDIO_IMAGE_STUDIO_TOOLS", c.Images.StudioTools},
		{"AE_STUDIO_GATEWAY_HOST", c.GatewayHost},
		{"AE_STUDIO_IDP_ISSUER", c.IDP.Issuer},
		{"AE_STUDIO_IDP_JWKS_URL", c.IDP.JWKSURL},
		{"AE_STUDIO_IDP_TOKEN_URL", c.IDP.TokenURL},
		{"AE_STUDIO_AEP_API_BASE_URL", c.AEPAPIBaseURL},
		{"AE_STUDIO_INTERNAL_CLIENT_ID", c.InternalClientID},
	} {
		if f.val == "" {
			missing = append(missing, f.env)
		}
	}
	if len(c.ConsoleOrigins) == 0 {
		missing = append(missing, "AE_STUDIO_CONSOLE_ORIGINS")
	}
	if len(c.IDP.UserAudiences) == 0 {
		missing = append(missing, "AE_STUDIO_IDP_USER_AUDIENCES")
	}
	if c.WebhookRelaySeed != "" && c.WebhookRelayImage == "" {
		missing = append(missing, "AE_STUDIO_WEBHOOK_RELAY_IMAGE")
	}
	return missing
}

// ServiceAuthConfig holds OAuth2 client_credentials settings for
// service-to-service authentication (e.g. BFF → OpenChoreo API).
type ServiceAuthConfig struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
	HostHeader   string // Thunder Host header for k3d routing
}

// WorkspaceConfig holds the /workspaces mount settings: the coding-agent run
// recordings and their retention. aep-api is the mount's only user.
type WorkspaceConfig struct {
	// Root is the workspace mount root (AEP_WORKSPACE_ROOT). Layout under it:
	// runs/<orgId>/<cycleId>.
	Root string
	// ReapInterval is the recording retention sweep cadence.
	ReapInterval time.Duration
	// RecordingMaxAge — runs/<orgId>/<cycleId> coding-agent feed recordings
	// older than this are removed by the retention sweep. Days, not hours: a
	// recording cannot be rebuilt, so the window is how long a run stays
	// inspectable (ADR-0027).
	RecordingMaxAge time.Duration
	// RecordingMaxBytes caps ONE cycle's recording. Zero — the default — is no
	// cap: a 55-minute run wrote about 300KB, so this is a safety valve for a
	// pathological producer, not an operating limit. A run that trips it records
	// a notice saying so and keeps running without recording.
	RecordingMaxBytes int64
	// OrgQuotaBytes is the per-org recordings quota before the oldest are
	// evicted.
	OrgQuotaBytes int64
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

// AgentManagerConfig holds the machine credentials AEP calls Agent Manager
// with.
//
// No base URL: Agent Manager's address is per ENVIRONMENT, read from that
// environment's AI gateway binding record, because two environments may be
// governed by different Agent Managers. Only the identity is configuration.
//
// The client id must match the `amp-publisher-*` wildcard in amp-api's
// KEY_MANAGER_AUDIENCE, or its tokens are rejected as "invalid jwt" whatever
// scopes they carry. Declared in
// deployments/single-cluster/thunder-resources/92-aep-amp-publisher-client.yaml.
type AgentManagerConfig struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
	// Resource is the OAuth resource indicator. Agent Manager's resource server
	// is urn:wso2:amp, and a mint that omits it can come back with no scopes.
	Resource string
	// HostHeader is the vhost the token URL is routed by — see
	// agentmanager.Config.HostHeader. Defaults to the one the service's other
	// Thunder clients already use.
	HostHeader string
}

// PlatformAPIConfig holds connection settings for the OpenChoreo platform API.
type PlatformAPIConfig struct {
	BaseURL    string
	HostHeader string
	// DataPlaneGatewayTLS says whether the DATA-PLANE gateway terminates TLS —
	// not whether this API does, and not which tier this is.
	//
	// It exists because OpenChoreo advertises a ReleaseBinding's external URLs
	// from the endpoint's SHAPE, not from what its gateway serves: a local plane
	// advertises BOTH an https and an http URL while `gateway.tls.enabled: false`
	// leaves http as its only listener. A consumer that prefers https then hands
	// out a URL nothing answers.
	//
	// It is stated rather than inferred on purpose. The tier was the obvious
	// proxy and is the wrong fact: a dev-tier plane WITH TLS would be told to
	// prefer plain http, which is the same bug mirrored. The single-cluster
	// values file (deployments/single-cluster/values-dp.yaml) is what this must
	// agree with, so the two are one edit apart.
	//
	// Defaults TRUE — every plane whose gateway fronts TLS, which is every one
	// but local dev.
	DataPlaneGatewayTLS bool
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
