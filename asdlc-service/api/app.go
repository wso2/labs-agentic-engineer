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

package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"gorm.io/gorm"

	"github.com/wso2/asdlc/asdlc-service/config"
	"github.com/wso2/asdlc/asdlc-service/controllers"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/component"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/design"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/idp"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/organization"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/orgcreds"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/project"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/requirements"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/skills"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/task"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/webhook"
	"github.com/wso2/asdlc/asdlc-service/internal/platform/auth"
	"github.com/wso2/asdlc/asdlc-service/internal/platform/tenant"
	"github.com/wso2/asdlc/asdlc-service/middleware"
	jwtmw "github.com/wso2/asdlc/asdlc-service/middleware/jwt"
	"github.com/wso2/asdlc/asdlc-service/middleware/jwtassertion"
	"github.com/wso2/asdlc/asdlc-service/middleware/logger"
	"github.com/wso2/asdlc/asdlc-service/middleware/orgensure"
	"github.com/wso2/asdlc/asdlc-service/repositories"
)

// AppParams holds all dependencies needed to build the HTTP handler.
type AppParams struct {
	Config                     config.Config
	ProjectController          project.ProjectController
	ComponentController        component.ComponentController
	RequirementsController     requirements.RequirementsController
	RequirementsChatController requirements.RequirementsChatController
	DesignController           design.DesignController
	TaskController             controllers.TaskController
	BoardController            task.BoardController
	ConfigController           component.ConfigController
	CollabController           requirements.CollabController
	WebhookController          webhook.WebhookController
	OrgGitHubController        orgcreds.OrgGitHubController
	OrgAnthropicController     orgcreds.OrgAnthropicController
	SkillController            skills.SkillController
	OrganizationController     organization.OrganizationController
	IDPController              idp.IDPController
	JWKSController             auth.JWKSController
	TaskRepo                   repositories.TaskRepository
	ConfigRepo                 repositories.ConfigRepository

	// OrganizationService backs the JIT org-provisioning middleware. nil
	// disables the middleware (tests, dev configurations without a DB).
	OrganizationService organization.OrganizationService

	// ThunderJWKS verifies User JWTs and Service JWTs presented to the BFF.
	// May be nil in dev/test, in which case inbound auth falls back to
	// unverified claim extraction — gated by IsLocalDevEnv.
	ThunderJWKS *jwtassertion.JWKSCache

	// --- Folded in from git-service ------------------------------------
	// What remains after the dead repo / git-ops / artifacts / credential
	// HTTP routes were removed: their callers use the gitrepo + artifacts
	// packages in-process, so only the runner-facing and agents-facing
	// surfaces stay. CredService + AnthropicCredService + DB also back the
	// local-dev SM-API resync helper (testSMAPIResyncHandler).
	DB                   *gorm.DB
	CredCtrl             orgcreds.CredentialsRefreshController
	CredService          *orgcreds.CredentialService
	AnthropicCredService *orgcreds.AnthropicCredentialService

	// TaskJWT verifies Task JWTs presented to /api/v1/credentials/refresh.
	// JWKS resolves to the BFF's /auth/external/jwks.json.
	TaskJWT jwtassertion.Middleware
}

