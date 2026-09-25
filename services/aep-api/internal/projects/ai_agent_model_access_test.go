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

package projects

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	ocmocks "github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/spec"
	"github.com/wso2/aep/aep-api/internal/spec/artifactstest"
)

// --- SyncProjectModelAccess (the builds-green sweep) --------------------------

// fakeKeyResolver serves a canned org key triplet.
type fakeKeyResolver struct{ triplet organization.SecretRefTriplet }

func (f fakeKeyResolver) DefaultKeyRef(context.Context, string) (organization.SecretRefTriplet, error) {
	return f.triplet, nil
}

// fakeSecretRefClient accepts any SecretReference upsert. GetSecretReference
// reports not-found so the create branch runs.
type fakeSecretRefClient struct{}

func (fakeSecretRefClient) GetSecretReference(context.Context, string, string) (*secretmanagersvc.SecretReference, error) {
	return nil, secretmanagersvc.ErrNotFound
}
func (fakeSecretRefClient) CreateSecretReference(context.Context, string, secretmanagersvc.CreateSecretReferenceRequest) (*secretmanagersvc.SecretReference, error) {
	return &secretmanagersvc.SecretReference{}, nil
}
func (fakeSecretRefClient) UpdateSecretReference(context.Context, string, string, secretmanagersvc.CreateSecretReferenceRequest) (*secretmanagersvc.SecretReference, error) {
	return &secretmanagersvc.SecretReference{}, nil
}
func (fakeSecretRefClient) DeleteSecretReference(context.Context, string, string) error { return nil }

func modelAccessStore(files map[string]string) *spec.ArtifactStore {
	return spec.NewArtifactStore(&artifactstest.FakeArtifactService{
		ListDesignFilesFunc: func(context.Context, string, string) (map[string]string, error) {
			return files, nil
		},
	})
}

func agentDesignJSON(name string) string {
	return "{\n  \"name\": \"" + name + "\",\n  \"type\": \"" + spec.ComponentTypeAIAgent +
		"\",\n  \"description\": \"Agent.\",\n  \"dependencies\": []\n}\n"
}

