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

package codingagent

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"

	"gorm.io/gorm"

	"github.com/wso2/asdlc/asdlc-service/internal/feature/component"
	"github.com/wso2/asdlc/asdlc-service/models"
)

// projectSPARuntimeConfigEmitter re-emits env-config.js across a project's
// SPAs. Consumer port for the RuntimeConfigService — defined here and
// satisfied structurally by the concrete, wired at the composition root, so
// the cascade needn't import the runtime-config package. Mirrors the
// runtimeConfigEmitter pattern in dispatch_service.go.
//
// See docs/design/cross-component-wiring-gaps.md §3.
type projectSPARuntimeConfigEmitter interface {
	EmitForProjectSPAs(ctx context.Context, orgID, projectID string) error
}

// accessGrantStore is the consumer port for the cross-project access-request
// store (marketplace P3.5). The cascade uses it to close the loop when a
// provider's `org-publish` task lands `deployed`: it finds the open request(s)
// targeting the just-deployed component and flips them all to `granted`.
// Satisfied structurally by *access.Repository — kept as a local interface so
// the cascade needn't import the access package. Mirrors the
// projectSPARuntimeConfigEmitter pattern above.
type accessGrantStore interface {
	FindOpenForTarget(ctx context.Context, orgID, providerProjectID, providerComponentName string) (*models.AccessRequest, error)
	ListByProviderTask(ctx context.Context, providerTaskID string) ([]models.AccessRequest, error)
	UpdateStatus(ctx context.Context, id, status string) error
}

// accessDesignStore is the consumer port for the durability write the grant
// uses to persist `exposesAPI.orgPublished:true` on the provider component. The
// store reads the design, matches the component (logical or OC name), flips the
// flag idempotently, and COMMITS the single design.md to the provider repo (no
// new version tag) using the org's service-identity credential. Satisfied by
// *artifacts.ArtifactStore.
type accessDesignStore interface {
	SetComponentOrgPublished(ctx context.Context, orgID, projectID, componentName string) error
}

// accessIssueCloser is the consumer port for closing the provider GitHub issue
// once the grant lands. Satisfied by gitrepo.IssueService.
type accessIssueCloser interface {
	CloseIssue(ctx context.Context, orgID, projectID string, number int, comment string) error
}

// DispatchCascadeHook is the post-commit cascade fired by the webhook
// projector whenever a task lands in `deployed`. It owns the per-project
// advisory lock + eligibility scan + DispatchService.DispatchTasks call. The
// dispatch itself (gating logic + URL resolution) lives in DispatchService —
// this type only takes the lock and invokes it.
type DispatchCascadeHook struct {
	db            *gorm.DB
	dispatch      DispatchService
	traitSync     *component.TraitSyncService
	runtimeConfig projectSPARuntimeConfigEmitter

	// P3.5 access-request grant close-out (all optional; nil ⇒ step skipped).
	accessStore  accessGrantStore
	accessDesign accessDesignStore
	accessIssues accessIssueCloser
}

func NewDispatchCascadeHook(db *gorm.DB, dispatch DispatchService) *DispatchCascadeHook {
	return &DispatchCascadeHook{db: db, dispatch: dispatch}
}

// SetTraitSync wires the trait sync service so the cascade can re-reconcile
// the `api-configuration` trait (jwtAuth + issuers) on every protected API
// in the project when a component lands deployed. Optional — when nil the
// cascade skips the re-emit step.
func (h *DispatchCascadeHook) SetTraitSync(t *component.TraitSyncService) {
	if h == nil {
		return
	}
	h.traitSync = t
}

// SetRuntimeConfig wires the env-config.js emitter so the cascade can
// re-emit window._env_ values on every SPA in the project when any
// component lands deployed (sibling API URLs become available). Optional.
func (h *DispatchCascadeHook) SetRuntimeConfig(r projectSPARuntimeConfigEmitter) {
	if h == nil {
		return
	}
	h.runtimeConfig = r
}

// SetAccessGrant wires the P3.5 access-request grant close-out: when a
// provider's `org-publish` task lands `deployed`, the cascade flips every
// open AccessRequest riding on it to `granted`, persists
// `exposesAPI.orgPublished:true` on the provider component (durability), and
// closes the provider GitHub issue. All three collaborators are required for
// the step to run; a nil store disables it. Optional + best-effort — the grant
// never breaks the deploy cascade.
func (h *DispatchCascadeHook) SetAccessGrant(store accessGrantStore, design accessDesignStore, issues accessIssueCloser) {
	if h == nil {
		return
	}
	h.accessStore = store
	h.accessDesign = design
	h.accessIssues = issues
}

