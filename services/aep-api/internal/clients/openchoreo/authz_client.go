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

package openchoreo

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/authz"
	ocgen "github.com/wso2/aep/aep-api/internal/clients/openchoreo/gen"
)

// AuthZClient defines operations for managing OC authz roles and bindings.
type AuthZClient interface {
	GetAuthzRole(ctx context.Context, namespace string, name string) (authz.CreatedAuthzRole, error)
	CreateAuthzRole(ctx context.Context, namespace string, name string, actions []string) (authz.CreatedAuthzRole, error)
	UpdateAuthzRole(ctx context.Context, namespace string, name string, actions []string) (authz.CreatedAuthzRole, error)
	DeleteAuthzRole(ctx context.Context, namespace string, name string) error

	GetAuthzRoleBinding(ctx context.Context, namespace string, bindingName string) (authz.CreatedAuthzRoleBinding, error)
	CreateAuthzRoleBinding(ctx context.Context, namespace string, bindingName string, roleName string, entitlement authz.EntitlementClaim) (authz.CreatedAuthzRoleBinding, error)
	DeleteAuthzRoleBinding(ctx context.Context, namespace string, bindingName string) error
}

type authZClient struct {
	oc *ocgen.ClientWithResponses
}

func NewAuthZClient(cfg Config) AuthZClient {
	oc, err := newGenClient(cfg)
	if err != nil {
		panic(fmt.Errorf("init openchoreo authz client: %w", err))
	}
	return &authZClient{oc: oc}
}

func (c *authZClient) GetAuthzRole(ctx context.Context, namespace string, name string) (authz.CreatedAuthzRole, error) {
	resp, err := c.oc.GetNamespaceRoleWithResponse(ctx, namespace, name)
	if err != nil {
		return authz.CreatedAuthzRole{}, fmt.Errorf("failed to get authz role %q: %w", name, err)
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		err := handleErrorResponse(ctx, http.MethodGet, nsBase(namespace)+"/authzroles/"+name, resp.StatusCode(), ErrorResponses{
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON404: resp.JSON404,
			JSON500: resp.JSON500,
		})
		if errors.Is(err, ErrNotFound) {
			err = fmt.Errorf("%w: %w", authz.ErrRoleNotFound, err)
		}
		return authz.CreatedAuthzRole{}, err
	}
	r := resp.JSON200
	return authz.CreatedAuthzRole{
		Name:    r.Metadata.Name,
		Actions: r.Spec.Actions,
	}, nil
}

func (c *authZClient) UpdateAuthzRole(ctx context.Context, namespace string, name string, actions []string) (authz.CreatedAuthzRole, error) {
	body := ocgen.AuthzRole{
		Metadata: ocgen.ObjectMeta{Name: name},
		Spec:     &ocgen.AuthzRoleSpec{Actions: actions},
	}
	resp, err := c.oc.UpdateNamespaceRoleWithResponse(ctx, namespace, name, body)
	if err != nil {
		return authz.CreatedAuthzRole{}, fmt.Errorf("failed to update authz role %q: %w", name, err)
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return authz.CreatedAuthzRole{}, handleErrorResponse(ctx, http.MethodPut, nsBase(namespace)+"/authzroles/"+name, resp.StatusCode(), ErrorResponses{
			JSON400: resp.JSON400,
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON404: resp.JSON404,
			JSON500: resp.JSON500,
		})
	}
	r := resp.JSON200
	return authz.CreatedAuthzRole{
		Name:    r.Metadata.Name,
		Actions: r.Spec.Actions,
	}, nil
}

func (c *authZClient) CreateAuthzRole(ctx context.Context, namespace string, name string, actions []string) (authz.CreatedAuthzRole, error) {
	body := ocgen.AuthzRole{
		Metadata: ocgen.ObjectMeta{Name: name},
		Spec:     &ocgen.AuthzRoleSpec{Actions: actions},
	}
	resp, err := c.oc.CreateNamespaceRoleWithResponse(ctx, namespace, body)
	if err != nil {
		return authz.CreatedAuthzRole{}, fmt.Errorf("failed to create authz role %q: %w", name, err)
	}
	if resp.StatusCode() != http.StatusCreated || resp.JSON201 == nil {
		err := handleErrorResponse(ctx, http.MethodPost, nsBase(namespace)+"/authzroles", resp.StatusCode(), ErrorResponses{
			JSON400: resp.JSON400,
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON409: resp.JSON409,
			JSON500: resp.JSON500,
		})
		if errors.Is(err, ErrConflict) {
			err = fmt.Errorf("%w: %w", authz.ErrRoleConflict, err)
		}
		return authz.CreatedAuthzRole{}, err
	}
	r := resp.JSON201
	return authz.CreatedAuthzRole{
		Name:    r.Metadata.Name,
		Actions: r.Spec.Actions,
	}, nil
}

