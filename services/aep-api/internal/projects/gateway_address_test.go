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
	"encoding/json"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// The gateway address is the one value that decides whether a SPA's browser
// traffic is authenticated, so its exact shape is pinned here rather than left
// to a deploy-time surprise: a wrong context prefix does not degrade, it 404s
// every call at the gateway.

// designFile parses a whole design fixture through the real codec.
func designFile(t *testing.T, body string) *spec.DesignFile {
	t.Helper()
	var d spec.DesignFile
	if err := json.Unmarshal([]byte(body), &d); err != nil {
		t.Fatalf("fixture parse: %v", err)
	}
	return &d
}

// TestAPIGatewayContextPath pins the prefix byte-for-byte against the value the
// api-configuration ClusterTrait renders on a real cluster. This string is the
// contract between the trait's RestApi `context` and every consumer proxying to
// the gateway; if the trait template changes, this test is what should fail.
func TestAPIGatewayContextPath(t *testing.T) {
	t.Parallel()

	got := APIGatewayContextPath("default", "default", "track-each-hire41-onboarding-api", "http")
	const want = "/default-default-track-each-hire41-onboarding-api-http"
	if got != want {
		t.Fatalf("context path\n got %q\nwant %q", got, want)
	}

	// An endpoint the design left unnamed defaults to "http", the same default
	// the workload and the trait both fall back to.
	if got := APIGatewayContextPath("default", "default", "proj-api", ""); got != "/default-default-proj-api-http" {
		t.Fatalf("default endpoint name: got %q", got)
	}
}

// TestAPIGatewayHost pins the derivation against the three places that must
// agree with it: the api-configuration ClusterTrait's Backend host (and its
// RestApi label, the same <org>-<env> pair), the gateway extension chart's
// APIGateway / runtime-Service naming, and setup-environment-gateway.sh.
//
// Two environments of one org must NOT collapse onto one address: that is the
// whole point of a per-environment gateway, and the failure if they did would be
// an environment's traffic authenticated against a sibling environment's
// identity tier.
func TestAPIGatewayHost(t *testing.T) {
	t.Parallel()

	const want = "api-platform-default-default-gw-gateway-gateway-runtime." +
		"default-default.svc.cluster.local:22893"
	if got := APIGatewayHost("default", "default"); got != want {
		t.Fatalf("derived host\n got %q\nwant %q", got, want)
	}
	if a, b := APIGatewayHost("acme", "default"), APIGatewayHost("acme", "staging"); a == b {
		t.Fatalf("two environments must not share a gateway address, both %q", a)
	}

	// A missing half yields nothing, not a name with a hole in it: no address
	// leaves the consumer on the direct lane, a wrong one 502s every call.
	for _, c := range []struct{ ns, env string }{{"", "default"}, {"default", ""}, {"", ""}} {
		if got := APIGatewayHost(c.ns, c.env); got != "" {
			t.Errorf("APIGatewayHost(%q, %q) = %q, want empty", c.ns, c.env, got)
		}
	}
}

// TestAPIGatewayHostIsFullyQualified guards a failure that only shows up in a
// cluster. The consumer of this address is nginx, whose `resolver` queries the
// name verbatim and does NOT apply /etc/resolv.conf search domains — so the
// `<service>.<namespace>` short form the api-configuration trait uses resolves
// for getaddrinfo inside the very same pod and is NXDOMAIN for nginx. The
// symptom is a 502 on every /api call, which reads like the API being down.
func TestAPIGatewayHostIsFullyQualified(t *testing.T) {
	t.Parallel()

	derived := APIGatewayHost("default", "default")
	host, port, found := strings.Cut(derived, ":")
	if !found || port == "" {
		t.Fatalf("gateway host must carry a port: %q", derived)
	}
	if !strings.HasSuffix(host, ".svc.cluster.local") {
		t.Fatalf("gateway host must be fully qualified for nginx's resolver, got %q", host)
	}
}

