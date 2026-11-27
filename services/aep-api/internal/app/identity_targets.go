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

package app

// identity_targets.go — the composition-root adapter that answers "which
// identity provider serves this project", for the identity domain's
// TargetResolver port.
//
// There is one identity provider per (org, environment) — the environment tier,
// "T2" — and a login minted on one is rejected by every other. Finding it is
// four reads this package is the only one able to make together:
//
//	the project's write target                             (OpenChoreo API)
//	the Environment's aep.wso2.com/thunder-* annotations   (OpenChoreo API)
//	the admin credential at the binding's secret path      (the secret store)
//	a thundersvc client built from the two                 (this file)
//
// The annotations and the OpenChoreo secret are the binding record's
// projections aep-api reads; the ConfigMap and the mirrored Secret that aectl
// also writes are for the in-cluster operator.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/clients/thundersvc"
	"github.com/wso2/aep/aep-api/internal/identity"
)

// identityTargetTTL bounds how long a resolved client is reused.
//
// Eviction on a rejected credential (see invalidatingDirectory) is the fast
// path and covers the case that matters — a rotated admin secret. The TTL is
// the backstop for the failures that never produce a rejected CALL: with a
// stale secret the token mint itself fails, so no directory call ever returns a
// 401 to evict on. Ten minutes bounds that without making the binding read a
// per-request cost.
const identityTargetTTL = 10 * time.Minute

// The keys of the binding's OpenChoreo secret, as aectl writes them.
const (
	credentialKeyClientID     = "clientId"
	credentialKeyClientSecret = "clientSecret"
)

// bindingRecovery is the only recovery for a missing binding, and it applies
// to one environment, so it is named with its scope.
const bindingRecovery = "aectl platform install binds only the environment named by " +
	"oc.pipeline_source_environment in oc.default_org_namespace; re-run it if that is this environment"

// The two ways to address an environment's Thunder, and the config value that
// picks one. See identityTargetResolver.adminBaseURL.
const (
	adminRouteIssuer  = "issuer"
	adminRouteBinding = "binding"
)

// environmentBindingReader is the OpenChoreo read this adapter needs — the
// Environment's binding annotations, narrowed from openchoreo.EnvironmentClient.
type environmentBindingReader interface {
	GetThunderBinding(ctx context.Context, orgID, environment string) (openchoreo.ThunderBinding, error)
}

// bindingCredentialReader reads the admin credential the binding names.
type bindingCredentialReader interface {
	ReadBindingCredential(ctx context.Context, namespace, name string) (map[string]string, error)
}

// writeTargetReader names the environment a project writes into, narrowed from
// openchoreo.WriteTargets. The identity provider a project's roles belong to is
// the one bound to that environment.
type writeTargetReader interface {
	Resolve(ctx context.Context, org, project string) (string, error)
	OrgDefaultRoot(ctx context.Context, org string) (string, error)
}

// identityTargetResolver resolves and caches one client per (org, environment).
type identityTargetResolver struct {
	environments environmentBindingReader
	credentials  bindingCredentialReader
	targets      writeTargetReader
	// adminRoute picks the address admin calls go to — the binding's in-cluster
	// Service, or the public issuer. See adminBaseURL.
	adminRoute string

	mu     sync.Mutex
	cached map[identity.Scope]*resolvedTarget
	now    func() time.Time
}

// resolvedTarget is one cached client and when it was built.
type resolvedTarget struct {
	target   identity.Target
	resolved time.Time
}

// newIdentityTargetResolver builds the adapter. A nil environments or
// credentials reader means the feature cannot work at all, and the caller wires
// nothing rather than handing the domain a resolver that fails every call.
func newIdentityTargetResolver(
	environments environmentBindingReader,
	credentials bindingCredentialReader,
	targets writeTargetReader,
	adminRoute string,
) *identityTargetResolver {
	if adminRoute != adminRouteBinding {
		adminRoute = adminRouteIssuer
	}
	return &identityTargetResolver{
		environments: environments,
		credentials:  credentials,
		targets:      targets,
		adminRoute:   adminRoute,
		cached:       map[identity.Scope]*resolvedTarget{},
		now:          time.Now,
	}
}

var _ identity.TargetResolver = (*identityTargetResolver)(nil)

