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
	"fmt"
	"log/slog"
)

// AuthZService is the domain service for AE authz operations. It owns the
// HTTP-facing operations, delegates AE→OC translation to a PermissionResolver,
// and creates OC AuthzRole resources via an OCAuthZClient.
type AuthZService struct {
	resolver PermissionResolver
	client   OCAuthZClient
}

func NewAuthZService(resolver PermissionResolver, client OCAuthZClient) *AuthZService {
	return &AuthZService{resolver: resolver, client: client}
}

func (s *AuthZService) ModifyRolePermissions(ctx context.Context, orgHandle string, rolePermissions map[string][]string) error {
	previousStates := make(map[string][]string, len(rolePermissions))

	for role, aePermissions := range rolePermissions {
		current, err := s.client.GetAuthzRole(ctx, orgHandle, role)
		if err != nil {
			s.restoreRoles(ctx, orgHandle, previousStates)
			return fmt.Errorf("authz: failed to get authz role %q before update: %w", role, err)
		}
		previousStates[role] = current.Actions

		ocPermissions := s.resolver.ResolveOcPermissions(aePermissions)
		result, err := s.client.UpdateAuthzRole(ctx, orgHandle, role, ocPermissions)
		if err != nil {
			s.restoreRoles(ctx, orgHandle, previousStates)
			return fmt.Errorf("authz: failed to update authz role %q: %w", role, err)
		}
		slog.Info("authz: updated OC AuthzRole", "name", result.Name, "actions", result.Actions)
	}
	return nil
}

func (s *AuthZService) restoreRoles(ctx context.Context, orgHandle string, previousStates map[string][]string) {
	for name, actions := range previousStates {
		if _, err := s.client.UpdateAuthzRole(ctx, orgHandle, name, actions); err != nil {
			slog.Error("authz: failed to restore AuthzRole after rollback", "name", name, "err", err)
		}
	}
}

func (s *AuthZService) EnsureAuthzRole(ctx context.Context, orgHandle string) error {
	aeRoles := Roles()
	for _, role := range aeRoles {
		if err := s.ensureRole(ctx, orgHandle, role); err != nil {
			return err
		}
		if err := s.ensureRoleBinding(ctx, orgHandle, role); err != nil {
			return err
		}
	}
	slog.Info("authz: ensured AE roles exist in OC", "orgHandle", orgHandle, "roles", aeRoles)
	return nil
}

func (s *AuthZService) ensureRole(ctx context.Context, orgHandle, role string) error {
	_, err := s.client.GetAuthzRole(ctx, orgHandle, role)
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrRoleNotFound) {
		return fmt.Errorf("authz: failed to check authz role %q: %w", role, err)
	}

	ocPermissions := s.resolver.ResolveOcPermissions(RolePermissions(role))
	result, createErr := s.client.CreateAuthzRole(ctx, orgHandle, role, ocPermissions)
	switch {
	case createErr == nil:
		slog.Info("authz: created OC AuthzRole", "name", result.Name, "actions", result.Actions)
		return nil
	case errors.Is(createErr, ErrRoleConflict):
		slog.Info("authz: authz role already exists, skipping create", "name", role)
		return nil
	default:
		return fmt.Errorf("authz: failed to create authz role %q: %w", role, createErr)
	}
}

func (s *AuthZService) ensureRoleBinding(ctx context.Context, orgHandle, role string) error {
	bindingName := role

	_, err := s.client.GetAuthzRoleBinding(ctx, orgHandle, bindingName)
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrRoleBindingNotFound) {
		return fmt.Errorf("authz: failed to check authz role binding %q: %w", bindingName, err)
	}

	entitlement := EntitlementClaim{Claim: "groups", Value: role}
	result, createErr := s.client.CreateAuthzRoleBinding(ctx, orgHandle, bindingName, role, entitlement)
	switch {
	case createErr == nil:
		slog.Info("authz: created OC AuthzRoleBinding", "name", result.Name, "role", result.RoleName)
		return nil
	case errors.Is(createErr, ErrRoleBindingConflict):
		slog.Info("authz: authz role binding already exists, skipping create", "name", bindingName)
		return nil
	default:
		return fmt.Errorf("authz: failed to create authz role binding %q: %w", bindingName, createErr)
	}
}

func (s *AuthZService) DeleteRoles(ctx context.Context, orgHandle string, roleNames []string) {
	for _, role := range roleNames {
		bindingName := role
		roleDeleteErr := s.client.DeleteAuthzRole(ctx, orgHandle, role)
		if roleDeleteErr != nil {
			slog.Error("authz: failed to delete AuthzRole", "name", role, "err", roleDeleteErr)
		}
		roleBindingDeleteErr := s.client.DeleteAuthzRoleBinding(ctx, orgHandle, bindingName)
		if roleBindingDeleteErr != nil {
			slog.Error("authz: failed to delete AuthzRoleBinding", "name", bindingName, "err", roleBindingDeleteErr)
		}
	}
}
