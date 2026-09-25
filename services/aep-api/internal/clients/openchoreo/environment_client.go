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
// List returns this package's own wire-mapping EnvironmentInfo; the
// environmentLister adapter in internal/app/tasks_adapters.go converts those
// rows to provisioning's domain type to satisfy provisioning.EnvironmentLister,
// so neither package depends on the other's type. GetThunderBinding is how
// aep-api finds the environment's own identity provider.
type EnvironmentClient interface {
	List(ctx context.Context, orgID string) ([]EnvironmentInfo, error)
	GetThunderBinding(ctx context.Context, orgID, environment string) (ThunderBinding, error)
	// GetAIGatewayBinding is how aep-api finds the environment's AI gateway —
	// the LLM proxy an Agent-Manager-governed agent's model traffic flows
	// through. An environment without one is not an error; see
	// ErrNoAIGatewayBinding.
	GetAIGatewayBinding(ctx context.Context, orgID, environment string) (AIGatewayBinding, error)
	GetGatewayAssertion(ctx context.Context, orgID, environment string) (GatewayAssertion, error)
}

// EnvironmentInfo is one OpenChoreo Environment as the BFF reads it: name,
// the openchoreo.dev/display-name annotation (empty when unset — the
// provisioning service fills the titlecased fallback, not this client),
// spec.isProduction, and the aep.wso2.com/validation annotation verbatim
// (empty or unrecognised is normalized to "off" by the provisioning service,
// not here — this type is a plain read, not a policy decision).
//
// provisioning has its own EnvironmentInfo; this one is the wire read, and
// environmentLister in internal/app/tasks_adapters.go converts between them,
// which keeps the two types — and the two packages — independent.
type EnvironmentInfo struct {
	Name         string
	DisplayName  string
	IsProduction bool
	Validation   string
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
	// OTelEndpoint is where this environment ingests traces — the OTLP base an
	// agent's exporter appends /v1/traces to.
	//
	// A DIFFERENT GATEWAY from Endpoint above, and that is the whole reason it
	// is carried rather than derived. Model traffic goes to the AI gateway;
	// AMP's trace route is served by the API platform gateway. Empty when the
	// environment predates the annotation — tracing is then simply not
	// composed, which is the safe direction.
	OTelEndpoint string
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

// List reads the org's Environments and maps the display-name and validation
// annotations and spec.isProduction onto each row. It does not apply the
// display-name fallback or the absent/unrecognised-is-off validation default
// — those are policy, applied once in
// provisioning.Service.ListOrgEnvironments, not here.
func (c *environmentClient) List(ctx context.Context, orgID string) ([]EnvironmentInfo, error) {
	if strings.TrimSpace(orgID) == "" {
		return []EnvironmentInfo{}, nil
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
	infos := make([]EnvironmentInfo, 0, len(resp.JSON200.Items))
	for _, item := range resp.JSON200.Items {
		info := EnvironmentInfo{
			Name:        item.Metadata.Name,
			DisplayName: annotation(item.Metadata.Annotations, AnnotationKeyDisplayName),
			Validation:  annotation(item.Metadata.Annotations, AnnotationKeyValidation),
		}
		if item.Spec != nil && item.Spec.IsProduction != nil {
			info.IsProduction = *item.Spec.IsProduction
		}
		infos = append(infos, info)
	}
	return infos, nil
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
		return GatewayAssertion{}, handleErrorResponse(resp.StatusCode(), ErrorResponses{
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

// AI gateway binding annotations, written onto the Environment by
// deployments/scripts/setup-environment-aigateway.sh. Same shape and the same
// reasoning as the Thunder binding above: aep-api runs outside the cluster, so
// the Environment is the one projection of the record it can read.
const (
	annAIGatewayEndpoint   = "aep.wso2.com/aigateway-endpoint"
	annAIGatewayInternal   = "aep.wso2.com/aigateway-internal-endpoint"
	annAIGatewayAdminURL   = "aep.wso2.com/aigateway-admin-url"
	annAIGatewayGateway    = "aep.wso2.com/aigateway-gateway"
	annAIGatewaySecretPath = "aep.wso2.com/aigateway-secret-path"
	annAIGatewayBinding    = "aep.wso2.com/aigateway-binding"
	// annOTelEndpoint is the environment's OTLP trace-ingest base, written by
	// setup-environment-gateway.sh. It is NOT on the AI gateway: AMP serves
	// /otel from the API PLATFORM gateway, a different Service on a different
	// port in the same namespace, so it cannot be derived from the AI gateway
	// endpoint. Posting spans to the AI gateway answers 404.
	annOTelEndpoint = "aep.wso2.com/otel-endpoint"
)

// ErrNoAIGatewayBinding is the answer for an environment with no AI gateway.
//
// Its own error because the recovery is specific and a caller cannot guess it:
// run setup-environment-aigateway.sh. It is ALSO not a failure — an environment
// that was never provisioned for Agent Manager deploys agents the way it did
// before, on the org's own Anthropic key. Callers distinguish this from a
// transport error precisely so they can take that path.
var ErrNoAIGatewayBinding = errors.New("openchoreo: environment has no AI gateway binding")

// AIGatewayBinding is one environment's AI gateway, as the Environment records
// it.
type AIGatewayBinding struct {
	OrgID       string
	Environment string
	// Endpoint is the gateway's PUBLIC address — the one a browser or a host
	// process reaches it at.
	Endpoint string
	// InternalEndpoint is the same gateway's in-cluster Service address, and it
	// is the one an AGENT uses: an agent runs in a pod, the public vhost is
	// published on no host port, and the gateway's router matches on "*" so it
	// serves whichever Host arrives. Empty falls back to Endpoint, which keeps
	// a binding written before this field existed working.
	InternalEndpoint string
	// AdminURL is Agent Manager's control API, including its /api/v1 base. It is
	// per-binding rather than configuration because two environments may be
	// governed by two different Agent Managers.
	AdminURL string
	// GatewayID is the AMP gateway UUID a provider is attached to.
	GatewayID string
	// SecretPath is where AEP's own AMP credential lives in the secret store.
	SecretPath string
	// Name is the binding record's own name, for logs and diagnostics.
	Name string
	// OTelEndpoint is where this environment ingests traces — the OTLP base an
	// agent's exporter appends /v1/traces to.
	//
	// A DIFFERENT GATEWAY from Endpoint above, and that is the whole reason it
	// is carried rather than derived. Model traffic goes to the AI gateway;
	// AMP's trace route is served by the API platform gateway. Empty when the
	// environment predates the annotation — tracing is then simply not
	// composed, which is the safe direction.
	OTelEndpoint string
}

// GetAIGatewayBinding reads the environment's AI gateway binding off its
// annotations. Mirrors GetThunderBinding exactly, including the choice to
// report a partial record as absent.
func (c *environmentClient) GetAIGatewayBinding(ctx context.Context, orgID, environment string) (AIGatewayBinding, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(environment) == "" {
		return AIGatewayBinding{}, fmt.Errorf("get ai gateway binding: org and environment are both required")
	}
	resp, err := c.oc.GetEnvironmentWithResponse(ctx, orgID, environment)
	if err != nil {
		return AIGatewayBinding{}, fmt.Errorf("failed to get environment %s/%s: %w", orgID, environment, err)
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return AIGatewayBinding{}, handleErrorResponse(resp.StatusCode(), ErrorResponses{
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
	return aiGatewayBindingFromAnnotations(orgID, environment, annotations)
}

// aiGatewayBindingFromAnnotations is the parse, split out so it can be tested
// without a server.
//
// OTelEndpoint is deliberately NOT required: an environment set up before the
// annotation existed still governs model traffic correctly, and the agent simply
// runs untraced. Requiring it would turn a missing graph into a broken deploy.
//
// Endpoint, AdminURL and GatewayID are each required: without the endpoint an
// agent has nowhere to send model traffic, without the admin URL nothing can be
// registered, and without the gateway id a provider cannot be attached to
// anything. A record missing any of them is reported ABSENT rather than half
// used — the same rule thunderBindingFromAnnotations applies, for the same
// reason: the failure would otherwise surface far from its cause.
func aiGatewayBindingFromAnnotations(orgID, environment string, annotations map[string]string) (AIGatewayBinding, error) {
	binding := AIGatewayBinding{
		OrgID:            orgID,
		Environment:      environment,
		Endpoint:         strings.TrimSpace(annotations[annAIGatewayEndpoint]),
		InternalEndpoint: strings.TrimSpace(annotations[annAIGatewayInternal]),
		AdminURL:         strings.TrimSpace(annotations[annAIGatewayAdminURL]),
		GatewayID:        strings.TrimSpace(annotations[annAIGatewayGateway]),
		SecretPath:       strings.TrimSpace(annotations[annAIGatewaySecretPath]),
		Name:             strings.TrimSpace(annotations[annAIGatewayBinding]),
		OTelEndpoint:     strings.TrimSpace(annotations[annOTelEndpoint]),
	}
	if binding.Endpoint == "" || binding.AdminURL == "" || binding.GatewayID == "" {
		return AIGatewayBinding{}, ErrNoAIGatewayBinding
	}
	if binding.InternalEndpoint == "" {
		binding.InternalEndpoint = binding.Endpoint
	}
	return binding, nil
}