// Scope names the (org, environment) whose identity provider serves the
// project: its write target, or with no project the org default pipeline's
// root. The write target's error is returned unchanged, so a caller can tell a
// configuration fact (*openchoreo.ErrNoWriteTarget) from a transient failure.
func (r *identityTargetResolver) Scope(ctx context.Context, orgID, projectID string) (identity.Scope, error) {
	var (
		environment string
		err         error
	)
	if projectID == "" {
		environment, err = r.targets.OrgDefaultRoot(ctx, orgID)
	} else {
		environment, err = r.targets.Resolve(ctx, orgID, projectID)
	}
	if err != nil {
		return identity.Scope{}, err
	}
	return identity.Scope{OrgID: orgID, Environment: environment}, nil
}

// Resolve returns that scope's directory, bound and authenticated.
func (r *identityTargetResolver) Resolve(ctx context.Context, scope identity.Scope) (identity.Target, error) {
	if strings.TrimSpace(scope.OrgID) == "" {
		return identity.Target{}, errors.New("identity target: no org to resolve an identity provider for")
	}
	if strings.TrimSpace(scope.Environment) == "" {
		return identity.Target{}, fmt.Errorf("identity target: no environment to resolve an identity provider for in %q", scope.OrgID)
	}
	if hit, ok := r.cachedTarget(scope); ok {
		return hit, nil
	}

	binding, err := r.environments.GetThunderBinding(ctx, scope.OrgID, scope.Environment)
	if err != nil {
		if errors.Is(err, openchoreo.ErrNoThunderBinding) {
			return identity.Target{}, fmt.Errorf(
				"environment %q of %q has no Thunder binding (%s): %w",
				scope.Environment, scope.OrgID, bindingRecovery, err)
		}
		return identity.Target{}, fmt.Errorf("read the Thunder binding of %s: %w", scope, err)
	}

	fields, err := r.credentials.ReadBindingCredential(ctx, scope.OrgID, binding.SecretRef)
	if err != nil {
		return identity.Target{}, fmt.Errorf("read the admin credential of %s: %w", scope, err)
	}
	clientID, clientSecret := fields[credentialKeyClientID], fields[credentialKeyClientSecret]
	if clientID == "" || clientSecret == "" {
		// Reported as a missing binding rather than an auth failure: the
		// recovery is the same.
		return identity.Target{}, fmt.Errorf(
			"the admin credential of %s is missing from the secret store (%s): %w",
			scope, bindingRecovery, openchoreo.ErrNoThunderBinding)
	}

	baseURL := r.adminBaseURL(binding)
	client := thundersvc.New(thundersvc.Config{
		BaseURL:                  baseURL,
		ClientID:                 clientID,
		ClientSecret:             clientSecret,
		SystemResourceIdentifier: binding.SystemResourceIdentifier,
	})
	target := identity.Target{
		OrgID:       scope.OrgID,
		Environment: scope.Environment,
		Issuer:      binding.Issuer,
		Directory: invalidatingDirectory{
			inner:      thunderDirectory{c: client},
			invalidate: func() { r.invalidate(scope) },
			scope:      scope,
		},
	}
	slog.InfoContext(ctx, "identity target resolved",
		"scope", scope.String(), "issuer", binding.Issuer, "adminURL", baseURL,
		"systemResource", binding.SystemResourceIdentifier, "binding", binding.Name,
		"clientID", clientID, "route", r.adminRoute)

	r.mu.Lock()
	r.cached[scope] = &resolvedTarget{target: target, resolved: r.now()}
	r.mu.Unlock()
	return target, nil
}

// adminBaseURL is the POLICY: which of the binding's two addresses admin calls
// go to.
//
//   - `issuer` (the default) uses the identity provider's PUBLIC issuer. It is
//     the only one that works from the local docker-compose stack, where aep-api
//     is outside the cluster and the binding's `*.svc.cluster.local` admin URL
//     resolves to nothing. The public hostname reaches the same instance through
//     the cluster's ingress.
//   - `binding` uses the in-cluster Service address the binding records. That is
//     right for an aep-api running INSIDE the cluster, where it is the shorter
//     path and does not depend on ingress — and where, on this cluster, the
//     public `*.amp.localhost` hostnames do not resolve from a pod at all.
//
// Falling back to the issuer when the binding carries no admin URL keeps a
// binding written before that annotation existed usable.
func (r *identityTargetResolver) adminBaseURL(binding openchoreo.ThunderBinding) string {
	if r.adminRoute == adminRouteBinding && binding.AdminURL != "" {
		return binding.AdminURL
	}
	return binding.Issuer
}

