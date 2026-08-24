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
	"testing"

	ocgen "github.com/wso2/aep/aep-api/internal/clients/openchoreo/gen"
)

func TestDeploymentFromReleaseBinding_HTTPSOnlyPopulatesEndpointURL(t *testing.T) {
	got := deploymentFromReleaseBinding(bindingWithExternal(httpsURL(
		"development-wc-019feb11-3f4674c4.gateway.dp.dev.cloud.wso2.com",
		"/hello-world-api-16-hello-api-http",
		443,
	), nil), false)
	want := "https://development-wc-019feb11-3f4674c4.gateway.dp.dev.cloud.wso2.com:443/hello-world-api-16-hello-api-http"
	if got.EndpointURL != want {
		t.Fatalf("EndpointURL = %q, want %q", got.EndpointURL, want)
	}
}

func TestDeploymentFromReleaseBinding_HTTPOnlyStillWorks(t *testing.T) {
	got := deploymentFromReleaseBinding(bindingWithExternal(nil, httpURL(
		"web.local",
		"/",
		80,
	)), false)
	want := "http://web.local:80/"
	if got.EndpointURL != want {
		t.Fatalf("EndpointURL = %q, want %q", got.EndpointURL, want)
	}
}

func TestDeploymentFromReleaseBinding_PrefersHTTPSWhenBothPresent(t *testing.T) {
	got := deploymentFromReleaseBinding(bindingWithExternal(
		httpsURL("gw.example.com", "/app", 443),
		httpURL("gw.example.com", "/app", 80),
	), false)
	want := "https://gw.example.com:443/app"
	if got.EndpointURL != want {
		t.Fatalf("EndpointURL = %q, want %q", got.EndpointURL, want)
	}
}

func TestDeploymentFromReleaseBinding_NeitherSchemeOmitsEndpointURL(t *testing.T) {
	got := deploymentFromReleaseBinding(bindingWithExternal(nil, nil), false)
	if got.EndpointURL != "" {
		t.Fatalf("EndpointURL = %q, want empty", got.EndpointURL)
	}
}

func bindingWithExternal(https, http *ocgen.EndpointURL) ocgen.ReleaseBinding {
	release := "hello-api-rel"
	eps := []ocgen.EndpointURLStatus{{
		Name: "http",
		ExternalURLs: &ocgen.EndpointGatewayURLs{
			Https: https,
			Http:  http,
		},
	}}
	return ocgen.ReleaseBinding{
		Metadata: ocgen.ObjectMeta{Name: "hello-api-development"},
		Spec: &ocgen.ReleaseBindingSpec{
			Environment: "development",
			ReleaseName: &release,
			Owner: struct {
				ComponentName string `json:"componentName"`
				ProjectName   string `json:"projectName"`
			}{ComponentName: "proj-hello-api", ProjectName: "proj"},
		},
		Status: &ocgen.ReleaseBindingStatus{Endpoints: &eps},
	}
}

func httpsURL(host, path string, port int32) *ocgen.EndpointURL {
	scheme := "https"
	p := path
	return &ocgen.EndpointURL{Host: host, Path: &p, Port: &port, Scheme: &scheme}
}

func httpURL(host, path string, port int32) *ocgen.EndpointURL {
	scheme := "http"
	p := path
	return &ocgen.EndpointURL{Host: host, Path: &p, Port: &port, Scheme: &scheme}
}

// TestDeploymentFromReleaseBinding_PrefersHTTPWhenTheGatewayTerminatesNoTLS is
// the local-plane case, and it is not a cosmetic preference.
//
// OpenChoreo advertises both schemes from the endpoint's SHAPE rather than from
// what its gateway serves. The local single-cluster data plane runs with
// `gateway.tls.enabled: false`, so its only listener is plain http and the
// advertised https URL connects to nothing. Handing that URL out was survivable
// while it only reached a console link; it stopped being survivable when the
// runner gained a reachability gate, which probes the deployed endpoints before
// starting the agent and refuses to run when they do not answer — so an
// unreachable advertised URL blocks VALIDATION rather than rendering a bad link.
func TestDeploymentFromReleaseBinding_PrefersHTTPWhenTheGatewayTerminatesNoTLS(t *testing.T) {
	got := deploymentFromReleaseBinding(bindingWithExternal(
		httpsURL("app.example", "/", 19443),
		httpURL("app.example", "/", 19080),
	), true)
	want := "http://app.example:19080/"
	if got.EndpointURL != want {
		t.Fatalf("EndpointURL = %q, want %q — the scheme the gateway actually serves", got.EndpointURL, want)
	}
}

// The fallback still holds under the preference: a binding advertising only
// https must not lose its URL just because plain http was preferred.
func TestDeploymentFromReleaseBinding_FallsBackWhenThePreferredSchemeIsAbsent(t *testing.T) {
	got := deploymentFromReleaseBinding(bindingWithExternal(
		httpsURL("app.example", "/app", 443), nil), true)
	want := "https://app.example:443/app"
	if got.EndpointURL != want {
		t.Fatalf("EndpointURL = %q, want %q — the only scheme advertised", got.EndpointURL, want)
	}
}