// THE REGRESSION, restated for the deployment path. Model access is granted by
// component TYPE rather than declared as a dependency (ADR-0016), so OpenChoreo
// never resolves it while rendering a release — the platform must compose it
// into the binding write itself. These values ride the deploy stage's single
// ApplyReleaseBinding call (DeploymentService.envVarsWithModelAccess), which is
// the only moment a binding is guaranteed to exist. An earlier design wrote them
// from the pre-build EnsureComponent pass and every agent's FIRST deploy came up
// with no MODEL_* at all, 503ing on /healthz and 500ing on every chat.
func TestModelAccessEnvVars_ReturnsTheThreeModelVars(t *testing.T) {
	svc := NewComponentService(
		&ocmocks.ComponentClientMock{}, nil, modelAccessStore(nil), nil, nil,
		fakeKeyResolver{triplet: organization.SecretRefTriplet{
			Name: "anthropic-default", KVPath: "user-app-secrets/acme/anthropic", Property: "api-key",
		}},
		fakeSecretRefClient{},
	).(*componentService)

	got, err := svc.ModelAccessEnvVars(context.Background(), "acme", "checkout-agent")
	if err != nil {
		t.Fatalf("ModelAccessEnvVars: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("env var count = %d, want 3: %+v", len(got), got)
	}

	byKey := map[string]openchoreo.WorkflowEnvVarRef{}
	for _, v := range got {
		byKey[v.Key] = v
	}
	if v := byKey[modelEndpointEnvVar]; v.Value != modelEndpointDefault {
		t.Errorf("%s = %q, want %q", modelEndpointEnvVar, v.Value, modelEndpointDefault)
	}
	if v := byKey[modelNameEnvVar]; v.Value != modelNameDefault {
		t.Errorf("%s = %q, want %q", modelNameEnvVar, v.Value, modelNameDefault)
	}
	// The key itself is never a literal: it is a SecretKeyRef naming the
	// org-scoped SecretReference, which ESO materialises into the consuming
	// namespace. A literal here would put the org's Anthropic key in a CR.
	key := byKey[modelAPIKeyEnvVar]
	if key.Value != "" {
		t.Errorf("%s carries a literal value %q — it must be a SecretKeyRef", modelAPIKeyEnvVar, key.Value)
	}
	if key.ValueFrom == nil || key.ValueFrom.SecretKeyRef == nil {
		t.Fatalf("%s has no SecretKeyRef: %+v", modelAPIKeyEnvVar, key)
	}
	if got, want := key.ValueFrom.SecretKeyRef.Name, modelAccessSecretRefName; got != want {
		t.Errorf("SecretKeyRef.Name = %q, want %q", got, want)
	}
	if got, want := key.ValueFrom.SecretKeyRef.Key, "api-key"; got != want {
		t.Errorf("SecretKeyRef.Key = %q, want %q (the triplet's property)", got, want)
	}
}

// An org with no connected Anthropic key is expected, not exceptional: a
// brand-new org before its first Settings visit. Yielding (nil, nil) lets the
// agent deploy and report 503 from /healthz — a state an operator can see and
// fix — where an error would fail the whole deploy and leave no agent at all.
func TestModelAccessEnvVars_NoConnectedKeyIsNotAnError(t *testing.T) {
	svc := NewComponentService(
		&ocmocks.ComponentClientMock{}, nil, modelAccessStore(nil), nil, nil,
		noKeyResolver{}, fakeSecretRefClient{},
	).(*componentService)

	got, err := svc.ModelAccessEnvVars(context.Background(), "acme", "checkout-agent")
	if err != nil {
		t.Fatalf("an org with no key must not error, got: %v", err)
	}
	if got != nil {
		t.Errorf("env vars = %+v, want nil", got)
	}
}

// noKeyResolver reports the org has no connected default key.
type noKeyResolver struct{}

func (noKeyResolver) DefaultKeyRef(context.Context, string) (organization.SecretRefTriplet, error) {
	return organization.SecretRefTriplet{}, &organization.NotFoundError{}
}

// The SecretReference must be authored in the ReleaseBinding's namespace —
// ocOrgID, passed through unmodified — never a namespace derived from the vault
// path. secretsprovider.SecretLocation.CPNamespace states the rule, and this
// file got it wrong twice in opposite directions: `wc-default-…` (did not
// exist, create 500'd) and then the vault key's segment 1, `wc-019f40d1-…`
// (existed, create SUCCEEDED, and the ReleaseBinding in `default` could not see
// it — OpenChoreo failed the whole render with `SecretReference
// "ai-agent-model-access" not found` and the agent never got MODEL_*).
//
// The second failure is why this test asserts on the namespace rather than on
// the call succeeding: a create that succeeds proves the namespace EXISTS, not
// that the consumer can see it.
func TestUpsertModelAccessSecretReference_UsesTheReleaseBindingNamespace(t *testing.T) {
	var gotNS []string
	sr := &namespaceCapturingSecretRefClient{seen: &gotNS}
	files := map[string]string{
		spec.DesignRootFile:                  "# Overview\n",
		"components/hotel-agent/design.json": agentDesignJSON("hotel-agent"),
	}
	svc := NewComponentService(&ocmocks.ComponentClientMock{}, nil, modelAccessStore(files), nil, nil,
		fakeKeyResolver{triplet: organization.SecretRefTriplet{
			// The vault path's org segment is DELIBERATELY different from the
			// control-plane namespace here — that difference is the bug.
			KVPath:   "user-app-secrets/wc-019f40d1-b04b186b/anthropic-secrets",
			Property: "api-key",
		}}, sr)

	if _, err := svc.(*componentService).ModelAccessEnvVars(context.Background(), "default", "checkout-agent"); err != nil {
		t.Fatalf("ModelAccessEnvVars: %v", err)
	}
	for _, ns := range gotNS {
		if ns != "default" {
			t.Fatalf("SecretReference authored in %q, want the ReleaseBinding's namespace %q — a vault-path-derived namespace makes the CR invisible to its consumer", ns, "default")
		}
	}
	if len(gotNS) == 0 {
		t.Fatal("no SecretReference upsert attempted")
	}
}

// namespaceCapturingSecretRefClient records the namespace of every upsert.
type namespaceCapturingSecretRefClient struct{ seen *[]string }

func (c *namespaceCapturingSecretRefClient) GetSecretReference(_ context.Context, ns, _ string) (*secretmanagersvc.SecretReference, error) {
	*c.seen = append(*c.seen, ns)
	return nil, secretmanagersvc.ErrNotFound
}
func (c *namespaceCapturingSecretRefClient) CreateSecretReference(_ context.Context, ns string, _ secretmanagersvc.CreateSecretReferenceRequest) (*secretmanagersvc.SecretReference, error) {
	*c.seen = append(*c.seen, ns)
	return &secretmanagersvc.SecretReference{}, nil
}
func (c *namespaceCapturingSecretRefClient) UpdateSecretReference(_ context.Context, ns, _ string, _ secretmanagersvc.CreateSecretReferenceRequest) (*secretmanagersvc.SecretReference, error) {
	*c.seen = append(*c.seen, ns)
	return &secretmanagersvc.SecretReference{}, nil
}
func (c *namespaceCapturingSecretRefClient) DeleteSecretReference(context.Context, string, string) error {
	return nil
}

// --- Agent Manager governed model access --------------------------------------

type fakeAIGatewayBindings struct {
	err error
}

func (f fakeAIGatewayBindings) GetAIGatewayBinding(context.Context, string, string) (openchoreo.AIGatewayBinding, error) {
	if f.err != nil {
		return openchoreo.AIGatewayBinding{}, f.err
	}
	return openchoreo.AIGatewayBinding{
		OrgID: "acme", Environment: openchoreo.DevEnvironmentName,
		Endpoint:  "http://ai-gateway.amp.localhost:8084",
		AdminURL:  "http://api.amp.localhost:8080/api/v1",
		GatewayID: "gw-uuid",
		// A DIFFERENT host and port from Endpoint, deliberately: AMP serves
		// /otel from the API platform gateway, and a test whose two addresses
		// coincide cannot tell a correct composition from one that reused the
		// model gateway.
		OTelEndpoint: "http://api-platform-acme-development-gw-gateway-gateway-runtime.acme-development:22893/otel",
	}, nil
}

// noOTelBindings is an environment provisioned before the otel-endpoint
// annotation existed.
type noOTelBindings struct{ fakeAIGatewayBindings }

func (noOTelBindings) GetAIGatewayBinding(context.Context, string, string) (openchoreo.AIGatewayBinding, error) {
	return openchoreo.AIGatewayBinding{
		OrgID: "acme", Environment: openchoreo.DevEnvironmentName,
		Endpoint:  "http://ai-gateway.amp.localhost:8084",
		AdminURL:  "http://api.amp.localhost:8080/api/v1",
		GatewayID: "gw-uuid",
	}, nil
}

// presentSecretRefClient reports that every AMP SecretReference IS stored.
//
// askedFor records every lookup, not the last: composition asks after the model
// key and then after the tracing token, and a single slot would silently hold
// whichever came second.
type presentSecretRefClient struct {
	fakeSecretRefClient
	askedFor []string
}

func (p *presentSecretRefClient) GetSecretReference(_ context.Context, _ string, name string) (*secretmanagersvc.SecretReference, error) {
	p.askedFor = append(p.askedFor, name)
	return &secretmanagersvc.SecretReference{}, nil
}

// A governed agent gets the gateway and its OWN key — never the org's Anthropic
// key. This is the composition half of the whole feature.
func TestModelAccessEnvVars_PrefersTheAMPBinding(t *testing.T) {
	sr := &presentSecretRefClient{}
	svc := NewComponentService(
		&ocmocks.ComponentClientMock{}, nil, modelAccessStore(nil), nil, nil,
		fakeKeyResolver{triplet: organization.SecretRefTriplet{
			Name: "anthropic-default", KVPath: "user-app-secrets/acme/anthropic", Property: "api-key",
		}},
		sr,
	).(*componentService)
	svc.SetAIGatewayBindings(fakeAIGatewayBindings{})

	got, err := svc.ModelAccessEnvVars(context.Background(), "acme", "checkout-agent")
	if err != nil {
		t.Fatalf("ModelAccessEnvVars: %v", err)
	}
	byKey := map[string]openchoreo.WorkflowEnvVarRef{}
	for _, v := range got {
		byKey[v.Key] = v
	}
	// MODEL_ENDPOINT comes from the SAME secret as the key, not from a literal:
	// Agent Manager generates a proxy path per agent, so the address is read
	// back rather than derived, and the two are stored together because the key
	// authenticates against that proxy alone.
	endpoint := byKey[modelEndpointEnvVar].ValueFrom
	if endpoint == nil || endpoint.SecretKeyRef == nil {
		t.Fatalf("MODEL_ENDPOINT = %+v, want a secretKeyRef to the agent's AMP secret", byKey[modelEndpointEnvVar])
	}
	if endpoint.SecretKeyRef.Key != organization.AMPModelURLKey {
		t.Errorf("MODEL_ENDPOINT key = %q, want %q", endpoint.SecretKeyRef.Key, organization.AMPModelURLKey)
	}
	if endpoint.SecretKeyRef.Name != organization.AMPModelKeySecretRefName("checkout-agent", openchoreo.DevEnvironmentName) {
		t.Errorf("MODEL_ENDPOINT refs %q, want the agent's own AMP secret", endpoint.SecretKeyRef.Name)
	}
	ref := byKey[modelAPIKeyEnvVar].ValueFrom.SecretKeyRef
	want := organization.AMPModelKeySecretRefName("checkout-agent", openchoreo.DevEnvironmentName)
	if ref.Name != want {
		t.Errorf("MODEL_API_KEY refs %q, want the agent's own AMP key %q", ref.Name, want)
	}
	if !slices.Contains(sr.askedFor, want) {
		t.Errorf("looked up SecretReferences %q, want %q among them — the writer and this reader must agree on one spelling", sr.askedFor, want)
	}
	// The temporary header override (see modelAPIKeyHeaderEnvVar). Without it
	// the agent sends `x-api-key` and Agent Manager's proxy rejects the turn.
	if got := byKey[modelAPIKeyHeaderEnvVar].Value; got != ampModelAPIKeyHeader {
		t.Errorf("MODEL_API_KEY_HEADER = %q, want %q", got, ampModelAPIKeyHeader)
	}
}

// THE REGRESSION GUARD for every environment that has no AI gateway. This path
// must stay byte-for-byte what it was before Agent Manager existed.
func TestModelAccessEnvVars_FallsBackToTheOrgKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		svc  func() *componentService
	}{
		{
			name: "no binding reader wired at all",
			svc: func() *componentService {
				return newModelAccessSvc(fakeSecretRefClient{})
			},
		},
		{
			name: "environment has no AI gateway binding",
			svc: func() *componentService {
				s := newModelAccessSvc(fakeSecretRefClient{})
				s.SetAIGatewayBindings(fakeAIGatewayBindings{err: openchoreo.ErrNoAIGatewayBinding})
				return s
			},
		},
		{
			name: "governed environment, but this agent has no stored key",
			svc: func() *componentService {
				// fakeSecretRefClient answers ErrNotFound for every lookup.
				s := newModelAccessSvc(fakeSecretRefClient{})
				s.SetAIGatewayBindings(fakeAIGatewayBindings{})
				return s
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.svc().ModelAccessEnvVars(context.Background(), "acme", "checkout-agent")
			if err != nil {
				t.Fatalf("ModelAccessEnvVars: %v", err)
			}
			byKey := map[string]openchoreo.WorkflowEnvVarRef{}
			for _, v := range got {
				byKey[v.Key] = v
			}
			if byKey[modelEndpointEnvVar].Value != modelEndpointDefault {
				t.Errorf("MODEL_ENDPOINT = %q, want the direct Anthropic endpoint", byKey[modelEndpointEnvVar].Value)
			}
			ref := byKey[modelAPIKeyEnvVar].ValueFrom.SecretKeyRef
			if ref.Name != modelAccessSecretRefName {
				t.Errorf("MODEL_API_KEY refs %q, want the org-scoped SecretReference", ref.Name)
			}
			// The ungoverned path must NOT set the header override: the agent
			// talks to Anthropic directly, where `x-api-key` is correct.
			if _, ok := byKey[modelAPIKeyHeaderEnvVar]; ok {
				t.Error("set MODEL_API_KEY_HEADER on the direct path; only the governed path overrides it")
			}
		})
	}
}