// NewHandler assembles the full HTTP handler with middleware and routes.
// The console's nginx proxy strips the /asdlc-api-service prefix before
// forwarding, so routes are registered at root level.
func NewHandler(params AppParams) http.Handler {
	mux := http.NewServeMux()

	// Health check — unauthenticated.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`)) //nolint:errcheck
	})

	// JWKS endpoint — unauthenticated, registered on the OUTER mux. git-service
	// fetches this to verify Task JWTs; gating it on a User JWT would create a
	// chicken/egg deadlock at first verify.
	if params.JWKSController != nil {
		registerJWKSRoute(mux, params.JWKSController)
	}

	// API routes — JWT-authenticated via JWKS-backed RS256 verification.
	// Org-scoped routes register through apiRouter, which applies the central
	// per-route tenant gate (BindUserOrg). The gate matches the {orgHandle}
	// path var against the verified JWT org and (in enforce mode) 404s on a
	// mismatch — the allowlist-by-construction IDOR fence (§6.1b). The
	// enumerated carve-outs (org listing, idp discover, _test/reset) register
	// via apiRouter.Public, which bypasses the gate intentionally.
	apiMux := http.NewServeMux()
	apiRouter := NewRouter(apiMux, tenant.BindUserOrg("orgHandle", tenant.ParseGateMode(params.Config.TenantGateMode)))
	slog.Info("tenant gate active", "mode", string(tenant.ParseGateMode(params.Config.TenantGateMode)))
	if params.ProjectController != nil {
		registerProjectRoutes(apiRouter, params.ProjectController)
	}
	if params.OrganizationController != nil {
		registerOrganizationRoutes(apiRouter, params.OrganizationController)
	}
	if params.ComponentController != nil {
		registerComponentRoutes(apiRouter, params.ComponentController)
	}
	if params.RequirementsController != nil {
		registerRequirementsRoutes(apiRouter, params.RequirementsController)
	}
	if params.RequirementsChatController != nil {
		registerRequirementsChatRoutes(apiRouter, params.RequirementsChatController)
	}
	if params.DesignController != nil {
		registerDesignRoutes(apiRouter, params.DesignController)
	}
	if params.TaskController != nil {
		registerTaskRoutes(apiRouter, params.TaskController)
	}
	if params.BoardController != nil {
		registerBoardRoutes(apiRouter, params.BoardController)
	}
	if params.ConfigController != nil {
		registerConfigRoutes(apiRouter, params.ConfigController)
	}
	if params.OrgGitHubController != nil {
		registerOrgGitHubRoutes(apiRouter, params.OrgGitHubController)
	}
	if params.OrgAnthropicController != nil {
		registerOrgAnthropicRoutes(apiRouter, params.OrgAnthropicController)
	}
	if params.SkillController != nil {
		registerSkillRoutes(apiRouter, params.SkillController)
	}
	if params.IDPController != nil {
		registerIDPRoutes(apiRouter, params.IDPController)
	}

	// Test-only reset endpoint — truncates local DB tables. INT-4: gated on
	// DEPLOYMENT_TIER=dev (not TestMode alone) so the global truncate cannot
	// mount on a shared/non-dev plane, where TEST_MODE is set on the release
	// binding (see config.go). The org-scoped delete (replacing the global
	// Where(1=1)) remains the deferred cleanup-phase leg.
	if params.Config.TestMode && params.Config.DeploymentTier == "dev" {
		apiMux.HandleFunc("POST /api/v1/_test/reset", testResetHandler(params))
	}

	// Local-dev secret repair — outside the User JWT path so
	// deployments/scripts/repair-secrets.sh can call it without an admin
	// token. Gated on the DISTINCT LocalOpenBaoRepairEnabled flag (read
	// from LOCAL_OPENBAO_REPAIR), NOT on TestMode alone, because TestMode
	// is already set on the wso2cloud dev release binding for the existing
	// _test/reset route. Splitting them means cloud release bindings have
	// to explicitly opt in to expose this surface (and they don't), so the
	// route never registers on deployed environments. The handler emits
	// decrypted plaintext per-org credentials — second gating is essential.
	// The shell script's kubectl-context check is the third safety net.
	if params.Config.TestMode && params.Config.LocalOpenBaoRepairEnabled {
		mux.HandleFunc("POST /api/v1/_test/sm-api-resync", testSMAPIResyncHandler(params))
	}

	// GitHub webhook receiver — outside JWT, HMAC-authed inside the handler.
	if params.WebhookController != nil {
		registerWebhookRoutes(mux, params.WebhookController)
	}

	// F3c — per-task verification-failed callback. Outside the Thunder JWT
	// (the runner pod has no user identity); authenticated inside the
	// handler with the per-task RS256 bearer the runner already holds.
	if params.TaskController != nil {
		mux.HandleFunc("POST /api/v1/tasks/{taskId}/verification-failed", params.TaskController.VerificationFailed)
		// Per-task skills pull endpoint — runner pod fetches its
		// snapshotted SKILL.md bodies at init time. Outside the Thunder
		// JWT path, authenticated inside the handler with the per-task
		// RS256 bearer the runner already holds.
		mux.HandleFunc("GET /api/v1/tasks/{taskId}/skills", params.TaskController.Skills)
		// WS2.4 — path-scoped credentials refresh that accepts both
		// TaskJWT and Thunder publisher cc tokens. Legacy
		// POST /api/v1/credentials/refresh stays mounted below for
		// pre-WS2.4 runner images.
		mux.HandleFunc("POST /api/v1/tasks/{taskId}/credentials/refresh", params.TaskController.RefreshCredentials)
	}

	// App-mode connect callback — outside JWT. The signed connect-state JWT
	// in the `state` query param is the authn here, not the console JWT.
	if params.OrgGitHubController != nil {
		registerConnectCallbackRoute(mux, params.OrgGitHubController)
	}

	// --- Surfaces retained from the git-service fold -------------------
	// The repo / git-ops / artifacts / credential HTTP routes that came
	// over from the standalone git-service were removed (callers use the
	// gitrepo + artifacts packages in-process). Two server-to-server
	// surfaces remain, each with its own auth posture independent of the
	// User-JWT-gated /api/ subtree.

	// agents-service resolves the per-org Anthropic key here WITHOUT a
	// Service JWT (its cloud release-binding carries no SERVICE_AUTH_GIT_*
	// envs). Mounted unauthenticated on the outer mux.
	if params.AnthropicCredService != nil {
		registerAnthropicEffectiveKeyUnauth(mux, params.AnthropicCredService)
	}

	// Per-task credentials refresh — runner pods present a Task JWT
	// (verified against the BFF's own /auth/external/jwks.json).
	taskMux := http.NewServeMux()
	if params.CredCtrl != nil {
		taskMux.HandleFunc("POST /api/v1/credentials/refresh", params.CredCtrl.Refresh)
	}
	if params.TaskJWT != nil {
		mux.Handle("/api/v1/credentials/", middleware.RequireTaskBearer(params.TaskJWT)(taskMux))
	} else {
		mux.Handle("/api/v1/credentials/", taskMux)
	}

	jwt := jwtmw.Middleware(jwtmw.Config{
		JWKS:                params.ThunderJWKS,
		AllowedIssuers:      splitAndTrim(params.Config.JWTAllowedIssuer),
		AllowedAudiences:    splitAndTrim(params.Config.JWTAllowedAudience),
		ResourceMetadataURL: params.Config.JWTResourceMetadataURL,
		IsLocalDevEnv:       params.ThunderJWKS == nil,
	})
	// JIT org-onboarding sits between JWT verification and the org-aware
	// route handlers. Tenants materialise on first authenticated request;
	// no env var, manifest, or seed names an org. See
	// docs/design/default-org-seed-removal.md §3.2.
	ensureOrg := orgensure.Middleware(params.OrganizationService)
	mux.Handle("/api/", jwt(ensureOrg(apiMux)))

	// Collab routes. GetCollabSession is org-scoped (on apiRouter/apiMux,
	// gated). ValidateCollabAccess (INT-8) is the raw server-to-server route
	// the collab-server calls — wrapped with the jwt verifier here so the
	// forwarded user Bearer is signature-verified before the handler enforces
	// the room's project-ownership oracle.
	if params.CollabController != nil {
		registerCollabRoutes(apiRouter, mux, params.CollabController, jwt)
	}

	// Global middleware stack (outermost applied last).
	var handler http.Handler = mux
	handler = middleware.ExtractAuthToken()(handler)
	handler = logger.RequestLogger()(handler)
	handler = middleware.AddCorrelationID()(handler)
	handler = middleware.RecovererOnPanic()(handler)

	return handler
}

