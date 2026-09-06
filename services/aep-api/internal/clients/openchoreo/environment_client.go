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
	"strings"

	ocgen "github.com/wso2/aep/aep-api/internal/clients/openchoreo/gen"
)

// EnvironmentClient reads OpenChoreo Environments in an org namespace.
// ListNames is the provisioning.EnvironmentLister surface; GetThunderBinding is
// how aep-api finds the environment's own identity provider.
type EnvironmentClient interface {
	ListNames(ctx context.Context, orgID string) ([]string, error)
	GetThunderBinding(ctx context.Context, orgID, environment string) (ThunderBinding, error)
}

// Thunder binding annotations, written onto the Environment by
// deployments/scripts/setup-environment-thunder.sh. They are the NON-SECRET half
// of the binding record; the credential itself lives in the secret store at
// SecretPath, and a third copy of both lives in-cluster for the operator.
//
// aep-api runs outside the cluster, so the OpenChoreo API is the only one of the
// binding's three projections it can read — which is exactly why the script
// writes this one.
const (
	annThunderIssuer                   = "aep.wso2.com/thunder-issuer"
	annThunderAdminURL                 = "aep.wso2.com/thunder-admin-url"
	annThunderSystemResourceIdentifier = "aep.wso2.com/thunder-system-resource-identifier"
	annThunderSecretPath               = "aep.wso2.com/thunder-secret-path"
	annThunderBinding                  = "aep.wso2.com/thunder-binding"
)

// ErrNoThunderBinding is the answer for an environment that exists but has no
// identity provider bound to it. It is its own error because the recovery is
// specific and a caller cannot guess it: run setup-environment-thunder.sh.
var ErrNoThunderBinding = errors.New("openchoreo: environment has no Thunder binding")

// ThunderBinding is one environment's identity provider, as the Environment
// records it.
type ThunderBinding struct {
	OrgID       string
	Environment string
	// Issuer is the public issuer — what a token minted there says, and the one
	// address a login published to a human is valid at.
	Issuer string
	// AdminURL is the in-cluster Service address of the same instance. It is
	// unreachable from outside the cluster, which is why a caller chooses
	// between this and Issuer rather than always taking one.
	AdminURL string
	// SystemResourceIdentifier is the `resource` indicator every scope=system
	// mint against this instance must carry.
	SystemResourceIdentifier string
	// SecretPath is where the admin client's credential lives in the secret
	// store, keyed by (org, environment).
	SecretPath string
	// Name is the binding record's own name, for logs and diagnostics.
	Name string
}

type environmentClient struct {
	oc *ocgen.ClientWithResponses
}

// NewEnvironmentClient builds the Environment list wrapper over the shared OC
// transport. Empty orgID returns an empty slice and does not call OC.
func NewEnvironmentClient(cfg Config) EnvironmentClient {
	oc, err := newGenClient(cfg)
	if err != nil {
		panic(fmt.Errorf("init openchoreo environment client: %w", err))
	}
	return &environmentClient{oc: oc}
}

func (c *environmentClient) ListNames(ctx context.Context, orgID string) ([]string, error) {
	if strings.TrimSpace(orgID) == "" {
		return []string{}, nil
	}
	resp, err := c.oc.ListEnvironmentsWithResponse(ctx, orgID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list environments: %w", err)
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return nil, handleErrorResponse(resp.StatusCode(), ErrorResponses{
			JSON400: resp.JSON400,
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON500: resp.JSON500,
		})
	}
	names := make([]string, 0, len(resp.JSON200.Items))
	for _, item := range resp.JSON200.Items {
		names = append(names, item.Metadata.Name)
	}
	return names, nil
}

// GetThunderBinding reads the environment's identity-provider binding off its
// annotations.
//
// An environment with none is ErrNoThunderBinding, distinguished from every
// transport failure, because the two need opposite responses: the first is
// "provision one", the second is "retry".
func (c *environmentClient) GetThunderBinding(ctx context.Context, orgID, environment string) (ThunderBinding, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(environment) == "" {
		return ThunderBinding{}, fmt.Errorf("get thunder binding: org and environment are both required")
	}
	resp, err := c.oc.GetEnvironmentWithResponse(ctx, orgID, environment)
	if err != nil {
		return ThunderBinding{}, fmt.Errorf("failed to get environment %s/%s: %w", orgID, environment, err)
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return ThunderBinding{}, handleErrorResponse(resp.StatusCode(), ErrorResponses{
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON404: resp.JSON404,
			JSON500: resp.JSON500,
		})
	}
	var annotations map[string]string
	if resp.JSON200.Metadata.Annotations != nil {
		annotations = *resp.JSON200.Metadata.Annotations
	}
	return thunderBindingFromAnnotations(orgID, environment, annotations)
}

// thunderBindingFromAnnotations is the parse, split out so it can be tested
// without a server.
//
// Every field the caller needs to MINT a token is required: an issuer with no
// credential path, or a credential with no resource indicator, produces the
// silent scope-drop this platform has already been bitten by (a 200 from the
// token endpoint, a scope-less token, and a 403 four requests later). A
// half-written binding is therefore reported as absent rather than used.
func thunderBindingFromAnnotations(orgID, environment string, annotations map[string]string) (ThunderBinding, error) {
	binding := ThunderBinding{
		OrgID:                    orgID,
		Environment:              environment,
		Issuer:                   strings.TrimSpace(annotations[annThunderIssuer]),
		AdminURL:                 strings.TrimSpace(annotations[annThunderAdminURL]),
		SystemResourceIdentifier: strings.TrimSpace(annotations[annThunderSystemResourceIdentifier]),
		SecretPath:               strings.TrimSpace(annotations[annThunderSecretPath]),
		Name:                     strings.TrimSpace(annotations[annThunderBinding]),
	}
	var missing []string
	for _, required := range []struct {
		key, value string
	}{
		{annThunderIssuer, binding.Issuer},
		{annThunderSystemResourceIdentifier, binding.SystemResourceIdentifier},
		{annThunderSecretPath, binding.SecretPath},
	} {
		if required.value == "" {
			missing = append(missing, required.key)
		}
	}
	if len(missing) > 0 {
		return ThunderBinding{}, fmt.Errorf("%w: %s/%s is missing %s — run setup-environment-thunder.sh %s %s",
			ErrNoThunderBinding, orgID, environment, strings.Join(missing, ", "), orgID, environment)
	}
	return binding, nil
}