func newModelAccessSvc(sr secretmanagersvc.OpenChoreoSecretReferenceClient) *componentService {
	return NewComponentService(
		&ocmocks.ComponentClientMock{}, nil, modelAccessStore(nil), nil, nil,
		fakeKeyResolver{triplet: organization.SecretRefTriplet{
			Name: "anthropic-default", KVPath: "user-app-secrets/acme/anthropic", Property: "api-key",
		}},
		sr,
	).(*componentService)
}

// --- Agent Manager tracing ----------------------------------------------------

// selectiveSecretRefClient reports only the named SecretReferences as present,
// which is what separates a governed agent that has a tracing token from one
// that does not.
type selectiveSecretRefClient struct {
	fakeSecretRefClient
	present map[string]bool
	asked   []string
}

func (s *selectiveSecretRefClient) GetSecretReference(_ context.Context, _ string, name string) (*secretmanagersvc.SecretReference, error) {
	s.asked = append(s.asked, name)
	if s.present[name] {
		return &secretmanagersvc.SecretReference{}, nil
	}
	return nil, secretmanagersvc.ErrNotFound
}

func tracingSvc(sr secretmanagersvc.OpenChoreoSecretReferenceClient) *componentService {
	svc := NewComponentService(
		&ocmocks.ComponentClientMock{}, nil, modelAccessStore(nil), nil, nil,
		fakeKeyResolver{triplet: organization.SecretRefTriplet{
			Name: "anthropic-default", KVPath: "user-app-secrets/acme/anthropic", Property: "api-key",
		}},
		sr,
	).(*componentService)
	svc.SetAIGatewayBindings(fakeAIGatewayBindings{})
	return svc
}

