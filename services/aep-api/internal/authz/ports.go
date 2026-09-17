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

package authz

import (
	"context"
	"errors"
)

// Sentinel errors an OCAuthZClient implementation must wrap its underlying
// client's errors with, so this package can branch on outcome without
// importing the client (which would cycle back through CreatedAuthzRole).
var (
	// ErrRoleNotFound is GetAuthzRole's answer when no AuthzRole exists by
	// that name in the namespace.
	ErrRoleNotFound = errors.New("authz: role not found")
	// ErrRoleConflict is CreateAuthzRole's answer when an AuthzRole by that
	// name already exists in the namespace.
	ErrRoleConflict = errors.New("authz: role already exists")
	// ErrRoleBindingNotFound is GetAuthzRoleBinding's answer when no
	// AuthzRoleBinding exists by that name in the namespace.
	ErrRoleBindingNotFound = errors.New("authz: role binding not found")
	// ErrRoleBindingConflict is CreateAuthzRoleBinding's answer when an
	// AuthzRoleBinding by that name already exists in the namespace.
	ErrRoleBindingConflict = errors.New("authz: role binding already exists")
)

// PermissionResolver maps AE-level permissions to their OC equivalents.
// Implementations must deduplicate the returned set when multiple AE
// permissions expand to overlapping OC permissions.
type PermissionResolver interface {
	// ResolveOcPermissions returns the deduplicated set of OC permissions that
	// cover all of the given AE permissions.
	ResolveOcPermissions(aePermissions []string) []string
}

// CreatedAuthzRole is the domain representation of an OC AuthzRole after creation.
type CreatedAuthzRole struct {
	Name    string
	Actions []string
}

// EntitlementClaim is the JWT claim/value pair a role binding grants the role
// to — whoever presents this claim=value in their token holds the role.
type EntitlementClaim struct {
	Claim string
	Value string
}

// CreatedAuthzRoleBinding is the domain representation of an OC
// AuthzRoleBinding after creation: its own resource name, which role it
// grants, and to whom (the JWT entitlement claim it matches).
type CreatedAuthzRoleBinding struct {
	Name        string
	RoleName    string
	Entitlement EntitlementClaim
}

// OCAuthZClient manages OC AuthzRole and AuthzRoleBinding resources.
// It is a narrowing of the OC client, mapped at the composition root.
type OCAuthZClient interface {
	GetAuthzRole(ctx context.Context, namespace string, name string) (CreatedAuthzRole, error)
	CreateAuthzRole(ctx context.Context, namespace string, name string, actions []string) (CreatedAuthzRole, error)
	UpdateAuthzRole(ctx context.Context, namespace string, name string, actions []string) (CreatedAuthzRole, error)
	DeleteAuthzRole(ctx context.Context, namespace string, name string) error

	GetAuthzRoleBinding(ctx context.Context, namespace string, bindingName string) (CreatedAuthzRoleBinding, error)
	CreateAuthzRoleBinding(ctx context.Context, namespace string, bindingName string, roleName string, entitlement EntitlementClaim) (CreatedAuthzRoleBinding, error)
	DeleteAuthzRoleBinding(ctx context.Context, namespace string, bindingName string) error
}