// OnTaskDeployed is the post-commit hook. Acquires a per-project advisory
// lock so concurrent deploys against the same project serialise (a
// dependent shared between two deps must dispatch exactly once when the
// second dep deploys). Inside the lock, calls DispatchTasks which handles
// the on_hold re-evaluation + actual dispatch.
//
// Errors are logged but never propagated — the deploy transition has
// already committed by the time we get here, and the cascade is
// best-effort. Operator-visible failure surfaces inside DispatchTasks
// (the dispatched task itself transitions to TaskStatusFailed with
// ErrorMessage populated).
func (h *DispatchCascadeHook) OnTaskDeployed(ctx context.Context, orgID, projectID, componentName string) {
	if h == nil || h.db == nil || h.dispatch == nil {
		return
	}
	// Per-project advisory lock — mirrors webhook.acquireProjectLock.
	// We take it in its own transaction so we can release before the
	// dispatch loop (which may itself perform writes). Holding it across
	// the dispatch loop would also be acceptable; we trade a tiny race
	// window (two near-simultaneous deploys releasing the lock before
	// the dispatch loop finishes) for shorter lock hold times. Since
	// DispatchTasks is itself idempotent on (DispatchedAt, LastCodingAgentRunName),
	// the worst case under that race is a single redundant lookup, not
	// a double-dispatch.
	if err := h.db.WithContext(ctx).Exec(
		`SELECT pg_advisory_xact_lock(?)`,
		hashCascadeKey("project:"+projectID),
	).Error; err != nil {
		slog.WarnContext(ctx, "dispatch cascade: acquire project lock failed",
			"project", projectID, "error", err)
		return
	}
	// Consumer→provider wiring now flows through the consumer's
	// `workload.yaml` deps block (resolved at dispatch) + OC's internal
	// address resolution; web-apps reach backends via the nginx `/api/*`
	// proxy (same-origin). No CORS, no backend-URL handoff via env-config.js.
	//
	// For OIDC-SPA web-apps the platform IDP redirect_uris are
	// registered by RuntimeConfigService.layerThunderKeys when the SPA
	// gets its env-config.js emitted below — no separate dispatch-side
	// Thunder call is needed.

	// jwtAuth re-emit: any dispatch in a project re-reconciles the
	// `api-configuration` trait (jwtAuth + issuers) on every service so a
	// BYO-IDP issuer change or auth toggle propagates to siblings.
	// Idempotent + best-effort.
	if h.traitSync != nil {
		if err := h.traitSync.SyncProjectAPITraits(ctx, orgID, projectID); err != nil {
			slog.WarnContext(ctx, "dispatch cascade: SyncProjectAPITraits failed",
				"project", projectID, "deployedComponent", componentName, "error", err)
		}
	}

	// Env-config.js re-emit: re-emit env-config.js on every SPA in the
	// project so the next pod restart picks up the latest `THUNDER_*` /
	// OIDC values + feature flags. (Backend API URLs no longer live in
	// window._env_.) Idempotent + best-effort.
	if h.runtimeConfig != nil {
		if err := h.runtimeConfig.EmitForProjectSPAs(ctx, orgID, projectID); err != nil {
			slog.WarnContext(ctx, "dispatch cascade: EmitForProjectSPAs failed",
				"project", projectID, "deployedComponent", componentName, "error", err)
		}
	}

	// P3.5 access-request grant close-out: if this just-deployed component is
	// the PROVIDER of an open cross-project access request, flip every consumer
	// request riding on its publish task to `granted`, persist
	// exposesAPI.orgPublished (durability), and close the provider issue. The
	// consumer cannot proceed past the save-and-proceed gate until the provider
	// is granted (block-at-proceed model, A2c); there is no separate org-service
	// On-Hold gate. This is purely the provider-side status close-out; the
	// DispatchTasks call below will unblock any waiting consumer tasks. Best-effort:
	// a sync failure here must never break the deploy cascade.
	h.grantAccessRequests(ctx, orgID, projectID, componentName)

	results, err := h.dispatch.DispatchTasks(ctx, orgID, projectID)
	if err != nil {
		slog.WarnContext(ctx, "dispatch cascade: DispatchTasks failed",
			"project", projectID, "deployedComponent", componentName, "error", err)
		return
	}
	slog.InfoContext(ctx, "dispatch cascade fired",
		"project", projectID,
		"deployedComponent", componentName,
		"dispatched", len(results),
	)
}

