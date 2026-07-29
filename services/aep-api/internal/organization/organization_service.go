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
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/wso2/aep/aep-api/internal/gen"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
)

// ensureCacheTTL bounds how long a successful EnsureForOuHandle result
// suppresses re-verification. Short enough that a deleted+recreated
// namespace re-resolves promptly; long enough to absorb the 3s/5s
// progress polls without per-request DB hits.
const ensureCacheTTL = 5 * time.Minute

// OrganizationService is the BFF's read-and-cache view of OC organizations.
//
// An organization maps 1:1 to an OpenChoreo namespace (which itself maps 1:1
// to a Kubernetes namespace). The local `organizations` table is a UUID
// side-car so other tables can foreign-key onto an org without depending on
// the namespace name.
//
// The BFF does NOT create OC namespaces. Tenant onboarding is the platform's
// job — in hosted environments, `platform-api-service` creates the namespace
// in response to Thunder's `notify_org_created` webhook. In local dev, the
// `seed-admin-org.sh` step in `setup.sh` does the equivalent at install time.
// Both paths land identical state in OC; the BFF reads it.
type OrganizationService interface {
	List(ctx context.Context) (*gen.OrganizationList, error)
	// EnsureForOuHandle verifies that the OC namespace named after the
	// caller's `ouHandle` exists, and caches the local Organization row's
	// UUID for FK use. It does NOT create the namespace — if OC reports
	// 404, the call returns ErrOrganizationNotProvisioned and the auth
	// middleware passes through, letting the controller surface a
	// user-meaningful error.
	//
	// `thunderOrgUUID` is the authoritative org UUID from the JWT's `ouId`
	// claim (empty for unauthenticated or non-user-JWT paths). When
	// non-empty and missing/stale on the local row, it's persisted onto
	// `thunder_org_uuid` so downstream callers (dispatcher / SecretRefWriter)
	// can compute the same per-org NS SM-API derives.
	EnsureForOuHandle(ctx context.Context, ouHandle string, thunderOrgUUID string) error
}

// ErrOrganizationNotProvisioned signals that the inbound JWT's `ouHandle`
// has no matching OC namespace yet. In hosted that means
// platform-api-service hasn't finished onboarding the user; the user's
// next request usually succeeds. In local dev it usually means
// `seed-admin-org.sh` did not run.
var ErrOrganizationNotProvisioned = errors.New("organization namespace not provisioned")

// OUValidator confirms a Thunder OU id exists. The org feature uses it to
// reject a stale/phantom JWT `ouId` before persisting it as the org's
// thunder_org_uuid — a non-existent OU poisons wc- namespace derivation,
// impersonation, and the publisher app's OU binding. Satisfied by
// clients/thundersvc.Client; nil disables the check (dev/tests without a
// Thunder admin client fall back to trusting the JWT, as before).
type OUValidator interface {
	OUExists(ctx context.Context, ouID string) (bool, error)
}

type organizationService struct {
	repo  OrganizationRepository
	nsCli openchoreo.NamespaceClient

	// ouValidator (optional) validates a JWT `ouId` against Thunder before the
	// org→OU mapping is (over)written. nil = trust the JWT (pre-existing
	// behavior, for dev/tests without a Thunder admin client).
	ouValidator OUValidator

	// ensureCache memoises EnsureForOuHandle's "yes, verified" result
	// for ensureCacheTTL so the auth middleware doesn't pay a DB+OC
	// round-trip on every authenticated request. Misses + errors are
	// not cached. Mirrors agent-manager's
	// publisher_credential_provisioner pattern (singleflight per
	// orgName + short-lived in-memory cache).
	ensureMu       sync.RWMutex
	ensureCache    map[string]time.Time
	ensureInflight singleflight.Group

	// ensureCacheTTLOverride, when non-zero, replaces ensureCacheTTL. Always
	// zero in production wiring; WithEnsureCacheTTL is a test-only knob so a
	// black-box test can prove the cache-expiry re-verify behavior through
	// real time passage instead of reaching into the unexported cache map.
	ensureCacheTTLOverride time.Duration
}

func NewOrganizationService(repo OrganizationRepository, nsCli openchoreo.NamespaceClient) *organizationService {
	return &organizationService{
		repo:        repo,
		nsCli:       nsCli,
		ensureCache: map[string]time.Time{},
	}
}

// SetOUValidator wires the Thunder OU-existence checker (composition root,
// after the Thunder admin client is constructed).
func (s *organizationService) SetOUValidator(v OUValidator) { s.ouValidator = v }

// WithEnsureCacheTTL overrides the ensure-cache TTL (default ensureCacheTTL).
// Test-only: production composition never calls this.
func (s *organizationService) WithEnsureCacheTTL(d time.Duration) *organizationService {
	s.ensureCacheTTLOverride = d
	return s
}

