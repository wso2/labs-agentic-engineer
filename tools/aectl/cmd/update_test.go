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
)

// helmUpgradeArgs must resolve the chart source cfg carries — a local
// ChartPath, an OCI ChartVersion, or (unpinned) neither — never something the
// caller didn't ask for. This is what lets `sre install` pin the platform
// chart it upgrades via cfg rather than falling back to platformUpdate's own
// unpinned-OCI default.
func TestHelmUpgradeArgs_ChartSource(t *testing.T) {
	cases := []struct {
		name string
		cfg  platformUpdateConfig
		want []string // args that must all be present, in order where adjacent
	}{
		{
			name: "local chart path wins",
			cfg:  platformUpdateConfig{Release: "aep-platform", Namespace: "wso2-aep", ChartPath: "deployments/helm-charts/platform", ChartVersion: "9.9.9"},
			want: []string{"deployments/helm-charts/platform"},
		},
		{
			name: "OCI chart pinned to a version",
			cfg:  platformUpdateConfig{Release: "aep-platform", Namespace: "wso2-aep", ChartVersion: "1.4.0"},
			want: []string{"oci://ghcr.io/wso2/aep/charts/aep-platform", "--version", "1.4.0"},
		},
		{
			name: "unpinned falls back to OCI latest",
			cfg:  platformUpdateConfig{Release: "aep-platform", Namespace: "wso2-aep"},
			want: []string{"oci://ghcr.io/wso2/aep/charts/aep-platform"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args, err := helmUpgradeArgs(tc.cfg)
			if err != nil {
				t.Fatalf("helmUpgradeArgs: %v", err)
			}
			joined := strings.Join(args, " ")
			for _, want := range tc.want {
				if !strings.Contains(joined, want) {
					t.Errorf("args = %q, want to contain %q", joined, want)
				}
			}
		})
	}

	// A local ChartPath is passed as-is with no --version flag (helm rejects
	// --version against a local chart path).
	args, err := helmUpgradeArgs(platformUpdateConfig{Release: "aep-platform", Namespace: "wso2-aep", ChartPath: "deployments/helm-charts/platform", ChartVersion: "9.9.9"})
	if err != nil {
		t.Fatalf("helmUpgradeArgs: %v", err)
	}
	if strings.Contains(strings.Join(args, " "), "--version 9.9.9") {
		t.Errorf("args = %q, ChartVersion must be ignored when ChartPath is set", args)
	}
}

// updatePlatformSreAgent (via sreAgentPlatformUpdateConfig) must forward the
// chart source it was given — otherwise `sre install`'s pinned chart could
// still be dropped on the floor between the caller and the underlying helm
// upgrade — and it must carry the handoff key's Secret and hash and
// the MCPHostname from sreParams, not a global.
func TestUpdatePlatformSreAgent_PassesChartSourceThrough(t *testing.T) {
	p := sreParams{AEPNamespace: "wso2-aep", ObsNamespace: "obs", RcaName: "sre-agent", MCPHostname: "aep-mcp.openchoreo.localhost"}
	cfg := sreAgentPlatformUpdateConfig(p, "deployments/helm-charts/platform", "", "abc123")
	args, err := helmUpgradeArgs(cfg)
	if err != nil {
		t.Fatalf("helmUpgradeArgs: %v", err)
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "sreAgent.org") {
		t.Errorf("args = %q, must not set an org: each handoff call names its own", joined)
	}
	for _, want := range []string{
		"deployments/helm-charts/platform",
		"sreAgent.enabled=true",
		"sreAgent.tokenSecret=sre-handoff",
		"sreAgent.tokenHash=abc123",
		"sreAgent.mcpHostname=aep-mcp.openchoreo.localhost",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args = %q, want to contain %q", joined, want)
		}
	}
}
