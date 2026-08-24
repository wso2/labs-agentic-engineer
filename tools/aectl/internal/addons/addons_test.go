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

package addons

import "testing"

func TestAvailable_OperatorMetadata(t *testing.T) {
	if len(Available) == 0 {
		t.Fatal("Available must contain at least one addon")
	}
	for _, a := range Available {
		t.Run(a.ID, func(t *testing.T) {
			if a.ID == "" {
				t.Error("ID must not be empty")
			}
			if a.Label == "" {
				t.Error("Label must not be empty")
			}
			if a.Description == "" {
				t.Error("Description must not be empty")
			}
			if len(a.Manifests) == 0 {
				t.Error("Manifests must not be empty")
			}
			if len(a.VerifyResources) == 0 {
				t.Error("VerifyResources must not be empty")
			}
			for i, v := range a.VerifyResources {
				if v.APIVersion == "" || v.Kind == "" || v.Name == "" {
					t.Errorf("VerifyResources[%d] has empty APIVersion/Kind/Name: %+v", i, v)
				}
			}
			op := a.Operator
			if op.ReleaseName == "" {
				return // no operator dependency; the remaining fields are intentionally zero
			}
			if op.Chart == "" {
				t.Error("Operator.Chart must not be empty when ReleaseName is set")
			}
			if op.Namespace == "" {
				t.Error("Operator.Namespace must not be empty when ReleaseName is set")
			}
			if op.DisplayName == "" {
				t.Error("Operator.DisplayName must not be empty when ReleaseName is set")
			}
		})
	}
}

func TestAvailable_ThunderApp(t *testing.T) {
	a := findAddon(t, "thunder-app")
	op := a.Operator
	if got, want := op.ReleaseName, "thunder-app-operator"; got != want {
		t.Errorf("ReleaseName = %q, want %q", got, want)
	}
	if got, want := op.Chart, "oci://ghcr.io/wso2/thunder-app-operator"; got != want {
		t.Errorf("Chart = %q, want %q", got, want)
	}
	if got, want := op.Namespace, "thunder-app-operator-system"; got != want {
		t.Errorf("Namespace = %q, want %q", got, want)
	}
	if op.Version != "" {
		t.Errorf("Version = %q, want empty (use registry default)", op.Version)
	}
}

func TestAvailable_PostgresCNPG(t *testing.T) {
	a := findAddon(t, "postgres-cnpg")
	op := a.Operator
	if got, want := op.ReleaseName, "cnpg"; got != want {
		t.Errorf("ReleaseName = %q, want %q", got, want)
	}
	if got, want := op.Chart, "oci://ghcr.io/cloudnative-pg/charts/cloudnative-pg"; got != want {
		t.Errorf("Chart = %q, want %q", got, want)
	}
	if got, want := op.Version, "0.29.0"; got != want {
		t.Errorf("Version = %q, want %q", got, want)
	}
	if got, want := op.Namespace, "cnpg-system"; got != want {
		t.Errorf("Namespace = %q, want %q", got, want)
	}
}

func findAddon(t *testing.T, id string) Addon {
	t.Helper()
	for _, a := range Available {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("addon %q not found in Available", id)
	return Addon{}
}
