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
	"github.com/wso2/asdlc/asdlc-service/internal/credentials"
	"github.com/wso2/asdlc/asdlc-service/middleware"
	jwtmw "github.com/wso2/asdlc/asdlc-service/middleware/jwt"
	"github.com/wso2/asdlc/asdlc-service/middleware/jwtassertion"
	"github.com/wso2/asdlc/asdlc-service/middleware/logger"
	"github.com/wso2/asdlc/asdlc-service/middleware/orgensure"
	"github.com/wso2/asdlc/asdlc-service/repositories"
	"github.com/wso2/asdlc/asdlc-service/services"
)

// AppParams holds all dependencies needed to build the HTTP handler.
type AppParams struct {
	Config                     config.Config
	ProjectController          controllers.ProjectController
	ComponentController        controllers.ComponentController
	RequirementsController     controllers.RequirementsController
	RequirementsChatController controllers.RequirementsChatController
	DesignController           controllers.DesignController
	TaskController         controllers.TaskController
	BoardController        controllers.BoardController
	ConfigController       controllers.ConfigController
	CollabController       controllers.CollabController
	WebhookController      controllers.WebhookController
	OrgGitHubController    controllers.OrgGitHubController
	OrgAnthropicController controllers.OrgAnthropicController
	SkillController        controllers.SkillController
	OrganizationController controllers.OrganizationController
	IDPController          controllers.IDPController
	JWKSController         controllers.JWKSController
	TaskRepo               repositories.TaskRepository
	ConfigRepo             repositories.ConfigRepository

	// OrganizationService backs the JIT org-provisioning middleware. nil
	// disables the middleware (tests, dev configurations without a DB).
	OrganizationService services.OrganizationService

	// ThunderJWKS verifies User JWTs and Service JWTs presented to the BFF.
	// May be nil in dev/test, in which case inbound auth falls back to
	// unverified claim extraction — gated by IsLocalDevEnv.
	ThunderJWKS *jwtassertion.JWKSCache

	// --- Folded in from git-service after WS0.1.i ----------------------
	// Controllers + services for the repo / git-ops / credential surfaces
	// the standalone git-service used to expose. Wired onto the same
	// outer mux but under separate sub-routers so JWT verification
	// matches the original audience expectations (Service JWT for
	// /api/v1/repos + /internal/credentials, Task JWT for
	// /api/v1/credentials/refresh).
	DB                   *gorm.DB
	RepoCtrl             controllers.RepoController
	GitOpsCtrl           controllers.GitOpsController
	IssueCtrl            controllers.IssueController
	GitProjectCtrl       controllers.GitProjectController
	BranchCtrl           controllers.BranchController
	PullRequestCtrl      controllers.PullRequestController
	WebhookRegCtrl       controllers.WebhookRegistrationController
	ArtifactCtrl         controllers.ArtifactController
	CredCtrl             controllers.CredentialsRefreshController
	RepoBoardCtrl        controllers.RepoBoardController
	RepoService          services.RepoService
	RepoRepo             repositories.RepoRepository
	CredService          *services.CredentialService
	BuildCredService     *services.BuildCredentialsService
	AnthropicCredService *services.AnthropicCredentialService
	Validator            *credentials.Validator

	// ServiceJWT verifies User/Service JWTs presented to /api/v1/repos/* and
	// /internal/credentials/*. JWKS resolves to Thunder.
	ServiceJWT jwtassertion.Middleware
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
	apiMux := http.NewServeMux()
	if params.ProjectController != nil {
		registerProjectRoutes(apiMux, params.ProjectController)
	}
	if params.OrganizationController != nil {
		registerOrganizationRoutes(apiMux, params.OrganizationController)
	}
	if params.ComponentController != nil {
		registerComponentRoutes(apiMux, params.ComponentController)
	}
	if params.RequirementsController != nil {
		registerRequirementsRoutes(apiMux, params.RequirementsController)
	}
	if params.RequirementsChatController != nil {
		registerRequirementsChatRoutes(apiMux, params.RequirementsChatController)
	}
	if params.DesignController != nil {
		registerDesignRoutes(apiMux, params.DesignController)
	}
	if params.TaskController != nil {
		registerTaskRoutes(apiMux, params.TaskController)
	}
	if params.BoardController != nil {
		registerBoardRoutes(apiMux, params.BoardController)
	}
	if params.ConfigController != nil {
		registerConfigRoutes(apiMux, params.ConfigController)
	}
	if params.OrgGitHubController != nil {
		registerOrgGitHubRoutes(apiMux, params.OrgGitHubController)
	}
	if params.OrgAnthropicController != nil {
		registerOrgAnthropicRoutes(apiMux, params.OrgAnthropicController)
	}
	if params.SkillController != nil {
		registerSkillRoutes(apiMux, params.SkillController)
	}
	if params.IDPController != nil {
		registerIDPRoutes(apiMux, params.IDPController)
	}

	// Test-only reset endpoint — truncates local DB tables.
	if params.Config.TestMode {
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

	if params.CollabController != nil {
		registerCollabRoutes(apiMux, mux, params.CollabController)
	}

	// --- Git-service-side routes (folded in after WS0.1.i) -------------
	// Wired onto a dedicated git-service mux to keep their auth posture
	// (Service JWT for /api/v1/repos + /internal/credentials, Task JWT
	// for /api/v1/credentials/refresh) decoupled from the BFF's User
	// JWT path. The dedicated mux is mounted at the outer mux so its
	// middleware chain is independent from the User-JWT-gated /api/.
	gsMux := http.NewServeMux()
	if params.RepoCtrl != nil {
		var orgScope func(http.Handler) http.Handler
		if params.RepoRepo != nil {
			orgScope = middleware.RequireOrgScope(params.RepoRepo)
		}
		registerRepoOnlyRoutes(gsMux,
			params.RepoCtrl,
			params.GitOpsCtrl,
			params.IssueCtrl,
			params.BranchCtrl,
			params.PullRequestCtrl,
			params.WebhookRegCtrl,
			params.ArtifactCtrl,
			orgScope,
		)
	}
	if params.CredService != nil {
		registerCredentialsInternalRoutes(gsMux, params.CredService, params.BuildCredService, params.Validator)
	}
	if params.AnthropicCredService != nil {
		registerAnthropicCredentialsRoutes(gsMux, params.AnthropicCredService)
		// agents-service calls /effective-key without a Service JWT
		// (matches cloud release-binding which carries no
		// SERVICE_AUTH_GIT_* envs). Mount on the outer mux to bypass
		// the gsMux's ServiceJWT wrapper.
		registerAnthropicEffectiveKeyUnauth(mux, params.AnthropicCredService)
	}
	if params.GitProjectCtrl != nil {
		registerOrgRoutes(mux, params.GitProjectCtrl)
	}
	if params.RepoBoardCtrl != nil {
		registerRepoBoardRoutes(mux, params.RepoBoardCtrl)
	}

	taskMux := http.NewServeMux()
	if params.CredCtrl != nil {
		taskMux.HandleFunc("POST /api/v1/credentials/refresh", params.CredCtrl.Refresh)
	}

	if params.ServiceJWT != nil {
		mux.Handle("/api/v1/repos/", params.ServiceJWT(gsMux))
		mux.Handle("/api/v1/repos", params.ServiceJWT(gsMux))
		mux.Handle("/internal/credentials/", params.ServiceJWT(gsMux))
		slog.Info("git-service routes mounted under Service JWT")
	} else {
		mux.Handle("/api/v1/repos/", gsMux)
		mux.Handle("/api/v1/repos", gsMux)
		mux.Handle("/internal/credentials/", gsMux)
		slog.Warn("git-service routes mounted WITHOUT Service JWT (dev/test)")
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
		OcOrgID        string                    `json:"ocOrgId"`
		Writes         []services.SMAPISeedBundle `json:"writes"`
		AnthropicError string                    `json:"anthropicError,omitempty"`
		GitHubPATError string                    `json:"githubPatError,omitempty"`
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