// grantAccessRequests is the P3.5 provider-side close-out fired when an
// `org-publish` task lands `deployed`. componentName is the just-deployed
// component — the PROVIDER, in OC component-name form. Steps (each best-effort,
// none aborts the others or the surrounding cascade):
//
//  1. FindOpenForTarget — is there an open (requested|in_progress) request whose
//     provider IS this component? If none, this is a normal deploy → return.
//  2. Persist exposesAPI.orgPublished:true on the provider component (durability,
//     so a future re-implementation doesn't silently drop namespace visibility).
//  3. Flip EVERY request riding on the provider task → granted.
//  4. Close the provider GitHub issue.
func (h *DispatchCascadeHook) grantAccessRequests(ctx context.Context, orgID, projectID, componentName string) {
	if h == nil || h.accessStore == nil {
		return
	}

	// (1) Is this deployed component the provider target of an open request?
	open, err := h.accessStore.FindOpenForTarget(ctx, orgID, projectID, componentName)
	if err != nil {
		slog.WarnContext(ctx, "access grant: FindOpenForTarget failed",
			"project", projectID, "component", componentName, "error", err)
		return
	}
	if open == nil {
		return // normal deploy — no access request waiting on this component.
	}

	// (2) Durability: set exposesAPI.orgPublished:true on the provider design.
	h.markOrgPublished(ctx, orgID, projectID, componentName)

	// (3) Flip every consumer request riding on this provider task to granted.
	flipped := 0
	rows, err := h.accessStore.ListByProviderTask(ctx, open.ProviderTaskID)
	if err != nil {
		// Fall back to flipping just the one row we found.
		slog.WarnContext(ctx, "access grant: ListByProviderTask failed; flipping single row",
			"providerTask", open.ProviderTaskID, "error", err)
		rows = []models.AccessRequest{*open}
	}
	for i := range rows {
		if rows[i].Status == models.AccessRequestStatusGranted {
			continue
		}
		if uerr := h.accessStore.UpdateStatus(ctx, rows[i].ID, models.AccessRequestStatusGranted); uerr != nil {
			slog.WarnContext(ctx, "access grant: UpdateStatus failed",
				"accessRequest", rows[i].ID, "error", uerr)
			continue
		}
		flipped++
	}

	// (4) Close the provider GitHub issue (once — all rows share it).
	if h.accessIssues != nil && open.ProviderIssueNumber > 0 {
		comment := fmt.Sprintf("Published %s org-wide (namespace visibility). Closing — consumers will resume automatically.", componentName)
		if cerr := h.accessIssues.CloseIssue(ctx, orgID, projectID, open.ProviderIssueNumber, comment); cerr != nil {
			slog.WarnContext(ctx, "access grant: CloseIssue failed",
				"project", projectID, "issue", open.ProviderIssueNumber, "error", cerr)
		}
	}

	slog.InfoContext(ctx, "access requests granted on provider deploy",
		"project", projectID, "providerComponent", componentName,
		"providerTask", open.ProviderTaskID, "granted", flipped)
}

// markOrgPublished durably persists exposesAPI.orgPublished:true on the named
// provider component and COMMITS the change to the provider repo (durability per
// P3.5 §4) — not just a working-tree write, so a future re-implementation can't
// silently drop namespace visibility. The store handles component matching
// (logical or OC name), idempotency (no-op if already published), and the
// service-identity commit. Best-effort: any failure is logged and swallowed —
// the grant proceeds.
func (h *DispatchCascadeHook) markOrgPublished(ctx context.Context, orgID, projectID, componentName string) {
	if h.accessDesign == nil {
		return
	}
	if err := h.accessDesign.SetComponentOrgPublished(ctx, orgID, projectID, componentName); err != nil {
		slog.WarnContext(ctx, "access grant: persist orgPublished durability failed",
			"project", projectID, "component", componentName, "error", err)
	}
}

func hashCascadeKey(s string) int64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return int64(h.Sum64()) //nolint:gosec
}