// splitAndTrim splits a comma-separated env value into a list, dropping
// empty entries. Lets JWT_ISSUER / JWT_AUDIENCE accept multiple values
// (e.g. "APP_FACTORY_CONSOLE,local-dev-seeder") so a single BFF can
// accept both end-user and S2S tokens that carry different `aud`
// claims, without weakening the matcher to a wildcard.
func splitAndTrim(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func testResetHandler(params AppParams) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// INT-4 defense-in-depth: the global truncate must be unreachable
		// outside DEPLOYMENT_TIER=dev even if a future refactor mounts this
		// route without the registration-time tier gate above.
		if params.Config.DeploymentTier != "dev" {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		ctx := r.Context()
		if params.TaskRepo != nil {
			if err := params.TaskRepo.DeleteAll(ctx); err != nil {
				_ = err
			}
		}
		if params.ConfigRepo != nil {
			if err := params.ConfigRepo.DeleteAll(ctx); err != nil {
				_ = err
			}
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"reset"}`)) //nolint:errcheck
	}
}

// testSMAPIResyncHandler walks per-org credential rows and returns the
// {kvPath, property, value} tuples the repair script needs to reseed
// OpenBao. Plaintext crosses the localhost boundary once per write — the
// repair script then runs `vault kv put` via `kubectl exec` against the
// in-cluster OpenBao to materialise the secret at the path the dispatcher
// will read on the next ExternalSecret sync.
//
// We don't call SM-API directly here because SM-API's auth requires a
// Thunder JWT with an `ouId` claim — only mintable from a user session.
// For a no-user repair flow the BFF would need a per-org impersonation
// token. The shell→vault path bypasses that entirely and matches how
// setup-asdlc.sh seeds other local secrets.
//
// Two safety gates: TestMode (registration) + the shell script's
// kubectl-context check (refuses to run unless pointed at the local k3d
// cluster). Plaintext is never logged.
func testSMAPIResyncHandler(params AppParams) http.HandlerFunc {
	type orgResult struct {
		OcOrgID        string                     `json:"ocOrgId"`
		Writes         []orgcreds.SMAPISeedBundle `json:"writes"`
		AnthropicError string                     `json:"anthropicError,omitempty"`
		GitHubPATError string                     `json:"githubPatError,omitempty"`
	}
	type response struct {
		Orgs []orgResult `json:"orgs"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if params.DB == nil || params.CredService == nil || params.AnthropicCredService == nil {
			http.Error(w, `{"error":"resync surface not wired"}`, http.StatusServiceUnavailable)
			return
		}

		orgIDs, err := collectResyncOrgs(ctx, params.DB, r.URL.Query().Get("org"))
		if err != nil {
			slog.ErrorContext(ctx, "sm-api resync: org list failed", "error", err)
			http.Error(w, `{"error":"org list failed"}`, http.StatusInternalServerError)
			return
		}

		out := response{Orgs: make([]orgResult, 0, len(orgIDs))}
		for _, ocOrgID := range orgIDs {
			res := orgResult{OcOrgID: ocOrgID}
			if bundle, err := params.AnthropicCredService.PrepareSMAPISeed(ctx, ocOrgID); err != nil {
				res.AnthropicError = err.Error()
			} else if bundle != nil {
				res.Writes = append(res.Writes, *bundle)
			}
			if bundle, err := params.CredService.PrepareSMAPISeed(ctx, ocOrgID); err != nil {
				res.GitHubPATError = err.Error()
			} else if bundle != nil {
				res.Writes = append(res.Writes, *bundle)
			}
			out.Orgs = append(out.Orgs, res)
			slog.InfoContext(ctx, "sm-api resync: org",
				"ocOrgId", ocOrgID,
				"writeCount", len(res.Writes))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// collectResyncOrgs returns the unique set of ocOrgIDs that have either an
// org_credentials or org_anthropic_credentials row with the SM-API triplet
// populated. When `only` is non-empty the set is filtered to that single id.
func collectResyncOrgs(ctx context.Context, db *gorm.DB, only string) ([]string, error) {
	seen := map[string]struct{}{}
	add := func(rows []string) {
		for _, id := range rows {
			if only != "" && id != only {
				continue
			}
			seen[id] = struct{}{}
		}
	}
	var patOrgs []string
	if err := db.WithContext(ctx).Raw(
		`SELECT oc_org_id FROM org_credentials WHERE sm_api_secret_ref_name IS NOT NULL`,
	).Scan(&patOrgs).Error; err != nil {
		return nil, err
	}
	add(patOrgs)
	var anthropicOrgs []string
	if err := db.WithContext(ctx).Raw(
		`SELECT oc_org_id FROM org_anthropic_credentials WHERE sm_api_secret_ref_name IS NOT NULL`,
	).Scan(&anthropicOrgs).Error; err != nil {
		return nil, err
	}
	add(anthropicOrgs)
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	return out, nil
}