// cachedTarget returns a live cache entry, dropping an expired one.
func (r *identityTargetResolver) cachedTarget(scope identity.Scope) (identity.Target, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	hit, ok := r.cached[scope]
	if !ok {
		return identity.Target{}, false
	}
	if r.now().Sub(hit.resolved) >= identityTargetTTL {
		delete(r.cached, scope)
		return identity.Target{}, false
	}
	return hit.target, true
}

// invalidate drops a cached client so the next Resolve re-reads the binding.
func (r *identityTargetResolver) invalidate(scope identity.Scope) {
	r.mu.Lock()
	_, had := r.cached[scope]
	delete(r.cached, scope)
	r.mu.Unlock()
	if had {
		slog.Warn("identity target invalidated — the identity provider rejected its admin credential",
			"scope", scope.String())
	}
}

// invalidatingDirectory drops the cached client when the identity provider
// REJECTS its credential.
//
// The wrap is here rather than in the domain deliberately: the identity domain
// must not know what a 401 is, and the cache is this file's business. Anything
// that is not a rejection — a 404, a conflict, a timeout — leaves the entry
// alone, because evicting on those would rebuild the client on every ordinary
// error and turn one bad request into two network reads.
type invalidatingDirectory struct {
	inner      identity.Directory
	invalidate func()
	scope      identity.Scope
}

var _ identity.Directory = invalidatingDirectory{}

// check passes an error through, evicting the cache first when it is the
// identity provider rejecting the credential.
func (d invalidatingDirectory) check(err error) error {
	if err != nil && thundersvc.IsAuthError(err) {
		d.invalidate()
	}
	return err
}

func (d invalidatingDirectory) ListGroups(ctx context.Context) ([]identity.DirectoryGroup, error) {
	groups, err := d.inner.ListGroups(ctx)
	return groups, d.check(err)
}

func (d invalidatingDirectory) FindGroupByName(ctx context.Context, name string) (*identity.DirectoryGroup, bool, error) {
	group, found, err := d.inner.FindGroupByName(ctx, name)
	return group, found, d.check(err)
}

func (d invalidatingDirectory) GroupMembers(ctx context.Context, groupID string) ([]string, error) {
	members, err := d.inner.GroupMembers(ctx, groupID)
	return members, d.check(err)
}

func (d invalidatingDirectory) CreateGroup(ctx context.Context, name, description string, memberIDs []string) (identity.DirectoryGroup, error) {
	group, err := d.inner.CreateGroup(ctx, name, description, memberIDs)
	return group, d.check(err)
}

func (d invalidatingDirectory) AddMembers(ctx context.Context, group identity.DirectoryGroup, memberIDs []string) (identity.DirectoryGroup, error) {
	updated, err := d.inner.AddMembers(ctx, group, memberIDs)
	return updated, d.check(err)
}

func (d invalidatingDirectory) RemoveMembers(ctx context.Context, group identity.DirectoryGroup, memberIDs []string) (identity.DirectoryGroup, error) {
	updated, err := d.inner.RemoveMembers(ctx, group, memberIDs)
	return updated, d.check(err)
}

func (d invalidatingDirectory) UserGroups(ctx context.Context, userID string) ([]identity.DirectoryGroup, error) {
	groups, err := d.inner.UserGroups(ctx, userID)
	return groups, d.check(err)
}

func (d invalidatingDirectory) FindUserByUsername(ctx context.Context, username string) (*identity.DirectoryAccount, bool, error) {
	account, found, err := d.inner.FindUserByUsername(ctx, username)
	return account, found, d.check(err)
}

func (d invalidatingDirectory) CreateUser(ctx context.Context, username, email, password string) (identity.DirectoryAccount, error) {
	account, err := d.inner.CreateUser(ctx, username, email, password)
	return account, d.check(err)
}

func (d invalidatingDirectory) SetUserPassword(ctx context.Context, userID, password string) error {
	return d.check(d.inner.SetUserPassword(ctx, userID, password))
}

func (d invalidatingDirectory) DeleteUser(ctx context.Context, userID string) error {
	return d.check(d.inner.DeleteUser(ctx, userID))
}

// The project-owned half. Identical shape, and for the same reason: the wrap
// exists so ONE place decides what a rejected credential means, and a verb that
// skipped it would leave a rotated admin secret failing this call forever while
// every other call recovered.

func (d invalidatingDirectory) EnsureResourceServer(ctx context.Context, identifier, name string) (identity.DirectoryID, error) {
	rs, err := d.inner.EnsureResourceServer(ctx, identifier, name)
	return rs, d.check(err)
}