// cacheTTL returns the effective ensure-cache TTL: the WithEnsureCacheTTL
// override when set, else the package default.
func (s *organizationService) cacheTTL() time.Duration {
	if s.ensureCacheTTLOverride > 0 {
		return s.ensureCacheTTLOverride
	}
	return ensureCacheTTL
}

// ouIsTrustworthy returns false ONLY when a wired validator positively reports
// the OU does not exist. Empty id, no validator, or a transient validation
// error all return true (fail-open — never block org-ensure on a Thunder
// hiccup; the publisher self-heal carries its own guard).
func (s *organizationService) ouIsTrustworthy(ctx context.Context, ouID string) bool {
	if ouID == "" || s.ouValidator == nil {
		return true
	}
	exists, err := s.ouValidator.OUExists(ctx, ouID)
	if err != nil {
		slog.WarnContext(ctx, "org OU validation failed — trusting the JWT ouId (fail-open)", "ouID", ouID, "error", err)
		return true
	}
	return exists
}

// List returns every namespace the BFF can see in OC, joined with the local
// Organization rows. Namespaces without a local row get one inserted on the
// fly (idempotent on UNIQUE name) so OC namespaces pick up a UUID without
// an explicit migration step.
func (s *organizationService) List(ctx context.Context) (*gen.OrganizationList, error) {
	views, err := s.nsCli.ListNamespaces(ctx)
	if err != nil {
		// Raw OC sentinel — the huma edge (mapOrganizationError) classifies it.
		return nil, err
	}

	if len(views) == 0 {
		return &gen.OrganizationList{Items: []gen.OrganizationView{}}, nil
	}

	names := make([]string, 0, len(views))
	for _, v := range views {
		names = append(names, v.Name)
	}

	rows, err := s.repo.ListByNames(ctx, names)
	if err != nil {
		return nil, fmt.Errorf("load organizations: %w", err)
	}
	byName := make(map[string]Organization, len(rows))
	for _, r := range rows {
		byName[r.Name] = r
	}

	for i, v := range views {
		row, ok := byName[v.Name]
		if !ok {
			row = s.backfillRow(ctx, v.Name, v, "")
			if row.UUID == uuid.Nil {
				continue
			}
		}
		views[i].UUID = row.UUID
		views[i].CreatedAt = row.CreatedAt
		if views[i].DisplayName == "" {
			views[i].DisplayName = row.DisplayName
		}
	}

	return &gen.OrganizationList{Items: views}, nil
}

// EnsureForOuHandle is the auth-middleware verify-and-cache path. It
// confirms the OC namespace named `ouHandle` exists and that we have a
// local row for it. On success the next handler runs with the cache
// warmed; on missing namespace it returns ErrOrganizationNotProvisioned
// which the middleware logs and lets through.
func (s *organizationService) EnsureForOuHandle(ctx context.Context, ouHandle string, thunderOrgUUID string) error {
	if ouHandle == "" {
		return fmt.Errorf("ouHandle is required")
	}

	// Hot path: recently-verified ouHandle. The thunder UUID
	// backfill is not gated by the cache — it runs once per (ouHandle,
	// thunderUUID) pair via singleflight so a stale row gets fixed up
	// even when the verify cache is warm.
	s.ensureMu.RLock()
	verifiedAt, ok := s.ensureCache[ouHandle]
	s.ensureMu.RUnlock()
	cacheWarm := ok && time.Since(verifiedAt) < s.cacheTTL()

	if !cacheWarm {
		// Coalesce concurrent first-sights of the same handle into one
		// DB+OC verify.
		if _, err, _ := s.ensureInflight.Do(ouHandle, func() (any, error) {
			// Re-check the cache inside the singleflight critical
			// section — a sibling call may have just populated it.
			s.ensureMu.RLock()
			verifiedAt, ok := s.ensureCache[ouHandle]
			s.ensureMu.RUnlock()
			if ok && time.Since(verifiedAt) < s.cacheTTL() {
				return nil, nil
			}
			if err := s.verifyForOuHandle(ctx, ouHandle, thunderOrgUUID); err != nil {
				return nil, err
			}
			s.ensureMu.Lock()
			s.ensureCache[ouHandle] = time.Now()
			s.ensureMu.Unlock()
			return nil, nil
		}); err != nil {
			return err
		}
	}

	// Best-effort: backfill thunder_org_uuid if it's missing on the row
	// (idempotent — the SQL is a no-op when already set to the right
	// value, and logs a warning on drift). Cheap UPDATE so we don't
	// guard it behind the verify cache.
	if thunderOrgUUID != "" {
		s.ensureThunderUUID(ctx, ouHandle, thunderOrgUUID)
	}
	return nil
}