// A governed agent whose tracing token was stored exports spans to its
// environment's gateway, authenticated with a credential of its own.
func TestModelAccessEnvVars_AddsTracingWhenTheTokenIsStored(t *testing.T) {
	modelRef := organization.AMPModelKeySecretRefName("checkout-agent", openchoreo.DevEnvironmentName)
	tracingRef := organization.AMPTracingTokenSecretRefName("checkout-agent", openchoreo.DevEnvironmentName)
	sr := &selectiveSecretRefClient{present: map[string]bool{modelRef: true, tracingRef: true}}

	got, err := tracingSvc(sr).ModelAccessEnvVars(context.Background(), "acme", "checkout-agent")
	if err != nil {
		t.Fatalf("ModelAccessEnvVars: %v", err)
	}
	byKey := map[string]openchoreo.WorkflowEnvVarRef{}
	for _, v := range got {
		byKey[v.Key] = v
	}

	// A LITERAL, unlike MODEL_ENDPOINT — the OTLP address is not a secret and
	// the binding already carries it.
	//
	// IT IS THE BINDING'S OWN VALUE, not the model gateway with a route glued
	// on. AMP serves /otel from the API platform gateway; composing it from
	// binding.Endpoint sends spans to the AI gateway, which answers 404. That
	// shipped once and was caught only against a live cluster.
	want := "http://api-platform-acme-development-gw-gateway-gateway-runtime.acme-development:22893/otel"
	if got := byKey[ampOTelEndpointEnvVar].Value; got != want {
		t.Errorf("AMP_OTEL_ENDPOINT = %q, want the binding's OTLP endpoint %q", got, want)
	}
	if strings.Contains(byKey[ampOTelEndpointEnvVar].Value, "ai-gateway") {
		t.Error("AMP_OTEL_ENDPOINT points at the MODEL gateway — spans would 404")
	}
	ref := byKey[ampAgentAPIKeyEnvVar].ValueFrom
	if ref == nil || ref.SecretKeyRef == nil {
		t.Fatalf("AMP_AGENT_API_KEY = %+v, want a secretKeyRef", byKey[ampAgentAPIKeyEnvVar])
	}
	if ref.SecretKeyRef.Name != tracingRef {
		t.Errorf("AMP_AGENT_API_KEY refs %q, want the agent's tracing secret %q", ref.SecretKeyRef.Name, tracingRef)
	}
	if ref.SecretKeyRef.Key != organization.AMPTracingTokenKey {
		t.Errorf("AMP_AGENT_API_KEY key = %q, want %q", ref.SecretKeyRef.Key, organization.AMPTracingTokenKey)
	}
	// Without this every agent's spans arrive as `unknown_service:node` and a
	// trace view cannot tell two agents apart.
	if got := byKey[otelServiceNameEnvVar].Value; got != "checkout-agent" {
		t.Errorf("OTEL_SERVICE_NAME = %q, want the component name", got)
	}
	// Prompts and completions are the agent's most sensitive traffic. Whether
	// they are exported is a deliberate platform decision, not a default
	// inherited from whatever the SDK ships with.
	if got := byKey[traceloopTraceContentEnvVar].Value; got != "false" {
		t.Errorf("TRACELOOP_TRACE_CONTENT = %q, want false", got)
	}
	// Tracing is additive: the agent still reaches its model exactly as before.
	if byKey[modelAPIKeyEnvVar].ValueFrom == nil {
		t.Error("MODEL_API_KEY missing — tracing must not displace model access")
	}
}

