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
	GetGatewayAssertion(ctx context.Context, orgID, environment string) (GatewayAssertion, error)
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

// Gateway-assertion annotations, written onto the Environment by
// deployments/scripts/setup-environment-gateway.sh when it provisions the
// environment gateway's signing keypair. They reach aep-api the same way the
// Thunder binding does and for the same reason: the OpenChoreo API is the only
// projection of an environment-level fact it can read from outside the cluster.
//
// All three describe the VERIFICATION half. The signing key itself stays in the
// gateway's namespace and is never projected here.
const (
	annGatewayAssertionIssuer      = "aep.wso2.com/gateway-assertion-issuer"
	annGatewayAssertionHeader      = "aep.wso2.com/gateway-assertion-header"
	annGatewayAssertionCertificate = "aep.wso2.com/gateway-assertion-certificate"
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
		return nil, handleErrorResponse(ctx, http.MethodGet, nsBase(orgID)+"/environments", resp.StatusCode(), ErrorResponses{
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
		return ThunderBinding{}, handleErrorResponse(ctx, http.MethodGet, nsBase(orgID)+"/environments/"+environment, resp.StatusCode(), ErrorResponses{
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

// GatewayAssertion is what a service behind this environment's gateway needs in
// order to believe the `x-jwt-assertion` the gateway puts on every upstream
// request: the public half of the gateway's signing keypair, the issuer that
// assertion carries, and the header it arrives in.
//
// It is the service's whole trust anchor. With it a service can prove a request
// came through the gateway — and therefore through the gateway's authentication
// and per-operation scope checks — which is why a generated service holds no
// scope table of its own.
type GatewayAssertion struct {
	OrgID       string
	Environment string
	// Issuer is the `iss` the gateway stamps. It names the GATEWAY, not the
	// IdP: an assertion is minted by the gateway after it has validated the
	// caller's IdP token, and a service pinning the IdP's issuer here would be
	// re-introducing the JWKS coupling the assertion removes.
	Issuer string
	// Header is where the assertion arrives, `x-jwt-assertion` by default. A
	// client-supplied value for it is overwritten by the gateway.
	Header string
	// Certificate is the PEM-encoded, self-signed X.509 certificate carrying the
	// public half. A certificate rather than a bare SPKI key because that is
	// what both generated stacks can read — Ballerina's crypto module decodes a
	// public key from certificate content and from nothing else. It carries no
	// trust of its own: it is a container, pinned by the platform that
	// published it and verified by nobody. Empty means this environment's
	// gateway publishes none.
	Certificate string
}

// Configured reports whether this environment actually publishes a verification
// half. Keyed on the certificate alone: the issuer and the header describe an
// assertion nobody can verify without it, so a binding missing the certificate
// is not a partial one — it is absent.
func (a GatewayAssertion) Configured() bool { return strings.TrimSpace(a.Certificate) != "" }

// GetGatewayAssertion reads the environment's gateway-assertion contract off
// its annotations.
//
// An environment that publishes none is NOT an error: the zero GatewayAssertion
// comes back and Configured() reports false. Unlike a missing Thunder binding —
// which means a deployment cannot mint a token at all — a missing assertion
// means only that this environment's gateway was provisioned before assertions
// existed, or by something that does not provision them. The deployment still
// proceeds; what changes is that no verification half is handed to the service,
// which then refuses to trust an assertion rather than trusting an unverifiable
// one.
func (c *environmentClient) GetGatewayAssertion(ctx context.Context, orgID, environment string) (GatewayAssertion, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(environment) == "" {
		return GatewayAssertion{}, fmt.Errorf("get gateway assertion: org and environment are both required")
	}
	resp, err := c.oc.GetEnvironmentWithResponse(ctx, orgID, environment)
	if err != nil {
		return GatewayAssertion{}, fmt.Errorf("failed to get environment %s/%s: %w", orgID, environment, err)
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return GatewayAssertion{}, handleErrorResponse(ctx, http.MethodGet, nsBase(orgID)+"/environments/"+environment, resp.StatusCode(), ErrorResponses{
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
	return gatewayAssertionFromAnnotations(orgID, environment, annotations), nil
}

// gatewayAssertionFromAnnotations is the parse, split out so it can be tested
// without a server.
//
// A certificate with no issuer is reported as ABSENT rather than partial: a
// service given a certificate but no issuer to pin would accept any assertion
// that key happens to verify, and the whole point of publishing the pair
// together is that it cannot.
func gatewayAssertionFromAnnotations(orgID, environment string, annotations map[string]string) GatewayAssertion {
	assertion := GatewayAssertion{
		OrgID:       orgID,
		Environment: environment,
		Issuer:      strings.TrimSpace(annotations[annGatewayAssertionIssuer]),
		Header:      strings.TrimSpace(annotations[annGatewayAssertionHeader]),
		Certificate: strings.TrimSpace(annotations[annGatewayAssertionCertificate]),
	}
	if assertion.Certificate == "" || assertion.Issuer == "" {
		return GatewayAssertion{OrgID: orgID, Environment: environment}
	}
	return assertion
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