func (s *organizationService) verifyForOuHandle(ctx context.Context, ouHandle, thunderOrgUUID string) error {
	row, err := s.repo.GetByName(ctx, ouHandle)
	if err != nil {
		return fmt.Errorf("lookup organization: %w", err)
	}
	if row != nil {
		return nil
	}
	// No local row — fall through to OC verify.

	view, err := s.nsCli.GetNamespace(ctx, ouHandle)
	if err != nil {
		if errors.Is(err, openchoreo.ErrNotFound) {
			return fmt.Errorf("%w: %s", ErrOrganizationNotProvisioned, ouHandle)
		}
		// Raw OC sentinel — the auth middleware only logs this, and the List
		// edge classifies it. No feature-local translation needed.
		return err
	}
	// Key the row by the handle, not view.Name: platform-api returns the
	// canonical namespace name (e.g. "wc-<uuid8>-<hash8>") in metadata.name,
	// but every per-org lookup — this verify path, ensureThunderUUID, the
	// dispatch org-UUID lookup, and the OC-client impersonation resolver — keys
	// by the handle the BFF puts in OC URLs. Storing view.Name made those
	// lookups miss forever (and re-backfill on every request).
	s.backfillRow(ctx, ouHandle, *view, thunderOrgUUID)
	return nil
}

// ensureThunderUUID upserts the Thunder org UUID onto the local row.
// No-op when the row already carries the same value; logs+overwrites
// on drift (Thunder is authoritative).
func (s *organizationService) ensureThunderUUID(ctx context.Context, ouHandle, thunderOrgUUID string) {
	parsed, err := uuid.Parse(thunderOrgUUID)
	if err != nil {
		slog.WarnContext(ctx, "ensureThunderUUID: invalid UUID in JWT", "ouHandle", ouHandle, "thunderOrgUUID", thunderOrgUUID, "error", err)
		return
	}
	row, err := s.repo.GetByName(ctx, ouHandle)
	if err != nil || row == nil {
		// Row may not exist yet (verify failed earlier); caller already logged.
		return
	}
	if row.ThunderOrgUUID != nil && *row.ThunderOrgUUID == parsed {
		return
	}
	// Trust-check BEFORE any write — both the fresh-org NULL→set path and the
	// overwrite path. A just-backfilled NULL row must not be poisoned with a
	// phantom (Thunder-unknown) ouId any more than an existing value may be
	// overwritten by one; a phantom OU poisons wc- namespace derivation + the
	// publisher OU binding (runner cc-token would 401).
	if !s.ouIsTrustworthy(ctx, parsed.String()) {
		current := "<null>"
		if row.ThunderOrgUUID != nil {
			current = row.ThunderOrgUUID.String()
		}
		slog.ErrorContext(ctx, "ensureThunderUUID: JWT ouId is NOT a known Thunder OU — REFUSING to persist a phantom onto thunder_org_uuid (keeping current).",
			"ouHandle", ouHandle, "current", current, "phantomFromJWT", parsed.String())
		return
	}
	if row.ThunderOrgUUID != nil {
		slog.WarnContext(ctx, "ensureThunderUUID: row UUID differs from JWT — overwriting (new OU validated against Thunder)",
			"ouHandle", ouHandle, "current", row.ThunderOrgUUID.String(), "newFromJWT", parsed.String())
	}
	if err := s.repo.SetThunderOrgUUID(ctx, ouHandle, parsed); err != nil {
		slog.WarnContext(ctx, "ensureThunderUUID: update failed", "ouHandle", ouHandle, "error", err)
	}
}

// backfillRow inserts a local row for an org we just discovered, keyed by
// `name`. Callers choose the key deliberately: the verify/ensure path keys by
// the org handle (the identifier the BFF puts in OC URLs and every per-org
// lookup), while List keys by the OC namespace name it enumerated. Returns the
// resulting (possibly racing) row; on hard failure returns a zero row and logs.
func (s *organizationService) backfillRow(ctx context.Context, name string, view gen.OrganizationView, thunderOrgUUID string) Organization {
	row := Organization{
		UUID:        uuid.New(),
		Name:        name,
		DisplayName: view.DisplayName,
	}
	if thunderOrgUUID != "" {
		if parsed, perr := uuid.Parse(thunderOrgUUID); perr != nil {
			slog.WarnContext(ctx, "backfillRow: invalid Thunder UUID in JWT — leaving column NULL", "name", name, "thunderOrgUUID", thunderOrgUUID, "error", perr)
		} else if !s.ouIsTrustworthy(ctx, parsed.String()) {
			slog.ErrorContext(ctx, "backfillRow: JWT ouId is NOT a known Thunder OU — creating row with NULL thunder_org_uuid (will re-backfill from a valid login); a phantom OU would poison wc- namespace + publisher binding",
				"name", name, "phantomFromJWT", parsed.String())
		} else {
			row.ThunderOrgUUID = &parsed
		}
	}
	err := s.repo.Create(ctx, &row)
	if err == nil {
		return row
	}
	if IsUniqueViolation(err) {
		// Lost the race with a concurrent caller; re-read.
		if existing, rerr := s.repo.GetByName(ctx, name); rerr == nil && existing != nil {
			return *existing
		}
	}
	slog.WarnContext(ctx, "backfill organization row failed", "name", name, "error", err)
	return Organization{}
}