func (c *authZClient) GetAuthzRoleBinding(ctx context.Context, namespace string, bindingName string) (authz.CreatedAuthzRoleBinding, error) {
	resp, err := c.oc.GetNamespaceRoleBindingWithResponse(ctx, namespace, bindingName)
	if err != nil {
		return authz.CreatedAuthzRoleBinding{}, fmt.Errorf("failed to get authz role binding %q: %w", bindingName, err)
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		err := handleErrorResponse(ctx, http.MethodGet, nsBase(namespace)+"/authzrolebindings/"+bindingName, resp.StatusCode(), ErrorResponses{
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON404: resp.JSON404,
			JSON500: resp.JSON500,
		})
		if errors.Is(err, ErrNotFound) {
			err = fmt.Errorf("%w: %w", authz.ErrRoleBindingNotFound, err)
		}
		return authz.CreatedAuthzRoleBinding{}, err
	}
	return createdAuthzRoleBindingFromResp(resp.JSON200), nil
}

func (c *authZClient) CreateAuthzRoleBinding(ctx context.Context, namespace string, bindingName string, roleName string, entitlement authz.EntitlementClaim) (authz.CreatedAuthzRoleBinding, error) {
	body := ocgen.AuthzRoleBinding{
		Metadata: ocgen.ObjectMeta{Name: bindingName},
		Spec: &ocgen.AuthzRoleBindingSpec{
			Entitlement: ocgen.AuthzEntitlementClaim{
				Claim: entitlement.Claim,
				Value: entitlement.Value,
			},
			RoleMappings: []ocgen.AuthzRoleMapping{
				{RoleRef: ocgen.AuthzRoleRef{Kind: ocgen.AuthzRoleRefKindAuthzRole, Name: roleName}},
			},
		},
	}
	resp, err := c.oc.CreateNamespaceRoleBindingWithResponse(ctx, namespace, body)
	if err != nil {
		return authz.CreatedAuthzRoleBinding{}, fmt.Errorf("failed to create authz role binding %q: %w", bindingName, err)
	}
	if resp.StatusCode() != http.StatusCreated || resp.JSON201 == nil {
		err := handleErrorResponse(ctx, http.MethodPost, nsBase(namespace)+"/authzrolebindings", resp.StatusCode(), ErrorResponses{
			JSON400: resp.JSON400,
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON409: resp.JSON409,
			JSON500: resp.JSON500,
		})
		if errors.Is(err, ErrConflict) {
			err = fmt.Errorf("%w: %w", authz.ErrRoleBindingConflict, err)
		}
		return authz.CreatedAuthzRoleBinding{}, err
	}
	return createdAuthzRoleBindingFromResp(resp.JSON201), nil
}

// createdAuthzRoleBindingFromResp maps the OC wire type into the domain type,
// shared by Get and Create since both respond with the same AuthzRoleBinding
// shape. RoleName reads the first role mapping: EnsureAuthzRole creates
// bindings with exactly one, and that is the only shape this client produces
// or looks up.
func createdAuthzRoleBindingFromResp(r *ocgen.AuthzRoleBinding) authz.CreatedAuthzRoleBinding {
	binding := authz.CreatedAuthzRoleBinding{Name: r.Metadata.Name}
	if r.Spec == nil {
		return binding
	}
	binding.Entitlement = authz.EntitlementClaim{
		Claim: r.Spec.Entitlement.Claim,
		Value: r.Spec.Entitlement.Value,
	}
	if len(r.Spec.RoleMappings) > 0 {
		binding.RoleName = r.Spec.RoleMappings[0].RoleRef.Name
	}
	return binding
}

func (c *authZClient) DeleteAuthzRole(ctx context.Context, namespace string, name string) error {
	resp, err := c.oc.DeleteNamespaceRoleWithResponse(ctx, namespace, name)
	if err != nil {
		return fmt.Errorf("failed to delete authz role %q: %w", name, err)
	}
	if resp.StatusCode() != http.StatusOK {
		return handleErrorResponse(ctx, http.MethodDelete, nsBase(namespace)+"/authzroles/"+name, resp.StatusCode(), ErrorResponses{
			JSON400: resp.JSON400,
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON404: resp.JSON404,
			JSON409: resp.JSON409,
			JSON500: resp.JSON500,
		})
	}
	return nil
}

func (c *authZClient) DeleteAuthzRoleBinding(ctx context.Context, namespace string, bindingName string) error {
	resp, err := c.oc.DeleteNamespaceRoleBindingWithResponse(ctx, namespace, bindingName)
	if err != nil {
		return fmt.Errorf("failed to delete authz role binding %q: %w", bindingName, err)
	}
	if resp.StatusCode() != http.StatusOK {
		err := handleErrorResponse(ctx, http.MethodDelete, nsBase(namespace)+"/authzrolebindings/"+bindingName, resp.StatusCode(), ErrorResponses{
			JSON400: resp.JSON400,
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON404: resp.JSON404,
			JSON500: resp.JSON500,
		})
		if errors.Is(err, ErrNotFound) {
			err = fmt.Errorf("%w: %w", authz.ErrRoleBindingNotFound, err)
		}
		return err
	}
	return nil
}