func (d invalidatingDirectory) FindResourceServer(ctx context.Context, identifier string) (identity.DirectoryID, bool, error) {
	rs, found, err := d.inner.FindResourceServer(ctx, identifier)
	return rs, found, d.check(err)
}

func (d invalidatingDirectory) ListResources(ctx context.Context, rs identity.DirectoryID) ([]identity.DirectoryResource, error) {
	resources, err := d.inner.ListResources(ctx, rs)
	return resources, d.check(err)
}

func (d invalidatingDirectory) CreateResource(ctx context.Context, rs identity.DirectoryID, handle, name, description string) (identity.DirectoryResource, error) {
	resource, err := d.inner.CreateResource(ctx, rs, handle, name, description)
	return resource, d.check(err)
}

func (d invalidatingDirectory) UpdateResource(ctx context.Context, rs, resource identity.DirectoryID, name, description string) error {
	return d.check(d.inner.UpdateResource(ctx, rs, resource, name, description))
}

func (d invalidatingDirectory) DeleteResource(ctx context.Context, rs, resource identity.DirectoryID) error {
	return d.check(d.inner.DeleteResource(ctx, rs, resource))
}

func (d invalidatingDirectory) ListActions(ctx context.Context, rs, resource identity.DirectoryID) ([]identity.DirectoryAction, error) {
	actions, err := d.inner.ListActions(ctx, rs, resource)
	return actions, d.check(err)
}

func (d invalidatingDirectory) CreateAction(ctx context.Context, rs, resource identity.DirectoryID, handle, name, description string) (identity.DirectoryAction, error) {
	action, err := d.inner.CreateAction(ctx, rs, resource, handle, name, description)
	return action, d.check(err)
}

func (d invalidatingDirectory) UpdateAction(ctx context.Context, rs, resource, action identity.DirectoryID, name, description string) error {
	return d.check(d.inner.UpdateAction(ctx, rs, resource, action, name, description))
}

func (d invalidatingDirectory) DeleteAction(ctx context.Context, rs, resource, action identity.DirectoryID) error {
	return d.check(d.inner.DeleteAction(ctx, rs, resource, action))
}

func (d invalidatingDirectory) DeleteResourceServer(ctx context.Context, rs identity.DirectoryID) error {
	return d.check(d.inner.DeleteResourceServer(ctx, rs))
}

func (d invalidatingDirectory) EnsureRole(ctx context.Context, id identity.DirectoryID, name, description string, rs identity.DirectoryID, permissions []string) (identity.DirectoryID, error) {
	role, err := d.inner.EnsureRole(ctx, id, name, description, rs, permissions)
	return role, d.check(err)
}

func (d invalidatingDirectory) ListRoles(ctx context.Context) ([]identity.RoleRef, error) {
	roles, err := d.inner.ListRoles(ctx)
	return roles, d.check(err)
}

func (d invalidatingDirectory) ListRolePermissions(ctx context.Context, role identity.DirectoryID) ([]string, error) {
	permissions, err := d.inner.ListRolePermissions(ctx, role)
	return permissions, d.check(err)
}

func (d invalidatingDirectory) DeleteRole(ctx context.Context, role identity.DirectoryID) error {
	return d.check(d.inner.DeleteRole(ctx, role))
}

func (d invalidatingDirectory) AssignRole(ctx context.Context, role identity.DirectoryID, principal identity.Principal) error {
	return d.check(d.inner.AssignRole(ctx, role, principal))
}

func (d invalidatingDirectory) UnassignRole(ctx context.Context, role identity.DirectoryID, principal identity.Principal) error {
	return d.check(d.inner.UnassignRole(ctx, role, principal))
}

func (d invalidatingDirectory) ListRoleAssignments(ctx context.Context, role identity.DirectoryID) ([]identity.Principal, error) {
	principals, err := d.inner.ListRoleAssignments(ctx, role)
	return principals, d.check(err)
}

// openChoreoBindingCredentials reads the binding's credential through the
// secret client (OpenChoreo's Secret API).
type openChoreoBindingCredentials struct {
	secrets secretmanagersvc.SecretManagementClient
}

func (r openChoreoBindingCredentials) ReadBindingCredential(ctx context.Context, namespace, name string) (map[string]string, error) {
	return r.secrets.GetSecretWithValue(ctx, secretmanagersvc.SecretLocation{ControlPlaneNamespace: namespace, RefName: name})
}
