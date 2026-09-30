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

package cmd

import (
	"strings"
	"testing"
	"text/template"
)

// renderSreTemplate parses and executes an sre value/manifest template
// against params, failing the test on any template error.
func renderSreTemplate(t *testing.T, tmpl string, params sreParams) string {
	t.Helper()
	tpl, err := template.New("t").Parse(tmpl)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}
	var buf strings.Builder
	if err := tpl.Execute(&buf, params); err != nil {
		t.Fatalf("execute template: %v", err)
	}
	return buf.String()
}

func TestSreAgentValues_ExtraEnvsFromAEOwnedSecret(t *testing.T) {
	out := renderSreTemplate(t, sreAgentValuesTmpl, sreParams{ObsNamespace: "openchoreo-observability-plane", Org: "default", RcaImageRepo: "ghcr.io/openchoreo/sre-agent", RcaImageTag: "v1.3.0@sha256:abc"})
	for _, want := range []string{
		"name: RCA_LLM_API_KEY", "name: RCA_MODEL_NAME", "name: RCA_LLM_BASE_URL", "name: AEP_MCP_TOKEN",
		"name: sre-agent-aep", "name: EXTENSIONS_DIR", "value: /opt/aep/sre-agent-extensions", "tag: v1.3.0@sha256:abc",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("values missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "RCA_LLM_API_KEY_FILE") {
		t.Error("the stock image reads RCA_LLM_API_KEY from env; no key file")
	}
}

func TestSreAgentAEOwnedSecretAndRole(t *testing.T) {
	sec := renderSreTemplate(t, sreAgentAEOwnedSecretTmpl, sreParams{ObsNamespace: "obs"})
	for _, k := range []string{"RCA_LLM_API_KEY: \"\"", "RCA_MODEL_NAME: \"\"", "RCA_LLM_BASE_URL: \"\"", "AEP_MCP_TOKEN: \"\""} {
		if !strings.Contains(sec, k) {
			t.Errorf("secret missing %s", k)
		}
	}
	role := renderSreTemplate(t, sreAgentPushRoleTmpl, sreParams{ObsNamespace: "obs", AEPNamespace: "wso2-aep", RcaName: "sre-agent"})
	for _, want := range []string{"resourceNames: [\"sre-agent-aep\"]", "deployments/scale", "resourceNames: [\"sre-agent\"]", "name: aep-api", "namespace: wso2-aep"} {
		if !strings.Contains(role, want) {
			t.Errorf("role missing %q", want)
		}
	}
}

// TestSreTemplates_CarryForceSync verifies every ExternalSecret aectl renders
// carries ESO's force-sync annotation with the per-run value, so a re-run
// with an unchanged spec still triggers a fresh sync instead of waiting out
// ESO's 1h refreshInterval (waitForExternalSecretRefresh would otherwise
// time out).
func TestSreTemplates_CarryForceSync(t *testing.T) {
	p := sreParams{ObsNamespace: "obs", PlatformSecretStore: "aep-platform", ForceSync: "1790592655"}
	for name, tmpl := range map[string]string{
		"sreAgentSecretsTmpl": sreAgentSecretsTmpl, "sreObserverClientSecretTmpl": sreObserverClientSecretTmpl, "srePlaneSecretsTmpl": srePlaneSecretsTmpl,
	} {
		out := renderSreTemplate(t, tmpl, p)
		if n := strings.Count(out, "kind: ExternalSecret"); n != strings.Count(out, `force-sync: "1790592655"`) {
			t.Errorf("%s: every ExternalSecret must carry force-sync (%d ExternalSecrets)", name, n)
		}
	}
}
