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

package ocname

import (
	"regexp"
	"testing"
)

// TestScopedComponentName pins the sibling-endpoint `component` value against the
// LIVE OpenChoreo lookup key. A friendly name here is the whole "deploying
// forever" bug: OC resolves a workload endpoint dependency by scoped name, so
// `todo-api` matched no ReleaseBinding and the consumer's Ready condition never
// flipped.
func TestScopedComponentName(t *testing.T) {
	t.Parallel()

	if got := ScopedComponentName("todo-api99", "todo-api"); got != "todo-api99-todo-api" {
		t.Errorf("ScopedComponentName = %q, want todo-api99-todo-api", got)
	}
	// No project ⇒ nothing to disambiguate against; the name passes through.
	if got := ScopedComponentName("", "todo-api"); got != "todo-api" {
		t.Errorf("ScopedComponentName with no project = %q, want todo-api", got)
	}
}

// TestServiceURLEnvName pins the `address` binding to the key a browser app's
// window._env_ already carries, so the coding agent's workload.yaml and the SPA
// config name the same variable.
func TestServiceURLEnvName(t *testing.T) {
	t.Parallel()

	for dep, want := range map[string]string{
		"todo-api":     "TODO_API_URL",
		"orders":       "ORDERS_URL",
		"employee-api": "EMPLOYEE_API_URL",
	} {
		if got := ServiceURLEnvName(dep); got != want {
			t.Errorf("ServiceURLEnvName(%q) = %q, want %q", dep, got, want)
		}
	}
}

func TestExternalResourceNaming(t *testing.T) {
	t.Parallel()

	if got := ExternalResourceName("weatherproj", "openweather"); got != "weatherproj-openweather" {
		t.Errorf("ExternalResourceName = %q", got)
	}
	if got := ExternalResourceBindingName("weatherproj", "openweather", "default"); got != "weatherproj-openweather-default" {
		t.Errorf("ExternalResourceBindingName = %q", got)
	}
}

// dns1035 is the label form OpenChoreo renders a Resource into and CloudNativePG
// validates a Cluster name against: lowercase, starts with a letter, ends
// alphanumeric, only [a-z0-9-] between. The bound name MUST stay in this form.
var dns1035 = regexp.MustCompile(`^[a-z]([-a-z0-9]*[a-z0-9])?$`)

// renderedName mirrors how OC 1.1.1 names a platform resource's per-env backing
// object (LIVE-VERIFIED): r-<resourceName>-<env>-<hash8>. This is the string
// CloudNativePG validates against its 50-char Cluster-name cap.
func renderedName(resourceName, env string) string {
	return "r-" + resourceName + "-" + env + "-" + "deadbeef" // 8-hex stand-in
}

// TestExternalResourceName_BoundsForCNPG pins the real cluster-name-overflow fix:
// OC names the CloudNativePG Cluster off the RESOURCE name as
// r-<resourceName>-<env>-<hash8>, so the RESOURCE name (not the binding) must be
// bounded. audit-evidence-hub-4-audit-db rendered to a 52-char Cluster name and
// was rejected. A short name is returned verbatim; an overflowing name is
// truncated with a deterministic hash tail — unique, stable, DNS-1035-legal.
func TestExternalResourceName_BoundsForCNPG(t *testing.T) {
	t.Parallel()

	// Short names pass through unchanged (existing resources keep readable names).
	if got := ExternalResourceName("aeh", "aeh-db"); got != "aeh-aeh-db" {
		t.Errorf("short name should be verbatim, got %q", got)
	}

	// The real overflow case: the exact project/dep from the audit-evidence-hub-4
	// build that failed with a 52-char CNPG Cluster name.
	long := ExternalResourceName("audit-evidence-hub-4", "audit-db")
	if len(long) > maxOCResourceName {
		t.Errorf("bounded resource name len = %d, want <= %d (%q)", len(long), maxOCResourceName, long)
	}
	// The env slug is the widest maxEnvNameLen allows, not the one AEP provisions
	// into — the bound has to hold at its own worst case.
	if rn := renderedName(long, "development"); len(rn) > cnpgMaxClusterName {
		t.Errorf("OC-rendered cluster name %q would be %d chars, want <= %d", rn, len(rn), cnpgMaxClusterName)
	}
	if !dns1035.MatchString(long) {
		t.Errorf("bounded name %q is not a valid DNS-1035 label", long)
	}

	// Stable: same inputs -> same output (deprovision/status/consumer wiring all
	// recompute it and must agree).
	if again := ExternalResourceName("audit-evidence-hub-4", "audit-db"); again != long {
		t.Errorf("not stable: %q != %q", again, long)
	}

	// Unique: two distinct deps in the same project must not collide after
	// truncation — the hash covers the full natural name (project + dep).
	other := ExternalResourceName("audit-evidence-hub-4", "audit-auth")
	if other == long {
		t.Errorf("distinct deps collided after truncation: %q", long)
	}

	// The binding name derived from a bounded resource name stays label-legal and
	// within its guard bound.
	b := ExternalResourceBindingName("audit-evidence-hub-4", "audit-db", "development") // widest env the bound allows
	if len(b) > maxOCBindingName {
		t.Errorf("binding name len = %d, want <= %d (%q)", len(b), maxOCBindingName, b)
	}
	if !dns1035.MatchString(b) {
		t.Errorf("binding name %q is not a valid DNS-1035 label", b)
	}
}
