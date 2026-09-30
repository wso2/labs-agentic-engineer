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

import "testing"

func TestSreMCPURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		host string
		port int
		want string
	}{
		{"dev gateway port", "aep-mcp.openchoreo.localhost", 8443, "https://aep-mcp.openchoreo.localhost:8443/mcp"},
		{"default https port is omitted", "aep-mcp.example.com", 443, "https://aep-mcp.example.com/mcp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := sreMCPURL(tc.host, tc.port); got != tc.want {
				t.Errorf("sreMCPURL(%q, %d) = %q, want %q", tc.host, tc.port, got, tc.want)
			}
		})
	}
}

// The flags default to the host the platform chart's sreAgent.mcpHostname
// routes and the https port k3d publishes for the control-plane gateway.
func TestSreInstallCmd_MCPFlagDefaults(t *testing.T) {
	t.Parallel()
	f := sreInstallCmd.Flags()
	for name, want := range map[string]string{
		"mcp-hostname": "aep-mcp.openchoreo.localhost",
		"mcp-port":     "8443",
	} {
		fl := f.Lookup(name)
		if fl == nil {
			t.Errorf("flag --%s is missing", name)
			continue
		}
		if fl.DefValue != want {
			t.Errorf("--%s default = %q, want %q", name, fl.DefValue, want)
		}
	}
}