// An environment provisioned before the otel-endpoint annotation existed still
// governs model traffic correctly and simply runs untraced. Composing a
// tracing block with no endpoint would point the agent at a relative URL.
func TestModelAccessEnvVars_OmitsTracingWithoutAnOTLPEndpoint(t *testing.T) {
	modelRef := organization.AMPModelKeySecretRefName("checkout-agent", openchoreo.DevEnvironmentName)
	tracingRef := organization.AMPTracingTokenSecretRefName("checkout-agent", openchoreo.DevEnvironmentName)
	sr := &selectiveSecretRefClient{present: map[string]bool{modelRef: true, tracingRef: true}}
	svc := tracingSvc(sr)
	svc.SetAIGatewayBindings(noOTelBindings{})

	got, err := svc.ModelAccessEnvVars(context.Background(), "acme", "checkout-agent")
	if err != nil {
		t.Fatalf("ModelAccessEnvVars: %v", err)
	}
	for _, v := range got {
		switch v.Key {
		case ampOTelEndpointEnvVar, ampAgentAPIKeyEnvVar, traceloopTraceContentEnvVar, otelServiceNameEnvVar:
			t.Errorf("%s composed with no OTLP endpoint recorded", v.Key)
		}
	}
	byKey := map[string]openchoreo.WorkflowEnvVarRef{}
	for _, v := range got {
		byKey[v.Key] = v
	}
	if byKey[modelAPIKeyEnvVar].ValueFrom == nil {
		t.Error("MODEL_API_KEY missing — model access must survive a missing OTLP endpoint")
	}
}

