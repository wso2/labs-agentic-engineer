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

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

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

// -- drift between the embedded types and the on-disk source -----------------

// crtSourceDir is where the ClusterResourceTypes this package embeds are
// authored. The literals here are shipped by `aectl` to a cluster that has no
// checkout, which is why they are copies at all.
const crtSourceDir = "../../../../deployments/single-cluster/resource-types"

// TestEmbeddedResourceTypes_MatchTheAuthoredSource is the drift gate between
// the two copies of every ClusterResourceType: the authored YAML under
// deployments/ and the string literal this package ships.
//
// The copies are DERIVED, not byte-identical — comments are stripped, and
// thunder-app's issuer/jwks_url are rendered literals here because `aectl`
// installs the add-on before any environment binding exists to resolve
// ${applied.app.status.*} from. Everything that decides BEHAVIOUR has to agree:
// the parameter schema (names, types, defaults, bounds), the rendered
// template, and the set of output names. Adding a parameter or an output on one
// side and forgetting the other is the failure this catches — silently, an
// `aectl`-installed cluster would render a CR missing a field the platform
// expects to read back.
func TestEmbeddedResourceTypes_MatchTheAuthoredSource(t *testing.T) {
	for _, tc := range []struct {
		id       string
		embedded string
		source   string
		// literalOutputs are output names whose VALUE legitimately differs
		// (see the doc comment); their presence is still checked.
		literalOutputs []string
	}{
		{
			id:             "thunder-app",
			embedded:       thunderAppResourceType,
			source:         "thunder-app/resourcetype.yaml",
			literalOutputs: []string{"issuer", "jwks_url"},
		},
		{
			id:       "postgres-cnpg",
			embedded: postgresCNPGResourceType,
			source:   "postgres-cnpg/resourcetype.yaml",
		},
	} {
		t.Run(tc.id, func(t *testing.T) {
			got := parseCRT(t, []byte(tc.embedded))
			want := parseCRT(t, readSourceCRT(t, tc.source))

			assertDeepEqual(t, "spec.parameters",
				dig(got, "spec", "parameters"), dig(want, "spec", "parameters"))
			assertDeepEqual(t, "spec.environmentConfigs",
				dig(got, "spec", "environmentConfigs"), dig(want, "spec", "environmentConfigs"))
			assertDeepEqual(t, "spec.resources",
				dig(got, "spec", "resources"), dig(want, "spec", "resources"))

			gotOutputs := outputsByName(t, got)
			wantOutputs := outputsByName(t, want)
			for name, wantValue := range wantOutputs {
				gotValue, ok := gotOutputs[name]
				if !ok {
					t.Errorf("embedded %s is missing output %q — add it here as well as in %s",
						tc.id, name, tc.source)
					continue
				}
				if slices.Contains(tc.literalOutputs, name) {
					continue
				}
				if gotValue != wantValue {
					t.Errorf("output %q = %q, want %q (from %s)", name, gotValue, wantValue, tc.source)
				}
			}
			for name := range gotOutputs {
				if _, ok := wantOutputs[name]; !ok {
					t.Errorf("embedded %s declares output %q, which %s does not", tc.id, name, tc.source)
				}
			}
		})
	}
}

func readSourceCRT(t *testing.T, rel string) []byte {
	t.Helper()
	path := filepath.Join(crtSourceDir, rel)
	raw, err := os.ReadFile(path) //nolint:gosec // a fixed in-repo path
	if err != nil {
		t.Fatalf("read authored ClusterResourceType %s: %v", path, err)
	}
	return raw
}

// parseCRT decodes the one ClusterResourceType document out of a manifest,
// skipping any leading comment-only or empty document.
func parseCRT(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	for {
		var doc map[string]any
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			t.Fatal("no ClusterResourceType document found")
		}
		if err != nil {
			t.Fatalf("decode ClusterResourceType: %v", err)
		}
		if kind, _ := doc["kind"].(string); kind == "ClusterResourceType" {
			return doc
		}
	}
}

func dig(doc map[string]any, path ...string) any {
	var cur any = doc
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[key]
	}
	return cur
}

func outputsByName(t *testing.T, doc map[string]any) map[string]string {
	t.Helper()
	raw, _ := dig(doc, "spec", "outputs").([]any)
	out := make(map[string]string, len(raw))
	for _, entryAny := range raw {
		entry, ok := entryAny.(map[string]any)
		if !ok {
			t.Fatalf("spec.outputs entry is not a mapping: %#v", entryAny)
		}
		name, _ := entry["name"].(string)
		out[name] = fmt.Sprint(entry["value"])
	}
	return out
}

func assertDeepEqual(t *testing.T, what string, got, want any) {
	t.Helper()
	if reflect.DeepEqual(got, want) {
		return
	}
	t.Errorf("%s drifted between the embedded literal and the authored file\n embedded: %#v\n authored: %#v",
		what, got, want)
}
