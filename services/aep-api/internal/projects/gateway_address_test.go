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

	got := APIGatewayContextPath("development", "default", "track-each-hire41-onboarding-api", "http")
	const want = "/development-default-track-each-hire41-onboarding-api-http"
	if got != want {
		t.Fatalf("context path\n got %q\nwant %q", got, want)
	}

	// An endpoint the design left unnamed defaults to "http", the same default
	// the workload and the trait both fall back to.
	if got := APIGatewayContextPath("development", "default", "proj-api", ""); got != "/development-default-proj-api-http" {
		t.Fatalf("default endpoint name: got %q", got)
	}
}

// TestDefaultAPIGatewayHostIsFullyQualified guards a failure that only shows up
// in a cluster. The consumer of this address is nginx, whose `resolver` queries
// the name verbatim and does NOT apply /etc/resolv.conf search domains — so the
// `<service>.<namespace>` short form the api-configuration trait uses resolves
// for getaddrinfo inside the very same pod and is NXDOMAIN for nginx. The
// symptom is a 502 on every /api call, which reads like the API being down.
func TestDefaultAPIGatewayHostIsFullyQualified(t *testing.T) {
	t.Parallel()

	host, _, found := strings.Cut(DefaultAPIGatewayHost, ":")
	if !found {
		t.Fatalf("default gateway host must carry a port: %q", DefaultAPIGatewayHost)
	}
	if !strings.HasSuffix(host, ".svc.cluster.local") {
		t.Fatalf("default gateway host must be fully qualified for nginx's resolver, got %q", host)
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
	got := GatewayEnvVars(DefaultAPIGatewayHost, "development", "default", sibs)
	if len(got) != 1 {
		t.Fatalf("want one env var, got %+v", got)
	}
	if got[0].Key != "ONBOARDING_API_GATEWAY_URL" {
		t.Fatalf("env key: got %q", got[0].Key)
	}
	const wantVal = "http://" + DefaultAPIGatewayHost + "/development-default-track-each-hire41-onboarding-api-http"
	if got[0].Value != wantVal {
		t.Fatalf("env value\n got %q\nwant %q", got[0].Value, wantVal)
	}

	// Every missing input independently yields nothing rather than a malformed
	// address: a half-formed gateway URL would send the SPA somewhere that 404s,
	// which is harder to diagnose than staying on the direct lane.
	for _, c := range []struct {
		name          string
		host, env, ns string
		sibs          []ProtectedSibling
	}{
		{"no host", "", "development", "default", sibs},
		{"no environment", DefaultAPIGatewayHost, "", "default", sibs},
		{"no namespace", DefaultAPIGatewayHost, "development", "", sibs},
		{"no siblings", DefaultAPIGatewayHost, "development", "default", nil},
		{"sibling missing component", DefaultAPIGatewayHost, "development", "default", []ProtectedSibling{{DepName: "x"}}},
	} {
		if got := GatewayEnvVars(c.host, c.env, c.ns, c.sibs); got != nil {
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
		Environment:        "development",
		ComponentNamespace: "default",
		GatewayHost:        DefaultAPIGatewayHost,
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