// THE CRASH-LOOP GUARD. Minting a tracing token is allowed to fail, so an agent
// may be governed and have no token. OpenChoreo's secretKeyRef carries no
// `optional` flag: naming a SecretReference that was never written does not
// cost the agent its traces, it stops the container from starting. A governed
// agent with no tracing token must therefore compose NO tracing vars at all.
func TestModelAccessEnvVars_OmitsTracingWhenNoTokenIsStored(t *testing.T) {
	modelRef := organization.AMPModelKeySecretRefName("checkout-agent", openchoreo.DevEnvironmentName)
	sr := &selectiveSecretRefClient{present: map[string]bool{modelRef: true}}

	got, err := tracingSvc(sr).ModelAccessEnvVars(context.Background(), "acme", "checkout-agent")
	if err != nil {
		t.Fatalf("ModelAccessEnvVars: %v", err)
	}
	for _, v := range got {
		switch v.Key {
		case ampOTelEndpointEnvVar, ampAgentAPIKeyEnvVar, traceloopTraceContentEnvVar, otelServiceNameEnvVar:
			t.Errorf("%s composed with no tracing token stored — the pod would fail to start", v.Key)
		}
	}
	// Still fully governed for model access.
	byKey := map[string]openchoreo.WorkflowEnvVarRef{}
	for _, v := range got {
		byKey[v.Key] = v
	}
	if byKey[modelAPIKeyEnvVar].ValueFrom == nil {
		t.Error("MODEL_API_KEY missing — a missing tracing token must not disturb model access")
	}
}