func TestProtectedSiblingsOf(t *testing.T) {
	t.Parallel()

	// A SPA depending on one protected service, one unprotected service, and a
	// platform resource — only the protected sibling may be addressed via the
	// gateway.
	design := designFile(t, `{"components":[
      {"name":"web","type":"web-application","dependencies":[
        {"kind":"component","name":"api","wiring":{"endpoint":{"component":"proj-api","name":"http","visibility":"project","envBindings":{"address":"API_URL"}}}},
        {"kind":"component","name":"open","wiring":{"endpoint":{"component":"proj-open","name":"http","visibility":"project","envBindings":{"address":"OPEN_URL"}}}},
        {"kind":"platform-resource","name":"user-auth","resourceType":"thunder-app"}
      ]},
      {"name":"api","type":"service","dependencies":[],"exposesAPI":{"auth":"end-user-required"}},
      {"name":"open","type":"service","dependencies":[]}
    ]}`)

	webapp := findDesignComponent(design, "web")
	if webapp == nil {
		t.Fatal("fixture: no web component")
	}
	got := ProtectedSiblingsOf(design, *webapp)
	if len(got) != 1 {
		t.Fatalf("want exactly the protected sibling, got %+v", got)
	}
	if got[0].DepName != "api" || got[0].ComponentName != "proj-api" || got[0].EndpointName != "http" {
		t.Fatalf("resolved sibling wrong: %+v", got[0])
	}
}

// TestProtectedSiblingsOfSkipsUnwiredDependency covers the ordering window: a
// dependency whose endpoint wiring has not been stamped yet cannot be addressed,
// and guessing the scoped name would produce a prefix that 404s. Skipping leaves
// the consumer on the direct lane until the next converge.
func TestProtectedSiblingsOfSkipsUnwiredDependency(t *testing.T) {
	t.Parallel()

	design := designFile(t, `{"components":[
      {"name":"web","type":"web-application","dependencies":[{"kind":"component","name":"api"}]},
      {"name":"api","type":"service","dependencies":[],"exposesAPI":{"auth":"end-user-required"}}
    ]}`)
	webapp := findDesignComponent(design, "web")
	if got := ProtectedSiblingsOf(design, *webapp); got != nil {
		t.Fatalf("unwired dependency must not be addressed, got %+v", got)
	}
}

func TestGatewayEnvVars(t *testing.T) {
	t.Parallel()

	sibs := []ProtectedSibling{{DepName: "onboarding-api", ComponentName: "track-each-hire41-onboarding-api", EndpointName: "http"}}

	// No override: the address is DERIVED from (namespace, environment), so the
	// same component in two environments is published two different gateways.
	got := GatewayEnvVars("", "default", "default", sibs)
	if len(got) != 1 {
		t.Fatalf("want one env var, got %+v", got)
	}
	if got[0].Key != "ONBOARDING_API_GATEWAY_URL" {
		t.Fatalf("env key: got %q", got[0].Key)
	}
	wantVal := "http://" + APIGatewayHost("default", "default") +
		"/default-default-track-each-hire41-onboarding-api-http"
	if got[0].Value != wantVal {
		t.Fatalf("env value\n got %q\nwant %q", got[0].Value, wantVal)
	}

	// The override WINS over the derivation — the escape hatch for a data plane
	// that names its gateway differently.
	pinned := GatewayEnvVars("gw.example:9000", "default", "default", sibs)
	if len(pinned) != 1 {
		t.Fatalf("want one env var, got %+v", pinned)
	}
	const wantPinned = "http://gw.example:9000/default-default-track-each-hire41-onboarding-api-http"
	if pinned[0].Value != wantPinned {
		t.Fatalf("override must win\n got %q\nwant %q", pinned[0].Value, wantPinned)
	}

	// Every missing input independently yields nothing rather than a malformed
	// address: a half-formed gateway URL would send the SPA somewhere that 404s,
	// which is harder to diagnose than staying on the direct lane. Without an
	// override a missing namespace or environment leaves the derivation with no
	// answer, which is the same nil.
	for _, c := range []struct {
		name              string
		override, env, ns string
		sibs              []ProtectedSibling
	}{
		{"no environment, derived", "", "", "default", sibs},
		{"no namespace, derived", "", "default", "", sibs},
		{"no environment, overridden", "gw.example:9000", "", "default", sibs},
		{"no namespace, overridden", "gw.example:9000", "default", "", sibs},
		{"no siblings", "", "default", "default", nil},
		{"sibling missing component", "", "default", "default", []ProtectedSibling{{DepName: "x"}}},
	} {
		if got := GatewayEnvVars(c.override, c.env, c.ns, c.sibs); got != nil {
			t.Errorf("%s: want nil, got %+v", c.name, got)
		}
	}
}

func TestMergeEnvVars(t *testing.T) {
	t.Parallel()

	user := []openchoreo.WorkflowEnvVarRef{
		{Key: "FEATURE_FLAG", Value: "on"},
		{Key: "ONBOARDING_API_GATEWAY_URL", Value: "http://stale"},
	}
	platform := []openchoreo.WorkflowEnvVarRef{{Key: "ONBOARDING_API_GATEWAY_URL", Value: "http://fresh"}}

	got := mergeEnvVars(user, platform)
	if len(got) != 2 {
		t.Fatalf("want the user var plus one platform var, got %+v", got)
	}
	if got[0].Key != "FEATURE_FLAG" {
		t.Fatalf("user var must survive: %+v", got)
	}
	// The platform owns this key. A stale user-set value cannot shadow the
	// address the platform just computed.
	if got[1].Value != "http://fresh" {
		t.Fatalf("platform must win the collision: %+v", got[1])
	}

	// Nothing to overlay leaves the slice identical.
	if got := mergeEnvVars(user, nil); len(got) != 2 || got[1].Value != "http://stale" {
		t.Fatalf("empty platform overlay must be a no-op, got %+v", got)
	}
}

// TestDesiredDeploymentForGatewayEnv is the rule that protects a user's config:
// the gateway address rides the env field, and that field is only touched when
// this write already manages it.
func TestDesiredDeploymentForGatewayEnv(t *testing.T) {
	t.Parallel()

	sibs := []ProtectedSibling{{DepName: "api", ComponentName: "proj-api", EndpointName: "http"}}
	base := DeploymentInputs{
		Component:          designComponent(t, `{"name":"web","type":"web-application","dependencies":[]}`),
		ComponentName:      "web",
		Environment:        "default",
		ComponentNamespace: "default",
		ProtectedSiblings:  sibs,
	}

	// Unmanaged (nil) env stays nil. Overlaying here would replace the user's
	// whole env with one platform variable.
	if got := DesiredDeploymentFor(base).Binding.Env; got != nil {
		t.Fatalf("unmanaged env must stay unmanaged, got %+v", got)
	}

	// Managed-but-empty is the ordinary case for a component with no user config,
	// and it does receive the address.
	in := base
	in.EnvVars = []openchoreo.WorkflowEnvVarRef{}
	got := DesiredDeploymentFor(in).Binding.Env
	if len(got) != 1 || got[0].Key != "API_GATEWAY_URL" {
		t.Fatalf("managed env must receive the gateway address, got %+v", got)
	}

	// No protected sibling means no address, and the user's env is untouched.
	in2 := base
	in2.ProtectedSiblings = nil
	in2.EnvVars = []openchoreo.WorkflowEnvVarRef{{Key: "FEATURE_FLAG", Value: "on"}}
	got2 := DesiredDeploymentFor(in2).Binding.Env
	if len(got2) != 1 || got2[0].Key != "FEATURE_FLAG" {
		t.Fatalf("no sibling must leave env alone, got %+v", got2)
	}
}
